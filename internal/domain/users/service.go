package users

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/shared"
)

// Login-failure throttle defaults: a soft cap meant to slow credential
// stuffing, not lock real users out. See auth.LoginThrottle for the tradeoff.
const (
	loginFailMax    = 10
	loginFailWindow = 15 * time.Minute
)

// Service owns the auth domain. users + sessions are global tables (no RLS),
// so every query here runs on the bare pool — no WithOrgTx.
type Service struct {
	pool     *pgxpool.Pool
	sessions *auth.SessionStore
	throttle *auth.LoginThrottle
}

func NewService(pool *pgxpool.Pool, sessions *auth.SessionStore) *Service {
	return &Service{
		pool:     pool,
		sessions: sessions,
		throttle: auth.NewLoginThrottle(loginFailMax, loginFailWindow),
	}
}

// --- View types (the wire shape; safe to JSON-marshal) -------------------

type UserView struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  *string   `json:"name,omitempty"`
}

type MembershipView struct {
	OrgID     uuid.UUID `json:"orgId"`
	OrgName   string    `json:"orgName"`
	OrgSlug   string    `json:"orgSlug"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type SessionView struct {
	ID        uuid.UUID `json:"id"`
	Current   bool      `json:"current"`
	UserAgent *string   `json:"userAgent,omitempty"`
	IP        *string   `json:"ip,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// --- Inputs --------------------------------------------------------------

type SignupInput struct {
	Email    string
	Password string
	Name     string
}

type LoginInput struct {
	Email    string
	Password string
	UA       string
	IP       string
}

// --- Operations ----------------------------------------------------------

// Signup creates a user. argon2id hash via auth.HashPassword. Returns 409 if
// the email is already registered (unique violation on the email column).
func (s *Service) Signup(ctx context.Context, in SignupInput) (*UserView, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !shared.ValidEmail(email) {
		return nil, shared.Validation("Invalid email.")
	}
	if err := validatePassword(in.Password); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, shared.Internal(err)
	}

	var u UserView
	err = s.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name)
		VALUES ($1, $2, NULLIF($3, ''))
		RETURNING id, email, name`,
		email, hash, name,
	).Scan(&u.ID, &u.Email, &u.Name)
	if err != nil {
		if shared.IsUniqueViolation(err) {
			return nil, shared.Conflict("Email is already registered.")
		}
		return nil, shared.Internal(err)
	}
	return &u, nil
}

// Login verifies credentials and mints a session. Any failure path returns a
// generic 401 — never reveal whether the email exists or which field was wrong.
func (s *Service) Login(ctx context.Context, in LoginInput) (rawToken string, _ *UserView, _ error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" {
		return "", nil, shared.Unauthorized()
	}
	// Per-account throttle: too many recent failures for this email → 429, even
	// when the password is now correct. Keyed by email so it survives an
	// attacker rotating source IPs past the per-IP limiter.
	if !s.throttle.Allowed(email) {
		return "", nil, shared.RateLimited()
	}
	var (
		id   uuid.UUID
		em   string
		name *string
		hash string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, name, password_hash FROM users WHERE email = $1`, email,
	).Scan(&id, &em, &name, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.throttle.Fail(email)
			return "", nil, shared.Unauthorized()
		}
		return "", nil, shared.Internal(err)
	}
	ok, err := auth.VerifyPassword(hash, in.Password)
	if err != nil || !ok {
		s.throttle.Fail(email)
		return "", nil, shared.Unauthorized()
	}
	raw, _, err := s.sessions.Create(ctx, id, in.UA, in.IP)
	if err != nil {
		return "", nil, shared.Internal(err)
	}
	s.throttle.Reset(email)
	return raw, &UserView{ID: id, Email: em, Name: name}, nil
}

// Logout revokes a specific session. The handler clears the cookie.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	if err := s.sessions.Revoke(ctx, sessionID); err != nil {
		return shared.Internal(err)
	}
	return nil
}

// LogoutAll revokes every session for the user (kill-switch / stolen-cookie path).
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	if err := s.sessions.RevokeAllForUser(ctx, userID); err != nil {
		return shared.Internal(err)
	}
	return nil
}

// Me returns the current user together with their org memberships. The
// cross-org membership listing goes through the user_memberships() SECURITY
// DEFINER function (migration 008) — the sanctioned escape hatch, since
// app_user is RLS-scoped to one org and cannot read memberships across orgs
// from the table directly. The function filters strictly to userID.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*UserView, []MembershipView, error) {
	var u UserView
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, name FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.Email, &u.Name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, shared.Unauthorized()
		}
		return nil, nil, shared.Internal(err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT org_id, org_name, org_slug, role, created_at
		FROM user_memberships($1)`, userID)
	if err != nil {
		return nil, nil, shared.Internal(err)
	}
	defer rows.Close()
	memberships := []MembershipView{}
	for rows.Next() {
		var m MembershipView
		if err := rows.Scan(&m.OrgID, &m.OrgName, &m.OrgSlug, &m.Role, &m.CreatedAt); err != nil {
			return nil, nil, shared.Internal(err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, shared.Internal(err)
	}
	return &u, memberships, nil
}

// ListSessions returns every active session for the user, flagging which one
// is the current request's cookie so the UI can render "this device".
func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID) ([]SessionView, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_agent, ip::text, created_at, expires_at
		FROM sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, shared.Internal(err)
	}
	defer rows.Close()
	out := []SessionView{}
	for rows.Next() {
		var v SessionView
		if err := rows.Scan(&v.ID, &v.UserAgent, &v.IP, &v.CreatedAt, &v.ExpiresAt); err != nil {
			return nil, shared.Internal(err)
		}
		v.Current = v.ID == currentSessionID
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, shared.Internal(err)
	}
	return out, nil
}

// RevokeSession revokes a session that BELONGS TO the caller. We scope by
// user_id in the WHERE clause so a user can't revoke someone else's session
// even by guessing an id; the absent row returns 404.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	cmd, err := s.pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = now()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		sessionID, userID)
	if err != nil {
		return shared.Internal(err)
	}
	if cmd.RowsAffected() == 0 {
		return shared.NotFound()
	}
	return nil
}

// --- Input validation ----------------------------------------------------

func validatePassword(s string) error {
	if len(s) < 8 {
		return shared.Validation("Password must be at least 8 characters.")
	}
	if len(s) > 1024 {
		return shared.Validation("Password too long.")
	}
	return nil
}
