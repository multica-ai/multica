package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workerReopenGuard struct {
	actorID            string
	targetStatus       string
	targetAssigneeType pgtype.Text
	targetAssigneeID   pgtype.UUID
}

type workerReopenDeniedError struct {
	status  int
	message string
}

func (e workerReopenDeniedError) Error() string { return e.message }

// enforceWorkerReopenOnLockedIssue is the authoritative check. Its caller
// must hold the issue row lock in the same transaction that performs the
// update, so a competing task insert or issue mutation cannot pass the check
// and then win the claim before the renewal is committed.
func enforceWorkerReopenOnLockedIssue(
	ctx context.Context,
	queries *db.Queries,
	issue db.Issue,
	guard workerReopenGuard,
) error {
	workerReopen, status, message := authorizeWorkerReopenWithQueries(
		ctx, queries, issue, guard.targetStatus, "agent", guard.actorID,
		guard.targetAssigneeType, guard.targetAssigneeID,
	)
	if status != 0 {
		return workerReopenDeniedError{status: status, message: message}
	}
	if !workerReopen {
		// The guard is also installed for ordinary agent status mutations so a
		// row that becomes terminal after advisory preflight cannot turn into an
		// unrenewed reopen. Non-terminal transitions remain on their existing
		// path and do not need a claim renewal.
		return nil
	}
	return nil
}

// authorizeWorkerReopen admits the narrow worker-only reopen path. A trusted
// worker may reopen a terminal issue only if the resulting issue remains
// assigned to that worker and the (issue, worker) claim has no active task.
// The caller distinguishes a non-worker reopen (which keeps the existing
// member behavior) from a rejected worker reopen using the returned status.
// Query failures fail closed: a worker must never reopen when claim occupancy
// cannot be verified.
func (h *Handler) authorizeWorkerReopen(
	ctx context.Context,
	issue db.Issue,
	targetStatus string,
	actorType string,
	actorID string,
	targetAssigneeType pgtype.Text,
	targetAssigneeID pgtype.UUID,
) (workerReopen bool, status int, message string) {
	return authorizeWorkerReopenWithQueries(
		ctx, h.Queries, issue, targetStatus, actorType, actorID,
		targetAssigneeType, targetAssigneeID,
	)
}

func authorizeWorkerReopenWithQueries(
	ctx context.Context,
	queries *db.Queries,
	issue db.Issue,
	targetStatus string,
	actorType string,
	actorID string,
	targetAssigneeType pgtype.Text,
	targetAssigneeID pgtype.UUID,
) (workerReopen bool, status int, message string) {
	if actorType != "agent" {
		return false, 0, ""
	}
	previousStatus := issuestatus.Effective(ctx, queries, issue.WorkspaceID, issue.Status)
	nextStatus := issuestatus.Effective(ctx, queries, issue.WorkspaceID, targetStatus)
	return authorizeWorkerReopenWithEffectiveStatuses(
		ctx, issue, targetStatus, actorType, actorID, targetAssigneeType,
		targetAssigneeID, previousStatus, nextStatus, queries,
	)
}

func (h *Handler) authorizeWorkerReopenWithEffectiveStatuses(
	ctx context.Context,
	issue db.Issue,
	targetStatus string,
	actorType string,
	actorID string,
	targetAssigneeType pgtype.Text,
	targetAssigneeID pgtype.UUID,
	previousStatus string,
	nextStatus string,
) (workerReopen bool, status int, message string) {
	return authorizeWorkerReopenWithEffectiveStatuses(
		ctx, issue, targetStatus, actorType, actorID, targetAssigneeType,
		targetAssigneeID, previousStatus, nextStatus, h.Queries,
	)
}

func authorizeWorkerReopenWithEffectiveStatuses(
	ctx context.Context,
	issue db.Issue,
	targetStatus string,
	actorType string,
	actorID string,
	targetAssigneeType pgtype.Text,
	targetAssigneeID pgtype.UUID,
	previousStatus string,
	nextStatus string,
	queries *db.Queries,
) (workerReopen bool, status int, message string) {
	if actorType != "agent" {
		return false, 0, ""
	}
	if !isIssueReopenTerminalStatus(previousStatus) || isIssueReopenTerminalStatus(nextStatus) {
		return false, 0, ""
	}

	// Backlog is deliberately a parking state. It cannot renew an agent claim,
	// so a worker cannot use this path to move a completed issue there.
	if nextStatus == "backlog" {
		slog.Info("worker reopen denied: target status cannot renew claim",
			"issue_id", uuidToString(issue.ID), "agent_id", actorID, "target_status", targetStatus)
		return false, http.StatusForbidden, "worker reopen must target an active status"
	}

	if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" ||
		!issue.AssigneeID.Valid || actorID == "" || actorID != uuidToString(issue.AssigneeID) ||
		!targetAssigneeType.Valid || targetAssigneeType.String != "agent" ||
		!targetAssigneeID.Valid || actorID != uuidToString(targetAssigneeID) {
		slog.Info("worker reopen denied: worker does not own issue claim",
			"issue_id", uuidToString(issue.ID), "agent_id", actorID)
		return false, http.StatusForbidden, "only the assigned worker can reopen this issue"
	}

	active, err := queries.HasActiveTaskForIssueAndAgent(ctx, db.HasActiveTaskForIssueAndAgentParams{
		IssueID: issue.ID,
		AgentID: targetAssigneeID,
	})
	if err != nil {
		slog.Warn("worker reopen denied: claim occupancy unavailable",
			"issue_id", uuidToString(issue.ID), "agent_id", actorID, "error", err)
		return false, http.StatusServiceUnavailable, "cannot verify whether the worker claim is active"
	}
	if active {
		slog.Info("worker reopen denied: issue claim is active",
			"issue_id", uuidToString(issue.ID), "agent_id", actorID)
		return false, http.StatusConflict, "cannot reopen an issue while its worker claim is active"
	}

	slog.Info("worker reopen authorized",
		"issue_id", uuidToString(issue.ID), "agent_id", actorID, "target_status", targetStatus)
	return true, 0, ""
}

func isIssueReopenTerminalStatus(status string) bool {
	return status == "done" || status == "cancelled"
}
