package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ── Response types ──────────────────────────────────────────────────────────

// WebhookDeliveryResponse is the authenticated-API view of webhook_delivery.
// The list endpoint returns these without `RawBody` / `SelectedHeaders`
// populated; the detail endpoint includes both for debugging. We never echo
// the signing secret or token through this surface.
type WebhookDeliveryResponse struct {
	ID                     string  `json:"id"`
	WorkspaceID            string  `json:"workspace_id"`
	AutopilotID            string  `json:"autopilot_id"`
	TriggerID              string  `json:"trigger_id"`
	Provider               string  `json:"provider"`
	Event                  string  `json:"event"`
	DedupeKey              *string `json:"dedupe_key"`
	DedupeSource           *string `json:"dedupe_source"`
	SignatureStatus        string  `json:"signature_status"`
	Status                 string  `json:"status"`
	AttemptCount           int32   `json:"attempt_count"`
	DispatchAttempts       int32   `json:"dispatch_attempts"`
	AvailableAt            string  `json:"available_at"`
	ContentType            *string `json:"content_type"`
	ResponseStatus         *int32  `json:"response_status"`
	AutopilotRunID         *string `json:"autopilot_run_id"`
	ReplayedFromDeliveryID *string `json:"replayed_from_delivery_id"`
	Error                  *string `json:"error"`
	ReasonCode             *string `json:"reason_code"`
	ReplayIdempotencyKey   *string `json:"replay_idempotency_key"`
	ReceivedAt             string  `json:"received_at"`
	LastAttemptAt          string  `json:"last_attempt_at"`
	CreatedAt              string  `json:"created_at"`

	// Detail-only fields. List responses leave these nil/empty so a page
	// of N deliveries never serialises ~N × 256 KiB of raw bodies. Detail
	// requests opt in by hitting GET /deliveries/{deliveryId}.
	SelectedHeaders json.RawMessage `json:"selected_headers,omitempty"`
	RawBody         *string         `json:"raw_body,omitempty"`
	ResponseBody    *string         `json:"response_body,omitempty"`
}

// slimDeliveryToResponse maps the projected list row (no raw_body /
// selected_headers / response_body) into the wire response shape.
func slimDeliveryToResponse(d db.ListWebhookDeliveriesByAutopilotRow) WebhookDeliveryResponse {
	resp := WebhookDeliveryResponse{
		ID:                   uuidToString(d.ID),
		WorkspaceID:          uuidToString(d.WorkspaceID),
		AutopilotID:          uuidToString(d.AutopilotID),
		TriggerID:            uuidToString(d.TriggerID),
		Provider:             d.Provider,
		Event:                d.Event,
		DedupeKey:            textToPtr(d.DedupeKey),
		DedupeSource:         textToPtr(d.DedupeSource),
		SignatureStatus:      d.SignatureStatus,
		Status:               d.Status,
		AttemptCount:         d.AttemptCount,
		DispatchAttempts:     d.DispatchAttempts,
		AvailableAt:          timestampToString(d.AvailableAt),
		ContentType:          textToPtr(d.ContentType),
		ReceivedAt:           timestampToString(d.ReceivedAt),
		LastAttemptAt:        timestampToString(d.LastAttemptAt),
		CreatedAt:            timestampToString(d.CreatedAt),
		ReasonCode:           textToPtr(d.ReasonCode),
		ReplayIdempotencyKey: textToPtr(d.ReplayIdempotencyKey),
	}
	if d.ResponseStatus.Valid {
		v := d.ResponseStatus.Int32
		resp.ResponseStatus = &v
	}
	if d.AutopilotRunID.Valid {
		v := uuidToString(d.AutopilotRunID)
		resp.AutopilotRunID = &v
	}
	if d.ReplayedFromDeliveryID.Valid {
		v := uuidToString(d.ReplayedFromDeliveryID)
		resp.ReplayedFromDeliveryID = &v
	}
	if d.Error.Valid {
		v := d.Error.String
		resp.Error = &v
	}
	return resp
}

func deliveryToResponse(d db.WebhookDelivery, detail bool) WebhookDeliveryResponse {
	resp := WebhookDeliveryResponse{
		ID:                   uuidToString(d.ID),
		WorkspaceID:          uuidToString(d.WorkspaceID),
		AutopilotID:          uuidToString(d.AutopilotID),
		TriggerID:            uuidToString(d.TriggerID),
		Provider:             d.Provider,
		Event:                d.Event,
		DedupeKey:            textToPtr(d.DedupeKey),
		DedupeSource:         textToPtr(d.DedupeSource),
		SignatureStatus:      d.SignatureStatus,
		Status:               d.Status,
		AttemptCount:         d.AttemptCount,
		DispatchAttempts:     d.DispatchAttempts,
		AvailableAt:          timestampToString(d.AvailableAt),
		ContentType:          textToPtr(d.ContentType),
		ReceivedAt:           timestampToString(d.ReceivedAt),
		LastAttemptAt:        timestampToString(d.LastAttemptAt),
		CreatedAt:            timestampToString(d.CreatedAt),
		ReasonCode:           textToPtr(d.ReasonCode),
		ReplayIdempotencyKey: textToPtr(d.ReplayIdempotencyKey),
	}
	if d.ResponseStatus.Valid {
		v := d.ResponseStatus.Int32
		resp.ResponseStatus = &v
	}
	if d.AutopilotRunID.Valid {
		v := uuidToString(d.AutopilotRunID)
		resp.AutopilotRunID = &v
	}
	if d.ReplayedFromDeliveryID.Valid {
		v := uuidToString(d.ReplayedFromDeliveryID)
		resp.ReplayedFromDeliveryID = &v
	}
	if d.Error.Valid {
		v := d.Error.String
		resp.Error = &v
	}
	if detail {
		if len(d.SelectedHeaders) > 0 {
			resp.SelectedHeaders = json.RawMessage(d.SelectedHeaders)
		}
		if len(d.RawBody) > 0 {
			s := string(d.RawBody)
			resp.RawBody = &s
		}
		if d.ResponseBody.Valid {
			v := d.ResponseBody.String
			resp.ResponseBody = &v
		}
	}
	return resp
}

// ── Handlers ────────────────────────────────────────────────────────────────

// ListAutopilotDeliveries returns recent deliveries for an autopilot. Slim
// projection — selected_headers / raw_body / response_body are omitted to
// keep list responses small. Use GetAutopilotDelivery for the full payload.
func (h *Handler) ListAutopilotDeliveries(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}

	limit := int32(20)
	offset := int32(0)
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = int32(v)
		}
	}
	if limit > 100 {
		limit = 100
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = int32(v)
		}
	}

	rows, err := h.Queries.ListWebhookDeliveriesByAutopilot(r.Context(), db.ListWebhookDeliveriesByAutopilotParams{
		AutopilotID: autopilot.ID,
		WorkspaceID: autopilot.WorkspaceID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		slog.Error("list deliveries failed", "error", err, "autopilot_id", autopilotID)
		writeError(w, http.StatusInternalServerError, "failed to list deliveries")
		return
	}

	resp := make([]WebhookDeliveryResponse, len(rows))
	for i, row := range rows {
		resp[i] = slimDeliveryToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": resp, "total": len(resp)})
}

// GetAutopilotDelivery returns one delivery in full, including the raw body
// and headers subset. Workspace-scoped via the autopilot lookup; the
// delivery is then re-checked to belong to that autopilot so a guessed
// delivery id from another workspace cannot leak data.
func (h *Handler) GetAutopilotDelivery(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	deliveryID := chi.URLParam(r, "deliveryId")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}
	delivery, ok := h.loadDeliveryForAutopilot(w, r, autopilot, deliveryID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, deliveryToResponse(delivery, true))
}

// ReplayAutopilotDelivery creates a NEW queued delivery row from a prior one;
// the existing durable worker owns dispatch. The new row carries
// `replayed_from_delivery_id` so the operator can correlate. Replay is
// rejected for deliveries that originally failed signature verification —
// re-running an attack payload against the autopilot would defeat the
// rejection in the first place.
//
// Replays bypass provider dedupe by inserting with a NULL dedupe_key:
// reusing the original key would silently collapse the replay onto the prior
// delivery (the partial unique index would fire). This is the intended
// behaviour — a replay is explicitly "run this again". API retries are
// independently deduplicated by (original delivery, Idempotency-Key).
func (h *Handler) ReplayAutopilotDelivery(w http.ResponseWriter, r *http.Request) {
	autopilotID := chi.URLParam(r, "id")
	deliveryID := chi.URLParam(r, "deliveryId")
	workspaceID := h.resolveWorkspaceID(r)

	autopilot, ok := h.loadAutopilotInWorkspace(w, r, autopilotID, workspaceID)
	if !ok {
		return
	}
	if !h.requireAutopilotWrite(w, r, autopilot, workspaceID) {
		return
	}
	original, ok := h.loadDeliveryForAutopilot(w, r, autopilot, deliveryID)
	if !ok {
		return
	}
	if original.Status == deliveryStatusRejected || original.SignatureStatus == sigStatusInvalid {
		writeError(w, http.StatusBadRequest, "cannot replay a delivery that failed signature verification")
		return
	}
	if len(original.RawBody) == 0 {
		writeError(w, http.StatusBadRequest, "original delivery has no raw body to replay")
		return
	}

	if autopilot.Status != "active" {
		writeError(w, http.StatusBadRequest, "autopilot is not active")
		return
	}

	trigRow, err := h.Queries.GetAutopilotTrigger(r.Context(), original.TriggerID)
	if err != nil {
		writeError(w, http.StatusNotFound, "trigger not found")
		return
	}
	if !trigRow.Enabled {
		writeError(w, http.StatusBadRequest, "trigger is disabled")
		return
	}

	// Build the envelope from the stored raw body using the original headers
	// subset for event inference. SelectedHeaders is small + JSON-shaped, so
	// we decode it back into a header map to reuse the same normalize path.
	headers := headersFromSelected(original.SelectedHeaders)
	envelope, err := normalizeWebhookPayload(original.RawBody, headers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "stored body no longer parses: "+err.Error())
		return
	}
	_ = envelope // the durable worker normalizes the stored payload again

	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idempotencyKey) > 255 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key is too long")
		return
	}
	if idempotencyKey == "" {
		idempotencyKey = service.NewRequestIdempotencyKey()
	}
	keyText := pgtype.Text{String: idempotencyKey, Valid: true}
	if existing, lookupErr := h.Queries.GetWebhookReplayByIdempotencyKey(r.Context(), db.GetWebhookReplayByIdempotencyKeyParams{
		ReplayedFromDeliveryID: original.ID, ReplayIdempotencyKey: keyText,
	}); lookupErr == nil {
		writeJSON(w, http.StatusAccepted, deliveryToResponse(existing, true))
		return
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to inspect replay request")
		return
	}

	contentType := ""
	if original.ContentType.Valid {
		contentType = original.ContentType.String
	}
	replay, err := h.Queries.CreateWebhookDelivery(r.Context(), db.CreateWebhookDeliveryParams{
		WorkspaceID:            autopilot.WorkspaceID,
		AutopilotID:            autopilot.ID,
		TriggerID:              original.TriggerID,
		Provider:               original.Provider,
		Event:                  envelope.Event,
		SignatureStatus:        sigStatusNotRequired,
		Status:                 deliveryStatusQueued,
		SelectedHeaders:        original.SelectedHeaders,
		ContentType:            pgtype.Text{String: contentType, Valid: contentType != ""},
		RawBody:                original.RawBody,
		ReplayedFromDeliveryID: original.ID,
		ReplayIdempotencyKey:   keyText,
	})
	if err != nil {
		if isUniqueViolation(err) {
			existing, lookupErr := h.Queries.GetWebhookReplayByIdempotencyKey(r.Context(), db.GetWebhookReplayByIdempotencyKeyParams{
				ReplayedFromDeliveryID: original.ID, ReplayIdempotencyKey: keyText,
			})
			if lookupErr == nil {
				writeJSON(w, http.StatusAccepted, deliveryToResponse(existing, true))
				return
			}
		}
		slog.Error("replay: insert delivery failed",
			"error", err,
			"original_delivery_id", uuidToString(original.ID),
		)
		writeError(w, http.StatusInternalServerError, "failed to create replay delivery")
		return
	}

	if h.WebhookDeliveryWorker != nil {
		h.WebhookDeliveryWorker.Notify()
	}
	writeJSON(w, http.StatusAccepted, deliveryToResponse(replay, true))
}

// loadDeliveryForAutopilot returns the delivery row when it exists in the
// same workspace AND belongs to the given autopilot. Cross-autopilot or
// cross-workspace IDs are returned as 404 — defense in depth against ID
// guessing.
func (h *Handler) loadDeliveryForAutopilot(w http.ResponseWriter, r *http.Request, autopilot db.Autopilot, deliveryID string) (db.WebhookDelivery, bool) {
	deliveryUUID, ok := parseUUIDOrBadRequest(w, deliveryID, "delivery id")
	if !ok {
		return db.WebhookDelivery{}, false
	}
	delivery, err := h.Queries.GetWebhookDeliveryInWorkspace(r.Context(), db.GetWebhookDeliveryInWorkspaceParams{
		ID:          deliveryUUID,
		WorkspaceID: autopilot.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "delivery not found")
			return db.WebhookDelivery{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load delivery")
		return db.WebhookDelivery{}, false
	}
	if uuidToString(delivery.AutopilotID) != uuidToString(autopilot.ID) {
		writeError(w, http.StatusNotFound, "delivery not found")
		return db.WebhookDelivery{}, false
	}
	return delivery, true
}

// headersFromSelected decodes the small headers-subset blob back into an
// http.Header. Only used by the replay path — fields we did not capture at
// ingress time are simply absent, which matches what would have happened if
// the original request had not sent them either.
func headersFromSelected(raw []byte) http.Header {
	out := http.Header{}
	if len(raw) == 0 {
		return out
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}
	canonical := map[string]string{
		"user-agent":        "User-Agent",
		"x-github-event":    "X-GitHub-Event",
		"x-github-delivery": "X-GitHub-Delivery",
		"x-gitlab-event":    "X-Gitlab-Event",
		"x-event-type":      "X-Event-Type",
		"idempotency-key":   "Idempotency-Key",
	}
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		header := canonical[k]
		if header == "" {
			continue
		}
		out.Set(header, s)
	}
	return out
}
