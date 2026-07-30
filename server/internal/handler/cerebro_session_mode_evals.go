package handler

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SessionModeEvalRunner runs one evaluation from the workspace eval catalog
// against an issue. It is satisfied by *evals.Store, which is wired with the
// same server-side run executor the eval Run-now button and the Workflow hooks
// use, so a Mode's evaluations execute through exactly one runner.
type SessionModeEvalRunner interface {
	RunForIssue(ctx context.Context, workspaceID, actorID, evalID, issueID uuid.UUID, actorType string) (string, string, error)
}

// runSessionModeEvals executes the evaluations configured on the Mode of the
// session this task belonged to, against the task's issue.
//
// FIR-4047: the Mode's evaluations field used to hold skill IDs and nothing ever
// ran. It now names rows in cerebro_eval, and this is where they run. The runs
// land in the normal eval history (cerebro_eval_run, scoped to the issue), so
// results are visible where every other eval result is.
//
// Advisory on purpose: a failed evaluation is recorded, never turned into a
// failed task. Blocking on an eval is the Workflow delivery gate's job, which
// already exists and is authored per workflow phase. Every error here is logged
// and swallowed — completing a task must not depend on the eval runner.
func (h *Handler) runSessionModeEvals(ctx context.Context, workspaceID string, task *db.AgentTaskQueue) {
	if h.SessionModeEvalRunner == nil || h.SessionModeProfiles == nil {
		return
	}
	if workspaceID == "" || !task.IssueID.Valid || !task.TriggerCommentID.Valid {
		return
	}

	mode, ok := h.sessionModeForTask(ctx, task)
	if !ok {
		return
	}
	config, err := h.SessionModeProfiles.Active(ctx, parseUUID(workspaceID), mode)
	if err != nil || len(config.EvalIDs) == 0 {
		return
	}

	wsID, err := uuid.Parse(workspaceID)
	if err != nil {
		return
	}
	issueID, err := uuid.Parse(uuidToString(task.IssueID))
	if err != nil {
		return
	}
	actorID, _ := uuid.Parse(uuidToString(task.AgentID))
	for _, raw := range config.EvalIDs {
		evalID, err := uuid.Parse(raw)
		if err != nil {
			slog.Warn("session mode eval: unusable eval id", "mode", mode, "eval_id", raw)
			continue
		}
		runID, status, err := h.SessionModeEvalRunner.RunForIssue(ctx, wsID, actorID, evalID, issueID, "agent")
		if err != nil {
			slog.Warn("session mode eval: run failed", "mode", mode, "eval_id", raw, "issue_id", uuidToString(task.IssueID), "error", err)
			continue
		}
		slog.Info("session mode eval: run recorded", "mode", mode, "eval_id", raw, "run_id", runID, "status", status)
	}
}

// sessionModeForTask resolves the Mode of the session this task ran in. The Mode
// lives on cerebro_session, keyed by issue plus the thread's root comment, so
// the trigger comment has to be walked up to its thread root first.
func (h *Handler) sessionModeForTask(ctx context.Context, task *db.AgentTaskQueue) (sessionmode.Mode, bool) {
	comment, err := h.Queries.GetComment(ctx, task.TriggerCommentID)
	if err != nil {
		return "", false
	}
	rootID := comment.ID
	if comment.ParentID.Valid {
		rootID = comment.ParentID
	}
	var raw string
	if err := h.DB.QueryRow(ctx, `
		SELECT mode FROM cerebro_session
		WHERE issue_id = $1 AND root_comment_id = $2`,
		comment.IssueID, rootID).Scan(&raw); err != nil {
		return "", false
	}
	return sessionmode.Normalize(raw)
}
