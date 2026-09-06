package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// User is the global identity. Not tenant-scoped; lookups don't need an org context.
type User struct {
	ID    uuid.UUID
	Email string
	Name  *string
}

// Session represents a row in the sessions table.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
}

type SessionStore struct {
	pool       *pgxpool.Pool
	ttl        time.Duration
	cookieName string
	secure     bool
}

func NewSessionStore(pool *pgxpool.Pool, ttl time.Duration, cookieName string, secure bool) *SessionStore {
	return &SessionStore{pool: pool, ttl: ttl, cookieName: cookieName, secure: secure}
}

// Create inserts a session row and returns the RAW token for the cookie.
// Only the hash is stored.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, ua, ip string) (rawToken string, _ *Session, _ error) {
	return s.createWithTTL(ctx, userID, ua, ip, s.ttl)
}

// CreateWithTTL is Create but with a caller-supplied lifetime instead of the
// store's configured TTL — for sessions that should expire sooner than a
// normal login (the P15 demo account; see users.Service.DemoLogin).
func (s *SessionStore) CreateWithTTL(ctx context.Context, userID uuid.UUID, ua, ip string, ttl time.Duration) (rawToken string, _ *Session, _ error) {
	return s.createWithTTL(ctx, userID, ua, ip, ttl)
}

func (s *SessionStore) createWithTTL(ctx context.Context, userID uuid.UUID, ua, ip string, ttl time.Duration) (rawToken string, _ *Session, _ error) {
	raw, err := NewSessionToken()
	if err != nil {
		return "", nil, fmt.Errorf("token: %w", err)
	}
	hash := HashToken(raw)
	expires := time.Now().Add(ttl)

	var sess Session
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, '')::inet, $5)
		RETURNING id, user_id, expires_at`,
		userID, hash, ua, ip, expires,
	).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt)
	if err != nil {
		return "", nil, fmt.Errorf("insert session: %w", err)
	}
	return raw, &sess, nil
}

// Resolve hashes the raw cookie token, looks up an active session, and
// returns the owning user plus the session row.
func (s *SessionStore) Resolve(ctx context.Context, rawToken string) (*User, *Session, error) {
	if rawToken == "" {
		return nil, nil, pgx.ErrNoRows
	}
	hash := HashToken(rawToken)
	var u User
	var sess Session
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.expires_at,
		       u.id, u.email, u.name
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > now()`,
		hash,
	).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt, &u.ID, &u.Email, &u.Name)
	if err != nil {
		return nil, nil, err
	}
	return &u, &sess, nil
}

// Revoke marks a single session revoked (logout for one device).
func (s *SessionStore) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		sessionID,
	)
	return err
}

// RevokeByRawToken revokes by cookie value, looking up via the stored hash.
func (s *SessionStore) RevokeByRawToken(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL`,
		HashToken(rawToken),
	)
	return err
}

// RevokeAllForUser logs the user out everywhere.
func (s *SessionStore) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	return err
}

// SetCookie writes the session cookie with safe defaults.
func (s *SessionStore) SetCookie(w http.ResponseWriter, rawToken string) {
	s.setCookie(w, rawToken, s.ttl)
}

// SetCookieWithTTL is SetCookie but with a caller-supplied Max-Age, so a
// cookie minted alongside CreateWithTTL doesn't outlive the session it
// actually points to (the server would reject it as expired regardless, but a
// matching Max-Age avoids a stale-looking "logged in" cookie in the browser).
func (s *SessionStore) SetCookieWithTTL(w http.ResponseWriter, rawToken string, ttl time.Duration) {
	s.setCookie(w, rawToken, ttl)
}

func (s *SessionStore) setCookie(w http.ResponseWriter, rawToken string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    rawToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

// ClearCookie expires the session cookie client-side.
func (s *SessionStore) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (s *SessionStore) CookieName() string { return s.cookieName }

// IsNotFound reports whether err is "no active session" (lookup miss).
func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
