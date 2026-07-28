package inbox

// FIR-3901 — read endpoints for "dead" failed runs: a run that failed and that
// nothing is going to pick up again on its own. Two consumers:
//
//   - the red failed bar at the top of an issue (per-issue)
//   - the red pip on inbox rows (per-workspace)
//
// The dead-run predicate and the resume_possible computation live in the query
// layer (cerebrodb.ListDeadFailedTasksForIssue) so both endpoints, and any
// future consumer, agree on what "still needs a human" means.

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
)

type deadFailedRunResponse struct {
	TaskID        string `json:"task_id"`
	AgentID       string `json:"agent_id,omitempty"`
	IssueID       string `json:"issue_id"`
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
	Error         string `json:"error,omitempty"`
	CompletedAt   string `json:"completed_at,omitempty"`
	Attempt       int32  `json:"attempt"`
	MaxAttempts   int32  `json:"max_attempts"`
	// ResumePossible is false when the daemon would not be able to pick the
	// conversation back up. BlockedReason then says why, in plain words, so the
	// UI can disable Resume with an explanation instead of silently starting a
	// blank run.
	ResumePossible bool   `json:"resume_possible"`
	BlockedReason  string `json:"blocked_reason,omitempty"`
	RuntimeName    string `json:"runtime_name,omitempty"`
}

// resumeBlockedReason explains a false resume_possible. Order matters: report
// the most actionable cause first. English copy — app-facing strings are
// English everywhere in this codebase.
func resumeBlockedReason(t cerebrodb.DeadFailedTask) string {
	if t.ResumePossible {
		return ""
	}
	if !t.HasSession {
		return "The run never got far enough to have a conversation to continue."
	}
	if isPoisonedReason(t.FailureReason.String) {
		return "The conversation itself is what failed, so continuing it would fail the same way."
	}
	if !t.RuntimeOnline {
		name := t.RuntimeName.String
		if name == "" {
			name = "The machine"
		}
		return name + " is offline — the conversation lives on that machine."
	}
	return "This run cannot be continued."
}

// isPoisonedReason mirrors the blacklist the resume lookup applies.
func isPoisonedReason(reason string) bool {
	switch reason {
	case "iteration_limit", "agent_fallback_message", "api_invalid_request", "codex_semantic_inactivity":
		return true
	}
	return false
}

func toDeadFailedResponse(t cerebrodb.DeadFailedTask) deadFailedRunResponse {
	out := deadFailedRunResponse{
		TaskID:         util.UUIDToString(t.ID),
		IssueID:        util.UUIDToString(t.IssueID),
		FailureReason:  t.FailureReason.String,
		Error:          t.Error.String,
		Attempt:        t.Attempt,
		MaxAttempts:    t.MaxAttempts,
		ResumePossible: t.ResumePossible,
		BlockedReason:  resumeBlockedReason(t),
		RuntimeName:    t.RuntimeName.String,
	}
	if t.AgentID.Valid {
		out.AgentID = util.UUIDToString(t.AgentID)
	}
	if t.ParentIssueID.Valid {
		out.ParentIssueID = util.UUIDToString(t.ParentIssueID)
	}
	if t.CompletedAt.Valid {
		out.CompletedAt = t.CompletedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return out
}

// ListDeadFailedRunsForIssue handles GET /api/issues/{id}/failed-runs.
// Drives the red failed bar at the top of an issue.
func (h *Handler) ListDeadFailedRunsForIssue(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	issueID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return
	}
	rows, err := h.Cerebro.ListDeadFailedTasksForIssue(r.Context(), issueID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failed runs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": mapDeadFailed(rows)})
}

// ListDeadFailedIssueTasks handles GET /api/inbox/failed-issue-tasks.
// Drives the red pip on inbox rows.
func (h *Handler) ListDeadFailedIssueTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, err := util.ParseUUID(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	rows, err := h.Cerebro.ListDeadFailedIssueTasksInWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list failed runs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": mapDeadFailed(rows)})
}

func mapDeadFailed(rows []cerebrodb.DeadFailedTask) []deadFailedRunResponse {
	out := make([]deadFailedRunResponse, 0, len(rows))
	for _, t := range rows {
		out = append(out, toDeadFailedResponse(t))
	}
	return out
}


