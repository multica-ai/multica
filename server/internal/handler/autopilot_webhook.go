package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// webhookTokenPrefix makes a leaked token recognisable in logs / audit trails
// without revealing the entropy bytes themselves. 32 random bytes encoded as
// URL-safe base64 (no padding) is 43 chars, so a full token is "awt_" + 43 = 47
// chars. URL-safe base64 keeps the token URL-friendly without escaping.
const webhookTokenPrefix = "awt_"

// generateWebhookToken returns a cryptographically random bearer token used as
// the public webhook URL secret. Format: "awt_" + URL-safe base64(32 bytes,
// no padding). UUIDs are intentionally not used here — they are lower entropy
// (122 bits vs 256) and visually overlap with internal IDs, which made
// accidental token-vs-ID confusion easy in early prototypes.
func generateWebhookToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return webhookTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// ── Payload normalization ───────────────────────────────────────────────────

// WebhookEnvelope is the canonical shape stored in autopilot_run.trigger_payload
// and surfaced to the agent. The handler normalises arbitrary JSON bodies into
// this shape so downstream consumers (run_only daemon prompt, create_issue
// description appendix) can rely on a stable schema regardless of which
// provider sent the webhook.
type WebhookEnvelope struct {
	Event        string          `json:"event"`
	EventPayload json.RawMessage `json:"eventPayload"`
	Request      WebhookRequest  `json:"request"`
}

type WebhookRequest struct {
	ReceivedAt  string `json:"receivedAt"`
	ContentType string `json:"contentType,omitempty"`
}

// normalizeWebhookPayload parses an incoming webhook body and returns a
// WebhookEnvelope. Rules:
//
//  1. Body must be a valid JSON object or array. Scalars / invalid JSON
//     return an error so the handler can respond 400.
//  2. If the body is an object containing a string `event` and any
//     `eventPayload`, those are preserved as-is.
//  3. Otherwise `event` is inferred from headers/body fields, and the entire
//     original body becomes `eventPayload`.
//  4. The default event is `webhook.received`.
//
// Inference order:
//
//	X-GitHub-Event (combined with body.action when present),
//	X-Gitlab-Event, X-Event-Type, body.event, body.type, body.action.
func normalizeWebhookPayload(body []byte, headers http.Header) (WebhookEnvelope, error) {
	body = stripBOM(body)
	if len(body) == 0 {
		return WebhookEnvelope{}, errors.New("empty body")
	}

	// First, validate JSON shape (object or array). Reject scalars early —
	// `"hello"` is technically valid JSON but has no useful interpretation
	// as a webhook payload and would land in the agent prompt as a bare
	// string.
	var asAny any
	if err := json.Unmarshal(body, &asAny); err != nil {
		return WebhookEnvelope{}, fmt.Errorf("invalid json: %w", err)
	}
	switch asAny.(type) {
	case map[string]any, []any:
		// ok
	default:
		return WebhookEnvelope{}, errors.New("body must be a JSON object or array")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	contentType := headers.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}

	env := WebhookEnvelope{
		Request: WebhookRequest{
			ReceivedAt:  now,
			ContentType: contentType,
		},
	}

	// 1. Caller-provided envelope.
	if obj, ok := asAny.(map[string]any); ok {
		if eventStr, ok := obj["event"].(string); ok && eventStr != "" {
			if rawPayload, ok := obj["eventPayload"]; ok {
				inner, err := json.Marshal(rawPayload)
				if err == nil {
					env.Event = eventStr
					env.EventPayload = inner
					return env, nil
				}
			}
			// `event` present but no eventPayload: still preserve event
			// string, fall through to use whole body as payload.
			env.Event = eventStr
			env.EventPayload = json.RawMessage(body)
			return env, nil
		}
	}

	// 2. Inferred event.
	event := inferEvent(headers, asAny)
	env.Event = event
	env.EventPayload = json.RawMessage(body)
	return env, nil
}

// inferEvent returns a best-effort event identifier from headers and body.
// The order matches the documented inference rules in PLAN.md.
func inferEvent(headers http.Header, body any) string {
	if gh := headers.Get("X-GitHub-Event"); gh != "" {
		if obj, ok := body.(map[string]any); ok {
			if action, ok := obj["action"].(string); ok && action != "" {
				return "github." + gh + "." + action
			}
		}
		return "github." + gh
	}
	if gl := headers.Get("X-Gitlab-Event"); gl != "" {
		return "gitlab." + gl
	}
	if xe := headers.Get("X-Event-Type"); xe != "" {
		return xe
	}
	if obj, ok := body.(map[string]any); ok {
		if e, ok := obj["event"].(string); ok && e != "" {
			return e
		}
		if t, ok := obj["type"].(string); ok && t != "" {
			return t
		}
		if a, ok := obj["action"].(string); ok && a != "" {
			return a
		}
	}
	return "webhook.received"
}

// stripBOM removes a leading UTF-8 byte-order-mark, which some clients
// (notably PowerShell-based scripts) prepend to JSON bodies.
func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
