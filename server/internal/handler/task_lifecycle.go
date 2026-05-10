package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RecoverOrphanedTasks is called by the daemon at startup for each runtime
// it owns. It atomically fails any dispatched/running tasks the server still
// believes belong to that runtime — those are the tasks the previous daemon
// process was running when it died — and triggers MaybeRetryFailedTask for
// each so the user sees a fresh attempt instead of a permanently stuck row.
//
// This is the targeted fix for "issue stuck at in_progress when daemon
// restarts mid-task": the runtime heartbeat sweeper takes up to 75s + the
// in-process task timeout (2.5h) to notice such tasks; the daemon itself
// knows the moment it comes back up, so we let it report orphan recovery.
func (h *Handler) RecoverOrphanedTasks(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	rows, err := h.Queries.RecoverOrphanedTasksForRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		slog.Warn("recover-orphans failed", "runtime_id", runtimeID, "error", err)
		writeError(w, http.StatusInternalServerError, "recover orphans failed")
		return
	}

	// Funnel through the shared post-failure pipeline so we get the same
	// task:failed events, agent reconcile, issue rollback, and auto-retry
	// behaviour as the runtime sweeper. This was previously a fast-path
	// that bypassed those side effects, leaving the UI stale when no retry
	// was created (max_attempts exhausted, autopilot, non-retryable reason).
	retried := h.TaskService.HandleFailedTasks(r.Context(), rows)

	if len(rows) > 0 {
		slog.Info("recover-orphans completed",
			"runtime_id", runtimeID,
			"orphaned", len(rows),
			"retried", retried,
		)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"orphaned": len(rows),
		"retried":  retried,
	})
}

// PinTaskSession lets the daemon persist the agent's session_id and
// work_dir as soon as they're known — typically right after the agent
// emits its first system message — so a crash mid-run doesn't lose the
// resume pointer needed to continue the conversation on the next attempt.
type PinTaskSessionRequest struct {
	SessionID string `json:"session_id,omitempty"`
	WorkDir   string `json:"work_dir,omitempty"`
}

func (h *Handler) PinTaskSession(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	if _, ok := h.requireDaemonTaskAccess(w, r, taskID); !ok {
		return
	}

	var req PinTaskSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SessionID == "" && req.WorkDir == "" {
		writeError(w, http.StatusBadRequest, "session_id or work_dir required")
		return
	}

	params := db.UpdateAgentTaskSessionParams{ID: parseUUID(taskID)}
	if req.SessionID != "" {
		params.SessionID = pgtype.Text{String: req.SessionID, Valid: true}
	}
	if req.WorkDir != "" {
		params.WorkDir = pgtype.Text{String: req.WorkDir, Valid: true}
	}
	if err := h.Queries.UpdateAgentTaskSession(r.Context(), params); err != nil {
		slog.Warn("pin-session failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RerunIssue manually re-enqueues the issue's current agent assignment as a
// fresh task. Useful when an issue is stuck or the user wants to retry a
// failed run. The new task is flagged force_fresh_session=true: the daemon
// claim handler skips the (agent_id, issue_id) session-resume lookup so the
// agent starts a clean session. A user clicking rerun has just judged the
// prior output bad — replaying the same conversation would replay the same
// poisoned state. (Automatic retry, by contrast, intentionally inherits the
// session — that path handles infrastructure failures, not bad output.)
func (h *Handler) RerunIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	task, err := h.TaskService.RerunIssue(r.Context(), issue.ID, pgtype.UUID{})
	if err != nil {
		slog.Warn("issue rerun failed", "issue_id", id, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, taskToResponse(*task))
}
