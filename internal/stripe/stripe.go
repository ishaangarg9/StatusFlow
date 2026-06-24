// Package stripe is a thin, standard-library-only client for the slice of the
// Stripe API that StatusFlow needs: creating a Checkout Session, creating a
// Billing Portal session, and verifying webhook signatures. We deliberately do
// NOT pull in stripe-go — matching the Resend transport (internal/domain/
// invitations/resend.go) and CLAUDE.md's "prefer the standard library". The
// surface is small enough that an SDK would add more dependency than value, and
// keeping it explicit makes the money-handling path auditable.
//
// Signature verification uses crypto/hmac (a stdlib primitive) — this is not
// hand-rolling crypto, it is the documented Stripe scheme (HMAC-SHA256 over
// "<timestamp>.<body>") implemented against the standard library.
package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.stripe.com"

// Client talks to the Stripe REST API with a secret key. Construct it via New;
// a zero Client is not usable.
type Client struct {
	secretKey string
	http      *http.Client
}

// New builds a client bound to a (test-mode) secret key. The HTTP client has a
// dial-timeout floor; the end-to-end bound is the caller's context deadline.
func New(secretKey string) *Client {
	return &Client{
		secretKey: secretKey,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			},
		},
	}
}

// CheckoutParams configures a subscription Checkout Session.
type CheckoutParams struct {
	PriceID    string // the Stripe Price for the Pro plan
	SuccessURL string
	CancelURL  string
	OrgID      string // our tenant id; bound as client_reference_id AND subscription metadata
	CustomerID string // reuse an existing Stripe customer when known (avoids dupes); optional
	Email      string // pre-fill the email when no customer exists yet; optional
}

// CreateCheckoutSession creates a subscription Checkout Session and returns the
// hosted-checkout URL the browser should be redirected to. The org id is bound
// both as client_reference_id (read on checkout.session.completed) and as
// subscription metadata (read on every later customer.subscription.* event), so
// the webhook can always resolve the tenant without a reverse lookup.
func (c *Client) CreateCheckoutSession(ctx context.Context, p CheckoutParams) (string, error) {
	form := url.Values{}
	form.Set("mode", "subscription")
	form.Set("line_items[0][price]", p.PriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", p.SuccessURL)
	form.Set("cancel_url", p.CancelURL)
	form.Set("client_reference_id", p.OrgID)
	form.Set("subscription_data[metadata][org_id]", p.OrgID)
	// Bind the tenant to the customer too, so portal-initiated changes that only
	// carry the customer can still be traced back if metadata is ever stripped.
	form.Set("metadata[org_id]", p.OrgID)
	if p.CustomerID != "" {
		form.Set("customer", p.CustomerID)
	} else if p.Email != "" {
		form.Set("customer_email", p.Email)
	}

	var out struct {
		URL string `json:"url"`
	}
	if err := c.post(ctx, "/v1/checkout/sessions", form, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("stripe: checkout session returned no url")
	}
	return out.URL, nil
}

// CreatePortalSession opens a Billing Portal session for an existing customer
// and returns the URL to redirect to (manage payment method, cancel, etc.).
func (c *Client) CreatePortalSession(ctx context.Context, customerID, returnURL string) (string, error) {
	form := url.Values{}
	form.Set("customer", customerID)
	form.Set("return_url", returnURL)

	var out struct {
		URL string `json:"url"`
	}
	if err := c.post(ctx, "/v1/billing_portal/sessions", form, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("stripe: portal session returned no url")
	}
	return out.URL, nil
}

// post issues a form-encoded POST with the secret key as a Bearer token and
// decodes a 2xx JSON body into out. Non-2xx responses are surfaced with the
// Stripe error message (never any secret).
func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("stripe: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %s: %w", path, err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("stripe: %s: status %d: %s", path, res.StatusCode, msg)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("stripe: %s: decode: %w", path, err)
		}
	}
	return nil
}

// Event is the decoded envelope of a webhook event. Object is the raw event
// payload (a Checkout Session or a Subscription, per Type) — the caller decodes
// it into the right shape.
type Event struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Object json.RawMessage // data.object, lifted out for convenience
}

// signatureTolerance bounds the clock skew between Stripe and us; an event whose
// timestamp is older/newer than this is rejected as a replay.
const signatureTolerance = 5 * time.Minute

// VerifyWebhook authenticates a raw webhook body against the Stripe-Signature
// header using the endpoint's signing secret, then decodes the event. It
// reproduces Stripe's scheme: signed_payload = "<t>.<body>", compared (constant
// time) against each v1 signature in the header, with a timestamp-tolerance
// check to defeat replays. A failure here means the body is NOT trusted and the
// caller must reject the request — this is the only thing standing between the
// public webhook endpoint and a forged "you're now on Pro" event.
func VerifyWebhook(payload []byte, sigHeader, secret string, now time.Time) (*Event, error) {
	if secret == "" {
		return nil, fmt.Errorf("stripe: webhook secret not configured")
	}
	ts, sigs := parseSignatureHeader(sigHeader)
	if ts == "" || len(sigs) == 0 {
		return nil, fmt.Errorf("stripe: malformed signature header")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("stripe: bad signature timestamp")
	}
	if diff := now.Sub(time.Unix(tsInt, 0)); diff < -signatureTolerance || diff > signatureTolerance {
		return nil, fmt.Errorf("stripe: signature timestamp outside tolerance")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(payload)
	expected := mac.Sum(nil)

	ok := false
	for _, s := range sigs {
		got, err := hex.DecodeString(s)
		if err != nil {
			continue
		}
		if hmac.Equal(got, expected) {
			ok = true
			break
		}
	}
	if !ok {
		return nil, fmt.Errorf("stripe: no matching signature")
	}

	var raw struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Data struct {
			Object json.RawMessage `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("stripe: decode event: %w", err)
	}
	return &Event{ID: raw.ID, Type: raw.Type, Object: raw.Data.Object}, nil
}

// parseSignatureHeader splits a "t=...,v1=...,v1=..." header into its timestamp
// and the list of v1 signatures.
func parseSignatureHeader(h string) (ts string, sigs []string) {
	for _, part := range strings.Split(h, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "t":
			ts = kv[1]
		case "v1":
			sigs = append(sigs, kv[1])
		}
	}
	return ts, sigs
}
