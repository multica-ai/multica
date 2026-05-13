package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
)

// maxWebhookBodyBytes is the request body size cap for webhook ingress.
// 256 KiB is plenty for normal provider webhooks (a max-size GitHub PR
// payload comes in well under this) and small enough that an attacker
// cannot wedge agent context windows by sending megabytes of arbitrary JSON.
const maxWebhookBodyBytes = 256 * 1024

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

// ── Public ingress ──────────────────────────────────────────────────────────

// HandleAutopilotWebhook is the public entry point for webhook-triggered
// autopilots. It runs OUTSIDE the authenticated route group: the bearer
// token in the URL path IS the credential. Workspace context is derived
// from the trigger row's joined autopilot.workspace_id, never from request
// headers — that's why this handler does not call resolveWorkspaceID.
//
// Response shapes:
//   - 200 {"status":"accepted",  "run_id", "autopilot_id", "trigger_id"}
//   - 200 {"status":"skipped",   "run_id", "reason"}                — runtime offline at dispatch
//   - 200 {"status":"ignored",   "reason":"trigger_disabled"}      — disabled trigger
//   - 200 {"status":"ignored",   "reason":"autopilot_paused"}      — paused autopilot
//   - 200 {"status":"ignored",   "reason":"autopilot_archived"}    — archived autopilot
//   - 400 {"error":"..."}                                          — invalid JSON / scalar / empty
//   - 404 {"error":"webhook not found"}                            — unknown token
//   - 413 {"error":"payload too large"}                            — body exceeded cap
//   - 429 {"error":"rate limit exceeded"}                          — over per-token budget
//   - 500 {"error":"..."}                                          — dispatch failure
func (h *Handler) HandleAutopilotWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// 1. Per-IP rate limit BEFORE we hit Postgres. Bounds the DB-probe blast
	//    radius for an attacker spraying random tokens: each token gets a
	//    fresh per-token bucket, so without this gate a spray turns the
	//    handler into an unauthenticated index probe.
	if h.WebhookIPRateLimiter != nil {
		if ip := clientIPForRateLimit(r); ip != "" {
			if !h.WebhookIPRateLimiter.Allow(r.Context(), ip) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
	}

	// 2. Look up the trigger by token. Distinguish "no row" from "DB error":
	//    collapsing both to 404 means a transient DB blip silently drops real
	//    deliveries (providers like GitHub don't retry on 404). For the no-row
	//    case we still return a generic "webhook not found" so we don't leak
	//    which tokens existed at some point.
	trigRow, err := h.Queries.GetWebhookTriggerByToken(r.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("webhook: token lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Stash the resolved trigger ID on the request context so the request
	// logger can include it in the audit line without revealing the bearer
	// token in the URL path.
	r = middleware.SetWebhookTriggerID(r, uuidToString(trigRow.ID))

	// 3. Per-token rate limit.
	if h.WebhookRateLimiter != nil {
		if !h.WebhookRateLimiter.Allow(r.Context(), token) {
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
	}

	// 4. Disabled trigger → ignored. We deliberately return 200 so the
	//    sender's webhook-retry machinery doesn't keep hammering us; the
	//    "ignored" status makes the no-op visible if the operator inspects
	//    delivery logs.
	if !trigRow.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": "trigger_disabled"})
		return
	}

	// 5. Load Autopilot and cross-check workspace consistency.
	autopilot, err := h.Queries.GetAutopilot(r.Context(), trigRow.AutopilotID)
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}
	if uuidToString(autopilot.WorkspaceID) != uuidToString(trigRow.AutopilotWorkspaceID) {
		// This should be impossible — the join is by primary key — but
		// fail closed if it ever happens rather than dispatching against
		// the wrong workspace.
		slog.Warn("webhook: trigger workspace mismatch",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
		)
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	if autopilot.Status == "archived" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": "autopilot_archived"})
		return
	}
	if autopilot.Status != "active" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ignored", "reason": "autopilot_paused"})
		return
	}

	// 6. Body size cap + JSON validation. http.MaxBytesReader stops the
	//    read mid-stream once the cap is exceeded so an oversized payload
	//    is rejected before being fully buffered.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	envelope, err := normalizeWebhookPayload(body, r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode envelope")
		return
	}

	// 7. Dispatch. DispatchAutopilot already publishes the workspace-scoped
	//    EventAutopilotRunStart, persists the payload, runs the admission
	//    check (offline runtime → records a `skipped` run), and bumps
	//    last_run_at. We don't add a second WS publish.
	run, err := h.AutopilotService.DispatchAutopilot(
		r.Context(),
		autopilot,
		trigRow.ID,
		"webhook",
		envelopeBytes,
	)
	if err != nil {
		slog.Warn("webhook dispatch failed",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
			"error", err,
		)
		writeError(w, http.StatusInternalServerError, "failed to dispatch autopilot")
		return
	}

	// 8. Bump last_fired_at (separate from autopilot.last_run_at). Done
	//    after dispatch returns — including the skipped path — so paused
	//    early-return arms above don't corrupt the meaning of "last fired".
	if err := h.Queries.TouchAutopilotTriggerFiredAt(r.Context(), trigRow.ID); err != nil {
		slog.Warn("webhook: failed to touch last_fired_at",
			"trigger_id", uuidToString(trigRow.ID),
			"error", err,
		)
	}

	// 9. Response shape: skipped runs surface their reason
	//    so providers can log it without parsing free-form text.
	if run.Status == "skipped" {
		reason := ""
		if run.FailureReason.Valid {
			reason = run.FailureReason.String
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "skipped",
			"run_id": uuidToString(run.ID),
			"reason": reason,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "accepted",
		"run_id":       uuidToString(run.ID),
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(trigRow.ID),
	})
}

// clientIPForRateLimit returns the IP used as a rate-limit bucket key.
// Order: X-Forwarded-For's first entry (the original client when the
// reverse proxy appends downstream hops), then X-Real-IP, then
// RemoteAddr's host portion. Returns "" if nothing usable is found so
// the caller can decide to fail-open rather than rate-limit "".
//
// We trust X-Forwarded-For here because the public webhook ingress is
// expected to sit behind a known reverse proxy (Cloudflare, ALB, Caddy).
// If an attacker controls XFF directly, they can already pick any IP —
// but the IP limiter is a safety net, not a security boundary; the
// per-token limiter and token-secrecy are.
func clientIPForRateLimit(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	remote := r.RemoteAddr
	if i := strings.LastIndexByte(remote, ':'); i >= 0 {
		return remote[:i]
	}
	return remote
}
