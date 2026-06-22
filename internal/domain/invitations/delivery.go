package invitations

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/auth"
	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

// maxDeliveryAttempts caps retries before a delivery is abandoned. Issuance
// stays valid regardless — an admin can always resend, which re-queues a fresh
// delivery — so giving up only stops the worker from looping on a dead address.
const maxDeliveryAttempts = 8

// Deliverer drains the invitation_outbox: for each due row it mints the accept
// token, records only its hash on the invitation, and hands the raw token to the
// Mailer. The raw token is generated here and never persisted, so it exists only
// in memory for the duration of one send. This runs in the worker process; the
// API only enqueues (invitations.Service.Create).
type Deliverer struct {
	pool   *pgxpool.Pool
	mailer Mailer
	log    *slog.Logger
}

func NewDeliverer(pool *pgxpool.Pool, mailer Mailer, log *slog.Logger) *Deliverer {
	return &Deliverer{pool: pool, mailer: mailer, log: log}
}

type claimedDelivery struct {
	outboxID     uuid.UUID
	orgID        uuid.UUID
	invitationID uuid.UUID
	email        string
	role         string
	expiresAt    time.Time
	acceptedAt   *time.Time
	attempts     int
}

// DrainOnce claims and processes up to limit due deliveries and returns how many
// rows it handled (sent or dropped). leaseSeconds is how long a claimed row is
// hidden from other workers before it's eligible for retry — long enough to
// cover a send, short enough that a crashed delivery is retried promptly.
func (d *Deliverer) DrainOnce(ctx context.Context, limit, leaseSeconds int) (int, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT outbox_id, org_id, invitation_id, email, role, expires_at, accepted_at, attempts
		 FROM claim_invitation_deliveries($1, $2)`, limit, leaseSeconds)
	if err != nil {
		return 0, err
	}
	var batch []claimedDelivery
	for rows.Next() {
		var c claimedDelivery
		if err := rows.Scan(&c.outboxID, &c.orgID, &c.invitationID, &c.email, &c.role,
			&c.expiresAt, &c.acceptedAt, &c.attempts); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	handled := 0
	for _, c := range batch {
		if d.process(ctx, c) {
			handled++
		}
	}
	return handled, nil
}

// process delivers (or drops) one claimed row. It returns true when the row
// reached a terminal state this pass (sent, or abandoned/already-terminal and
// removed). A transient send failure returns false and leaves the row to be
// retried after its lease.
func (d *Deliverer) process(ctx context.Context, c claimedDelivery) bool {
	// Terminal already: accepted or expired → nothing to deliver, drop the row.
	if c.acceptedAt != nil || time.Now().After(c.expiresAt) {
		d.deleteOutbox(ctx, c)
		return true
	}
	// Exhausted retries → stop looping on a likely-dead address. The invitation
	// itself remains valid (resend re-queues), so we only drop the outbox row.
	if c.attempts > maxDeliveryAttempts {
		d.log.Warn("invitation delivery abandoned", "invitation", c.invitationID, "org", c.orgID, "attempts", c.attempts)
		d.deleteOutbox(ctx, c)
		return true
	}

	// Mint a fresh token and record only its hash on the invitation. Done under
	// WithOrgTx so RLS is armed; scoped to an unaccepted invite so we never
	// overwrite the token of one accepted in a race.
	raw, err := auth.NewSessionToken()
	if err != nil {
		d.recordError(ctx, c, "token: "+err.Error())
		return false
	}
	hash := auth.HashToken(raw)
	err = tenancy.WithOrgTx(ctx, d.pool, c.orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE invitations SET token_hash = $1
			 WHERE org_id = $2 AND id = $3 AND accepted_at IS NULL`,
			hash, c.orgID, c.invitationID)
		return err
	})
	if err != nil {
		d.recordError(ctx, c, "store hash: "+err.Error())
		return false
	}

	// Hand the raw token to the transport. This is the ONLY place it leaves the
	// process; it is never persisted or logged.
	if err := d.mailer.SendInvitation(ctx, Invite{
		To: c.email, RawToken: raw, OrgID: c.orgID, Role: authz.Role(c.role), ExpiresAt: c.expiresAt,
	}); err != nil {
		d.recordError(ctx, c, "send: "+err.Error())
		return false
	}

	d.deleteOutbox(ctx, c)
	return true
}

func (d *Deliverer) deleteOutbox(ctx context.Context, c claimedDelivery) {
	if err := tenancy.WithOrgTx(ctx, d.pool, c.orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM invitation_outbox WHERE org_id = $1 AND id = $2`,
			c.orgID, c.outboxID)
		return err
	}); err != nil {
		// Worst case the row is redelivered after its lease; the token is bound to
		// the email and single-use, so a duplicate send is harmless.
		d.log.Error("delete delivered outbox row", "outbox", c.outboxID, "org", c.orgID, "err", err)
	}
}

// recordError stores the failure for observability. The claim already bumped
// next_attempt_at (the retry lease) and attempts, so this only annotates.
func (d *Deliverer) recordError(ctx context.Context, c claimedDelivery, msg string) {
	d.log.Warn("invitation delivery failed", "invitation", c.invitationID, "org", c.orgID, "attempt", c.attempts, "err", msg)
	if err := tenancy.WithOrgTx(ctx, d.pool, c.orgID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE invitation_outbox SET last_error = $1 WHERE org_id = $2 AND id = $3`,
			msg, c.orgID, c.outboxID)
		return err
	}); err != nil {
		d.log.Error("record delivery error", "outbox", c.outboxID, "org", c.orgID, "err", err)
	}
}
