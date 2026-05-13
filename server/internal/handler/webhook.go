package handler

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Response types ──────────────────────────────────────────────────────────

// WebhookSubscriptionResponse is the safe-for-API shape. The raw `secret` is
// returned EXACTLY ONCE on create / rotate via WebhookSecretCarrierResponse.
// On every other read the field is omitted and `secret_redacted: true` is set.
type WebhookSubscriptionResponse struct {
	ID                       string   `json:"id"`
	WorkspaceID              string   `json:"workspace_id"`
	Name                     string   `json:"name"`
	URL                      string   `json:"url"`
	EventFilter              []string `json:"event_filter"`
	State                    string   `json:"state"`
	PauseThreshold           int32    `json:"pause_threshold"`
	ConsecutiveFailures      int32    `json:"consecutive_failures"`
	AllowHTTP                bool     `json:"allow_http"`
	PerAttemptTimeoutSeconds int32    `json:"per_attempt_timeout_seconds"`
	EventTaxonomyPinnedAt    *string  `json:"event_taxonomy_pinned_at"`
	SecretRedacted           bool     `json:"secret_redacted"`
	CreatedBy                string   `json:"created_by"`
	CreatedAt                string   `json:"created_at"`
	UpdatedAt                string   `json:"updated_at"`
}

// WebhookSecretCarrierResponse wraps the subscription response and includes
// the raw secret one time. Returned by create + rotate-secret only.
type WebhookSecretCarrierResponse struct {
	WebhookSubscriptionResponse
	Secret string `json:"secret"`
}

type WebhookDeliveryResponse struct {
	ID                        string  `json:"id"`
	SubscriptionID            string  `json:"subscription_id"`
	EventID                   string  `json:"event_id"`
	EventType                 string  `json:"event_type"`
	Payload                   any     `json:"payload"`
	Attempt                   int32   `json:"attempt"`
	Status                    string  `json:"status"`
	NextAttemptAt             *string `json:"next_attempt_at"`
	LastResponseStatus        *int32  `json:"last_response_status"`
	LastResponseBodyTruncated *string `json:"last_response_body_truncated,omitempty"`
	LastError                 *string `json:"last_error"`
	CreatedAt                 string  `json:"created_at"`
	CompletedAt               *string `json:"completed_at"`
}

func webhookSubscriptionToResponse(s db.WebhookSubscription) WebhookSubscriptionResponse {
	return WebhookSubscriptionResponse{
		ID:                       uuidToString(s.ID),
		WorkspaceID:              uuidToString(s.WorkspaceID),
		Name:                     s.Name,
		URL:                      s.Url,
		EventFilter:              s.EventFilter,
		State:                    s.State,
		PauseThreshold:           s.PauseThreshold,
		ConsecutiveFailures:      s.ConsecutiveFailures,
		AllowHTTP:                s.AllowHttp,
		PerAttemptTimeoutSeconds: s.PerAttemptTimeoutSeconds,
		EventTaxonomyPinnedAt:    timestampToPtr(s.EventTaxonomyPinnedAt),
		SecretRedacted:           true,
		CreatedBy:                uuidToString(s.CreatedBy),
		CreatedAt:                timestampToString(s.CreatedAt),
		UpdatedAt:                timestampToString(s.UpdatedAt),
	}
}

func webhookDeliveryToResponse(d db.WebhookDelivery, includeBody bool) WebhookDeliveryResponse {
	var payload any
	if d.Payload != nil {
		json.Unmarshal(d.Payload, &payload)
	}
	resp := WebhookDeliveryResponse{
		ID:             uuidToString(d.ID),
		SubscriptionID: uuidToString(d.SubscriptionID),
		EventID:        uuidToString(d.EventID),
		EventType:      d.EventType,
		Payload:        payload,
		Attempt:        d.Attempt,
		Status:         d.Status,
		NextAttemptAt:  timestampToPtr(d.NextAttemptAt),
		LastError:      textToPtr(d.LastError),
		CreatedAt:      timestampToString(d.CreatedAt),
		CompletedAt:    timestampToPtr(d.CompletedAt),
	}
	if d.LastResponseStatus.Valid {
		v := d.LastResponseStatus.Int32
		resp.LastResponseStatus = &v
	}
	if includeBody {
		resp.LastResponseBodyTruncated = textToPtr(d.LastResponseBodyTruncated)
	}
	return resp
}

// ── Request types ───────────────────────────────────────────────────────────

type CreateWebhookSubscriptionRequest struct {
	Name                     string   `json:"name"`
	URL                      string   `json:"url"`
	EventFilter              []string `json:"event_filter"`
	Secret                   *string  `json:"secret,omitempty"`
	PauseThreshold           *int32   `json:"pause_threshold,omitempty"`
	AllowHTTP                *bool    `json:"allow_http,omitempty"`
	PerAttemptTimeoutSeconds *int32   `json:"per_attempt_timeout_seconds,omitempty"`
}

type UpdateWebhookSubscriptionRequest struct {
	Name                     *string   `json:"name,omitempty"`
	URL                      *string   `json:"url,omitempty"`
	EventFilter              *[]string `json:"event_filter,omitempty"`
	State                    *string   `json:"state,omitempty"`
	PauseThreshold           *int32    `json:"pause_threshold,omitempty"`
	AllowHTTP                *bool     `json:"allow_http,omitempty"`
	PerAttemptTimeoutSeconds *int32    `json:"per_attempt_timeout_seconds,omitempty"`
}

// ── Validation helpers ──────────────────────────────────────────────────────

// validateWebhookEventFilter checks that every entry is either '*' or a
// known event-type string. Returns the offending entry on the first invalid
// match. Wildcard '*' is allowed but the caller is responsible for setting
// `event_taxonomy_pinned_at` when '*' is present.
func validateWebhookEventFilter(filter []string) (bad string, ok bool) {
	if len(filter) == 0 {
		return "", false
	}
	known := knownEventTypes()
	for _, e := range filter {
		if e == "*" {
			continue
		}
		if _, found := known[e]; !found {
			return e, false
		}
	}
	return "", true
}

// knownEventTypes lists every event-type constant the bus publishes today.
// New event types must be added here so subscribers can filter on them
// without making them up. The wildcard '*' opts in to all current AND
// future events (with `event_taxonomy_pinned_at` recording the moment).
func knownEventTypes() map[string]struct{} {
	// Mirrored from server/pkg/protocol/events.go — keep in sync.
	types := []string{
		"issue:created", "issue:updated", "issue:deleted",
		"comment:created", "comment:updated", "comment:deleted",
		"reaction:added", "reaction:removed",
		"issue_reaction:added", "issue_reaction:removed",
		"agent:status", "agent:created", "agent:archived", "agent:restored",
		"task:queued", "task:dispatch", "task:progress",
		"task:completed", "task:failed", "task:message", "task:cancelled",
		"inbox:new", "inbox:read", "inbox:archived",
		"workspace:updated", "workspace:deleted",
		"member:added", "member:updated", "member:removed",
		"subscriber:added", "subscriber:removed",
		"activity:created",
		"skill:created", "skill:updated", "skill:deleted",
		"chat:message", "chat:done", "chat:session_read", "chat:session_deleted",
		"project:created", "project:updated", "project:deleted",
		"project_resource:created", "project_resource:deleted",
		"label:created", "label:updated", "label:deleted",
		"issue_labels:changed",
		"pin:created", "pin:deleted", "pin:reordered",
		"invitation:created", "invitation:accepted",
		"invitation:declined", "invitation:revoked",
		"autopilot:created", "autopilot:updated", "autopilot:deleted",
		"autopilot:run_start", "autopilot:run_done",
		"webhook:auto_paused", "webhook:test",
	}
	m := make(map[string]struct{}, len(types))
	for _, t := range types {
		m[t] = struct{}{}
	}
	return m
}

// generateWebhookSecret returns a 32-byte URL-safe base64 string. Suitable
// for HMAC-SHA256. The caller is responsible for sending it back to the
// subscriber exactly once and never logging it.
func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *Handler) ListWebhookSubscriptions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var stateFilter pgtype.Text
	if s := r.URL.Query().Get("state"); s != "" {
		stateFilter = pgtype.Text{String: s, Valid: true}
	}

	subs, err := h.Queries.ListWebhookSubscriptions(r.Context(), db.ListWebhookSubscriptionsParams{
		WorkspaceID: parseUUID(workspaceID),
		State:       stateFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list webhook subscriptions")
		return
	}

	resp := make([]WebhookSubscriptionResponse, len(subs))
	for i, s := range subs {
		resp[i] = webhookSubscriptionToResponse(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"webhooks": resp, "total": len(resp)})
}

func (h *Handler) GetWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, webhookSubscriptionToResponse(sub))
}

func (h *Handler) loadWebhookSubscriptionInWorkspace(w http.ResponseWriter, r *http.Request, subID, workspaceID string) (db.WebhookSubscription, bool) {
	parsed, ok := parseUUIDOrBadRequest(w, subID, "id")
	if !ok {
		return db.WebhookSubscription{}, false
	}
	sub, err := h.Queries.GetWebhookSubscriptionInWorkspace(r.Context(), db.GetWebhookSubscriptionInWorkspaceParams{
		ID:          parsed,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "webhook not found")
		return db.WebhookSubscription{}, false
	}
	return sub, true
}

func (h *Handler) CreateWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	// Admin / owner only — webhook subscriptions are credential-issuing
	// surfaces (the secret travels back to the requester once on this
	// response). Mirror the autopilot admin-only model.
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req CreateWebhookSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if bad, ok := validateWebhookEventFilter(req.EventFilter); !ok {
		if bad == "" {
			writeError(w, http.StatusBadRequest, "event_filter must be a non-empty array")
		} else {
			writeError(w, http.StatusBadRequest, "event_filter contains unknown event type: "+bad)
		}
		return
	}

	// HTTPS-only by default. HTTP only if the request explicitly opts in
	// AND the workspace allows it (tracked on the subscription row, no
	// separate workspace flag for v1).
	allowHTTP := false
	if req.AllowHTTP != nil && *req.AllowHTTP {
		allowHTTP = true
	}
	if strings.HasPrefix(strings.ToLower(req.URL), "http://") && !allowHTTP {
		writeError(w, http.StatusBadRequest, "url must use https:// (set allow_http=true to permit http://; SSRF protections still apply)")
		return
	}
	if !strings.HasPrefix(strings.ToLower(req.URL), "https://") && !strings.HasPrefix(strings.ToLower(req.URL), "http://") {
		writeError(w, http.StatusBadRequest, "url must start with https:// or http://")
		return
	}

	// Generate a secret if the caller didn't provide one (recommended path).
	secret := ""
	if req.Secret != nil && strings.TrimSpace(*req.Secret) != "" {
		secret = strings.TrimSpace(*req.Secret)
	} else {
		generated, err := generateWebhookSecret()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to generate secret")
			return
		}
		secret = generated
	}

	// If '*' is in the filter, record the pin-time so audits can see what
	// taxonomy snapshot the subscriber opted into.
	var taxonomyPinnedAt pgtype.Timestamptz
	for _, e := range req.EventFilter {
		if e == "*" {
			taxonomyPinnedAt = pgtype.Timestamptz{Time: nowTime(), Valid: true}
			break
		}
	}

	params := db.CreateWebhookSubscriptionParams{
		WorkspaceID: parseUUID(workspaceID),
		Name:        req.Name,
		Url:         req.URL,
		Secret:      secret,
		EventFilter: req.EventFilter,
		CreatedBy:   member.ID,
	}
	if req.PauseThreshold != nil {
		params.PauseThreshold = pgtype.Int4{Int32: *req.PauseThreshold, Valid: true}
	}
	if req.AllowHTTP != nil {
		params.AllowHttp = pgtype.Bool{Bool: *req.AllowHTTP, Valid: true}
	}
	if req.PerAttemptTimeoutSeconds != nil {
		params.PerAttemptTimeoutSeconds = pgtype.Int4{Int32: *req.PerAttemptTimeoutSeconds, Valid: true}
	}
	if taxonomyPinnedAt.Valid {
		params.EventTaxonomyPinnedAt = taxonomyPinnedAt
	}

	sub, err := h.Queries.CreateWebhookSubscription(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create webhook subscription")
		return
	}

	resp := WebhookSecretCarrierResponse{
		WebhookSubscriptionResponse: webhookSubscriptionToResponse(sub),
		Secret:                      secret,
	}
	resp.SecretRedacted = false
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	var req UpdateWebhookSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params := db.UpdateWebhookSubscriptionParams{ID: sub.ID}
	if req.Name != nil {
		params.Name = pgtype.Text{String: *req.Name, Valid: true}
	}
	if req.URL != nil {
		params.Url = pgtype.Text{String: *req.URL, Valid: true}
	}
	if req.EventFilter != nil {
		if bad, ok := validateWebhookEventFilter(*req.EventFilter); !ok {
			writeError(w, http.StatusBadRequest, "event_filter contains unknown event type: "+bad)
			return
		}
		params.EventFilter = *req.EventFilter
	}
	if req.State != nil {
		params.State = pgtype.Text{String: *req.State, Valid: true}
	}
	if req.PauseThreshold != nil {
		params.PauseThreshold = pgtype.Int4{Int32: *req.PauseThreshold, Valid: true}
	}
	if req.AllowHTTP != nil {
		params.AllowHttp = pgtype.Bool{Bool: *req.AllowHTTP, Valid: true}
	}
	if req.PerAttemptTimeoutSeconds != nil {
		params.PerAttemptTimeoutSeconds = pgtype.Int4{Int32: *req.PerAttemptTimeoutSeconds, Valid: true}
	}

	updated, err := h.Queries.UpdateWebhookSubscription(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update webhook subscription")
		return
	}
	writeJSON(w, http.StatusOK, webhookSubscriptionToResponse(updated))
}

func (h *Handler) DeleteWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}
	if err := h.Queries.DeleteWebhookSubscription(r.Context(), sub.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete webhook subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RotateWebhookSubscriptionSecret(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}
	updated, err := h.Queries.RotateWebhookSubscriptionSecret(r.Context(), db.RotateWebhookSubscriptionSecretParams{
		ID:     sub.ID,
		Secret: secret,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate secret")
		return
	}
	resp := WebhookSecretCarrierResponse{
		WebhookSubscriptionResponse: webhookSubscriptionToResponse(updated),
		Secret:                      secret,
	}
	resp.SecretRedacted = false
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	limit := int32(50)
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = int32(n)
		}
	}
	includeBody := r.URL.Query().Get("include_body") == "true"

	deliveries, err := h.Queries.ListWebhookDeliveries(r.Context(), db.ListWebhookDeliveriesParams{
		SubscriptionID: sub.ID,
		Limit:          limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}
	resp := make([]WebhookDeliveryResponse, len(deliveries))
	for i, d := range deliveries {
		resp[i] = webhookDeliveryToResponse(d, includeBody)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": resp, "total": len(resp)})
}

func (h *Handler) TestWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	sub, ok := h.loadWebhookSubscriptionInWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	// Synthesize a test delivery row that the dispatcher will pick up. The
	// test event's payload is intentionally minimal — receivers should
	// recognize event_type="webhook:test" and treat as a connectivity probe.
	testPayload, _ := json.Marshal(map[string]any{
		"test":           true,
		"subscription":   uuidToString(sub.ID),
		"workspace_id":   workspaceID,
		"emitted_for":    "connectivity test",
		"docs":           "respond 2xx within timeout to confirm reachability + signature verification",
	})
	eventID := mustNewUUIDv7()
	delivery, err := h.Queries.CreateWebhookDelivery(r.Context(), db.CreateWebhookDeliveryParams{
		SubscriptionID: sub.ID,
		EventID:        eventID,
		EventType:      "webhook:test",
		Payload:        testPayload,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue test delivery")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"queued":      true,
		"delivery_id": uuidToString(delivery.ID),
		"hint":        "GET /api/webhooks/{id}/deliveries to see status",
	})
}

// nowTime is a tiny indirection so tests can swap the clock if needed.
// Production uses time.Now().
var nowTime = func() time.Time { return time.Now() }

// mustNewUUIDv7 panics on UUIDv7 generation failure (extremely unlikely with
// a working entropy source). Used inline where webhook code mints event IDs;
// callers in user-input paths should use a checked variant instead.
func mustNewUUIDv7() pgtype.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic("uuid.NewV7 failed: " + err.Error())
	}
	var out pgtype.UUID
	out.Bytes = id
	out.Valid = true
	return out
}

// We don't import io anywhere here yet; future request-body-size limit can use it.
var _ = io.Discard
