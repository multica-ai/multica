package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/multica-ai/multica/server/internal/automation"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hookEventListLimit is defensive; a correlation chain is bounded by the depth /
// width guardrails, but cap the debug read anyway.
const hookEventListLimit = 1000

// DryRunHookRequest dry-runs a candidate spec against a historical event.
type DryRunHookRequest struct {
	Hook    automation.HookSpec `json:"hook"`
	EventID string              `json:"event_id"`
}

// ExplainHookRequest explains a stored hook revision's decision for an event.
type ExplainHookRequest struct {
	HookID   string `json:"hook_id"`
	EventID  string `json:"event_id"`
	Revision int32  `json:"revision,omitempty"`
}

// DomainEventResponse is the read-only API view of a domain event.
type DomainEventResponse struct {
	ID                   string          `json:"id"`
	Seq                  int64           `json:"seq"`
	Type                 string          `json:"type"`
	SubjectType          string          `json:"subject_type"`
	SubjectID            string          `json:"subject_id"`
	ActorType            string          `json:"actor_type"`
	ActorID              string          `json:"actor_id,omitempty"`
	Payload              json.RawMessage `json:"payload"`
	CorrelationID        string          `json:"correlation_id"`
	CausationExecutionID string          `json:"causation_execution_id,omitempty"`
	HopCount             int32           `json:"hop_count"`
	CreatedAt            string          `json:"created_at"`
}

// DryRunHook evaluates a candidate hook spec against a historical event without
// executing anything or mutating any durable state (§10, decision 2A).
func (h *Handler) DryRunHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req DryRunHookRequest
	if !decodeJSONBodyStrict(w, r, &req) {
		return
	}
	eventUUID, ok := parseUUIDOrBadRequest(w, req.EventID, "event_id")
	if !ok {
		return
	}
	result, err := h.HookService.DryRun(r.Context(), workspaceUUID, req.Hook, eventUUID)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ExplainHook explains why a stored hook (its active or a specified revision)
// would or would not fire for a historical event. Read-only.
func (h *Handler) ExplainHook(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req ExplainHookRequest
	if !decodeJSONBodyStrict(w, r, &req) {
		return
	}
	hookUUID, ok := parseUUIDOrBadRequest(w, req.HookID, "hook_id")
	if !ok {
		return
	}
	eventUUID, ok := parseUUIDOrBadRequest(w, req.EventID, "event_id")
	if !ok {
		return
	}
	result, err := h.HookService.Explain(r.Context(), workspaceUUID, hookUUID, eventUUID, req.Revision)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ListEventsByCorrelation returns the domain events in a correlation chain, for
// execution-chain debugging (GET /api/events?correlation_id=). Read-only.
func (h *Handler) ListEventsByCorrelation(w http.ResponseWriter, r *http.Request) {
	if !h.hookEnabled(w, r) {
		return
	}
	workspaceUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	correlationUUID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("correlation_id"), "correlation_id")
	if !ok {
		return
	}
	events, err := h.HookService.EventsByCorrelation(r.Context(), workspaceUUID, correlationUUID)
	if err != nil {
		h.writeHookError(w, err)
		return
	}
	resp := make([]DomainEventResponse, 0, len(events))
	for i, e := range events {
		if i >= hookEventListLimit {
			break
		}
		resp = append(resp, domainEventToResponse(e))
	}
	writeJSON(w, http.StatusOK, resp)
}

func domainEventToResponse(e db.DomainEvent) DomainEventResponse {
	return DomainEventResponse{
		ID:                   uuidToString(e.ID),
		Seq:                  e.Seq,
		Type:                 e.Type,
		SubjectType:          e.SubjectType,
		SubjectID:            uuidToString(e.SubjectID),
		ActorType:            e.ActorType,
		ActorID:              uuidToString(e.ActorID),
		Payload:              rawJSON(e.Payload),
		CorrelationID:        uuidToString(e.CorrelationID),
		CausationExecutionID: uuidToString(e.CausationExecutionID),
		HopCount:             e.HopCount,
		CreatedAt:            timestampToString(e.CreatedAt),
	}
}

// decodeJSONBodyStrict decodes exactly one JSON document into dst, rejecting
// unknown fields and any trailing second document.
func decodeJSONBodyStrict(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body: expected exactly one JSON document")
		return false
	}
	return true
}
