package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeObjects stands in for the bucket: uploads are put() by the test.
type fakeObjects struct {
	mu      sync.Mutex
	objects map[string]fakeObject
	signed  []string
}

type fakeObject struct {
	data        []byte
	contentType string
}

func newFakeObjects() *fakeObjects { return &fakeObjects{objects: map[string]fakeObject{}} }

func (f *fakeObjects) put(key string, data []byte, contentType string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = fakeObject{data, contentType}
}

func (f *fakeObjects) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok
}

func (f *fakeObjects) SignUpload(_ context.Context, key, contentType string, _ int64, _ time.Duration) (string, map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signed = append(f.signed, key)
	return "https://bucket.test/" + key + "?signed", map[string]string{"Content-Type": contentType}, nil
}

func (f *fakeObjects) SignDownload(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://bucket.test/" + key + "?get", nil
}

func (f *fakeObjects) Head(_ context.Context, key string) (int64, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objects[key]
	return int64(len(o.data)), o.contentType, ok, nil
}

func (f *fakeObjects) ReadFirst(_ context.Context, key string, n int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.objects[key].data
	return d[:min(int64(len(d)), n)], nil
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// bombPNG is a valid PNG header claiming 50 000 × 50 000 pixels, with no image data behind it.
func bombPNG() []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 50000)
	binary.BigEndian.PutUint32(ihdr[4:], 50000)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

type templateFixture struct {
	svc                    *Templates
	objects                *fakeObjects
	space, canvas          uuid.UUID
	owner, member, member2 uuid.UUID
}

func newTemplateFixture(t *testing.T) templateFixture {
	pool := testPool(t)
	f := templateFixture{objects: newFakeObjects(), space: newSpace(t, pool)}
	f.svc = NewTemplates(pool, f.objects)
	f.owner, f.member, f.member2 = newUser(t, pool, "x"), newUser(t, pool, "x"), newUser(t, pool, "x")
	addMember(t, pool, f.space, f.owner, "owner")
	addMember(t, pool, f.space, f.member, "member")
	addMember(t, pool, f.space, f.member2, "member")
	c, err := NewCanvases(pool).Create(context.Background(), f.space, f.owner, "templates", DefaultCooldown)
	if err != nil {
		t.Fatal(err)
	}
	f.canvas = c.ID
	return f
}

// upload creates a template as member, stores data under its key, and completes it.
func (f templateFixture) upload(t *testing.T, data []byte) (uuid.UUID, Template, error) {
	t.Helper()
	ctx := context.Background()
	up, err := f.svc.Create(ctx, f.space, f.canvas, f.member, int64(len(data)), "image/png", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	f.objects.put(TemplateKey(f.space, up.TemplateID), data, "image/png")
	tpl, err := f.svc.Complete(ctx, f.space, f.canvas, up.TemplateID, f.member)
	return up.TemplateID, tpl, err
}

func TestTemplateUploadBecomesReady(t *testing.T) {
	f := newTemplateFixture(t)
	id, tpl, err := f.upload(t, pngOf(t, 40, 30))
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Width != 40 || tpl.Height != 30 || tpl.X != 10 || tpl.Y != 20 || tpl.URL == "" {
		t.Errorf("ready template: %+v", tpl)
	}
	key := TemplateKey(f.space, id)
	if !strings.HasPrefix(key, "spaces/"+f.space.String()+"/templates/") || f.objects.signed[0] != key {
		t.Errorf("object key %q, signed %v", key, f.objects.signed)
	}

	list, err := f.svc.List(context.Background(), f.space, f.canvas)
	if err != nil || len(list) != 1 || list[0].ID != id || list[0].UploadedByName != "Tester" {
		t.Fatalf("list: %+v, %v", list, err)
	}

	if _, err := f.svc.Complete(context.Background(), f.space, f.canvas, id, f.member); !errors.Is(err, ErrTemplateFinished) {
		t.Errorf("completing twice: err = %v, want ErrTemplateFinished", err)
	}
}

func TestTemplateLimitsBeforeSigning(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	cases := []struct {
		size  int64
		ctype string
		x, y  int
	}{
		{100, "image/jpeg", 0, 0},
		{TemplateMaxBytes + 1, "image/png", 0, 0},
		{0, "image/png", 0, 0},
		{100, "image/png", 256, 0},
		{100, "image/png", 0, -1},
	}
	for _, c := range cases {
		if _, err := f.svc.Create(ctx, f.space, f.canvas, f.member, c.size, c.ctype, c.x, c.y); !errors.Is(err, ErrInvalidTemplate) {
			t.Errorf("%+v: err = %v, want ErrInvalidTemplate", c, err)
		}
	}
	if len(f.objects.signed) != 0 {
		t.Errorf("signed %d URLs for refused requests", len(f.objects.signed))
	}

	other := newSpace(t, f.svc.pool)
	if _, err := f.svc.Create(ctx, other, f.canvas, f.member, 100, "image/png", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("another space's canvas: err = %v, want ErrNotFound", err)
	}
}

func TestTemplateRejectedFilesAreDeleted(t *testing.T) {
	f := newTemplateFixture(t)
	cases := map[string][]byte{
		"not a png":   []byte("GIF89a, not a png at all"),
		"too large":   pngOf(t, 257, 10),
		"bomb header": bombPNG(),
	}
	for name, data := range cases {
		id, _, err := f.upload(t, data)
		var rej *RejectedError
		if !errors.As(err, &rej) {
			t.Errorf("%s: err = %v, want RejectedError", name, err)
			continue
		}
		if f.objects.has(TemplateKey(f.space, id)) {
			t.Errorf("%s: rejected file still in the bucket", name)
		}
	}

	// Declared one size, uploaded another.
	ctx := context.Background()
	up, err := f.svc.Create(ctx, f.space, f.canvas, f.member, 10, "image/png", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.objects.put(TemplateKey(f.space, up.TemplateID), pngOf(t, 5, 5), "image/png")
	var rej *RejectedError
	if _, err := f.svc.Complete(ctx, f.space, f.canvas, up.TemplateID, f.member); !errors.As(err, &rej) {
		t.Errorf("size mismatch: err = %v, want RejectedError", err)
	}

	// Nothing uploaded.
	up, _ = f.svc.Create(ctx, f.space, f.canvas, f.member, 10, "image/png", 0, 0)
	if _, err := f.svc.Complete(ctx, f.space, f.canvas, up.TemplateID, f.member); !errors.As(err, &rej) {
		t.Errorf("no upload: err = %v, want RejectedError", err)
	}

	if list, _ := f.svc.List(ctx, f.space, f.canvas); len(list) != 0 {
		t.Errorf("rejected templates listed: %d", len(list))
	}
}

func TestTemplateOnlyUploaderCompletes(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()
	up, err := f.svc.Create(ctx, f.space, f.canvas, f.member, 100, "image/png", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Complete(ctx, f.space, f.canvas, up.TemplateID, f.member2); !errors.Is(err, ErrNotFound) {
		t.Errorf("another member completes: err = %v, want ErrNotFound", err)
	}
}

func TestTemplateDelete(t *testing.T) {
	f := newTemplateFixture(t)
	ctx := context.Background()

	id, _, err := f.upload(t, pngOf(t, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(ctx, f.space, f.canvas, id, f.member2); !errors.Is(err, ErrForbidden) {
		t.Errorf("another member deletes: err = %v, want ErrForbidden", err)
	}
	if err := f.svc.Delete(ctx, f.space, f.canvas, id, f.member); err != nil {
		t.Errorf("uploader deletes: %v", err)
	}
	if f.objects.has(TemplateKey(f.space, id)) {
		t.Error("deleted template's file still in the bucket")
	}

	id, _, err = f.upload(t, pngOf(t, 8, 8))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(ctx, f.space, f.canvas, id, f.owner); err != nil {
		t.Errorf("owner deletes: %v", err)
	}
	if list, _ := f.svc.List(ctx, f.space, f.canvas); len(list) != 0 {
		t.Errorf("after deletes, %d listed", len(list))
	}
}

func TestTemplateCanvasFull(t *testing.T) {
	f := newTemplateFixture(t)
	for range templatesPerCanvas {
		if _, _, err := f.upload(t, pngOf(t, 2, 2)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.svc.Create(context.Background(), f.space, f.canvas, f.member, 100, "image/png", 0, 0)
	if !errors.Is(err, ErrCanvasFull) {
		t.Errorf("21st template: err = %v, want ErrCanvasFull", err)
	}
}

func TestTemplatesOff(t *testing.T) {
	svc := NewTemplates(nil, nil)
	if _, err := svc.List(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, ErrTemplatesOff) {
		t.Errorf("no bucket: err = %v, want ErrTemplatesOff", err)
	}
}

func TestTemplatesRLS(t *testing.T) {
	f := newTemplateFixture(t)
	if _, _, err := f.upload(t, pngOf(t, 4, 4)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.svc.pool.QueryRow(context.Background(), "SELECT count(*) FROM templates").Scan(&n); err != nil || n != 0 {
		t.Errorf("no tenant: %d rows visible, err %v; want 0", n, err)
	}
}
