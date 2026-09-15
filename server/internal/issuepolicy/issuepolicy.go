// Package issuepolicy resolves pinned workflow state and catalog behavior for
// issue automation policies. Categories and concrete status behaviors stay distinct.
package issuepolicy

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/issueworkflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	AutopilotNone     = "none"
	AutopilotComplete = "complete"
	AutopilotFail     = "fail"
)

// State is the domain state consumers may reason about. Phase and Outcome are
// lifecycle semantics. Behavior remains only as the rollout adapter for
// behaviors that deliberately distinguish in-progress, review, and blocked.
type State struct {
	Phase    string
	Outcome  string
	Behavior string
}

type Querier interface {
	issuestatus.Querier
	GetIssueWorkflowStatusByID(context.Context, db.GetIssueWorkflowStatusByIDParams) (db.IssueWorkflowStatus, error)
	GetIssueWorkflowStatusByLegacyKey(context.Context, db.GetIssueWorkflowStatusByLegacyKeyParams) (db.IssueWorkflowStatus, error)
}

// ResolveIssue reads the stable lifecycle node only when the release flag is
// enabled. The adapter path remains authoritative while the flag is off, and
// is also the rolling-deploy fallback when an older writer left a stale pin.
func ResolveIssue(ctx context.Context, q Querier, issue db.Issue, workflowEnabled bool) State {
	if workflowEnabled && issue.WorkflowID.Valid && issue.WorkflowStatusID.Valid {
		if node, err := q.GetIssueWorkflowStatusByID(ctx, db.GetIssueWorkflowStatusByIDParams{
			WorkspaceID: issue.WorkspaceID,
			WorkflowID:  issue.WorkflowID,
			ID:          issue.WorkflowStatusID,
		}); err == nil && issueworkflow.LegacyProjection(node) == issue.Status {
			return State{
				Phase:    node.Phase,
				Outcome:  text(node.Outcome),
				Behavior: issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status),
			}
		}
	}
	return catalogState(ctx, q, issue.WorkspaceID, issue.Status)
}

// ResolveStatus resolves an arbitrary status key against the issue's pinned
// lifecycle. It is used for the from-side of transition policies.
func ResolveStatus(ctx context.Context, q Querier, workspaceID, workflowID pgtype.UUID, status string, workflowEnabled bool) State {
	if workflowEnabled && workflowID.Valid {
		if node, err := q.GetIssueWorkflowStatusByLegacyKey(ctx, db.GetIssueWorkflowStatusByLegacyKeyParams{
			WorkspaceID: workspaceID,
			WorkflowID:  workflowID,
			LegacyStatusKey: pgtype.Text{
				String: status,
				Valid:  true,
			},
		}); err == nil {
			return State{
				Phase:    node.Phase,
				Outcome:  text(node.Outcome),
				Behavior: issuestatus.Effective(ctx, q, workspaceID, status),
			}
		}
	}
	return catalogState(ctx, q, workspaceID, status)
}

func catalogState(ctx context.Context, q Querier, workspaceID pgtype.UUID, status string) State {
	category := issuestatus.Category(ctx, q, workspaceID, status)
	outcome, _ := issueworkflow.CategoryOutcome(category)
	return State{Phase: category, Outcome: text(outcome), Behavior: issuestatus.Effective(ctx, q, workspaceID, status)}
}

func text(value pgtype.Text) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func (s State) IsParked() bool { return s.Behavior == issuestatus.Backlog }

func (s State) IsTerminal() bool { return s.Outcome == "completed" || s.Outcome == "cancelled" }

func (s State) AllowsRunTrigger() bool { return !s.IsParked() && !s.IsTerminal() }

// AgentOwnsActiveWork is the explicit failure-recovery policy. Review and
// blocked share the started phase but intentionally remain human/external work.
func (s State) AgentOwnsActiveWork() bool { return s.Behavior == issuestatus.InProgress }

func (s State) AutopilotResolution() string {
	if s.Outcome == "completed" {
		return AutopilotComplete
	}
	if s.Outcome == "cancelled" {
		return AutopilotFail
	}
	switch s.Behavior {
	case issuestatus.Done, issuestatus.InReview:
		return AutopilotComplete
	case issuestatus.Cancelled, issuestatus.Blocked:
		return AutopilotFail
	default:
		return AutopilotNone
	}
}

func (s State) DismissesTaskFailure() bool {
	return s.Behavior == issuestatus.InReview || s.IsTerminal()
}
