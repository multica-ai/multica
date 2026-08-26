package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
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
	// The pin can arrive after the user has already cancelled the run — it is
	// asynchronous, and for Codex it waits for the rollout to reach the store.
	// The cancel transaction then found no session to publish, so the chat's
	// resume pointer is still on the previous turn and would shadow the session
	// this pin is about to record (the claim handler reads the pointer before
	// the GetLastChatTaskSession fallback). Advancing it here closes that half
	// of GH #6340.
	//
	// Both writes commit together. Landing the session on the task row first and
	// the pointer second leaves the same window the cancel path had: the row
	// already names the new session while the pointer still names the previous
	// turn, and a follow-up claimed in between resumes the older one. The lock
	// comes first for the same reason it does in CancelTaskWithResult —
	// chat_session -> agent_task_queue is the global order, and ErrNoRows simply
	// means there is no session to lock or advance.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Warn("pin-session failed to start tx", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if _, err := qtx.LockChatSessionForTask(r.Context(), params.ID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("pin-session failed to lock chat session", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	if err := qtx.UpdateAgentTaskSession(r.Context(), params); err != nil {
		slog.Warn("pin-session failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	// The statement re-reads the row, ignores anything that is not a cancelled
	// chat task, and refuses to move the pointer when a newer turn already owns
	// a session — so a straggler pin cannot drag the conversation backwards.
	if err := qtx.AdvanceCancelledChatSessionPointer(r.Context(), params.ID); err != nil {
		slog.Warn("advance cancelled chat session pointer failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Warn("pin-session commit failed", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "pin session failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RerunIssueRequest is the optional body of POST /api/issues/{id}/rerun.
// All fields are optional; an empty body keeps the legacy "rerun the issue's
// current assignee" behaviour used by the CLI.
type RerunIssueRequest struct {
	// TaskID identifies the execution-log row the user clicked retry on.
	// When set, the rerun targets the agent that ran that specific task
	// (and reuses its leader/worker role) rather than the issue's current
	// assignee — so clicking retry on row that belonged to a now-displaced
	// agent re-fires that same agent, not the new assignee.
	TaskID string `json:"task_id,omitempty"`
}

// RerunIssue manually re-enqueues an agent run for the issue. By default it
// targets the issue's current assignee (agent or squad leader); if the
// request body carries task_id, the rerun targets the agent that ran that
// specific past task instead. The new task is flagged force_fresh_session=true:
// the daemon claim handler skips the (agent_id, issue_id) session-resume
// lookup so the agent starts a clean session. A user clicking rerun has just
// judged the prior output bad — replaying the same conversation would replay
// the same poisoned state. (Automatic retry, by contrast, intentionally
// inherits the session — that path handles infrastructure failures, not bad
// output.)
func (h *Handler) RerunIssue(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, id)
	if !ok {
		return
	}

	// Body is optional. A zero-length body or `{}` keeps the legacy
	// assignee-driven rerun behaviour the CLI relies on.
	var req RerunIssueRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	var sourceTaskID pgtype.UUID
	if req.TaskID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.TaskID, "task_id")
		if !ok {
			return
		}
		sourceTaskID = parsed
	}

	// A manual rerun is a direct human action: attribute the new run to the
	// rerunning member (MUL-4302 §5). Resolve the actor the same way assign/promote
	// does; an agent A2A actor is not a human and threads an invalid actor.
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	actorUserID := memberActorUserID(actorType, actorID)

	// Re-validate the operator's invoke permission on the resolved target agent
	// before cancelling / creating anything (MUL-4525). Issue visibility does not
	// grant the right to trigger a private agent — a task_id rerun must gate the
	// historical agent, not the (possibly reassigned) current assignee.
	originatorUserID := h.invokeOriginatorFromRequest(r, actorType, actorID)
	canInvoke := func(agent db.Agent) bool {
		return h.canInvokeAgent(r.Context(), agent, actorType, actorID, originatorUserID, workspaceID)
	}

	task, err := h.TaskService.RerunIssue(r.Context(), issue.ID, sourceTaskID, pgtype.UUID{}, actorUserID, canInvoke)
	if errors.Is(err, service.ErrRerunInvokeNotAllowed) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}
	if err != nil {
		slog.Warn("issue rerun failed", "issue_id", id, "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := taskToResponse(*task, uuidToString(issue.WorkspaceID))
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.Attribution})
	writeJSON(w, http.StatusAccepted, resp)
}

// RetrySourceContextQuickCreate manually re-enqueues a failed issue-less
// quick-create while atomically moving its pending immutable source context to
// the new task. The workspace middleware supplies tenancy; the service also
// requires the original requester and the normal private-agent invoke gate.
func (h *Handler) RetrySourceContextQuickCreate(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	requesterID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	canInvoke := func(agent db.Agent) bool {
		return h.canInvokeAgent(r.Context(), agent, "member", userID, userID, uuidToString(workspaceID))
	}
	task, err := h.TaskService.RetrySourceContextQuickCreate(r.Context(), workspaceID, requesterID, taskID, canInvoke)
	if errors.Is(err, service.ErrRerunInvokeNotAllowed) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}
	if errors.Is(err, service.ErrSourceContextRetryUnavailable) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":  "source_context_retry_unavailable",
			"error": "This context can no longer be retried. Start again from the branch point.",
		})
		return
	}
	if err != nil {
		slog.Warn("source context quick-create retry failed", "task_id", uuidToString(taskID), "error", err)
		writeError(w, http.StatusInternalServerError, "retry source context quick create")
		return
	}
	resp := taskToResponse(*task, uuidToString(workspaceID))
	h.hydrateTaskAttributions(r.Context(), []*TaskAttribution{resp.Attribution})
	writeJSON(w, http.StatusAccepted, resp)
}
