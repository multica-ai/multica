package composio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Webhook header names Composio sets on every delivery.
const (
	HeaderWebhookID        = "webhook-id"
	HeaderWebhookTimestamp = "webhook-timestamp"
	HeaderWebhookSignature = "webhook-signature"
)

// DefaultWebhookTolerance is the default replay window — matches the
// official Composio SDKs (300 s, see Composio webhook docs).
const DefaultWebhookTolerance = 300 * time.Second

// Sentinel errors returned by [VerifyWebhook] so callers can distinguish
// the failure mode with errors.Is.
var (
	ErrMissingWebhookHeaders   = errors.New("composio: missing webhook headers")
	ErrInvalidWebhookSignature = errors.New("composio: invalid webhook signature")
	ErrWebhookTimestampStale   = errors.New("composio: webhook timestamp outside tolerance")
	ErrWebhookSecretMissing    = errors.New("composio: webhook secret is empty")
)

// WebhookHeaders carries the three headers that participate in the signature
// computation. Pass these straight from the inbound HTTP request.
type WebhookHeaders struct {
	ID        string
	Timestamp string
	Signature string
}

// HeadersFromHTTP pulls the three webhook headers off an http.Header.
// It is case-insensitive (http.Header normalizes its keys).
func HeadersFromHTTP(h http.Header) WebhookHeaders {
	return WebhookHeaders{
		ID:        h.Get(HeaderWebhookID),
		Timestamp: h.Get(HeaderWebhookTimestamp),
		Signature: h.Get(HeaderWebhookSignature),
	}
}

// VerifyOptions tweaks [VerifyWebhook]. Zero values mean defaults.
type VerifyOptions struct {
	// Tolerance is how far the webhook-timestamp may drift from `now`.
	// Zero means [DefaultWebhookTolerance]; a negative value disables the
	// check entirely (useful only for replaying historical deliveries in
	// tests).
	Tolerance time.Duration

	// Now overrides the wall clock used for the tolerance check.
	// Tests use this; production should leave it nil.
	Now func() time.Time
}

// VerifyWebhook checks the HMAC-SHA256 signature attached by Composio to
// every webhook delivery and enforces a replay-window tolerance.
//
// The signing string is constructed as
//
//	"<webhook-id>.<webhook-timestamp>.<rawBody>"
//
// and HMAC-SHA256'd with secret. The result is base64 encoded.
//
// Composio's `webhook-signature` header is a comma-separated list of
// `<version>,<signature>` pairs (e.g. `v1,abc123…`); this function accepts
// any of them whose version starts with "v" so future-proofs work.
//
// secret must be the value from the matching webhook subscription —
// fetch via the Composio dashboard or the
// `GET /webhook_subscriptions/{id}` endpoint.
func VerifyWebhook(secret string, headers WebhookHeaders, rawBody []byte, opts VerifyOptions) error {
	if secret == "" {
		return ErrWebhookSecretMissing
	}
	if headers.ID == "" || headers.Timestamp == "" || headers.Signature == "" {
		return ErrMissingWebhookHeaders
	}

	tolerance := opts.Tolerance
	if tolerance == 0 {
		tolerance = DefaultWebhookTolerance
	}
	if tolerance > 0 {
		ts, err := strconv.ParseInt(headers.Timestamp, 10, 64)
		if err != nil {
			// Composio's docs show timestamps as Unix seconds, but allow a
			// fallback in case future deliveries use RFC3339.
			t, terr := time.Parse(time.RFC3339, headers.Timestamp)
			if terr != nil {
				return fmt.Errorf("composio: invalid webhook-timestamp %q: %w", headers.Timestamp, err)
			}
			ts = t.Unix()
		}
		now := time.Now().UTC()
		if opts.Now != nil {
			now = opts.Now().UTC()
		}
		delta := now.Sub(time.Unix(ts, 0))
		if delta < 0 {
			delta = -delta
		}
		if delta > tolerance {
			return fmt.Errorf("%w: drift=%s tolerance=%s", ErrWebhookTimestampStale, delta, tolerance)
		}
	}

	signingString := headers.ID + "." + headers.Timestamp + "." + string(rawBody)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingString))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Composio's header takes the form "v1,<sig>[ v2,<sig> ...]" — accept
	// any version-tagged signature plus the bare-base64 form for forward-
	// compat.
	candidates := strings.Fields(strings.ReplaceAll(headers.Signature, ",", " "))
	if len(candidates) == 0 {
		return ErrInvalidWebhookSignature
	}
	want := []byte(expected)
	for _, cand := range candidates {
		// Skip version tags like "v1" / "v2".
		if len(cand) <= 3 && strings.HasPrefix(cand, "v") {
			continue
		}
		if hmac.Equal([]byte(cand), want) {
			return nil
		}
	}
	return ErrInvalidWebhookSignature
}

// VerifyHTTPRequest is a convenience wrapper that reads & verifies an
// inbound *http.Request in one call. It consumes the body and returns it
// to the caller so the handler can json-decode after a successful verify.
//
// On error the returned body slice is still populated (when read succeeded)
// so handlers can choose to log it.
func VerifyHTTPRequest(secret string, r *http.Request, opts VerifyOptions) ([]byte, error) {
	if r == nil || r.Body == nil {
		return nil, errors.New("composio: VerifyHTTPRequest: request body is nil")
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("composio: read webhook body: %w", err)
	}
	_ = r.Body.Close()
	if verr := VerifyWebhook(secret, HeadersFromHTTP(r.Header), body, opts); verr != nil {
		return body, verr
	}
	return body, nil
}

// --- Event envelope -----------------------------------------------------

// EventEnvelope is the V3 webhook payload as documented by Composio.
//
// Spec: https://docs.composio.dev/docs/setting-up-triggers/subscribing-to-events#webhook-payload-versions
//
// The `data` and `metadata` blocks vary per event; they stay as
// json.RawMessage so callers can decode into a strongly-typed struct
// matching whatever Type they care about.
type EventEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// ParseEvent decodes a V3 envelope. It does NOT verify the signature —
// always call [VerifyWebhook] / [VerifyHTTPRequest] first.
func ParseEvent(rawBody []byte) (*EventEnvelope, error) {
	var out EventEnvelope
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("composio: parse webhook envelope: %w", err)
	}
	return &out, nil
}
