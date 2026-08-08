package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// RunEnqueueSource identifies which kind of issue write would start an agent
// run. It is surfaced in preview responses so the UI can explain each trigger.
type RunEnqueueSource string

const (
	// RunSourceAssign covers issue creation and assignee changes — the issue
	// is being handed to an agent/squad. Parks silently on backlog.
	RunSourceAssign RunEnqueueSource = "assign"
	// RunSourceStatus covers a status-only move that hands an already-assigned
	// issue back to its agent: promoting out of backlog, or reopening a
	// reviewed/closed issue into todo.
	RunSourceStatus RunEnqueueSource = "status"
)

// isRunEnqueueingStatusMove reports whether a status-only change on an
// already-assigned issue hands the work back to the agent. Two distinct moves
// qualify, and they are deliberately asymmetric:
//
//   - backlog → anything active: promotion out of the parking lot. The issue
//     was never dispatched, so any landing status other than the terminal ones
//     means "start now".
//   - in_review / done / cancelled → todo: a reopen. The reviewer rejecting a
//     delivery, or anyone pulling a closed issue back into the ready queue, is
//     re-dispatching it to the same assignee. Only `todo` counts here: it is
//     the one status that means "this is ready to be worked", whereas moving a
//     reviewed issue to in_progress/blocked/backlog is bookkeeping, not a
//     hand-off.
//
// Before the reopen arm existed, a rework request with an unchanged assignee
// produced no task at all and the returned issue sat silently in the queue
// until someone re-@mentioned the agent.
//
// Every other move (in_progress → todo churn, → backlog parking, → in_review
// hand-off to a reviewer) stays inert, so parking and reviewing keep their
// no-wake semantics.
func isRunEnqueueingStatusMove(prev, next string) bool {
	switch prev {
	case "backlog":
		return next != "done" && next != "cancelled"
	case "in_review", "done", "cancelled":
		return next == "todo"
	}
	return false
}

// IssueTriggerProbe carries the request-scoped checks WillEnqueueRun cannot
// resolve from issue state alone.
//
// CanAccessAgent is the private-agent gate. The write paths enforce it on the
// enqueue side instead (canDispatchIssueAgent for a direct agent,
// canEnqueueSquadLeader inside the squad enqueue helper, plus the 403 that
// validateAssigneePair raises on an unauthorised assign) and therefore pass an
// allow-all probe so the gate is never duplicated or sunk into the service
// layer. Preview passes the real gate so it never leaks a private agent's
// readiness to a member who cannot see it, and so preview and the write path
// reach the same verdict. A nil func is treated as allow-all.
//
// IsSelfLoop reports whether the status move (promoting out of backlog, or
// reopening into todo) would be the calling agent re-triggering its own running
// task. Only the status source consults it; create and assign never do. A nil
// func means "not a self-loop".
type IssueTriggerProbe struct {
	CanAccessAgent func(agent db.Agent) bool
	IsSelfLoop     func() bool
}

// IssueTriggerInput describes one prospective issue write in its post-write
// shape. AssigneeChanged / StatusChanged mark which fields the write touches;
// IsCreate marks a brand-new issue (no prior task to cancel, no self-loop).
type IssueTriggerInput struct {
	Issue           db.Issue
	PrevStatus      string
	IsCreate        bool
	AssigneeChanged bool
	StatusChanged   bool
}

// IssueRunTrigger is the resolved decision shared by preview and the write
// paths. AgentID is the agent that will actually run — the assignee for an
// agent issue, the squad leader for a squad issue.
type IssueRunTrigger struct {
	IssueID      pgtype.UUID
	AgentID      pgtype.UUID
	AssigneeType string
	Source       RunEnqueueSource
}

func allowAllAgents(db.Agent) bool { return true }

// WillEnqueueRun is the single predicate answering "will this issue write
// start an agent run, and for whom". It is the one source of truth shared by
// the issue update / batch-update write paths and the preview endpoint,
// replacing the per-site copies that drifted (squad omitted, self-loop
// omitted, four entry points inconsistent — see MUL-3375).
//
// It is intentionally a distinct predicate from the comment trigger
// (assignee fallback comment routing): issue writes park on backlog while comments fire
// in any status. The two only share leaf readiness checks (AgentReadiness,
// the pending-task dedup), not the top-level decision.
//
// The decision must equal the real enqueue conditions so preview never claims
// a net-new run that the write path then drops. The write enqueues through
// CreateAgentTask, guarded by the (issue_id, agent_id) partial unique index
// over pending (queued/dispatched) tasks; the pending check below mirrors that
// guard, and only the status source needs it:
//   - status source (backlog → active, or reopen → todo) can re-fire against an
//     assignee that already holds a pending task (e.g. one a @mention raised
//     while the issue sat in backlog, or a rework request flipped twice); the
//     check keeps preview from promising a run the unique index would coalesce
//     away.
//   - assign source (create / assignee change) skips the check: a create
//     targets a fresh issue with no prior task, and a reassignment no longer
//     cancels existing tasks (#4963 / MUL-4113) — in the rare case the new
//     assignee already holds a pending task the insert simply no-ops on the
//     same unique index, so the assignee still ends up with one pending run.
func (s *IssueService) WillEnqueueRun(ctx context.Context, in IssueTriggerInput, probe IssueTriggerProbe) (IssueRunTrigger, bool) {
	issue := in.Issue
	if !issue.AssigneeType.Valid || !issue.AssigneeID.Valid {
		return IssueRunTrigger{}, false
	}
	canAccess := probe.CanAccessAgent
	if canAccess == nil {
		canAccess = allowAllAgents
	}

	var source RunEnqueueSource
	switch {
	case in.IsCreate || in.AssigneeChanged:
		// Backlog is the parking lot: assigning into backlog never starts a run.
		if issue.Status == "backlog" {
			return IssueRunTrigger{}, false
		}
		source = RunSourceAssign
	case in.StatusChanged && isRunEnqueueingStatusMove(in.PrevStatus, issue.Status):
		if probe.IsSelfLoop != nil && probe.IsSelfLoop() {
			return IssueRunTrigger{}, false
		}
		source = RunSourceStatus
	default:
		return IssueRunTrigger{}, false
	}

	switch issue.AssigneeType.String {
	case "agent":
		agent, err := s.Queries.GetAgent(ctx, issue.AssigneeID)
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return IssueRunTrigger{}, false
		}
		if !canAccess(agent) {
			return IssueRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, issue.ID, issue.AssigneeID) {
			return IssueRunTrigger{}, false
		}
		return IssueRunTrigger{
			IssueID:      issue.ID,
			AgentID:      issue.AssigneeID,
			AssigneeType: "agent",
			Source:       source,
		}, true

	case "squad":
		squad, err := s.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
			ID:          issue.AssigneeID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			return IssueRunTrigger{}, false
		}
		leader, err := s.Queries.GetAgent(ctx, squad.LeaderID)
		if err != nil {
			return IssueRunTrigger{}, false
		}
		ready, _, err := AgentReadiness(ctx, s.Queries, leader)
		if err != nil || !ready {
			return IssueRunTrigger{}, false
		}
		if !canAccess(leader) {
			return IssueRunTrigger{}, false
		}
		if source == RunSourceStatus && s.hasPendingRun(ctx, issue.ID, squad.LeaderID) {
			return IssueRunTrigger{}, false
		}
		return IssueRunTrigger{
			IssueID:      issue.ID,
			AgentID:      squad.LeaderID,
			AssigneeType: "squad",
			Source:       source,
		}, true
	}
	return IssueRunTrigger{}, false
}

// hasPendingRun reports whether the agent already holds a queued or dispatched
// task for the issue (the (issue_id, agent_id) unique-index slot). Errors fail
// closed to "pending" so preview never over-promises a run.
func (s *IssueService) hasPendingRun(ctx context.Context, issueID, agentID pgtype.UUID) bool {
	pending, err := s.Queries.HasPendingTaskForIssueAndAgent(ctx, db.HasPendingTaskForIssueAndAgentParams{
		IssueID: issueID,
		AgentID: agentID,
		// Key dedup on the reviewed head so a pending run against an old HEAD
		// does not shadow a request after HEAD advanced (TEN-356).
		HeadSha: headShaText(s.TaskService.ResolveIssueReviewSHA(ctx, issueID)),
	})
	if err != nil {
		return true
	}
	return pending
}
