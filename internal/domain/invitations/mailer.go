package invitations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/ishaangarg9/statusflow/internal/authz"
)

// Invite is everything a Mailer needs to deliver an invitation. RawToken is the
// secret accept token — it travels ONLY through this out-of-band channel and is
// never logged or returned over the API (CLAUDE.md §6).
type Invite struct {
	To        string
	RawToken  string
	OrgID     uuid.UUID
	Role      authz.Role
	ExpiresAt time.Time
}

// Mailer delivers an invitation's accept link to the invitee. It is the only
// place the raw token leaves the server, which is why the service routes
// delivery through it instead of returning the token to the HTTP layer. Phase 6
// (hardening) swaps the dev implementation for a real SMTP/provider transport.
type Mailer interface {
	SendInvitation(ctx context.Context, inv Invite) error
}

// OutboxMailer is the DEVELOPMENT transport: it writes each invitation to a file
// in an outbox directory (the conventional "filesystem mail" dev pattern). This
// is a genuine out-of-band delivery channel — distinct from the slog application
// log, which must never carry a token — so the token may appear in the message
// body exactly as it would in a real email.
//
// It is dev-only: it persists live, redeemable tokens to disk in plaintext and
// never prunes them. A non-dev deployment MUST wire a real transport instead
// (Phase 6); do not ship this to production.
type OutboxMailer struct {
	dir     string
	initErr error
}

// NewOutboxMailer writes invitations to dir. An empty dir defaults to
// <tmp>/statusflow-invites. The directory is created once here rather than on
// every send.
func NewOutboxMailer(dir string) *OutboxMailer {
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "statusflow-invites")
	}
	m := &OutboxMailer{dir: dir}
	// 0700: the outbox holds live tokens; keep it traversable only by the owner.
	m.initErr = os.MkdirAll(dir, 0o700)
	return m
}

func (m *OutboxMailer) SendInvitation(_ context.Context, inv Invite) error {
	if m.initErr != nil {
		return fmt.Errorf("outbox unavailable: %w", m.initErr)
	}
	name := fmt.Sprintf("invite-%d-%s.txt", time.Now().UnixNano(), uuid.NewString()[:8])
	body := fmt.Sprintf(
		"To: %s\nOrg: %s\nRole: %s\nExpires: %s\n\nAccept this invitation by POSTing the token below to /api/invitations/accept:\n\n%s\n",
		inv.To, inv.OrgID, inv.Role, inv.ExpiresAt.Format(time.RFC3339), inv.RawToken,
	)
	// 0600: the outbox holds live tokens; keep it readable only by the owner.
	if err := os.WriteFile(filepath.Join(m.dir, name), []byte(body), 0o600); err != nil {
		return fmt.Errorf("outbox write: %w", err)
	}
	return nil
}
