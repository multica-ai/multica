package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

type AdvanceWorkflowParams struct {
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	ClosedStage int32
	Actor       WorkflowActor
}

type ResumeWorkflowParams struct {
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	Actor       WorkflowActor
}

func (s *WorkflowService) AdvanceFromClosedStage(ctx context.Context, p AdvanceWorkflowParams) (WorkflowMutationResult, error) {
	if p.ClosedStage < 1 {
		return WorkflowMutationResult{}, fmt.Errorf("%w: closed stage must be positive", ErrWorkflowConflict)
	}
	return s.reconcile(ctx, reconcileWorkflowParams{
		WorkspaceID: p.WorkspaceID,
		IssueID:     p.IssueID,
		ClosedStage: pgtype.Int4{Int32: p.ClosedStage, Valid: true},
		Actor:       p.Actor,
	})
}

func (s *WorkflowService) Resume(ctx context.Context, p ResumeWorkflowParams) (WorkflowMutationResult, error) {
	return s.reconcile(ctx, reconcileWorkflowParams{
		WorkspaceID: p.WorkspaceID,
		IssueID:     p.IssueID,
		Actor:       p.Actor,
	})
}

type reconcileWorkflowParams struct {
	WorkspaceID pgtype.UUID
	IssueID     pgtype.UUID
	ClosedStage pgtype.Int4
	Actor       WorkflowActor
}

func (s *WorkflowService) reconcile(ctx context.Context, p reconcileWorkflowParams) (WorkflowMutationResult, error) {
	if !validWorkflowActor(p.Actor) {
		return WorkflowMutationResult{}, fmt.Errorf("%w: invalid workflow actor", ErrWorkflowConflict)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("begin workflow reconcile tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	run, err := qtx.LockActiveWorkflowRunForIssue(ctx, db.LockActiveWorkflowRunForIssueParams{
		WorkspaceID: p.WorkspaceID,
		IssueID:     p.IssueID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowMutationResult{}, ErrWorkflowRunNotFound
	}
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock active workflow run: %w", err)
	}

	parent, err := qtx.LockWorkflowParent(ctx, db.LockWorkflowParentParams{ID: p.IssueID, WorkspaceID: p.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowMutationResult{}, ErrWorkflowRunNotFound
	}
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock workflow parent: %w", err)
	}
	children, err := qtx.LockWorkflowChildren(ctx, db.LockWorkflowChildrenParams{
		WorkspaceID:   p.WorkspaceID,
		ParentIssueID: p.IssueID,
	})
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock workflow children: %w", err)
	}
	spec, err := ValidateWorkflowDefinition(run.DefinitionSnapshot)
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("stored workflow snapshot invalid: %w", err)
	}
	if run.CurrentStage < 1 || int(run.CurrentStage) > len(spec.Stages) {
		return WorkflowMutationResult{}, fmt.Errorf("%w: current stage %d outside definition", ErrWorkflowConflict, run.CurrentStage)
	}

	parentEffective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, parent.Status)
	if workflowTerminal(parentEffective) {
		updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
			Status: "cancelled", CurrentStage: run.CurrentStage, Kind: "parent_terminal",
			Actor: p.Actor, CancelledAt: workflowTimestamp(), Payload: map[string]any{"reason": "parent_terminal"},
		})
		if err != nil {
			return WorkflowMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return WorkflowMutationResult{}, fmt.Errorf("commit parent-terminal workflow cancellation: %w", err)
		}
		return WorkflowMutationResult{Run: updated, Transitions: []db.WorkflowTransition{transition}, Outcome: "parent_terminal"}, nil
	}
	if parentEffective == issuestatus.Backlog {
		return WorkflowMutationResult{}, fmt.Errorf("%w: parent returned to backlog", ErrWorkflowConflict)
	}
	stageChildren := make(map[int32][]db.Issue, len(spec.Stages))
	for _, child := range children {
		effective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status)
		if !child.Stage.Valid {
			if !workflowTerminal(effective) {
				return WorkflowMutationResult{}, fmt.Errorf("%w: active child is unstaged", ErrWorkflowConflict)
			}
			continue
		}
		if child.Stage.Int32 < 1 || int(child.Stage.Int32) > len(spec.Stages) {
			return WorkflowMutationResult{}, fmt.Errorf("%w: child stage %d outside definition", ErrWorkflowConflict, child.Stage.Int32)
		}
		stageChildren[child.Stage.Int32] = append(stageChildren[child.Stage.Int32], child)
	}

	if run.Status == "running" {
		if p.ClosedStage.Valid && p.ClosedStage.Int32 < run.CurrentStage {
			return WorkflowMutationResult{Run: run, Outcome: "noop"}, nil
		}
		if p.ClosedStage.Valid && p.ClosedStage.Int32 > run.CurrentStage {
			return WorkflowMutationResult{}, fmt.Errorf("%w: closed stage %d is ahead of current stage %d", ErrWorkflowConflict, p.ClosedStage.Int32, run.CurrentStage)
		}
		current := stageChildren[run.CurrentStage]
		if len(current) == 0 {
			return WorkflowMutationResult{}, fmt.Errorf("%w: running stage %d has no children", ErrWorkflowConflict, run.CurrentStage)
		}
		if !workflowStageAllTerminal(ctx, qtx, p.WorkspaceID, current) {
			return WorkflowMutationResult{Run: run, Outcome: "noop"}, nil
		}
		return s.advanceAfterClosedStage(ctx, tx, qtx, p, spec, run, parent, stageChildren)
	}
	if run.Status != "blocked_materialization" {
		return WorkflowMutationResult{}, fmt.Errorf("%w: unsupported active run status %q", ErrWorkflowConflict, run.Status)
	}
	current := stageChildren[run.CurrentStage]
	if len(current) == 0 {
		return WorkflowMutationResult{Run: run, Outcome: "noop"}, nil
	}
	allTerminal, allPendingBacklog := workflowBlockedStageState(ctx, qtx, p.WorkspaceID, current)
	if allTerminal {
		updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
			Status: "running", CurrentStage: run.CurrentStage, Kind: "stage_satisfied", Actor: p.Actor,
			Payload: map[string]any{"stage": run.CurrentStage, "reason": "materialized_terminal"},
		})
		if err != nil {
			return WorkflowMutationResult{}, err
		}
		result, err := s.advanceAfterClosedStage(ctx, tx, qtx, p, spec, updated, parent, stageChildren)
		if err != nil {
			return WorkflowMutationResult{}, err
		}
		result.Transitions = append([]db.WorkflowTransition{transition}, result.Transitions...)
		return result, nil
	}
	if !allPendingBacklog {
		return WorkflowMutationResult{}, fmt.Errorf("%w: blocked stage %d contains active work", ErrWorkflowConflict, run.CurrentStage)
	}
	return s.materializeBlockedStage(ctx, tx, qtx, p, run, current)
}

func (s *WorkflowService) advanceAfterClosedStage(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	p reconcileWorkflowParams,
	spec WorkflowDefinitionSpec,
	run db.WorkflowRun,
	parent db.Issue,
	stageChildren map[int32][]db.Issue,
) (WorkflowMutationResult, error) {
	transitions := make([]db.WorkflowTransition, 0, 2)
	for nextStage := run.CurrentStage + 1; int(nextStage) <= len(spec.Stages); nextStage++ {
		rows := stageChildren[nextStage]
		if len(rows) == 0 {
			updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
				Status: "blocked_materialization", CurrentStage: nextStage, Kind: "materialization_blocked", Actor: p.Actor,
				Payload: map[string]any{"stage": nextStage, "reason": "stage_not_materialized"},
			})
			if err != nil {
				return WorkflowMutationResult{}, err
			}
			transitions = append(transitions, transition)
			if err := tx.Commit(ctx); err != nil {
				return WorkflowMutationResult{}, fmt.Errorf("commit workflow materialization block: %w", err)
			}
			return WorkflowMutationResult{Run: updated, Transitions: transitions, Outcome: "blocked_materialization"}, nil
		}
		allTerminal, allPendingBacklog := workflowBlockedStageState(ctx, qtx, p.WorkspaceID, rows)
		if allTerminal {
			updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
				Status: "running", CurrentStage: nextStage, Kind: "stage_satisfied", Actor: p.Actor,
				Payload: map[string]any{"stage": nextStage, "reason": "already_terminal"},
			})
			if err != nil {
				return WorkflowMutationResult{}, err
			}
			transitions = append(transitions, transition)
			run = updated
			continue
		}
		if !allPendingBacklog {
			return WorkflowMutationResult{}, fmt.Errorf("%w: stage %d contains active work before activation", ErrWorkflowConflict, nextStage)
		}

		toPromote := make([]db.Issue, 0, len(rows))
		for _, child := range rows {
			effective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status)
			if workflowTerminal(effective) || effective != issuestatus.Backlog {
				continue
			}
			toPromote = append(toPromote, child)
		}
		// Advance the durable run before activating children. The issue-level
		// workflow admission guard only permits non-backlog work in the run's
		// current stage, and the whole transaction still rolls back atomically if
		// any child promotion fails.
		updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
			Status: "running", CurrentStage: nextStage, Kind: "stage_advanced", Actor: p.Actor,
			Payload: map[string]any{"stage": nextStage, "promoted_count": len(toPromote)},
		})
		if err != nil {
			return WorkflowMutationResult{}, err
		}
		changes := make([]WorkflowIssueChange, 0, len(toPromote))
		for _, child := range toPromote {
			after, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: child.ID, Status: issuestatus.Todo, WorkspaceID: p.WorkspaceID})
			if err != nil {
				return WorkflowMutationResult{}, fmt.Errorf("promote workflow stage %d child: %w", nextStage, err)
			}
			changes = append(changes, WorkflowIssueChange{Before: child, After: after})
		}
		transitions = append(transitions, transition)
		if err := tx.Commit(ctx); err != nil {
			return WorkflowMutationResult{}, fmt.Errorf("commit workflow stage advancement: %w", err)
		}
		return WorkflowMutationResult{Run: updated, Transitions: transitions, Changes: changes, Outcome: "stage_advanced"}, nil
	}

	changes := make([]WorkflowIssueChange, 0, 1)
	parentEffective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, parent.Status)
	if parentEffective != issuestatus.InReview {
		after, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: parent.ID, Status: issuestatus.InReview, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return WorkflowMutationResult{}, fmt.Errorf("move workflow parent to review: %w", err)
		}
		changes = append(changes, WorkflowIssueChange{Before: parent, After: after})
	}
	updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
		Status: "completed_pending_review", CurrentStage: run.CurrentStage, Kind: "completed_pending_review", Actor: p.Actor,
		CompletedAt: workflowTimestamp(), Payload: map[string]any{"reason": "all_declared_stages_satisfied"},
	})
	if err != nil {
		return WorkflowMutationResult{}, err
	}
	transitions = append(transitions, transition)
	if err := tx.Commit(ctx); err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("commit workflow completion: %w", err)
	}
	return WorkflowMutationResult{
		Run: updated, Transitions: transitions, Changes: changes, Outcome: "completed_pending_review",
	}, nil
}

func (s *WorkflowService) materializeBlockedStage(
	ctx context.Context,
	tx pgx.Tx,
	qtx *db.Queries,
	p reconcileWorkflowParams,
	run db.WorkflowRun,
	rows []db.Issue,
) (WorkflowMutationResult, error) {
	toPromote := make([]db.Issue, 0, len(rows))
	for _, child := range rows {
		effective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status)
		if workflowTerminal(effective) || effective != issuestatus.Backlog {
			continue
		}
		toPromote = append(toPromote, child)
	}
	updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
		Status: "running", CurrentStage: run.CurrentStage, Kind: "materialized", Actor: p.Actor,
		Payload: map[string]any{"stage": run.CurrentStage, "promoted_count": len(toPromote)},
	})
	if err != nil {
		return WorkflowMutationResult{}, err
	}
	changes := make([]WorkflowIssueChange, 0, len(toPromote))
	for _, child := range toPromote {
		after, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: child.ID, Status: issuestatus.Todo, WorkspaceID: p.WorkspaceID})
		if err != nil {
			return WorkflowMutationResult{}, fmt.Errorf("activate materialized workflow child: %w", err)
		}
		changes = append(changes, WorkflowIssueChange{Before: child, After: after})
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("commit workflow materialization: %w", err)
	}
	return WorkflowMutationResult{
		Run: updated, Transitions: []db.WorkflowTransition{transition}, Changes: changes, Outcome: "materialized",
	}, nil
}

func workflowStageAllTerminal(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, rows []db.Issue) bool {
	for _, child := range rows {
		if !workflowTerminal(issuestatus.Effective(ctx, q, workspaceID, child.Status)) {
			return false
		}
	}
	return true
}

func workflowBlockedStageState(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, rows []db.Issue) (allTerminal, allPendingBacklog bool) {
	allTerminal = true
	allPendingBacklog = true
	for _, child := range rows {
		effective := issuestatus.Effective(ctx, q, workspaceID, child.Status)
		if workflowTerminal(effective) {
			continue
		}
		allTerminal = false
		if effective != issuestatus.Backlog {
			allPendingBacklog = false
		}
	}
	return allTerminal, allPendingBacklog
}

func validWorkflowActor(actor WorkflowActor) bool {
	switch actor.Type {
	case "system":
		return !actor.ID.Valid
	case "member", "agent":
		return actor.ID.Valid
	default:
		return false
	}
}

func workflowTimestamp() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
}

type workflowTransitionMutation struct {
	Status       string
	CurrentStage int32
	Kind         string
	Actor        WorkflowActor
	CompletedAt  pgtype.Timestamptz
	CancelledAt  pgtype.Timestamptz
	Payload      map[string]any
}

func persistWorkflowTransition(
	ctx context.Context,
	q *db.Queries,
	run db.WorkflowRun,
	m workflowTransitionMutation,
) (db.WorkflowRun, db.WorkflowTransition, error) {
	updated, err := q.UpdateWorkflowRun(ctx, db.UpdateWorkflowRunParams{
		Status: m.Status, CurrentStage: m.CurrentStage,
		CompletedAt: m.CompletedAt, CancelledAt: m.CancelledAt,
		ID: run.ID, WorkspaceID: run.WorkspaceID, ExpectedRevision: run.Revision,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.WorkflowRun{}, db.WorkflowTransition{}, ErrWorkflowConflict
	}
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowTransition{}, fmt.Errorf("update workflow run: %w", err)
	}
	payload := m.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowTransition{}, fmt.Errorf("marshal workflow transition payload: %w", err)
	}
	key := transitionKey(m.Kind, run.CurrentStage, m.CurrentStage, updated.Revision)
	transition, err := q.CreateWorkflowTransition(ctx, db.CreateWorkflowTransitionParams{
		ID:             dbid.NewV7(),
		WorkspaceID:    run.WorkspaceID,
		WorkflowRunID:  run.ID,
		IdempotencyKey: key,
		Kind:           m.Kind,
		FromStage:      pgtype.Int4{Int32: run.CurrentStage, Valid: true},
		ToStage:        pgtype.Int4{Int32: m.CurrentStage, Valid: true},
		FromStatus:     pgtype.Text{String: run.Status, Valid: true},
		ToStatus:       m.Status,
		ActorType:      m.Actor.Type,
		ActorID:        m.Actor.ID,
		Payload:        encoded,
	})
	if err != nil {
		return db.WorkflowRun{}, db.WorkflowTransition{}, fmt.Errorf("append workflow transition: %w", err)
	}
	return updated, transition, nil
}

func transitionKey(kind string, fromStage, toStage int32, nextRevision int64) string {
	return fmt.Sprintf("%s:%d:%d:%d", kind, fromStage, toStage, nextRevision)
}
