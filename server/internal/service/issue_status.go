package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/domainevent"
	"github.com/multica-ai/multica/server/internal/issueevent"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The shared, transaction-aware issue-status-change command (MUL-4332 review a′).
//
// Before this, a status change was written four different ways — the single and
// batch HTTP handlers, the GitHub-merge path, and the hook executor — each locking,
// updating and emitting the domain event on its own. The executor's copy did the
// write but none of the side effects a manual change drives, so an automated status
// change silently skipped realtime / activity / inbox / autopilot. This primitive is
// the one in-transaction write every path routes through, and IssueTransition is the
// one typed result they publish.
//
// It is split into an IN-TRANSACTION half (applyIssueStatusChangeInTx: lock, write,
// build the event + typed payload) and a POST-COMMIT half (the caller publishes the
// returned transition once its transaction commits). The realtime / activity / inbox
// / subscriber fanout is best-effort (the client refetch self-heals realtime, and a
// dropped activity/inbox row is acceptable — review a′). The DURABLE reactions Elon
// gated execution on — the assignee-run enqueue and the Autopilot run sync — are NOT
// part of this best-effort publish; they must land in the transition transaction and
// are a following slice.

// ErrIssueNotInWorkspace is returned when the target issue does not exist in the
// change's workspace. Callers map it to their own terminal / not-found outcome.
var ErrIssueNotInWorkspace = errors.New("issue not in workspace")

// IssueChangeCausation threads a reaction's emitted event into its originating
// correlation chain so the depth guard can see the chain grow. It is the zero value
// for a root (human/GitHub) change, which starts a fresh correlation.
type IssueChangeCausation struct {
	CorrelationID pgtype.UUID
	ExecutionID   pgtype.UUID
	ActionIndex   pgtype.Int4
	HopCount      int32
}

// IssueStatusChange is one authoritative status transition. Actor labels who is
// making it on the emitted domain event. It deliberately carries no hook UUID as an
// authorization principal — when the durable assignee-enqueue lands, its
// authorization/attribution takes the hook's STORED principal, never the hook
// identity (review a′).
type IssueStatusChange struct {
	IssueID     pgtype.UUID
	WorkspaceID pgtype.UUID
	ToStatus    string
	Actor       domainevent.Actor
	Causation   IssueChangeCausation
}

// IssueTransition is the result of one status change: the locked before/after
// pre-image, whether the status actually moved, and the typed issue:updated payload
// the caller publishes post-commit.
//
// BusActorType/BusActorID are the NORMALIZED actor for the best-effort side effects,
// always one of member|agent|system so they satisfy the activity_log / inbox
// actor_type CHECK. A hook-driven change maps to system, with its hook identity kept
// as explicit attribution on Payload.Automation and durably on the domain event's
// actor_type='hook'. Publishing the raw hook actor here would silently fail the
// activity_log CHECK and drop the row (review round: activity actor contract).
type IssueTransition struct {
	Before       db.Issue
	After        db.Issue
	Changed      bool
	BusActorType string
	BusActorID   string
	Payload      issueevent.IssueUpdatedPayload
}

// sideEffectActor maps a change's actor to the (type, id) the activity_log / inbox
// actor_type CHECK permits (member|agent|system), plus explicit automation
// attribution for anything else. A hook — or any future automation actor — is
// recorded as system on the side-effect path, its identity preserved as an
// AutomationSource, while the durable domain_event keeps the true actor.
func sideEffectActor(a domainevent.Actor) (actorType, actorID string, automation *issueevent.AutomationSource) {
	switch a.Type {
	case domainevent.ActorMember, domainevent.ActorAgent:
		return a.Type, util.UUIDToString(a.ID), nil
	case domainevent.ActorSystem, "":
		return domainevent.ActorSystem, "", nil
	default:
		return domainevent.ActorSystem, "", &issueevent.AutomationSource{Type: a.Type, ID: util.UUIDToString(a.ID)}
	}
}

// applyIssueStatusChangeInTx locks the issue (workspace-scoped, so a change can never
// cross tenants), writes the new status, computes the diff from the LOCKED
// before/after, and builds the causation-stamped issue.status_changed event and the
// typed payload. It writes NO event and runs NO side effect: the caller's transaction
// commits the returned events, and publishes the transition afterward. A no-op
// (unchanged status) returns Changed=false and no event.
func applyIssueStatusChangeInTx(ctx context.Context, qtx *db.Queries, prefix string, ch IssueStatusChange) (IssueTransition, []domainevent.Event, error) {
	before, err := qtx.LockIssueRowForUpdate(ctx, db.LockIssueRowForUpdateParams{ID: ch.IssueID, WorkspaceID: ch.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return IssueTransition{}, nil, ErrIssueNotInWorkspace
		}
		return IssueTransition{}, nil, err
	}

	busType, busID, automation := sideEffectActor(ch.Actor)
	tr := IssueTransition{Before: before, After: before, BusActorType: busType, BusActorID: busID}

	// A no-op is a TRUE no-op: return under the lock before writing, so the target's
	// updated_at is not bumped and nothing downstream is re-driven (review nit).
	if before.Status == ch.ToStatus {
		return tr, nil, nil
	}

	after, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID: ch.IssueID, WorkspaceID: ch.WorkspaceID, Status: ch.ToStatus,
	})
	if err != nil {
		return IssueTransition{}, nil, err
	}
	tr.After = after
	tr.Changed = true
	tr.Payload = issueevent.Build(before, after, issueToMap(after, prefix), true)
	tr.Payload.Automation = automation

	evt := domainevent.IssueStatusChanged(ch.WorkspaceID, ch.IssueID, ch.Actor,
		domainevent.IssueStatusChangedPayload{From: before.Status, To: after.Status})
	if ch.Causation.CorrelationID.Valid {
		evt.CorrelationID = ch.Causation.CorrelationID
		evt.CausationExecutionID = ch.Causation.ExecutionID
		evt.CausationActionIndex = ch.Causation.ActionIndex
		evt.HopCount = ch.Causation.HopCount
	}
	return tr, []domainevent.Event{evt}, nil
}

// issuePrefixInTx reads a workspace's issue prefix for the client-facing issue
// representation. A missing prefix is non-fatal (the client re-fetches).
func issuePrefixInTx(ctx context.Context, qtx *db.Queries, workspaceID pgtype.UUID) string {
	ws, err := qtx.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return ""
	}
	return ws.IssuePrefix
}
