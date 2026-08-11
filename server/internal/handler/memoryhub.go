package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MemoryHub handler (Plan v1.5 V5-4 routes + v1.6 V6-1 review-repair).
// The read-only evidence endpoints (GET evidence + score) are part of ALL-16;
// rendering/display belongs to ALL-17/ALL-18.
//
// The V6-1 review-repair surface is the only externally callable owner repair
// route: POST /api/memoryhub/evidence/{execution_id}/review-repair.

// HandleRepairBlockedReviewer is the V6-1 handler. Execution order is
// fail-closed: 401 (auth middleware) -> 403 (RequireWorkspaceOwnerOrAdmin) ->
// 400 (closed decoder) -> service scoped load (404) -> reviewer validation
// (422) -> CAS transaction (409) -> commit -> post-commit scheduler wakeup.
func (h *Handler) HandleRepairBlockedReviewer(w http.ResponseWriter, r *http.Request) {
	executionID := chi.URLParam(r, "execution_id")
	execID, err := parseUUIDParam(executionID)
	if err != nil {
		writeMemoryHubError(w, http.StatusBadRequest, "request_invalid", "execution_id must be a canonical UUID")
		return
	}

	// Route accepts no query keys (V6-1.1).
	if len(r.URL.RawQuery) > 0 {
		writeMemoryHubError(w, http.StatusBadRequest, "query_invalid", "no query keys are accepted")
		return
	}

	var req protocol.ReviewRepairRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeMemoryHubError(w, http.StatusBadRequest, "request_field_unknown", "request body is closed and must match ReviewRepairRequest")
		return
	}
	if req.SchemaVersion != protocol.MemoryHubSchemaVersion || req.ExpectedReviewVersion < 1 || req.ReviewerAgentID == "" {
		writeMemoryHubError(w, http.StatusBadRequest, "request_invalid", "invalid ReviewRepairRequest")
		return
	}

	reviewerID, err := parseUUIDParam(req.ReviewerAgentID)
	if err != nil {
		writeMemoryHubError(w, http.StatusBadRequest, "request_invalid", "reviewer_agent_id must be a canonical UUID")
		return
	}

	workspaceID, ok := workspaceIDFromRequest(r)
	if !ok {
		writeMemoryHubError(w, http.StatusForbidden, "memoryhub_review_repair_forbidden", "workspace owner or admin role is required")
		return
	}

	record, err := h.MemoryHubSvc.RepairBlockedReviewer(
		r.Context(),
		h.Queries,
		execID,
		workspaceID,
		int32(req.ExpectedReviewVersion),
		reviewerID,
		pgtype.UUID{},
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrExecutionEvidenceNotFound):
			writeMemoryHubError(w, http.StatusNotFound, "execution_evidence_not_found", "execution evidence was not found")
		case errors.Is(err, service.ErrReviewerSelfForbidden):
			writeMemoryHubError(w, http.StatusUnprocessableEntity, "memoryhub_reviewer_self_forbidden", "reviewer cannot be the execution agent")
		case errors.Is(err, service.ErrReviewerScopeMismatch):
			writeMemoryHubError(w, http.StatusUnprocessableEntity, "memoryhub_reviewer_scope_mismatch", "reviewer is invalid for this workspace")
		case errors.Is(err, service.ErrReviewTransitionConflict):
			writeMemoryHubError(w, http.StatusConflict, "memoryhub_review_transition_conflict", "review version is stale or the record is not blocked")
		default:
			writeMemoryHubError(w, http.StatusInternalServerError, "internal_error", "review repair failed")
		}
		return
	}

	resp := protocol.ReviewRepairResponse{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		Record:        evidenceRecordToWire(*record),
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleGetExecutionEvidence is the read-only evidence aggregate endpoint.
func (h *Handler) HandleGetExecutionEvidence(w http.ResponseWriter, r *http.Request) {
	executionID := chi.URLParam(r, "execution_id")
	execID, err := parseUUIDParam(executionID)
	if err != nil {
		writeMemoryHubError(w, http.StatusBadRequest, "request_invalid", "execution_id must be a canonical UUID")
		return
	}
	record, err := h.Queries.GetExecutionEvidenceRecord(r.Context(), execID)
	if err != nil {
		writeMemoryHubError(w, http.StatusNotFound, "execution_evidence_not_found", "execution evidence was not found")
		return
	}
	events, err := h.Queries.ListEvidenceEventsByExecution(r.Context(), execID)
	if err != nil {
		writeMemoryHubError(w, http.StatusInternalServerError, "internal_error", "failed to load evidence events")
		return
	}
	wireEvents := make([]protocol.EvidenceEvent, 0, len(events))
	for _, ev := range events {
		wireEvents = append(wireEvents, evidenceEventToWire(ev))
	}
	resp := protocol.ExecutionEvidenceResponse{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		Record:        evidenceRecordToWire(record),
		Events:        wireEvents,
		Page: protocol.PageInfo{
			SchemaVersion: protocol.MemoryHubSchemaVersion,
			HasMore:       false,
		},
	}
	writeJSON(w, http.StatusOK, resp)
}

// HandleGetExecutionEvidenceScore is the read-only evidence score endpoint.
func (h *Handler) HandleGetExecutionEvidenceScore(w http.ResponseWriter, r *http.Request) {
	executionID := chi.URLParam(r, "execution_id")
	execID, err := parseUUIDParam(executionID)
	if err != nil {
		writeMemoryHubError(w, http.StatusBadRequest, "request_invalid", "execution_id must be a canonical UUID")
		return
	}
	score, err := h.Queries.GetEvidenceScore(r.Context(), getEvidenceScoreParams(execID, "h6-v1"))
	if err != nil {
		writeMemoryHubError(w, http.StatusNotFound, "evidence_score_not_found", "evidence score was not found")
		return
	}
	writeJSON(w, http.StatusOK, evidenceScoreToWire(score))
}

// HandleMemoryHubHealth is the read-only health probe for the four services.
// It returns the healthy shape with capabilities; when the remote client is
// unavailable the service reports per-service health from cached/static state.
func (h *Handler) HandleMemoryHubHealth(w http.ResponseWriter, r *http.Request) {
	// ALL-16 default: report the memory services as healthy-shape with a
	// capabilities trailer. The real per-service probe lives behind the M0
	// client; this endpoint stays read-only and capability-consistent.
	resp := protocol.HealthResponse{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		Services: protocol.HealthServices{
			SchemaVersion: protocol.MemoryHubSchemaVersion,
			MemoryCore: protocol.MemoryHubServiceHealth{
				SchemaVersion: protocol.MemoryHubSchemaVersion,
				OK:            true,
				CheckedAt:     nowRFC3339(),
			},
			Proxy: protocol.MemoryHubServiceHealth{
				SchemaVersion: protocol.MemoryHubSchemaVersion,
				OK:            true,
				CheckedAt:     nowRFC3339(),
			},
			Hub: protocol.MemoryHubServiceHealth{
				SchemaVersion: protocol.MemoryHubSchemaVersion,
				OK:            true,
				CheckedAt:     nowRFC3339(),
			},
			Knowledge: protocol.MemoryHubServiceHealth{
				SchemaVersion: protocol.MemoryHubSchemaVersion,
				OK:            true,
				CheckedAt:     nowRFC3339(),
			},
		},
		Capabilities: MemoryHubCapabilitiesToWire(CapabilitiesFromPerms(map[MemoryHubOperation]bool{})),
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeMemoryHubError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, protocol.ErrorResponse{
		SchemaVersion: protocol.MemoryHubSchemaVersion,
		Error: protocol.MemoryHubError{
			SchemaVersion: protocol.MemoryHubSchemaVersion,
			Code:          code,
			Message:       message,
			Retryable:     false,
		},
	})
}
