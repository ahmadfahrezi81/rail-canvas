package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	TemplateMaxBytes     = 1 << 20 // 1 MB
	TemplateMaxSide      = 256     // the canvas size
	templatesPerCanvas   = 20
	templateUploadTTL    = 5 * time.Minute
	templateDownloadTTL  = time.Hour
	templateHeaderToRead = 1024 // a PNG's size is in its first 33 bytes
)

var (
	ErrTemplatesOff     = errors.New("templates are not configured")
	ErrInvalidTemplate  = errors.New("invalid template")
	ErrCanvasFull       = fmt.Errorf("a canvas holds at most %d templates", templatesPerCanvas)
	ErrTemplateFinished = errors.New("template already completed")
)

// RejectedError: the uploaded file failed a check; it has been deleted.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return "template rejected: " + e.Reason }

// ObjectStore is the bucket. The real one is platform.Bucket; tests use a fake.
type ObjectStore interface {
	// SignUpload returns a URL that accepts exactly one PUT of this size and type, and the headers it must carry.
	SignUpload(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (url string, headers map[string]string, err error)
	SignDownload(ctx context.Context, key string, ttl time.Duration) (string, error)
	// Head reports the stored size and type; found is false when nothing was uploaded.
	Head(ctx context.Context, key string) (size int64, contentType string, found bool, err error)
	// ReadFirst reads at most n bytes from the start of the object.
	ReadFirst(ctx context.Context, key string, n int64) ([]byte, error)
	Delete(ctx context.Context, key string) error
	// Get reads a whole object, refusing one larger than max bytes.
	Get(ctx context.Context, key string, max int64) ([]byte, error)
	// Put stores a small file the server made itself (snapshots). Browsers upload through SignUpload.
	Put(ctx context.Context, key, contentType string, body []byte) error
}

type Template struct {
	ID             uuid.UUID
	X, Y           int
	Width, Height  int
	UploadedBy     uuid.NullUUID
	UploadedByName string
	CreatedAt      time.Time
	URL            string
	URLExpiresAt   time.Time
	ThumbURL       string // empty until the worker made the thumbnail
}

type TemplateUpload struct {
	TemplateID uuid.UUID
	URL        string
	Headers    map[string]string
	ExpiresAt  time.Time
}

type Templates struct {
	pool    *pgxpool.Pool
	objects ObjectStore // nil when the bucket is not configured
	queue   Queue       // nil: no thumbnails are made
}

func NewTemplates(pool *pgxpool.Pool, objects ObjectStore) *Templates {
	return &Templates{pool: pool, objects: objects}
}

// WithQueue has the worker make a thumbnail of each completed template.
func (s *Templates) WithQueue(q Queue) *Templates {
	s.queue = q
	return s
}

// TemplateKey is the only place an object key is built: from the URL's space and a server-made id.
func TemplateKey(spaceID, templateID uuid.UUID) string {
	return fmt.Sprintf("spaces/%s/templates/%s/original", spaceID, templateID)
}

// Create records a pending template and signs its upload. Nothing is signed for a request over the limits.
func (s *Templates) Create(ctx context.Context, spaceID, canvasID, userID uuid.UUID, size int64, contentType string, x, y int) (TemplateUpload, error) {
	if s.objects == nil {
		return TemplateUpload{}, ErrTemplatesOff
	}
	switch {
	case contentType != "image/png":
		return TemplateUpload{}, fmt.Errorf("%w: PNG only", ErrInvalidTemplate)
	case size < 1 || size > TemplateMaxBytes:
		return TemplateUpload{}, fmt.Errorf("%w: at most 1 MB", ErrInvalidTemplate)
	case x < 0 || y < 0 || x >= TemplateMaxSide || y >= TemplateMaxSide:
		return TemplateUpload{}, fmt.Errorf("%w: position off the board", ErrInvalidTemplate)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return TemplateUpload{}, err
	}
	key := TemplateKey(spaceID, id)
	err = inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		n, err := q.CountReadyTemplates(ctx, store.CountReadyTemplatesParams{SpaceID: spaceID, CanvasID: canvasID})
		if err != nil {
			return err
		}
		if n >= templatesPerCanvas {
			return ErrCanvasFull
		}
		_, err = q.CreateTemplate(ctx, store.CreateTemplateParams{
			ID: id, SpaceID: spaceID, CanvasID: canvasID, UploadedBy: uuid.NullUUID{UUID: userID, Valid: true},
			ObjectKey: key, X: int16(x), Y: int16(y), Bytes: int32(size),
		})
		return err
	})
	if err != nil {
		return TemplateUpload{}, err
	}

	url, headers, err := s.objects.SignUpload(ctx, key, contentType, size, templateUploadTTL)
	if err != nil {
		return TemplateUpload{}, err
	}
	return TemplateUpload{TemplateID: id, URL: url, Headers: headers, ExpiresAt: time.Now().Add(templateUploadTTL)}, nil
}

// Complete checks the uploaded file without decoding its pixels, then marks it ready, or rejects and deletes it.
func (s *Templates) Complete(ctx context.Context, spaceID, canvasID, templateID, userID uuid.UUID) (Template, error) {
	if s.objects == nil {
		return Template{}, ErrTemplatesOff
	}
	var t store.Template
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		var err error
		t, err = q.GetTemplate(ctx, store.GetTemplateParams{ID: templateID, SpaceID: spaceID, CanvasID: canvasID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return Template{}, err
	}
	// Only the uploader completes; to anyone else the upload does not exist.
	if !t.UploadedBy.Valid || t.UploadedBy.UUID != userID {
		return Template{}, ErrNotFound
	}
	if t.Status != "pending" {
		return Template{}, ErrTemplateFinished
	}

	width, height, reason, err := s.check(ctx, t.ObjectKey, int64(t.Bytes))
	if err != nil {
		return Template{}, err
	}
	if reason != "" {
		return Template{}, s.reject(ctx, spaceID, t, reason)
	}

	var ready store.Template
	err = inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		var err error
		ready, err = q.MarkTemplateReady(ctx, store.MarkTemplateReadyParams{
			Width: pgtype.Int2{Int16: int16(width), Valid: true}, Height: pgtype.Int2{Int16: int16(height), Valid: true},
			ID: templateID, SpaceID: spaceID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTemplateFinished // completed concurrently
		}
		return err
	})
	if err != nil {
		return Template{}, err
	}
	// A thumbnail is a nicety: the template is ready either way.
	if s.queue != nil {
		if err := s.queue.Thumbnail(ctx, spaceID, templateID); err != nil {
			slog.WarnContext(ctx, "thumbnail not queued", "template_id", templateID, "err", err)
		}
	}
	return s.withURL(ctx, Template{
		ID: ready.ID, X: int(ready.X), Y: int(ready.Y), Width: width, Height: height,
		UploadedBy: ready.UploadedBy, CreatedAt: ready.CreatedAt,
	}, ready.ObjectKey, ready.ThumbKey)
}

// check returns the PNG's size, or a reason to reject it.
func (s *Templates) check(ctx context.Context, key string, declared int64) (width, height int, reason string, err error) {
	size, contentType, found, err := s.objects.Head(ctx, key)
	switch {
	case err != nil:
		return 0, 0, "", err
	case !found:
		return 0, 0, "no file was uploaded", nil
	case size > TemplateMaxBytes || size != declared:
		return 0, 0, "the file is not the size that was declared", nil
	case contentType != "image/png":
		return 0, 0, "the file is not a PNG", nil
	}
	head, err := s.objects.ReadFirst(ctx, key, templateHeaderToRead)
	if err != nil {
		return 0, 0, "", err
	}
	// Reads the header only: a file claiming 50 000 × 50 000 is refused here, never decoded.
	cfg, err := png.DecodeConfig(bytes.NewReader(head))
	switch {
	case err != nil:
		return 0, 0, "the file is not a PNG", nil
	case cfg.Width > TemplateMaxSide || cfg.Height > TemplateMaxSide:
		return 0, 0, fmt.Sprintf("at most %d × %d pixels", TemplateMaxSide, TemplateMaxSide), nil
	}
	return cfg.Width, cfg.Height, "", nil
}

func (s *Templates) reject(ctx context.Context, spaceID uuid.UUID, t store.Template, reason string) error {
	if err := s.objects.Delete(ctx, t.ObjectKey); err != nil {
		return err
	}
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		_, err := q.MarkTemplateRejected(ctx, store.MarkTemplateRejectedParams{
			Reason: pgtype.Text{String: reason, Valid: true}, ID: t.ID, SpaceID: spaceID,
		})
		return err
	})
	if err != nil {
		return err
	}
	return &RejectedError{Reason: reason}
}

// List: live rows; each URL lasts templateDownloadTTL, so clients refetch within it.
func (s *Templates) List(ctx context.Context, spaceID, canvasID uuid.UUID) ([]Template, error) {
	if s.objects == nil {
		return nil, ErrTemplatesOff
	}
	var rows []store.ListReadyTemplatesRow
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		if _, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var err error
		rows, err = q.ListReadyTemplates(ctx, store.ListReadyTemplatesParams{SpaceID: spaceID, CanvasID: canvasID})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Template, 0, len(rows))
	for _, r := range rows {
		t, err := s.withURL(ctx, Template{
			ID: r.ID, X: int(r.X), Y: int(r.Y), Width: int(r.Width.Int16), Height: int(r.Height.Int16),
			UploadedBy: r.UploadedBy, UploadedByName: r.UploadedByName.String, CreatedAt: r.CreatedAt,
		}, r.ObjectKey, r.ThumbKey)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Delete: the uploader or the space's owner. The file goes first, so a failure never leaves an orphan file.
func (s *Templates) Delete(ctx context.Context, spaceID, canvasID, templateID, userID uuid.UUID) error {
	if s.objects == nil {
		return ErrTemplatesOff
	}
	var t store.Template
	err := inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		var err error
		t, err = q.GetTemplate(ctx, store.GetTemplateParams{ID: templateID, SpaceID: spaceID, CanvasID: canvasID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if t.UploadedBy.Valid && t.UploadedBy.UUID == userID {
			return nil
		}
		role, err := memberRole(ctx, q, spaceID, userID)
		if err != nil {
			return err
		}
		if role != "owner" {
			return ErrForbidden
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.objects.Delete(ctx, t.ObjectKey); err != nil {
		return err
	}
	// The thumbnail's key is fixed, so it is deleted even if the worker is still making it.
	if err := s.objects.Delete(ctx, TemplateThumbKey(spaceID, templateID)); err != nil {
		return err
	}
	return inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		return q.DeleteTemplate(ctx, store.DeleteTemplateParams{ID: templateID, SpaceID: spaceID})
	})
}

func (s *Templates) withURL(ctx context.Context, t Template, key string, thumbKey pgtype.Text) (Template, error) {
	url, err := s.objects.SignDownload(ctx, key, templateDownloadTTL)
	if err != nil {
		return Template{}, err
	}
	t.URL, t.URLExpiresAt = url, time.Now().Add(templateDownloadTTL)
	if thumbKey.Valid {
		if t.ThumbURL, err = s.objects.SignDownload(ctx, thumbKey.String, templateDownloadTTL); err != nil {
			return Template{}, err
		}
	}
	return t, nil
}
