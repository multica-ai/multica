// Package issuepolicy translates lifecycle state into the explicit decisions
// made by cross-cutting issue workflows. It is the compatibility boundary
// between legacy status categories and the additive Lifecycle model.
package issuepolicy

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuelifecycle"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	AutopilotNone     = "none"
	AutopilotComplete = "complete"
	AutopilotFail     = "fail"
)

// State is the domain state consumers may reason about. Phase and Outcome are
// lifecycle semantics. LegacyCategory remains only as the rollout adapter for
// behaviors that deliberately distinguish in-progress, review, and blocked.
type State struct {
	Phase          string
	Outcome        string
	LegacyCategory string
}

type Querier interface {
	issuestatus.Querier
	GetIssueLifecycleStatusByID(context.Context, db.GetIssueLifecycleStatusByIDParams) (db.IssueLifecycleStatus, error)
	GetIssueLifecycleStatusByLegacyKey(context.Context, db.GetIssueLifecycleStatusByLegacyKeyParams) (db.IssueLifecycleStatus, error)
}

// ResolveIssue reads the stable lifecycle node only when the release flag is
// enabled. The adapter path remains authoritative while the flag is off, and
// is also the rolling-deploy fallback when an older writer left a stale pin.
func ResolveIssue(ctx context.Context, q Querier, issue db.Issue, lifecycleEnabled bool) State {
	if lifecycleEnabled && issue.LifecycleID.Valid && issue.LifecycleStatusID.Valid {
		if node, err := q.GetIssueLifecycleStatusByID(ctx, db.GetIssueLifecycleStatusByIDParams{
			WorkspaceID: issue.WorkspaceID,
			LifecycleID: issue.LifecycleID,
			ID:          issue.LifecycleStatusID,
		}); err == nil && node.LegacyStatusKey.Valid && node.LegacyStatusKey.String == issue.Status {
			return State{
				Phase:          node.Phase,
				Outcome:        text(node.Outcome),
				LegacyCategory: issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status),
			}
		}
	}
	return FromLegacyCategory(issuestatus.Effective(ctx, q, issue.WorkspaceID, issue.Status))
}

// ResolveStatus resolves an arbitrary status key against the issue's pinned
// lifecycle. It is used for the from-side of transition policies.
func ResolveStatus(ctx context.Context, q Querier, workspaceID, lifecycleID pgtype.UUID, status string, lifecycleEnabled bool) State {
	if lifecycleEnabled && lifecycleID.Valid {
		if node, err := q.GetIssueLifecycleStatusByLegacyKey(ctx, db.GetIssueLifecycleStatusByLegacyKeyParams{
			WorkspaceID: workspaceID,
			LifecycleID: lifecycleID,
			LegacyStatusKey: pgtype.Text{
				String: status,
				Valid:  true,
			},
		}); err == nil {
			return State{
				Phase:          node.Phase,
				Outcome:        text(node.Outcome),
				LegacyCategory: issuestatus.Effective(ctx, q, workspaceID, status),
			}
		}
	}
	return FromLegacyCategory(issuestatus.Effective(ctx, q, workspaceID, status))
}

func FromLegacyCategory(category string) State {
	phase, outcome, err := issuelifecycle.LegacyCategoryPhase(category)
	if err != nil {
		return State{LegacyCategory: category}
	}
	return State{Phase: phase, Outcome: text(outcome), LegacyCategory: category}
}

func text(value pgtype.Text) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func (s State) IsParked() bool { return s.Phase == issuelifecycle.PhaseBacklog }

func (s State) IsTerminal() bool { return s.Outcome == "completed" || s.Outcome == "cancelled" }

func (s State) AllowsRunTrigger() bool { return !s.IsParked() && !s.IsTerminal() }

// AgentOwnsActiveWork is the explicit failure-recovery policy. Review and
// blocked share the started phase but intentionally remain human/external work.
func (s State) AgentOwnsActiveWork() bool { return s.LegacyCategory == issuestatus.InProgress }

func (s State) AutopilotResolution() string {
	switch s.LegacyCategory {
	case issuestatus.Done, issuestatus.InReview:
		return AutopilotComplete
	case issuestatus.Cancelled, issuestatus.Blocked:
		return AutopilotFail
	default:
		return AutopilotNone
	}
}

func (s State) DismissesTaskFailure() bool {
	return s.LegacyCategory == issuestatus.InReview || s.IsTerminal()
}
