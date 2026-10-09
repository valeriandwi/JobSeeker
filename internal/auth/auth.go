package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
	"github.com/valeriandwipurnomo/jobseeker/internal/storage"
)

// ErrEmailTaken is returned when a registration collides with an existing user.
var ErrEmailTaken = fmt.Errorf("%w: email already registered", httpx.ErrConflict)

// ErrInvalidCredentials is returned for unknown email or wrong password. The two
// cases are deliberately indistinguishable to the caller.
var ErrInvalidCredentials = fmt.Errorf("%w: invalid email or password", httpx.ErrUnauthorized)

// ErrSessionExpired indicates a session token that existed but is past its TTL.
var ErrSessionExpired = fmt.Errorf("%w: session expired", httpx.ErrUnauthorized)

// User is the public representation of an account.
type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

// Store is the database access layer for users and sessions.
type Store struct {
	db *storage.DB
}

// NewStore wires a Store to the connection pool.
func NewStore(db *storage.DB) *Store { return &Store{db: db} }

// CreateUser inserts a user, normalizing the email. A duplicate address yields
// ErrEmailTaken.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash, displayName string) (User, error) {
	const q = `
		insert into users (email, password_hash, display_name)
		values ($1, $2, $3)
		returning id, email, display_name, created_at`
	var u User
	err := s.db.Pool.QueryRow(ctx, q, NormalizeEmail(email), passwordHash, displayName).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrEmailTaken
		}
		return User{}, fmt.Errorf("inserting user: %w", err)
	}
	return u, nil
}

// UserByEmail returns the user and their password hash.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, string, error) {
	const q = `
		select id, email, display_name, created_at, password_hash
		from users where lower(email) = lower($1)`
	var u User
	var hash string
	err := s.db.Pool.QueryRow(ctx, q, NormalizeEmail(email)).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return User{}, "", fmt.Errorf("loading user by email: %w", err)
	}
	return u, hash, nil
}

// UpdatePasswordHash rewrites the stored hash (used on rehash-on-login).
func (s *Store) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, hash string) error {
	const q = `update users set password_hash = $2, updated_at = now() where id = $1`
	if _, err := s.db.Pool.Exec(ctx, q, userID, hash); err != nil {
		return fmt.Errorf("updating password hash: %w", err)
	}
	return nil
}

// CreateSession stores a session digest for a user.
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, digest []byte, expiresAt time.Time, userAgent, ip string) error {
	const q = `
		insert into sessions (user_id, token_hash, expires_at, user_agent, ip)
		values ($1, $2, $3, $4, nullif($5, '')::inet)`
	var err error
	if _, err = s.db.Pool.Exec(ctx, q, userID, digest, expiresAt, userAgent, ip); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	return nil
}

// SessionUser resolves a session digest to its user, rejecting expired sessions.
func (s *Store) SessionUser(ctx context.Context, digest []byte) (User, time.Time, error) {
	const q = `
		select u.id, u.email, u.display_name, u.created_at, s.expires_at
		from sessions s
		join users u on u.id = s.user_id
		where s.token_hash = $1`
	var u User
	var expires time.Time
	err := s.db.Pool.QueryRow(ctx, q, digest).Scan(&u.ID, &u.Email, &u.DisplayName, &u.CreatedAt, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, time.Time{}, httpx.ErrUnauthorized
	}
	if err != nil {
		return User{}, time.Time{}, fmt.Errorf("loading session: %w", err)
	}
	if time.Now().After(expires) {
		return User{}, expires, ErrSessionExpired
	}
	return u, expires, nil
}

// TouchSession records activity on a session (best effort; a failure to touch
// must never fail the request).
func (s *Store) TouchSession(ctx context.Context, digest []byte) error {
	const q = `update sessions set last_seen_at = now() where token_hash = $1`
	if _, err := s.db.Pool.Exec(ctx, q, digest); err != nil {
		return fmt.Errorf("touching session: %w", err)
	}
	return nil
}

// DeleteSession revokes a single session.
func (s *Store) DeleteSession(ctx context.Context, digest []byte) error {
	const q = `delete from sessions where token_hash = $1`
	if _, err := s.db.Pool.Exec(ctx, q, digest); err != nil {
		return fmt.Errorf("deleting session: %w", err)
	}
	return nil
}

// DeleteSessionsForUser revokes every session of a user.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	const q = `delete from sessions where user_id = $1`
	tag, err := s.db.Pool.Exec(ctx, q, userID)
	if err != nil {
		return 0, fmt.Errorf("deleting user sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpiredSessions prunes expired rows; call it from the scheduler.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	const q = `delete from sessions where expires_at < now()`
	tag, err := s.db.Pool.Exec(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("pruning sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SessionConfig controls session token lifetime and cookie attributes.
type SessionConfig struct {
	CookieName   string
	TTL          time.Duration
	CookieSecure bool
}

// Service implements registration, login and session lifecycle.
type Service struct {
	store *Store
	cfg   SessionConfig
	now   func() time.Time
	// dummyHash equalizes response time for unknown accounts.
	dummyHash string
}

// NewService builds the auth service.
func NewService(store *Store, cfg SessionConfig) *Service {
	dummy, err := HashPassword("dummy-password-for-timing-equalization")
	if err != nil {
		dummy = ""
	}
	return &Service{store: store, cfg: cfg, now: time.Now, dummyHash: dummy}
}

// Register creates an account and immediately issues a session.
func (s *Service) Register(ctx context.Context, email, password, displayName string, meta SessionMeta) (User, string, error) {
	if err := ValidateEmail(email); err != nil {
		return User{}, "", httpx.NewValidationError(err.Error(), httpx.FieldError{Field: "email", Message: err.Error()})
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, "", httpx.NewValidationError(err.Error(), httpx.FieldError{Field: "password", Message: err.Error()})
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, "", err
	}
	user, err := s.store.CreateUser(ctx, email, hash, displayName)
	if err != nil {
		return User{}, "", err
	}
	raw, err := s.issue(ctx, user, meta)
	if err != nil {
		return User{}, "", err
	}
	return user, raw, nil
}

// Login verifies credentials and issues a session.
func (s *Service) Login(ctx context.Context, email, password string, meta SessionMeta) (User, string, error) {
	user, hash, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			// Burn comparable CPU so timing does not reveal account existence.
			if s.dummyHash != "" {
				_, _ = VerifyPassword(password, s.dummyHash)
			}
			return User{}, "", ErrInvalidCredentials
		}
		return User{}, "", err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return User{}, "", fmt.Errorf("verifying password: %w", err)
	}
	if !ok {
		return User{}, "", ErrInvalidCredentials
	}
	if NeedsRehash(hash) {
		if newHash, herr := HashPassword(password); herr == nil {
			_ = s.store.UpdatePasswordHash(ctx, user.ID, newHash)
		}
	}
	raw, err := s.issue(ctx, user, meta)
	if err != nil {
		return User{}, "", err
	}
	return user, raw, nil
}

// Authenticate resolves a raw session token to a user.
func (s *Service) Authenticate(ctx context.Context, raw string) (User, error) {
	if raw == "" {
		return User{}, httpx.ErrUnauthorized
	}
	digest := TokenDigest(raw)
	user, _, err := s.store.SessionUser(ctx, digest)
	if err != nil {
		return User{}, err
	}
	// Best effort; never blocks the request.
	go func() {
		touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = s.store.TouchSession(touchCtx, digest)
	}()
	return user, nil
}

// Logout revokes the session behind a raw token.
func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	return s.store.DeleteSession(ctx, TokenDigest(raw))
}

// RevokeAll ends every session of a user (password change, incident response).
func (s *Service) RevokeAll(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.store.DeleteSessionsForUser(ctx, userID)
}

func (s *Service) issue(ctx context.Context, user User, meta SessionMeta) (string, error) {
	raw, digest, err := NewSessionToken()
	if err != nil {
		return "", err
	}
	expires := s.now().Add(s.cfg.TTL)
	if err := s.store.CreateSession(ctx, user.ID, digest, expires, meta.UserAgent, meta.IP); err != nil {
		return "", err
	}
	return raw, nil
}

// SessionMeta carries request metadata recorded with a session.
type SessionMeta struct {
	UserAgent string
	IP        string
}

// SetSessionCookie writes the session cookie.
func (s *Service) SetSessionCookie(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(s.cfg.TTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie expires the session cookie.
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// CookieName exposes the configured cookie name (used by middleware and tests).
func (s *Service) CookieName() string { return s.cfg.CookieName }
