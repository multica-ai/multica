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
	// RunSourceStatus covers promoting an already-assigned issue out of
	// backlog into an active status.
	RunSourceStatus RunEnqueueSource = "status"
)

// IssueTriggerProbe carries the request-scoped checks WillEnqueueRun cannot
// resolve from issue state alone.
//
// CanAccessAgent is the private-agent gate. The write paths enforce it at the
// HTTP boundary (validateAssigneePair on assign, canEnqueueSquadLeader inside
// the squad enqueue helper) and therefore pass an allow-all probe so the gate
// is never duplicated or sunk into the service layer. Preview passes the real
// gate so it never leaks a private agent's readiness to a member who cannot
// see it. A nil func is treated as allow-all.
//
// IsSelfLoop reports whether promoting this issue out of backlog would be the
// calling agent re-triggering its own running task. Only the status source
// consults it; create and assign never do. A nil func means "not a self-loop".
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
// a run that the write path then drops. In particular:
//   - assign source (create / assignee change) cancels existing tasks before
//     enqueuing, so a pre-existing pending task is moot — not checked here.
//   - status source (backlog → active) enqueues without cancelling, so a live
//     pending task would be blocked by the (issue_id, agent_id) unique index;
//     reflected by the pending check below.
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
	case in.StatusChanged && in.PrevStatus == "backlog" &&
		issue.Status != "done" && issue.Status != "cancelled":
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
