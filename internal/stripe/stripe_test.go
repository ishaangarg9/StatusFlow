package stripe

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"
)

// sign reproduces Stripe's signature scheme for a given timestamp so the tests
// can forge valid (and deliberately invalid) headers.
func sign(secret string, ts int64, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(payload)
	return "t=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook_Valid(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"client_reference_id":"org"}}}`)
	now := time.Now()
	header := sign(secret, now.Unix(), body)

	ev, err := VerifyWebhook(body, header, secret, now)
	if err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if ev.Type != "checkout.session.completed" || ev.ID != "evt_1" {
		t.Fatalf("decoded wrong event: %+v", ev)
	}
	if len(ev.Object) == 0 {
		t.Fatal("event object not lifted out")
	}
}

func TestVerifyWebhook_TamperedBodyRejected(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"type":"x","data":{"object":{}}}`)
	now := time.Now()
	header := sign(secret, now.Unix(), body)

	// Flip the body after signing — the signature must no longer match.
	tampered := []byte(`{"type":"customer.subscription.deleted","data":{"object":{}}}`)
	if _, err := VerifyWebhook(tampered, header, secret, now); err == nil {
		t.Fatal("expected tampered body to fail verification")
	}
}

func TestVerifyWebhook_WrongSecretRejected(t *testing.T) {
	body := []byte(`{"type":"x","data":{"object":{}}}`)
	now := time.Now()
	header := sign("whsec_attacker", now.Unix(), body)
	if _, err := VerifyWebhook(body, header, "whsec_real", now); err == nil {
		t.Fatal("expected signature from wrong secret to be rejected")
	}
}

func TestVerifyWebhook_StaleTimestampRejected(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"type":"x","data":{"object":{}}}`)
	old := time.Now().Add(-1 * time.Hour)
	header := sign(secret, old.Unix(), body)
	// Signature is valid, but the timestamp is far outside tolerance (replay).
	if _, err := VerifyWebhook(body, header, secret, time.Now()); err == nil {
		t.Fatal("expected stale-timestamp event to be rejected as a replay")
	}
}

func TestVerifyWebhook_NoSecretRejected(t *testing.T) {
	body := []byte(`{}`)
	if _, err := VerifyWebhook(body, "t=1,v1=abc", "", time.Now()); err == nil {
		t.Fatal("expected verification to fail when no secret is configured")
	}
}

func TestVerifyWebhook_MalformedHeaderRejected(t *testing.T) {
	if _, err := VerifyWebhook([]byte(`{}`), "garbage", "whsec_test", time.Now()); err == nil {
		t.Fatal("expected malformed signature header to be rejected")
	}
}
