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

	rows, err := h.TaskService.RecoverOrphanedTasksForRuntime(r.Context(), parseUUID(runtimeID))
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

// RerunIssue preserves the issue-scoped API used by installed clients. A
// task_id repeats that run; an empty body targets the current assignee.
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
	h.rerunIssue(w, r, issue, sourceTaskID, false)
}

// RerunTaskByUser repeats an issue-linked run without requiring its issue ID.
func (h *Handler) RerunTaskByUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	source, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID: taskID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if !source.IssueID.Valid {
		writeError(w, http.StatusBadRequest, "only issue-linked runs can be rerun with this endpoint")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, uuidToString(source.IssueID))
	if !ok {
		return
	}
	var req struct {
		Fresh bool `json:"fresh"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	h.rerunIssue(w, r, issue, source.ID, req.Fresh)
}

func (h *Handler) rerunIssue(w http.ResponseWriter, r *http.Request, issue db.Issue, sourceTaskID pgtype.UUID, fresh bool) {
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

	task, err := h.TaskService.RerunIssue(r.Context(), issue.ID, sourceTaskID, pgtype.UUID{}, actorUserID, fresh, canInvoke)
	if errors.Is(err, service.ErrRerunInvokeNotAllowed) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}
	if errors.Is(err, service.ErrIssueInTriage) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonIssueInTriage)
		return
	}
	// Not a dispatch refusal: the issue may well be runnable, and only the named
	// source is ineligible. It falls through to the 400 below with the
	// sentinel's own sentence, like the sibling "does not belong to this issue".
	if err != nil {
		slog.Warn("issue rerun failed", "issue_id", uuidToString(issue.ID), "error", err)
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
	if writeIssueLimitReached(w, err) {
		return
	}
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

// MoveTaskResponse reports the outcome of a manual queue reorder. Moved is
// false when the task was already at the edge of its agent's queue, or could
// not gain a position within the coarse 0–4 priority scale; Priority is then
// unchanged.
type MoveTaskResponse struct {
	TaskID   string `json:"task_id"`
	Priority int32  `json:"priority"`
	Moved    bool   `json:"moved"`
}

// MoveTaskByUser moves one queued task one step up or down in its agent's
// claim order (priority DESC, created_at ASC, id ASC — the order
// ClaimAgentTask uses), letting a member hand-reorder the queue.
//
// Tenancy and privacy mirror CancelTaskByUser exactly: the task is loaded
// through GetAgentTaskInWorkspace (the single tenant guard regardless of which
// optional source FK is set), a chat task is private to the member who opened
// the conversation, and every other task passes canAccessPrivateAgent so the
// id-only endpoint is never more permissive than the Activity tab that
// exposes it. The move runs under the agent's claim lock (the same row lock
// ClaimTask takes before selecting work), so the reorder cannot race a claim
// in flight; the broadcast on commit lets every client re-sort its queue from
// the existing task:queued event.
func (h *Handler) MoveTaskByUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}
	var req struct {
		Direction string `json:"direction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Direction != "up" && req.Direction != "down" {
		writeError(w, http.StatusBadRequest, "direction must be up or down")
		return
	}

	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID:          taskID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	if task.ChatSessionID.Valid {
		// Chat privacy: only the member who opened the conversation may
		// reorder its tasks, even though the workspace is shared.
		cs, err := h.Queries.GetChatSessionInWorkspace(r.Context(), db.GetChatSessionInWorkspaceParams{
			ID:          task.ChatSessionID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		if uuidToString(cs.CreatorID) != userID {
			writeError(w, http.StatusForbidden, "not your task")
			return
		}
	} else {
		// Issue / autopilot / quick_create tasks all surface on the agent
		// Activity tab, which gates private agents. Mirror that gate here.
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID:          task.AgentID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
			writeError(w, http.StatusForbidden, "you do not have access to this agent")
			return
		}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start move transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// ClaimTask takes this same row lock before selecting work. Holding it
	// here makes "reorder" one server-authoritative decision instead of a
	// priority write racing an in-flight claim.
	if _, err := qtx.GetAgentForClaimUpdate(r.Context(), task.AgentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock agent")
		return
	}

	var (
		movedID   pgtype.UUID
		movedPrio int32
		moveErr   error
	)
	if req.Direction == "up" {
		var row db.MoveQueuedTaskUpRow
		row, moveErr = qtx.MoveQueuedTaskUp(r.Context(), taskID)
		movedID, movedPrio = row.ID, row.Priority
	} else {
		var row db.MoveQueuedTaskDownRow
		row, moveErr = qtx.MoveQueuedTaskDown(r.Context(), taskID)
		movedID, movedPrio = row.ID, row.Priority
	}
	if errors.Is(moveErr, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "task is no longer queued")
		return
	}
	if moveErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to move queued task")
		return
	}
	queuedTask, err := qtx.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load moved task")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit queued task move")
		return
	}
	h.TaskService.BroadcastTaskQueued(r.Context(), queuedTask)

	writeJSON(w, http.StatusOK, MoveTaskResponse{
		TaskID:   uuidToString(movedID),
		Priority: movedPrio,
		Moved:    movedPrio != task.Priority,
	})
}
