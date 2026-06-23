package invitations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// resendEndpoint is Resend's transactional-email API.
const resendEndpoint = "https://api.resend.com/emails"

// ResendMailer is the production Mailer: it delivers the accept link over
// Resend's HTTP API using only the standard library (no SDK dependency). Like
// every Mailer this is the one channel the raw token leaves the process — it is
// placed in the email body/link and is never logged or persisted.
type ResendMailer struct {
	apiKey  string
	from    string       // RFC-5322 From, e.g. "StatusFlow <noreply@statusflow.example>"
	baseURL string       // app base URL for the accept link; token-only if empty
	client  *http.Client // see NewResendMailer for the timeout policy
}

// NewResendMailer builds the transport. apiKey and from are required (the caller
// validates config at boot); baseURL is optional — when set the email carries a
// clickable accept link, otherwise it falls back to the raw token plus the API
// path, matching the dev outbox.
func NewResendMailer(apiKey, from, baseURL string) *ResendMailer {
	return &ResendMailer{
		apiKey:  apiKey,
		from:    from,
		baseURL: strings.TrimRight(baseURL, "/"),
		// No overall client Timeout: the end-to-end bound is the caller's context
		// deadline (the Deliverer sets it to sendTimeout), so tuning that knob
		// actually takes effect rather than being silently capped here. A dial
		// timeout is the floor for any caller that forgets to pass a deadline.
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			},
		},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (m *ResendMailer) SendInvitation(ctx context.Context, inv Invite) error {
	payload, err := json.Marshal(resendRequest{
		From:    m.from,
		To:      []string{inv.To},
		Subject: "You've been invited to a StatusFlow organization",
		Text:    m.body(inv),
	})
	if err != nil {
		return fmt.Errorf("resend: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resendEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	// Surface the status (and a bounded slice of the body) for the Deliverer's
	// last_error annotation — note the body is Resend's API error, never the token.
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	return fmt.Errorf("resend: status %d: %s", res.StatusCode, strings.TrimSpace(string(snippet)))
}

// body renders the plaintext email. The raw token is the secret; when a base URL
// is configured it is carried in a clickable accept link, otherwise the invitee
// posts it to the API path directly (same shape as the dev outbox).
func (m *ResendMailer) body(inv Invite) string {
	var action string
	if m.baseURL != "" {
		action = fmt.Sprintf("Accept your invitation:\n\n%s/invitations/accept?token=%s\n",
			m.baseURL, url.QueryEscape(inv.RawToken))
	} else {
		action = fmt.Sprintf("Accept by POSTing this token to /api/invitations/accept:\n\n%s\n", inv.RawToken)
	}
	return fmt.Sprintf(
		"You've been invited to join a StatusFlow organization as %s.\n\nThis invitation expires %s.\n\n%s",
		inv.Role, inv.ExpiresAt.Format(time.RFC1123), action,
	)
}
