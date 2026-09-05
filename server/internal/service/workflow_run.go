package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var ErrWorkflowConflict = errors.New("workflow state conflict")
var ErrActiveWorkflowRun = errors.New("active workflow run exists")
var ErrWorkflowDefinitionNotFound = errors.New("workflow definition not found")
var ErrWorkflowRunNotFound = errors.New("workflow run not found")
var ErrWorkflowOrderViolation = errors.New("workflow stage order violation")

type WorkflowActor struct {
	Type string
	ID   pgtype.UUID
}

type WorkflowIssueChange struct {
	Before db.Issue
	After  db.Issue
}

type WorkflowMutationResult struct {
	Run         db.WorkflowRun
	Transitions []db.WorkflowTransition
	Changes     []WorkflowIssueChange
	Outcome     string
}

type StartWorkflowParams struct {
	WorkspaceID  pgtype.UUID
	IssueID      pgtype.UUID
	DefinitionID pgtype.UUID
	Actor        WorkflowActor
}

func (s *WorkflowService) Start(ctx context.Context, p StartWorkflowParams) (WorkflowMutationResult, error) {
	if p.Actor.Type != "member" && p.Actor.Type != "agent" {
		return WorkflowMutationResult{}, fmt.Errorf("%w: invalid workflow actor", ErrWorkflowConflict)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("begin workflow start tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	definition, err := qtx.GetWorkflowDefinitionInWorkspace(ctx, db.GetWorkflowDefinitionInWorkspaceParams{
		ID: p.DefinitionID, WorkspaceID: p.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowMutationResult{}, ErrWorkflowDefinitionNotFound
	}
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("load workflow definition: %w", err)
	}
	spec, err := ValidateWorkflowDefinition(definition.Definition)
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("stored workflow definition invalid: %w", err)
	}

	parent, err := qtx.LockWorkflowParent(ctx, db.LockWorkflowParentParams{ID: p.IssueID, WorkspaceID: p.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkflowMutationResult{}, ErrWorkflowRunNotFound
	}
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock workflow parent: %w", err)
	}
	parentEffective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, parent.Status)
	if parentEffective == issuestatus.Backlog || workflowTerminal(parentEffective) {
		return WorkflowMutationResult{}, fmt.Errorf("%w: parent status %q cannot start workflow", ErrWorkflowConflict, parent.Status)
	}

	if _, err := qtx.GetActiveWorkflowRunForIssue(ctx, db.GetActiveWorkflowRunForIssueParams{
		WorkspaceID: p.WorkspaceID, IssueID: p.IssueID,
	}); err == nil {
		return WorkflowMutationResult{}, ErrActiveWorkflowRun
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return WorkflowMutationResult{}, fmt.Errorf("check active workflow run: %w", err)
	}

	children, err := qtx.LockWorkflowChildren(ctx, db.LockWorkflowChildrenParams{
		WorkspaceID: p.WorkspaceID, ParentIssueID: p.IssueID,
	})
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock workflow children: %w", err)
	}
	stageOneActive := 0
	for _, child := range children {
		effective := issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status)
		terminal := workflowTerminal(effective)
		if child.Stage.Valid && (child.Stage.Int32 < 1 || int(child.Stage.Int32) > len(spec.Stages)) {
			return WorkflowMutationResult{}, fmt.Errorf("%w: child %s stage %d outside definition", ErrWorkflowConflict, util.UUIDToString(child.ID), child.Stage.Int32)
		}
		if terminal {
			continue
		}
		if !child.Stage.Valid {
			return WorkflowMutationResult{}, fmt.Errorf("%w: active child %s is unstaged", ErrWorkflowConflict, util.UUIDToString(child.ID))
		}
		if child.Stage.Int32 == 1 {
			stageOneActive++
			continue
		}
		if effective != issuestatus.Backlog {
			return WorkflowMutationResult{}, fmt.Errorf("%w: later-stage child %s is already active", ErrWorkflowConflict, util.UUIDToString(child.ID))
		}
	}
	if stageOneActive == 0 {
		return WorkflowMutationResult{}, fmt.Errorf("%w: stage 1 has no non-terminal child", ErrWorkflowConflict)
	}

	run, err := qtx.CreateWorkflowRun(ctx, db.CreateWorkflowRunParams{
		ID: dbid.NewV7(), WorkspaceID: p.WorkspaceID, IssueID: p.IssueID,
		WorkflowDefinitionID: definition.ID, DefinitionSnapshot: definition.Definition,
		Status: "running", CurrentStage: 1, StartedByType: p.Actor.Type, StartedByID: p.Actor.ID,
	})
	if err != nil {
		if workflowActiveRunUniqueViolation(err) {
			return WorkflowMutationResult{}, ErrActiveWorkflowRun
		}
		return WorkflowMutationResult{}, fmt.Errorf("create workflow run: %w", err)
	}

	payload, err := json.Marshal(map[string]string{"workflow_definition_id": util.UUIDToString(definition.ID)})
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("marshal workflow start transition: %w", err)
	}
	transition, err := qtx.CreateWorkflowTransition(ctx, db.CreateWorkflowTransitionParams{
		ID: dbid.NewV7(), WorkspaceID: p.WorkspaceID, WorkflowRunID: run.ID,
		IdempotencyKey: "start", Kind: "started",
		ToStage: pgtype.Int4{Int32: 1, Valid: true}, ToStatus: "running",
		ActorType: p.Actor.Type, ActorID: p.Actor.ID, Payload: payload,
	})
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("create workflow start transition: %w", err)
	}

	changes := make([]WorkflowIssueChange, 0)
	for _, child := range children {
		if !child.Stage.Valid || child.Stage.Int32 != 1 || workflowTerminal(issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status)) {
			continue
		}
		if issuestatus.Effective(ctx, qtx, p.WorkspaceID, child.Status) != issuestatus.Backlog {
			continue
		}
		after, err := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
			ID: child.ID, Status: issuestatus.Todo, WorkspaceID: p.WorkspaceID,
		})
		if err != nil {
			return WorkflowMutationResult{}, fmt.Errorf("promote workflow stage 1 child: %w", err)
		}
		changes = append(changes, WorkflowIssueChange{Before: child, After: after})
	}

	if err := tx.Commit(ctx); err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("commit workflow start: %w", err)
	}
	return WorkflowMutationResult{
		Run: run, Transitions: []db.WorkflowTransition{transition}, Changes: changes, Outcome: "started",
	}, nil
}

func workflowTerminal(effective string) bool {
	return effective == issuestatus.Done || effective == issuestatus.Cancelled
}

func workflowActiveRunUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "workflow_run_one_active_per_issue"
}

func IsWorkflowOrderViolation(err error) bool {
	if errors.Is(err, ErrWorkflowOrderViolation) {
		return true
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == "issue_workflow_order_guard"
}

func (s *WorkflowService) Cancel(ctx context.Context, workspaceID, issueID pgtype.UUID, actor WorkflowActor) (WorkflowMutationResult, error) {
	if !validWorkflowActor(actor) {
		return WorkflowMutationResult{}, fmt.Errorf("%w: invalid workflow actor", ErrWorkflowConflict)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("begin workflow cancel tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	run, err := qtx.LockActiveWorkflowRunForIssue(ctx, db.LockActiveWorkflowRunForIssueParams{WorkspaceID: workspaceID, IssueID: issueID})
	if errors.Is(err, pgx.ErrNoRows) {
		latest, latestErr := qtx.GetLatestWorkflowRunForIssue(ctx, db.GetLatestWorkflowRunForIssueParams{WorkspaceID: workspaceID, IssueID: issueID})
		if errors.Is(latestErr, pgx.ErrNoRows) {
			return WorkflowMutationResult{}, ErrWorkflowRunNotFound
		}
		if latestErr != nil {
			return WorkflowMutationResult{}, fmt.Errorf("get latest workflow run for cancel: %w", latestErr)
		}
		if latest.Status == "cancelled" {
			return WorkflowMutationResult{Run: latest, Outcome: "already_cancelled"}, nil
		}
		return WorkflowMutationResult{}, fmt.Errorf("%w: workflow run is already terminal", ErrWorkflowConflict)
	}
	if err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("lock active workflow run for cancel: %w", err)
	}
	updated, transition, err := persistWorkflowTransition(ctx, qtx, run, workflowTransitionMutation{
		Status: "cancelled", CurrentStage: run.CurrentStage, Kind: "cancelled", Actor: actor,
		CancelledAt: workflowTimestamp(), Payload: map[string]any{"reason": "explicit_cancel"},
	})
	if err != nil {
		return WorkflowMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WorkflowMutationResult{}, fmt.Errorf("commit workflow cancel: %w", err)
	}
	return WorkflowMutationResult{
		Run: updated, Transitions: []db.WorkflowTransition{transition}, Outcome: "cancelled",
	}, nil
}
