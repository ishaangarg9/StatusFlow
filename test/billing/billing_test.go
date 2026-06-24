// Package billing_test is the Phase 10 proof: it exercises the billing domain
// against a REAL Postgres as the restricted app_user, end to end —
//
//  1. a Free org's monitor cap is enforced server-side (the 4th create is 402);
//  2. a signature-verified Stripe webhook is the ONLY thing that upgrades the
//     org to Pro (no API lets the client set its own plan); and
//  3. once Pro, the previously-blocked create succeeds.
//
// Skips (not fails) when DATABASE_URL is unset, matching the other suites.
package billing_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ishaangarg9/statusflow/internal/authz"
	"github.com/ishaangarg9/statusflow/internal/db"
	"github.com/ishaangarg9/statusflow/internal/domain/billing"
	"github.com/ishaangarg9/statusflow/internal/domain/monitors"
	"github.com/ishaangarg9/statusflow/internal/shared"
	"github.com/ishaangarg9/statusflow/internal/tenancy"
)

const webhookSecret = "whsec_billing_test"

func setup(t *testing.T) (context.Context, *pgxpool.Pool, authz.AuthContext) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping billing proof (needs real Postgres as app_user)")
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as app_user: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()[:8]
	org := uuid.New()
	user := uuid.New()

	if _, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')`,
		user, "billing-"+suffix+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := tenancy.WithOrgTx(ctx, pool, org, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO organizations (id, name, slug) VALUES ($1, $2, $3)`,
			org, "billing-"+suffix, "billing-"+suffix); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO memberships (org_id, user_id, role) VALUES ($1, $2, 'owner')`,
			org, user)
		return err
	}); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = tenancy.WithOrgTx(cctx, pool, org, func(tx pgx.Tx) error {
			_, err := tx.Exec(cctx, `DELETE FROM organizations WHERE id = $1`, org)
			return err
		})
		_, _ = pool.Exec(cctx, `DELETE FROM users WHERE id = $1`, user)
	})

	return ctx, pool, authz.AuthContext{UserID: user, OrgID: org, Role: authz.RoleOwner}
}

// signEvent forges a valid Stripe-Signature header for a payload (the webhook
// secret is shared between this test and the service config).
func signEvent(payload []byte) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func newMonitor(t *testing.T, ctx context.Context, svc *monitors.Service, ac authz.AuthContext, n int) error {
	t.Helper()
	_, err := svc.Create(ctx, ac, monitors.CreateInput{
		Name: fmt.Sprintf("m%d", n),
		URL:  "https://example.test",
	})
	return err
}

func TestFreePlanCapThenWebhookUpgrade(t *testing.T) {
	ctx, pool, ac := setup(t)

	monSvc := monitors.NewService(pool)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	billSvc := billing.NewService(pool, nil, billing.Config{WebhookSecret: webhookSecret}, logger)

	// Free plan allows 3 monitors.
	for i := 1; i <= 3; i++ {
		if err := newMonitor(t, ctx, monSvc, ac, i); err != nil {
			t.Fatalf("free monitor %d should succeed: %v", i, err)
		}
	}

	// The 4th must be refused with a 402 plan-limit error — enforced server-side,
	// independent of the caller's role.
	err := newMonitor(t, ctx, monSvc, ac, 4)
	var ae *shared.AppError
	if !errors.As(err, &ae) || ae.Status != 402 {
		t.Fatalf("4th monitor on Free should be 402 plan_limit, got %v", err)
	}

	// Upgrade ONLY via a signed webhook (nothing the client can call sets plan).
	payload := []byte(fmt.Sprintf(
		`{"id":"evt_test","type":"checkout.session.completed","data":{"object":{"client_reference_id":"%s","customer":"cus_test","subscription":"sub_test"}}}`,
		ac.OrgID))
	if err := billSvc.HandleWebhook(ctx, payload, signEvent(payload)); err != nil {
		t.Fatalf("valid webhook should apply: %v", err)
	}

	// The projection now reads Pro/active with the customer stored.
	var plan, status, customer string
	if err := tenancy.WithOrgTx(ctx, pool, ac.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT plan, status, stripe_customer_id FROM subscriptions WHERE org_id = $1`,
			ac.OrgID).Scan(&plan, &status, &customer)
	}); err != nil {
		t.Fatalf("read subscription after webhook: %v", err)
	}
	if plan != "pro" || status != "active" || customer != "cus_test" {
		t.Fatalf("webhook did not upgrade org: plan=%s status=%s customer=%s", plan, status, customer)
	}

	// Pro is unlimited, so the previously-blocked create now succeeds.
	if err := newMonitor(t, ctx, monSvc, ac, 4); err != nil {
		t.Fatalf("4th monitor on Pro should succeed: %v", err)
	}
}

func TestWebhookRejectsForgedSignature(t *testing.T) {
	ctx, pool, ac := setup(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	billSvc := billing.NewService(pool, nil, billing.Config{WebhookSecret: webhookSecret}, logger)

	payload := []byte(fmt.Sprintf(
		`{"id":"evt_forge","type":"checkout.session.completed","data":{"object":{"client_reference_id":"%s"}}}`,
		ac.OrgID))

	// A signature from the wrong secret must be rejected as ErrInvalidSignature,
	// and must NOT have upgraded the org.
	bad := signWith("whsec_attacker", payload)
	if err := billSvc.HandleWebhook(ctx, payload, bad); !errors.Is(err, billing.ErrInvalidSignature) {
		t.Fatalf("forged webhook should be ErrInvalidSignature, got %v", err)
	}

	var count int
	if err := tenancy.WithOrgTx(ctx, pool, ac.OrgID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM subscriptions WHERE org_id = $1`, ac.OrgID).Scan(&count)
	}); err != nil {
		t.Fatalf("count subscriptions: %v", err)
	}
	if count != 0 {
		t.Fatal("forged webhook must not have created a subscription row")
	}
}

func signWith(secret string, payload []byte) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
