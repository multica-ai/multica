package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const agentActionSummaryMaxBytes = 4096

func taskMessageAuditSummary(msg TaskMessageRequest) (argsSummary, resultSummary, status string, ok bool) {
	switch msg.Type {
	case "tool_use", "tool-use":
		argsSummary = "{}"
		if msg.Input != nil {
			if raw, err := json.Marshal(msg.Input); err == nil {
				argsSummary = redact.Text(string(raw))
			}
		}
		return truncateAgentActionSummary(argsSummary), "", "started", true
	case "tool_result", "tool-result":
		return "", truncateAgentActionSummary(redact.Text(msg.Output)), "completed", true
	default:
		return "", "", "", false
	}
}

func truncateAgentActionSummary(value string) string {
	if len(value) <= agentActionSummaryMaxBytes {
		return value
	}
	return value[:agentActionSummaryMaxBytes]
}

type AgentActionResponse struct {
	ID            int64  `json:"id"`
	AgentID       string `json:"agent_id"`
	IssueID       string `json:"issue_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	MessageSeq    *int32 `json:"message_seq,omitempty"`
	ToolName      string `json:"tool_name"`
	ArgsSummary   string `json:"args_summary,omitempty"`
	ResultSummary string `json:"result_summary,omitempty"`
	Status        string `json:"status,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func agentActionToResponse(action db.AgentActionLog) AgentActionResponse {
	resp := AgentActionResponse{
		ID:            action.ID,
		AgentID:       action.AgentID.String,
		IssueID:       action.IssueID.String,
		TaskID:        action.TaskID.String,
		ToolName:      action.ToolName,
		ArgsSummary:   action.ArgsSummary.String,
		ResultSummary: action.ResultSummary.String,
		Status:        action.Status.String,
		CreatedAt:     timestampToString(action.CreatedAt),
	}
	if action.MessageSeq.Valid {
		seq := action.MessageSeq.Int32
		resp.MessageSeq = &seq
	}
	return resp
}

// ListAgentActions returns the bounded, redacted audit trail for an agent.
// Action payloads can contain sensitive business data, so this endpoint is
// restricted to agent owners and workspace administrators.
func (h *Handler) ListAgentActions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		if parsed > 500 {
			parsed = 500
		}
		limit = parsed
	}

	actions, err := h.Queries.ListAgentActionsByAgent(r.Context(), db.ListAgentActionsByAgentParams{
		AgentID: pgtype.Text{String: id, Valid: id != ""},
		Limit:   int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent actions")
		return
	}

	resp := make([]AgentActionResponse, len(actions))
	for i, action := range actions {
		resp[i] = agentActionToResponse(action)
	}
	writeJSON(w, http.StatusOK, resp)
}
