package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// continueTaskInChatTitleLimit bounds the seeded chat title. Long issue titles
// would otherwise dominate the chat list; the auto-titler is not involved here
// because a seeded session already has a non-empty title.
const continueTaskInChatTitleLimit = 80

// ContinueTaskInChatResponse is the created (or reopened) conversation plus the
// two facts the UI cannot infer on its own.
type ContinueTaskInChatResponse struct {
	ChatSession ChatSessionResponse `json:"chat_session"`
	// Reopened is true when this member had already continued the same task and
	// we handed back that existing conversation instead of forking a second one.
	Reopened bool `json:"reopened"`
	// SessionCarried reports whether the source task actually had a resumable
	// provider session to inherit. When false the chat still opens — with the
	// task's work_dir when there was one — but the agent starts a fresh
	// conversation, so the UI must say so rather than implying continuity that
	// does not exist. Several backends only report their session id at
	// completion, and a task that failed early may never have recorded one.
	SessionCarried bool `json:"session_carried"`
	// WorkDirCarried reports whether the chat inherited the task's working
	// directory. Independent of SessionCarried: a task can leave a reusable
	// directory without a resumable conversation.
	WorkDirCarried bool `json:"work_dir_carried"`
}

// ContinueTaskInChat opens a chat conversation that continues a finished agent
// task: the new chat_session inherits that task's provider session id, work_dir
// and runtime, so the member's first message resumes the same agent conversation
// in the same directory instead of cold-starting one that knows nothing about the
// run they just watched.
//
// Why this is a seeded INSERT and not "create a chat, then send a message": the
// resume pointer lives on chat_session (session_id / work_dir / runtime_id) and
// is read at claim time, so it has to be present before the first task is
// enqueued. A chat created through the normal path has already committed a NULL
// pointer by then.
//
// Two gates, and they are not redundant:
//   - GetAgentTaskInWorkspace is the tenancy guard. Every agent_task_queue row
//     carries a NOT NULL agent_id and agents are workspace-scoped, so this works
//     for issue, autopilot and quick_create tasks alike — the same reasoning
//     CancelTaskByUser documents. Cross-workspace ids 404 rather than 403 so the
//     endpoint never confirms that a task exists elsewhere.
//   - canInvokeAgent is the admission guard. Continuing in chat WILL start agent
//     runs, so it needs the invoke permission that CreateChatSession uses, not
//     the softer visibility gate that Cancel uses. Being allowed to watch a run
//     (or stop it) does not imply being allowed to start new ones.
//
// Terminal tasks only. A non-terminal task still owns its provider session and
// its working directory: resuming that session from a second client is undefined
// for the long-lived ACP backends, and a reused work_dir has no mutual exclusion
// (only local_directory tasks take a path lock), so two live runs would share one
// directory. Rather than silently degrade — dropping the session and handing the
// user a chat that quietly lost the context they asked for — this returns 409
// with a reason the UI can act on.
func (h *Handler) ContinueTaskInChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	taskUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task id")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
		ID:          taskUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// A chat task already has its conversation; "continue" there is just opening
	// it. Refuse rather than mint a second session onto the same provider session
	// — and say which one, so a caller that reached this by id can navigate.
	if task.ChatSessionID.Valid {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "this task already belongs to a chat conversation",
			"reason":          "already_chat_task",
			"chat_session_id": uuidToString(task.ChatSessionID),
		})
		return
	}

	if !isTerminalTaskStatus(task.Status) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "the task is still running; stop it before continuing in chat",
			"reason":      "task_not_terminal",
			"task_status": task.Status,
		})
		return
	}

	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          task.AgentID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID,
		h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		h.writeDispatchBlocked(w, http.StatusForbidden, ReasonInvocationNotAllowed)
		return
	}

	creatorID := parseUUID(userID)

	// Idempotent replay: a second click reopens the conversation this member
	// already started from this task. Checked before the tx so the common repeat
	// case does not take the workspace lock.
	if existing, err := h.Queries.GetChatSessionContinuingTask(r.Context(), db.GetChatSessionContinuingTaskParams{
		ContinuedFromTaskID: taskUUID,
		CreatorID:           creatorID,
		WorkspaceID:         wsUUID,
	}); err == nil {
		writeJSON(w, http.StatusOK, reopenedContinuation(existing))
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to look up existing conversation")
		return
	}

	sessionID, workDir := resumePointerFromTask(task)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Same create/delete protocol CreateChatSession follows (#5219): take
	// FOR KEY SHARE on the workspace row so a session cannot be created into a
	// workspace whose delete is in progress and then be orphaned by the sweep.
	if _, err := qtx.LockWorkspaceForChatSessionCreate(r.Context(), wsUUID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to lock workspace")
		return
	}

	session, err := qtx.CreateChatSessionContinuingTask(r.Context(), db.CreateChatSessionContinuingTaskParams{
		WorkspaceID:         wsUUID,
		AgentID:             task.AgentID,
		CreatorID:           creatorID,
		Title:               h.continuedChatTitle(r, task),
		RuntimeID:           task.RuntimeID,
		SessionID:           sessionID,
		WorkDir:             workDir,
		ContinuedFromTaskID: taskUUID,
		// project_id is deliberately left unset. It is a chat-specific context
		// selection the member makes, and inheriting one here would drag in
		// LockProjectForChatSessionCreate plus a "project was deleted meanwhile"
		// failure path for no gain: what carries the run's context into the
		// conversation is the provider session and work_dir, not the project
		// pointer. The member can pick a project in the chat if they want one.
		ProjectID: pgtype.UUID{},
	})
	if err != nil {
		// A concurrent double-click loses the unique-index race rather than
		// creating a fork; hand back the winner. Postgres blocks the second
		// inserter until the first transaction resolves and only raises the
		// violation when that first one COMMITTED, so the winning row is
		// already visible to a new snapshot.
		//
		// Read through h.Queries (the pool), NOT qtx: this transaction is
		// aborted by the failed insert and would reject any further query.
		// Do not "tidy" this into qtx.
		if existing, lookupErr := h.Queries.GetChatSessionContinuingTask(r.Context(), db.GetChatSessionContinuingTaskParams{
			ContinuedFromTaskID: taskUUID,
			CreatorID:           creatorID,
			WorkspaceID:         wsUUID,
		}); lookupErr == nil {
			writeJSON(w, http.StatusOK, reopenedContinuation(existing))
			return
		}
		slog.Warn("continue task in chat: create session failed",
			"task_id", uuidToString(taskUUID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit chat session create")
		return
	}

	writeJSON(w, http.StatusCreated, ContinueTaskInChatResponse{
		ChatSession:    chatSessionToResponse(session),
		Reopened:       false,
		SessionCarried: sessionID.Valid,
		WorkDirCarried: workDir.Valid,
	})
}

// reopenedContinuation renders an existing continuation. The carried flags are
// derived from the stored row rather than remembered from the original request,
// so a reopen reports what the conversation actually holds today.
func reopenedContinuation(existing db.ChatSession) ContinueTaskInChatResponse {
	return ContinueTaskInChatResponse{
		ChatSession:    chatSessionToResponse(existing),
		Reopened:       true,
		SessionCarried: existing.SessionID.Valid && strings.TrimSpace(existing.SessionID.String) != "",
		WorkDirCarried: existing.WorkDir.Valid && strings.TrimSpace(existing.WorkDir.String) != "",
	}
}

// resumePointerFromTask decides what a continuation may inherit from a finished
// task. The two halves are independent on purpose.
//
// work_dir is offered whenever the task recorded one. Reuse is best-effort
// downstream: when the directory is gone (GC'd) or absent on the claiming
// runtime, execenv falls back to a fresh Prepare, so offering a stale path
// costs nothing.
//
// session_id is withheld when the source task's failure is one a resume cannot
// survive. service.ResumeUnsafeFailure is the same judgment the rerun path
// applies to a specific source task; without it, continuing from a task that
// died on a poisoned conversation would resume that poison into the chat and
// fail the member's first message for reasons they cannot see. Withholding the
// session degrades to a cold start in the same directory, which is recoverable.
func resumePointerFromTask(task db.AgentTaskQueue) (sessionID pgtype.Text, workDir pgtype.Text) {
	if task.WorkDir.Valid && strings.TrimSpace(task.WorkDir.String) != "" {
		workDir = task.WorkDir
	}
	if !task.SessionID.Valid || strings.TrimSpace(task.SessionID.String) == "" {
		return sessionID, workDir
	}
	if service.ResumeUnsafeFailure(task.FailureReason.String, task.Error.String) {
		return sessionID, workDir
	}
	// MUL-5305: a task whose provider session was withheld because its rollout
	// never landed has no resumable conversation, even though session_id is set.
	if task.SessionRolloutMissing {
		return sessionID, workDir
	}
	sessionID = task.SessionID
	return sessionID, workDir
}

// isTerminalTaskStatus mirrors the terminal set used across the task service
// (completed / failed / cancelled). Everything else — queued, dispatched,
// running, waiting_local_directory, deferred — still owns its session and
// workdir.
func isTerminalTaskStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// continuedChatTitle seeds a title so the new conversation is identifiable in
// the chat list without waiting for the auto-titler. The issue title is the most
// recognizable handle a member has for a background run; tasks with no issue
// (autopilot / quick_create) fall back to their trigger summary and finally to a
// generic label. A lookup failure is not worth failing the request over.
func (h *Handler) continuedChatTitle(r *http.Request, task db.AgentTaskQueue) string {
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), task.IssueID); err == nil {
			if title := strings.TrimSpace(issue.Title); title != "" {
				return truncateChatTitle(title)
			}
		}
	}
	if summary := strings.TrimSpace(task.TriggerSummary.String); summary != "" {
		return truncateChatTitle(summary)
	}
	return "Continued run"
}

func truncateChatTitle(s string) string {
	runes := []rune(s)
	if len(runes) <= continueTaskInChatTitleLimit {
		return s
	}
	return strings.TrimSpace(string(runes[:continueTaskInChatTitleLimit])) + "…"
}
