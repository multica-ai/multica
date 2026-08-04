package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/automation"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Read-only Event Hooks debug surface (MUL-4332 PR3, decision 2A): dry-run,
// explain and the correlation query. These reuse the single automation.Evaluate
// evaluator so an explanation can never drift from real execution, and they
// perform NO action and mutate NO durable state.

// ErrHookEventNotFound is returned when a referenced domain event does not exist
// in the workspace.
var ErrHookEventNotFound = errors.New("event not found")

// issueStateReader is the workspace-scoped StateReader the evaluator reads
// current issue state through.
type issueStateReader struct {
	q           *db.Queries
	workspaceID pgtype.UUID
}

func (r *issueStateReader) IssueField(ctx context.Context, issueID, field string) (string, bool, error) {
	uid, err := util.ParseUUID(issueID)
	if err != nil {
		return "", false, nil // a malformed id can never resolve to a workspace issue
	}
	issue, err := r.q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: uid, WorkspaceID: r.workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	switch field {
	case automation.IssueFieldStatus:
		return issue.Status, true, nil
	case automation.IssueFieldAssigneeID:
		return util.UUIDToString(issue.AssigneeID), issue.AssigneeID.Valid, nil
	case automation.IssueFieldParentIssueID:
		return util.UUIDToString(issue.ParentIssueID), issue.ParentIssueID.Valid, nil
	}
	return "", false, nil
}

// DryRun evaluates a candidate hook spec against a historical event, read-only.
// The spec's shape is validated (so garbage is rejected early), the event's
// `when` is matched against its historical payload, and `if` conditions read
// current workspace state.
func (s *HookService) DryRun(ctx context.Context, workspaceID pgtype.UUID, spec automation.HookSpec, eventID pgtype.UUID) (automation.Evaluation, error) {
	if err := automation.Validate(spec); err != nil {
		return automation.Evaluation{}, err
	}
	view, err := s.loadEventView(ctx, workspaceID, eventID)
	if err != nil {
		return automation.Evaluation{}, err
	}
	rev := automation.EvalRevision{
		EventType:  spec.When.Event,
		Match:      spec.When.Match,
		Conditions: spec.If,
		FireMode:   spec.Fire.Mode,
	}
	return automation.Evaluate(ctx, view, rev, &issueStateReader{q: s.Queries, workspaceID: workspaceID})
}

// Explain evaluates a stored hook's revision against a historical event,
// read-only. revisionNumber == 0 explains the active revision.
func (s *HookService) Explain(ctx context.Context, workspaceID, hookID, eventID pgtype.UUID, revisionNumber int32) (automation.Evaluation, error) {
	hook, err := s.Queries.GetHookInWorkspace(ctx, db.GetHookInWorkspaceParams{ID: hookID, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.Evaluation{}, ErrHookNotFound
		}
		return automation.Evaluation{}, err
	}
	var rawRev db.HookRevision
	if revisionNumber > 0 {
		rawRev, err = s.Queries.GetHookRevisionByNumber(ctx, db.GetHookRevisionByNumberParams{HookID: hookID, Revision: revisionNumber})
	} else {
		rawRev, err = s.Queries.GetHookRevision(ctx, hook.ActiveRevisionID)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.Evaluation{}, ErrHookNotFound
		}
		return automation.Evaluation{}, err
	}

	view, err := s.loadEventView(ctx, workspaceID, eventID)
	if err != nil {
		return automation.Evaluation{}, err
	}
	rev, err := revisionToEval(rawRev)
	if err != nil {
		return automation.Evaluation{}, err
	}
	return automation.Evaluate(ctx, view, rev, &issueStateReader{q: s.Queries, workspaceID: workspaceID})
}

// EventsByCorrelation returns up to limit domain events in a correlation chain
// (ordered by seq), for execution-chain debugging. The limit is enforced in the
// query, not by truncating a fully-loaded chain.
func (s *HookService) EventsByCorrelation(ctx context.Context, workspaceID, correlationID pgtype.UUID, limit int32) ([]db.DomainEvent, error) {
	return s.Queries.ListDomainEventsByCorrelation(ctx, db.ListDomainEventsByCorrelationParams{
		WorkspaceID: workspaceID, CorrelationID: correlationID, Limit: limit,
	})
}

func (s *HookService) loadEventView(ctx context.Context, workspaceID, eventID pgtype.UUID) (automation.EventView, error) {
	event, err := s.Queries.GetDomainEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return automation.EventView{}, ErrHookEventNotFound
		}
		return automation.EventView{}, err
	}
	// GetDomainEvent is not workspace-scoped; enforce tenant isolation here.
	if !principalMatches(event.WorkspaceID, workspaceID) {
		return automation.EventView{}, ErrHookEventNotFound
	}
	return eventToView(event)
}

func eventToView(e db.DomainEvent) (automation.EventView, error) {
	var payload map[string]any
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return automation.EventView{}, err
		}
	}
	return automation.EventView{
		Type:      e.Type,
		SubjectID: util.UUIDToString(e.SubjectID),
		ActorType: e.ActorType,
		ActorID:   util.UUIDToString(e.ActorID),
		Payload:   payload,
	}, nil
}

func revisionToEval(rev db.HookRevision) (automation.EvalRevision, error) {
	var conds []automation.ConditionSpec
	if len(rev.Conditions) > 0 {
		if err := json.Unmarshal(rev.Conditions, &conds); err != nil {
			return automation.EvalRevision{}, err
		}
	}
	return automation.EvalRevision{
		EventType:  rev.EventType,
		Match:      rev.Match,
		Conditions: conds,
		FireMode:   rev.FireMode,
	}, nil
}
