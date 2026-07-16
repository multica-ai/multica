package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// orphanRecoveryBatchSize bounds how many orphaned tasks one recover-orphans
// PAGE fails (MUL-4332 review point 2). Registration upserts the runtime back to
// `online`, so the every-tick offline sweep will NOT reap anything this call leaves
// behind (review round 3, point 1); the daemon therefore drains all pages via the
// keyset cursor rather than relying on a follow-up sweep. This bound only caps the
// lock hold and event volume per page.
const orphanRecoveryBatchSize = 500

// RecoverOrphansRequest is the optional body of POST recover-orphans. Empty on the
// first page; the daemon threads back the keyset cursor from the prior page's
// response to fetch the next one. Both fields move together — a partial cursor is
// rejected.
type RecoverOrphansRequest struct {
	CursorCreatedAt string `json:"cursor_created_at,omitempty"`
	CursorID        string `json:"cursor_id,omitempty"`
}

// RecoverOrphansResponse reports one page of recovery. HasMore + NextCursor* let the
// daemon drain the runtime's orphans across pages; NextCursor* advances over every
// candidate this page locked (failed OR skipped-poison), so a page of poison at the
// front cannot pin the drain — the next page steps past it (review round 3, point 1).
type RecoverOrphansResponse struct {
	Orphaned            int    `json:"orphaned"`
	Retried             int    `json:"retried"`
	Skipped             int    `json:"skipped"`
	HasMore             bool   `json:"has_more"`
	NextCursorCreatedAt string `json:"next_cursor_created_at,omitempty"`
	NextCursorID        string `json:"next_cursor_id,omitempty"`
}

// RecoverOrphanedTasks is called by the daemon at startup for each runtime it owns.
// It atomically fails a bounded page of the dispatched/running tasks the server
// still believes belong to that runtime — those the previous daemon process was
// running when it died — emitting each row's task.failed event in the same
// transaction, then runs the shared post-failure pipeline (auto-retry, issue
// rollback, reconcile). The daemon calls it repeatedly, threading the keyset cursor,
// until HasMore is false.
//
// This is the targeted fix for "issue stuck at in_progress when daemon restarts
// mid-task": the runtime heartbeat sweeper takes up to 75s + the in-process task
// timeout (2.5h) to notice such tasks; the daemon knows the moment it comes back up.
// Because registration flips the runtime back online, the offline sweep will not
// catch a row beyond this page (review round 3, point 1) — hence the cursor-driven
// drain instead of a single capped call.
func (h *Handler) RecoverOrphanedTasks(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	if _, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID); !ok {
		return
	}

	// Optional keyset cursor: absent on the first page, threaded back by the daemon
	// for each subsequent page.
	var req RecoverOrphansRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	afterCreatedAt, afterID, ok := parseOrphanCursor(w, req)
	if !ok {
		return
	}

	// Select one page of the runtime's orphaned tasks and fail the resolvable ones
	// with their task.failed events atomically (MUL-4332 review point 2), so
	// daemon-driven recovery converges onto the outbox like the runtime sweepers and
	// an unresolvable poison row cannot block the rest. We capture the raw candidate
	// page (before poison isolation drops rows) to compute has_more and the cursor.
	var candidates []db.AgentTaskQueue
	rows, err := h.TaskService.FailBulkTasksWithEvents(r.Context(),
		func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
			c, e := qtx.SelectOrphanedTasksForRuntime(r.Context(), db.SelectOrphanedTasksForRuntimeParams{
				RuntimeID:      parseUUID(runtimeID),
				AfterCreatedAt: afterCreatedAt,
				AfterID:        afterID,
				MaxPerTick:     orphanRecoveryBatchSize,
			})
			candidates = c
			return c, e
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(r.Context(), db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "daemon restarted while task was in flight", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_recovery", Valid: true},
			})
		})
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

	// A full candidate page means there may be more behind it. The cursor advances
	// over the LAST candidate we locked (failed or skipped), so the next page never
	// re-selects this page — including any poison rows left un-failed at the front.
	resp := RecoverOrphansResponse{
		Orphaned: len(rows),
		Retried:  retried,
		Skipped:  len(candidates) - len(rows),
		HasMore:  len(candidates) == orphanRecoveryBatchSize,
	}
	if resp.HasMore && len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		resp.NextCursorCreatedAt = last.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
		resp.NextCursorID = uuidToString(last.ID)
	}

	if len(candidates) > 0 {
		slog.Info("recover-orphans page",
			"runtime_id", runtimeID,
			"candidates", len(candidates),
			"orphaned", resp.Orphaned,
			"skipped", resp.Skipped,
			"retried", retried,
			"has_more", resp.HasMore,
		)
	}

	writeJSON(w, http.StatusOK, resp)
}

// parseOrphanCursor turns the optional (cursor_created_at, cursor_id) request pair
// into keyset params. Neither present → the first page (both NULL). Both present →
// the keyset. Exactly one present is a malformed cursor and is rejected, so a bad
// client can never silently restart the drain from the beginning (an infinite loop).
func parseOrphanCursor(w http.ResponseWriter, req RecoverOrphansRequest) (pgtype.Timestamptz, pgtype.UUID, bool) {
	if req.CursorCreatedAt == "" && req.CursorID == "" {
		return pgtype.Timestamptz{}, pgtype.UUID{}, true
	}
	if req.CursorCreatedAt == "" || req.CursorID == "" {
		writeError(w, http.StatusBadRequest, "cursor_created_at and cursor_id must be set together")
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, req.CursorCreatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor_created_at")
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	id, ok := parseUUIDOrBadRequest(w, req.CursorID, "cursor_id")
	if !ok {
		return pgtype.Timestamptz{}, pgtype.UUID{}, false
	}
	return pgtype.Timestamptz{Time: ts, Valid: true}, id, true
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
