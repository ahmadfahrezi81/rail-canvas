package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	bcryptCost    = 12
	sessionTTL    = 30 * 24 * time.Hour
	inviteTTL     = 7 * 24 * time.Hour
	wsTicketTTL   = 30 * time.Second
	uniqueViolate = "23505"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUnauthenticated    = errors.New("unauthenticated")
	ErrInvalidInvite      = errors.New("invalid or expired invite code")
	ErrEmailTaken         = errors.New("email already registered")
	ErrForbidden          = errors.New("forbidden")
	ErrAlreadyBootstrap   = errors.New("already bootstrapped: users exist")
)

type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
}

type Auth struct {
	pool *pgxpool.Pool
}

func NewAuth(pool *pgxpool.Pool) *Auth {
	return &Auth{pool: pool}
}

// Login returns a new session token. Unknown email and wrong password are indistinguishable.
func (a *Auth) Login(ctx context.Context, email, password string) (string, User, error) {
	row, err := store.New(a.pool).GetUserByEmail(ctx, NormalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password)) // same timing as a real check
		return "", User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", User{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)) != nil || row.Status != "active" {
		return "", User{}, ErrInvalidCredentials
	}

	token, err := a.newSession(ctx, store.New(a.pool), row.ID)
	return token, toUser(row), err
}

type SignupParams struct {
	Code, Email, DisplayName, Password string
}

// Signup creates a user from an invite, makes them a member, and logs them in.
func (a *Auth) Signup(ctx context.Context, p SignupParams) (string, User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcryptCost)
	if err != nil {
		return "", User{}, err
	}

	var token string
	var user User
	err = pgx.BeginFunc(ctx, a.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.CreateUser(ctx, store.CreateUserParams{
			Email: NormalizeEmail(p.Email), PasswordHash: string(hash), DisplayName: p.DisplayName,
		})
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
		if err != nil {
			return err
		}
		spaceID, err := q.RedeemInvite(ctx, store.RedeemInviteParams{
			UserID: uuid.NullUUID{UUID: row.ID, Valid: true}, CodeHash: hashSecret(normalizeCode(p.Code)),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvite
		}
		if err != nil {
			return err
		}
		if err := q.AddMember(ctx, store.AddMemberParams{SpaceID: spaceID, UserID: row.ID, Role: "member"}); err != nil {
			return err
		}
		user = toUser(row)
		token, err = a.newSession(ctx, q, row.ID)
		return err
	})
	return token, user, err
}

func (a *Auth) Logout(ctx context.Context, token string) error {
	return store.New(a.pool).RevokeSession(ctx, hashSecret(token))
}

// Authenticate resolves a bearer token. Live on every request: a cached
// session would keep a suspended or logged-out user working.
func (a *Auth) Authenticate(ctx context.Context, token string) (User, error) {
	row, err := store.New(a.pool).GetSessionUser(ctx, hashSecret(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUnauthenticated
	}
	if err != nil {
		return User{}, err
	}
	return toUser(row), nil
}

// CreateInvite is for owners. The code is returned once; only its hash is stored.
func (a *Auth) CreateInvite(ctx context.Context, userID, spaceID uuid.UUID) (string, time.Time, error) {
	q := store.New(a.pool)
	role, err := memberRole(ctx, q, spaceID, userID)
	if err != nil {
		return "", time.Time{}, err
	}
	if role != "owner" {
		return "", time.Time{}, ErrForbidden
	}

	code := newInviteCode()
	expires, err := q.CreateInvite(ctx, store.CreateInviteParams{
		SpaceID: spaceID, CodeHash: hashSecret(code), CreatedBy: userID, ExpiresAt: time.Now().Add(inviteTTL),
	})
	return code, expires, err
}

// Join adds an existing user to the space an invite belongs to.
func (a *Auth) Join(ctx context.Context, userID uuid.UUID, code string) (uuid.UUID, error) {
	var spaceID uuid.UUID
	err := pgx.BeginFunc(ctx, a.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		spaceID, err = q.RedeemInvite(ctx, store.RedeemInviteParams{
			UserID: uuid.NullUUID{UUID: userID, Valid: true}, CodeHash: hashSecret(normalizeCode(code)),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidInvite
		}
		if err != nil {
			return err
		}
		return q.AddMember(ctx, store.AddMemberParams{SpaceID: spaceID, UserID: userID, Role: "member"})
	})
	return spaceID, err
}

// CreateWSTicket: a single-use, 30-second ticket to open the WebSocket
// (browsers cannot send Authorization on a WebSocket).
func (a *Auth) CreateWSTicket(ctx context.Context, userID uuid.UUID) (string, time.Time, error) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	ticket := base64.RawURLEncoding.EncodeToString(b)
	expires := time.Now().Add(wsTicketTTL)
	err := store.New(a.pool).CreateWSTicket(ctx, store.CreateWSTicketParams{
		TicketHash: hashSecret(ticket), UserID: userID, ExpiresAt: expires,
	})
	return ticket, expires, err
}

// RedeemTicket uses a ticket up. Used, expired, unknown or a suspended user: ErrUnauthenticated.
func (a *Auth) RedeemTicket(ctx context.Context, ticket string) (uuid.UUID, error) {
	if ticket == "" {
		return uuid.Nil, ErrUnauthenticated
	}
	row, err := store.New(a.pool).RedeemWSTicket(ctx, hashSecret(ticket))
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrUnauthenticated
	}
	return row.ID, err
}

type BootstrapParams struct {
	SpaceName, SpaceSlug, Email, DisplayName, Password string
}

// Bootstrap creates the first space and its owner. Refuses once any user exists.
func (a *Auth) Bootstrap(ctx context.Context, p BootstrapParams) (uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcryptCost)
	if err != nil {
		return uuid.Nil, err
	}
	var spaceID uuid.UUID
	err = pgx.BeginFunc(ctx, a.pool, func(tx pgx.Tx) error {
		q := store.New(tx)
		if n, err := q.CountUsers(ctx); err != nil || n > 0 {
			return errors.Join(err, ErrAlreadyBootstrap)
		}
		space, err := q.CreateSpace(ctx, store.CreateSpaceParams{Name: p.SpaceName, Slug: p.SpaceSlug})
		if err != nil {
			return err
		}
		user, err := q.CreateUser(ctx, store.CreateUserParams{
			Email: NormalizeEmail(p.Email), PasswordHash: string(hash), DisplayName: p.DisplayName,
		})
		if err != nil {
			return err
		}
		spaceID = space.ID
		return q.AddMember(ctx, store.AddMemberParams{SpaceID: space.ID, UserID: user.ID, Role: "owner"})
	})
	return spaceID, err
}

func (a *Auth) newSession(ctx context.Context, q *store.Queries, userID uuid.UUID) (string, error) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: hashSecret(token), ExpiresAt: time.Now().Add(sessionTTL),
	})
	return token, err
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// newInviteCode: 128 random bits as 26 base32 characters.
func newInviteCode() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// normalizeCode forgives case and the spaces or dashes people add when copying.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(code)))
}

// hashSecret: SHA-256 is enough for random tokens; bcrypt is only for passwords.
func hashSecret(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)
	return h
})

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolate
}

func toUser(row store.User) User {
	return User{ID: row.ID, Email: row.Email, DisplayName: row.DisplayName}
}
