package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

// signature_status values mirror the CHECK constraint on webhook_delivery.
const (
	sigStatusNotRequired = "not_required"
	sigStatusValid       = "valid"
	sigStatusInvalid     = "invalid"
	sigStatusMissing     = "missing"
)

// delivery status values mirror the CHECK constraint on webhook_delivery.
//
// "Duplicate" is a *response* status, not a delivery status — duplicates
// don't get their own row; they bump attempt_count on the existing dedupe
// target. Likewise "skipped" is a *response* status reported when the
// autopilot service skipped the run (e.g. runtime offline); the delivery
// row itself records `dispatched` and links the skipped run via
// autopilot_run_id, because from the ingress's perspective we DID hand
// the payload to the autopilot machinery.
const (
	deliveryStatusQueued     = "queued"
	deliveryStatusDispatched = "dispatched"
	deliveryStatusRejected   = "rejected"
	deliveryStatusIgnored    = "ignored"
	deliveryStatusFailed     = "failed"
)

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

// ── Dedupe + signature helpers ──────────────────────────────────────────────

// extractDedupeKey returns the provider-specific idempotency identifier from
// request headers, plus a short tag naming the header it came from. Returns
// ("", "") when no recognised header is present.
//
//	github  -> X-GitHub-Delivery
//	generic -> Idempotency-Key
//
// Other providers fall back to the generic header to keep manual replays from
// Postman / curl behaving the same way regardless of trigger config.
func extractDedupeKey(provider string, headers http.Header) (string, string) {
	if v := strings.TrimSpace(headers.Get("X-GitHub-Delivery")); v != "" && provider == "github" {
		return v, "x-github-delivery"
	}
	if v := strings.TrimSpace(headers.Get("Idempotency-Key")); v != "" {
		return v, "idempotency-key"
	}
	if v := strings.TrimSpace(headers.Get("X-GitHub-Delivery")); v != "" {
		return v, "x-github-delivery"
	}
	return "", ""
}

// verifyWebhookSignatureForProvider returns one of sigStatus* describing the
// outcome of HMAC verification for the configured trigger.
//
// When no signing secret is configured the result is `not_required` — the
// trigger has opted into bearer-token-only authentication. When a secret IS
// configured the request must carry the expected header; otherwise the
// outcome is `missing` (caller still records a rejected delivery).
//
//	github  -> X-Hub-Signature-256: sha256=<hex>
//	generic -> X-Hub-Signature-256 (same shape; lets curl/Postman opt in)
func verifyWebhookSignatureForProvider(provider, secret string, headers http.Header, rawBody []byte) string {
	if secret == "" {
		return sigStatusNotRequired
	}
	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return sigStatusMissing
	}
	if !verifyHubSignature(secret, sig, rawBody) {
		return sigStatusInvalid
	}
	_ = provider
	return sigStatusValid
}

// verifyHubSignature implements the GitHub-compatible HMAC-SHA256 scheme:
// `X-Hub-Signature-256: sha256=<hex(hmac(body, secret))>`. The hmac.Equal
// comparison is constant-time so partial-prefix attacks cannot leak timing.
func verifyHubSignature(secret, header string, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// selectedHeadersJSON returns the small, debugging-friendly subset of request
// headers we persist on a delivery row. Signature header is recorded as
// present/absent only — never the value, so a delivery dump cannot leak the
// HMAC of a sensitive body.
func selectedHeadersJSON(headers http.Header) []byte {
	out := map[string]any{}
	add := func(name string) {
		if v := headers.Get(name); v != "" {
			out[strings.ToLower(name)] = v
		}
	}
	add("User-Agent")
	add("X-GitHub-Event")
	add("X-GitHub-Delivery")
	add("X-Gitlab-Event")
	add("X-Event-Type")
	add("Idempotency-Key")
	if v := headers.Get("X-Hub-Signature-256"); v != "" {
		out["x-hub-signature-256-present"] = true
	}
	b, err := json.Marshal(out)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// ── Public ingress ──────────────────────────────────────────────────────────

// HandleAutopilotWebhook is the public entry point for webhook-triggered
// autopilots. It runs OUTSIDE the authenticated route group: the bearer
// token in the URL path IS the credential.
//
// Flow (persist-first, synchronous admission + durable async dispatch):
//
//  1. High absolute per-IP ceiling plus a non-consuming check for prior
//     bad-credential debt (both before DB I/O).
//  2. Token lookup. ErrNoRows → 404; other DB errors → 500.
//  3. Read raw body (capped). Oversized → 413.
//  4. Normalize JSON envelope. Invalid → 400 (no persistence — there is no
//     dedupe identifier we can trust from an unparsable body).
//  5. Extract dedupe key from headers per provider.
//  6. Verify signature (or `not_required` when no secret is configured).
//  7. INSERT webhook_delivery row (status=queued). On dedupe collision (23505
//     against `(trigger_id, dedupe_key)`) treat as duplicate: bump
//     attempt_count on the existing row and return its delivery_id +
//     autopilot_run_id with 200.
//  8. If signature invalid/missing: charge bad-credential debt, UPDATE
//     delivery → rejected, return 401.
//  9. If trigger disabled / autopilot paused / archived: UPDATE delivery →
//     ignored, return 200.
//  10. Create or reuse the delivery's idempotent run, persist the compatible
//     200 accepted/skipped acknowledgement, and wake the database-leased
//     worker. The worker re-checks state/filter scope, dispatches idempotently,
//     and bumps last_fired_at.
//
// Response shapes:
//   - 200 {"status":"accepted",  "delivery_id", "run_id", "autopilot_id", "trigger_id"}
//   - 200 {"status":"skipped",   "delivery_id", "run_id", "reason"}
//   - 200 {"status":"ignored",   "delivery_id", "reason"}
//   - 200 {"status":"duplicate", "delivery_id", "run_id?"}
//   - 400 {"error":"..."}                                          — invalid JSON / scalar / empty
//   - 401 {"status":"rejected",  "delivery_id", "reason":"..."}    — signature failure
//   - 404 {"error":"webhook not found"}                            — unknown token
//   - 413 {"error":"payload too large"}                            — body exceeded cap
//   - 429 {"error":"rate limit exceeded"}                          — IP safety/debt gate
//   - 500 {"error":"..."}                                          — internal failure
func (h *Handler) HandleAutopilotWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// 1. Always retain a high absolute ceiling. The lower shared-IP limiter
	//    tracks only requests already classified as bad credentials, so a NAT
	//    full of legitimate GitHub hooks does not consume its budget.
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), ip) {
		writeWebhookRateLimit(w, r, h.WebhookAbsoluteIPRateLimiter, ip, "absolute_ip", h.Metrics)
		return
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !webhookLimiterCheck(r.Context(), h.WebhookIPRateLimiter, ip) {
		writeWebhookRateLimit(w, r, h.WebhookIPRateLimiter, ip, "bad_credential_ip", h.Metrics)
		return
	}

	// 2. Token lookup. Distinguish "no row" from "DB error": collapsing both
	//    to 404 means a transient DB blip silently drops real deliveries
	//    (providers like GitHub don't retry on 404). For no-row we still
	//    return a generic message so we don't leak which tokens existed.
	trigRow, err := h.Queries.GetWebhookTriggerByToken(r.Context(), pgtype.Text{String: token, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if ip != "" && h.WebhookIPRateLimiter != nil {
				h.WebhookIPRateLimiter.Allow(r.Context(), ip)
			}
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("webhook: token lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	middleware.SetWebhookTriggerID(r, uuidToString(trigRow.ID))

	// 3. Body size cap + JSON validation. http.MaxBytesReader stops the read
	//    mid-stream once the cap is exceeded so an oversized payload is
	//    rejected before being fully buffered.
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

	// 4. Cross-check autopilot/workspace consistency BEFORE we persist the
	//    delivery — webhook_delivery.workspace_id is NOT NULL and a stale FK
	//    row would otherwise fail INSERT after we've already paid the body
	//    read. Same ErrNoRows-vs-DB-error split as token lookup.
	autopilot, err := h.Queries.GetAutopilot(r.Context(), trigRow.AutopilotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "webhook not found")
			return
		}
		slog.Error("webhook: autopilot lookup failed",
			"error", err,
			"trigger_id", uuidToString(trigRow.ID),
		)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if uuidToString(autopilot.WorkspaceID) != uuidToString(trigRow.AutopilotWorkspaceID) {
		slog.Warn("webhook: trigger workspace mismatch",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
		)
		writeError(w, http.StatusNotFound, "webhook not found")
		return
	}

	// 5. Normalize body. Invalid JSON → 400 without persistence: we have no
	//    dedupe identifier from the body, and replaying an unparsable payload
	//    is not useful.
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
	// 6. Provider + dedupe + signature.
	provider := trigRow.Provider
	if provider == "" {
		provider = "generic"
	}
	dedupeKey, dedupeSource := extractDedupeKey(provider, r.Header)
	sigStatus := verifyWebhookSignatureForProvider(provider, trigRow.SigningSecret.String, r.Header, body)

	// 7. Persist (INSERT delivery). Dedupe collision → bump existing row.
	delivery, dup, err := h.persistInboundDelivery(r, persistDeliveryInput{
		WorkspaceID:     autopilot.WorkspaceID,
		AutopilotID:     autopilot.ID,
		TriggerID:       trigRow.ID,
		Provider:        provider,
		Event:           envelope.Event,
		DedupeKey:       dedupeKey,
		DedupeSource:    dedupeSource,
		SignatureStatus: sigStatus,
		ContentType:     envelope.Request.ContentType,
		RawBody:         body,
		SelectedHeaders: selectedHeadersJSON(r.Header),
	})
	if err != nil {
		slog.Error("webhook: persist delivery failed",
			"error", err,
			"trigger_id", uuidToString(trigRow.ID),
		)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if dup {
		// A previous delivery already covered this dedupe key. Return the
		// original delivery_id + admitted run_id with 200 so the caller can
		// correlate. The delivery row is not linked until worker completion,
		// so retrying before then must fall back to the run's durable delivery
		// anchor.
		resp := map[string]any{
			"status":      "duplicate",
			"delivery_id": uuidToString(delivery.ID),
		}
		runID := delivery.AutopilotRunID
		if !runID.Valid {
			run, runErr := h.Queries.GetAutopilotRunByWebhookDelivery(r.Context(), delivery.ID)
			switch {
			case runErr == nil:
				runID = run.ID
			case !errors.Is(runErr, pgx.ErrNoRows):
				slog.Error("webhook: resolve duplicate run failed",
					"delivery_id", uuidToString(delivery.ID),
					"error", runErr,
				)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if runID.Valid {
			resp["run_id"] = uuidToString(runID)
		}
		if delivery.Status == deliveryStatusQueued && h.WebhookDeliveryWorker != nil {
			h.WebhookDeliveryWorker.Notify()
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// 8. Signature failure → rejected delivery + 401. No dispatch, no replay.
	//    Providers will look for 4xx feedback when their secret is wrong.
	if sigStatus == sigStatusInvalid || sigStatus == sigStatusMissing {
		reason := "invalid_signature"
		if sigStatus == sigStatusMissing {
			reason = "missing_signature"
		}
		respBody := map[string]any{
			"status":      "rejected",
			"delivery_id": uuidToString(delivery.ID),
			"reason":      reason,
		}
		if ip != "" && h.WebhookIPRateLimiter != nil {
			h.WebhookIPRateLimiter.Allow(r.Context(), ip)
		}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusRejected, http.StatusUnauthorized, respBody, reason)
		writeJSON(w, http.StatusUnauthorized, respBody)
		return
	}

	// 9. Trigger disabled / autopilot paused / archived → ignored. We return
	//     200 so the sender's webhook-retry machinery doesn't keep hammering
	//     us; the "ignored" status + delivery row makes the no-op visible if
	//     the operator inspects the delivery log.
	if !trigRow.Enabled {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "trigger_disabled"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "trigger_disabled")
		writeJSON(w, http.StatusOK, respBody)
		return
	}
	if autopilot.Status == "archived" {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "autopilot_archived"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "autopilot_archived")
		writeJSON(w, http.StatusOK, respBody)
		return
	}
	if autopilot.Status != "active" {
		respBody := map[string]any{"status": "ignored", "delivery_id": uuidToString(delivery.ID), "reason": "autopilot_paused"}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "autopilot_paused")
		writeJSON(w, http.StatusOK, respBody)
		return
	}

	// 10. Event filter scope → ignored. If the trigger declares a concrete
	//     event_filters list and the incoming event is outside that scope,
	//     record an ignored delivery without creating an expensive run/task.
	if !webhookEventAllowedByTriggerScope(trigRow.EventFilters, envelope) {
		respBody := map[string]any{
			"status":      "ignored",
			"delivery_id": uuidToString(delivery.ID),
			"reason":      "event_filtered",
			"event":       envelope.Event,
		}
		h.finaliseDeliveryTerminal(r, delivery.ID, deliveryStatusIgnored, http.StatusOK, respBody, "event_filtered")
		writeJSON(w, http.StatusOK, respBody)
		return
	}

	// 11. Allocate the idempotent run synchronously so existing webhook clients
	//     keep the v0.4.0 response contract. The queued delivery remains the
	//     durable dispatch source: the worker resumes this run after the response
	//     and can recover it after a process crash.
	run, err := h.AutopilotService.AdmitAutopilotWebhookDelivery(
		r.Context(),
		autopilot,
		trigRow.ID,
		envelopeBytes,
		delivery.ID,
	)
	if err != nil {
		slog.Warn("webhook admission failed",
			"trigger_id", uuidToString(trigRow.ID),
			"autopilot_id", uuidToString(autopilot.ID),
			"delivery_id", uuidToString(delivery.ID),
			"error", err,
		)
		if h.WebhookDeliveryWorker != nil {
			h.WebhookDeliveryWorker.Notify()
		}
		writeError(w, http.StatusInternalServerError, "failed to admit autopilot")
		return
	}

	respBody := map[string]any{
		"status":       "accepted",
		"delivery_id":  uuidToString(delivery.ID),
		"run_id":       uuidToString(run.ID),
		"autopilot_id": uuidToString(autopilot.ID),
		"trigger_id":   uuidToString(trigRow.ID),
	}
	if run.Status == "skipped" {
		respBody = map[string]any{
			"status":      "skipped",
			"delivery_id": uuidToString(delivery.ID),
			"run_id":      uuidToString(run.ID),
		}
		if run.FailureReason.Valid {
			respBody["reason"] = run.FailureReason.String
		}
	}
	bodyJSON, _ := json.Marshal(respBody)
	if _, err := h.Queries.AcknowledgeWebhookDelivery(r.Context(), db.AcknowledgeWebhookDeliveryParams{
		ID:             delivery.ID,
		ResponseStatus: pgtype.Int4{Int32: http.StatusOK, Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}); err != nil {
		slog.Warn("webhook: persist acknowledgement metadata failed",
			"delivery_id", uuidToString(delivery.ID),
			"error", err,
		)
	}
	if h.WebhookDeliveryWorker != nil {
		h.WebhookDeliveryWorker.Notify()
	}
	writeJSON(w, http.StatusOK, respBody)
}

// ── Event filter helpers ────────────────────────────────────────────────────

// WebhookEventFilter declares one event and an optional list of actions.
// A nil/empty Actions means "any action" for this event.
type WebhookEventFilter struct {
	Event   string   `json:"event"`
	Actions []string `json:"actions,omitempty"`
}

// validateWebhookEventFilters enforces the contract at the HTTP boundary so
// that malformed shapes never reach the database. The matcher (read path)
// trusts whatever is stored — see webhookEventAllowedByTriggerScope.
func validateWebhookEventFilters(filters []WebhookEventFilter) error {
	for i, f := range filters {
		if strings.TrimSpace(f.Event) == "" {
			return fmt.Errorf("event_filters[%d].event must not be empty", i)
		}
		for j, a := range f.Actions {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("event_filters[%d].actions[%d] must not be empty", i, j)
			}
		}
	}
	return nil
}

// encodeWebhookEventFilters returns the JSONB bytes to persist for a CREATE.
// nil/empty input maps to nil bytes (column stays NULL → matcher allows
// every event), so we never write an explicit `[]` on create.
func encodeWebhookEventFilters(filters []WebhookEventFilter) ([]byte, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	return json.Marshal(filters)
}

// encodeWebhookEventFiltersAlways always returns non-nil bytes, even for an
// empty slice (`[]byte("[]")`). The UPDATE handler uses this so an explicit
// empty array in the PATCH body can overwrite (via COALESCE) the existing
// row to a cleared state — passing nil would be indistinguishable from
// "field omitted, leave alone".
func encodeWebhookEventFiltersAlways(filters []WebhookEventFilter) ([]byte, error) {
	if filters == nil {
		filters = []WebhookEventFilter{}
	}
	return json.Marshal(filters)
}

// webhookEventAllowedByTriggerScope returns true when the trigger has no
// filters (NULL / empty) or when the incoming envelope matches at least one
// declared filter.
func webhookEventAllowedByTriggerScope(eventFilters []byte, envelope WebhookEnvelope) bool {
	if len(eventFilters) == 0 {
		return true
	}
	var filters []WebhookEventFilter
	if err := json.Unmarshal(eventFilters, &filters); err != nil {
		// Strict write-time validation should prevent malformed bytes
		// from ever reaching this branch. If a corrupt row somehow
		// exists, fail closed — silently widening the allowlist on a
		// "only allow X" policy is worse than dropping events until an
		// operator notices.
		slog.Warn("webhook: malformed event_filters, denying", "error", err)
		return false
	}
	if len(filters) == 0 {
		return true
	}
	_, eventName, eventAction := splitWebhookEvent(envelope.Event)
	actionCandidates := webhookActionCandidates(eventAction, envelope.EventPayload)
	for _, f := range filters {
		if f.Event != eventName {
			continue
		}
		if len(f.Actions) == 0 {
			return true
		}
		for _, action := range actionCandidates {
			for _, allowed := range f.Actions {
				if action == allowed {
					return true
				}
			}
		}
		// Intentionally do NOT return false here: the UI allows several
		// filters that share the same event name (e.g. two workflow_run
		// rows covering disjoint actions). Earlier code short-circuited
		// on the first event-name hit, which made one row silently shadow
		// the others depending on iteration order — see PR #3231 review.
		// Keep scanning so any later filter still gets its chance.
	}
	return false
}

// splitWebhookEvent splits a normalized event like "github.workflow_run.completed"
// into (provider, eventName, action). For unqualified events it returns ("", event, "").
func splitWebhookEvent(event string) (provider, name, action string) {
	parts := strings.Split(event, ".")
	if isKnownProvider(parts[0]) {
		if len(parts) >= 3 {
			return parts[0], parts[1], strings.Join(parts[2:], ".")
		}
		if len(parts) == 2 {
			return parts[0], parts[1], ""
		}
		return parts[0], "", ""
	}
	if len(parts) >= 2 {
		return "", parts[0], strings.Join(parts[1:], ".")
	}
	return "", event, ""
}

func isKnownProvider(prefix string) bool {
	switch prefix {
	case "github", "gitlab", "bitbucket", "gitea":
		return true
	}
	return false
}

// webhookActionCandidates extracts possible action values from the event
// action suffix and from well-known payload fields.
func webhookActionCandidates(eventAction string, payload json.RawMessage) []string {
	seen := map[string]struct{}{}
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		seen[v] = struct{}{}
	}
	add(eventAction)
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err == nil {
		for _, key := range []string{"action", "state", "conclusion", "status"} {
			if v, ok := obj[key].(string); ok {
				add(v)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	return out
}

// ── Persistence helpers ─────────────────────────────────────────────────────

type persistDeliveryInput struct {
	WorkspaceID     pgtype.UUID
	AutopilotID     pgtype.UUID
	TriggerID       pgtype.UUID
	Provider        string
	Event           string
	DedupeKey       string
	DedupeSource    string
	SignatureStatus string
	ContentType     string
	RawBody         []byte
	SelectedHeaders []byte
}

// persistInboundDelivery INSERTs a fresh `queued` delivery, returning (row,
// false, nil) on the happy path. On dedupe-key unique-violation it returns
// (existing-row, true, nil) after bumping attempt_count on the prior row.
// Any other error bubbles up so the handler can 500 cleanly.
func (h *Handler) persistInboundDelivery(r *http.Request, in persistDeliveryInput) (db.WebhookDelivery, bool, error) {
	params := db.CreateWebhookDeliveryParams{
		WorkspaceID:     in.WorkspaceID,
		AutopilotID:     in.AutopilotID,
		TriggerID:       in.TriggerID,
		Provider:        in.Provider,
		Event:           in.Event,
		SignatureStatus: in.SignatureStatus,
		Status:          deliveryStatusQueued,
		SelectedHeaders: in.SelectedHeaders,
		RawBody:         in.RawBody,
	}
	if in.DedupeKey != "" {
		params.DedupeKey = pgtype.Text{String: in.DedupeKey, Valid: true}
		params.DedupeSource = pgtype.Text{String: in.DedupeSource, Valid: true}
	}
	if in.ContentType != "" {
		params.ContentType = pgtype.Text{String: in.ContentType, Valid: true}
	}

	delivery, err := h.Queries.CreateWebhookDelivery(r.Context(), params)
	if err == nil {
		return delivery, false, nil
	}
	if !isUniqueViolation(err) || in.DedupeKey == "" {
		return db.WebhookDelivery{}, false, err
	}
	// Dedupe collision: fetch the original row, bump attempt count.
	existing, lookupErr := h.Queries.GetWebhookDeliveryByTriggerAndDedupe(r.Context(), db.GetWebhookDeliveryByTriggerAndDedupeParams{
		TriggerID: in.TriggerID,
		DedupeKey: pgtype.Text{String: in.DedupeKey, Valid: true},
	})
	if lookupErr != nil {
		return db.WebhookDelivery{}, false, fmt.Errorf("lookup duplicate delivery: %w", lookupErr)
	}
	bumped, bumpErr := h.Queries.BumpWebhookDeliveryAttempt(r.Context(), existing.ID)
	if bumpErr != nil {
		// Still treat as duplicate; just log the bump failure so the
		// operator can investigate, returning the row we DID read.
		slog.Warn("webhook: failed to bump attempt_count",
			"delivery_id", uuidToString(existing.ID),
			"error", bumpErr,
		)
		return existing, true, nil
	}
	return bumped, true, nil
}

// finaliseDeliveryTerminal records a non-dispatched outcome (rejected,
// ignored, failed). HTTP status and full response body are captured so a
// future Deliveries UI can show exactly what we returned.
func (h *Handler) finaliseDeliveryTerminal(
	r *http.Request,
	id pgtype.UUID,
	status string,
	httpStatus int,
	responseBody any,
	errMsg string,
) {
	bodyJSON, _ := json.Marshal(responseBody)
	params := db.UpdateWebhookDeliveryTerminalParams{
		ID:             id,
		Status:         status,
		ResponseStatus: pgtype.Int4{Int32: int32(httpStatus), Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}
	if errMsg != "" {
		params.Error = pgtype.Text{String: errMsg, Valid: true}
	}
	if _, err := h.Queries.UpdateWebhookDeliveryTerminal(r.Context(), params); err != nil {
		slog.Warn("webhook: finalise terminal failed",
			"delivery_id", uuidToString(id),
			"status", status,
			"error", err,
		)
	}
	h.Metrics.RecordWebhookDelivery(h.deliveryProvider(r.Context(), id), status)
}

// finaliseDeliveryWithRun records a delivery that produced (or was admission-
// skipped to) an autopilot_run. Same response-capture as the terminal path.
func (h *Handler) finaliseDeliveryWithRun(
	r *http.Request,
	id pgtype.UUID,
	status string,
	runID pgtype.UUID,
	httpStatus int,
	responseBody any,
) {
	bodyJSON, _ := json.Marshal(responseBody)
	params := db.UpdateWebhookDeliveryDispatchedParams{
		ID:             id,
		Status:         status,
		AutopilotRunID: runID,
		ResponseStatus: pgtype.Int4{Int32: int32(httpStatus), Valid: true},
		ResponseBody:   pgtype.Text{String: string(bodyJSON), Valid: true},
	}
	if _, err := h.Queries.UpdateWebhookDeliveryDispatched(r.Context(), params); err != nil {
		slog.Warn("webhook: finalise with run failed",
			"delivery_id", uuidToString(id),
			"run_id", uuidToString(runID),
			"error", err,
		)
	}
	h.Metrics.RecordWebhookDelivery(h.deliveryProvider(r.Context(), id), status)
}

// deliveryProvider best-effort reads the provider for a delivery id so the
// webhook delivery metric carries useful provenance. On lookup failure we
// fall back to "generic" — the metric must always be incremented exactly
// once per finalise call so the dashboard counts line up with autopilot_run
// volume.
func (h *Handler) deliveryProvider(ctx context.Context, id pgtype.UUID) string {
	if h.Queries == nil {
		return "generic"
	}
	row, err := h.Queries.GetWebhookDelivery(ctx, id)
	if err != nil || row.Provider == "" {
		return "generic"
	}
	return row.Provider
}

// ── Rate-limit / IP plumbing ────────────────────────────────────────────────

func writeWebhookRateLimit(w http.ResponseWriter, r *http.Request, limiter WebhookRateLimiter, key, gate string, metrics *obsmetrics.BusinessMetrics) {
	retryAfter := time.Second
	if limiter != nil {
		if retry := webhookLimiterRetryAfter(r.Context(), limiter, key); retry > 0 {
			retryAfter = retry
		}
	}
	seconds := int64((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
	metrics.RecordWebhookRateLimited(gate)
	writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
}

// clientIPForRateLimit returns the IP used as a rate-limit bucket key.
//
// Default behaviour: use the host portion of r.RemoteAddr. Forwarded
// headers (X-Forwarded-For, X-Real-IP) are IGNORED unless the operator
// has explicitly opted in via MULTICA_TRUSTED_PROXIES — and even then
// only when r.RemoteAddr is itself inside one of the listed CIDRs.
func (h *Handler) clientIPForRateLimit(r *http.Request) string {
	remoteIP := remoteAddrHost(r.RemoteAddr)
	if len(h.cfg.TrustedProxies) == 0 {
		return remoteIP
	}
	remoteAddr, ok := parseNetIPAddr(remoteIP)
	if !ok || !addrInPrefixes(remoteAddr, h.cfg.TrustedProxies) {
		// Source isn't a trusted proxy — headers can't be believed.
		return remoteIP
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return remoteIP
}

func remoteAddrHost(remote string) string {
	if remote == "" {
		return ""
	}
	if strings.HasPrefix(remote, "[") {
		if end := strings.IndexByte(remote, ']'); end > 0 {
			return remote[1:end]
		}
	}
	if i := strings.LastIndexByte(remote, ':'); i >= 0 && !strings.Contains(remote, "]") {
		if strings.Count(remote, ":") == 1 {
			return remote[:i]
		}
	}
	return remote
}

func parseNetIPAddr(s string) (netip.Addr, bool) {
	if s == "" {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func addrInPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
