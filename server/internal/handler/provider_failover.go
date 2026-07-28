package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FailoverHandoffResponse is the observable, user-facing view of one provider
// failover decision (td-836aa9). It exposes the full auditable record: what
// failed, whether the policy would/did hand off, why, and the linkage to the
// original task and Claude fallback.
type FailoverHandoffResponse struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	OriginalTaskID  string          `json:"original_task_id"`
	ChainRootTaskID string          `json:"chain_root_task_id"`
	IssueID         string          `json:"issue_id,omitempty"`
	ChatSessionID   string          `json:"chat_session_id,omitempty"`
	SourceAgentID   string          `json:"source_agent_id"`
	SourceProvider  string          `json:"source_provider"`
	TargetProvider  string          `json:"target_provider"`
	TargetAgentID   string          `json:"target_agent_id,omitempty"`
	FallbackTaskID  string          `json:"fallback_task_id,omitempty"`
	TriggerReason   string          `json:"trigger_reason"`
	State           string          `json:"state"`
	Mode            string          `json:"mode"`
	WouldFailOver   bool            `json:"would_fail_over"`
	DeclineReason   string          `json:"decline_reason,omitempty"`
	SideEffects     json.RawMessage `json:"side_effects"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

func failoverHandoffToResponse(h db.ProviderFailoverHandoff) FailoverHandoffResponse {
	sideEffects := json.RawMessage(h.SideEffects)
	if len(sideEffects) == 0 {
		sideEffects = json.RawMessage("{}")
	}
	resp := FailoverHandoffResponse{
		ID:              uuidToString(h.ID),
		WorkspaceID:     uuidToString(h.WorkspaceID),
		OriginalTaskID:  uuidToString(h.OriginalTaskID),
		ChainRootTaskID: uuidToString(h.ChainRootTaskID),
		IssueID:         uuidToString(h.IssueID),
		ChatSessionID:   uuidToString(h.ChatSessionID),
		SourceAgentID:   uuidToString(h.SourceAgentID),
		SourceProvider:  h.SourceProvider,
		TargetProvider:  h.TargetProvider,
		TargetAgentID:   uuidToString(h.TargetAgentID),
		FallbackTaskID:  uuidToString(h.FallbackTaskID),
		TriggerReason:   h.TriggerReason,
		State:           h.State,
		Mode:            h.Mode,
		WouldFailOver:   h.WouldFailOver,
		SideEffects:     sideEffects,
	}
	if h.DeclineReason.Valid {
		resp.DeclineReason = h.DeclineReason.String
	}
	if h.CreatedAt.Valid {
		resp.CreatedAt = h.CreatedAt.Time.Format(time.RFC3339)
	}
	if h.UpdatedAt.Valid {
		resp.UpdatedAt = h.UpdatedAt.Time.Format(time.RFC3339)
	}
	return resp
}

// ListIssueFailoverHandoffs — GET /api/issues/{id}/failover-handoffs
//
// Returns every provider-failover decision recorded for the issue, newest
// first. This is the observability surface for the GPT->Claude failover policy:
// shadow-mode "would have" decisions and active-mode handoffs alike are visible
// here so operators can evaluate a rollout and users can see why a run was
// handed off (or why it was not).
func (h *Handler) ListIssueFailoverHandoffs(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	rows, err := h.Queries.ListFailoverHandoffsForIssue(r.Context(), issue.ID)
	if err != nil {
		slog.Error("failed to list failover handoffs", "issue_id", issueID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list failover handoffs")
		return
	}

	resp := make([]FailoverHandoffResponse, len(rows))
	for i, row := range rows {
		resp[i] = failoverHandoffToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}
