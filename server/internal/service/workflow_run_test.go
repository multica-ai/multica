package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

const twoStageWorkflow = `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`

func startWorkflow(t *testing.T, fx *workflowTestFixture, definitionID, issueID pgtype.UUID) (WorkflowMutationResult, error) {
	t.Helper()
	return fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID:  fx.workspaceID,
		IssueID:      issueID,
		DefinitionID: definitionID,
		Actor:        WorkflowActor{Type: "member", ID: fx.userID},
	})
}

func TestWorkflowStartPromotesOnlyStageOneBacklog(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Release", twoStageWorkflow)
	parent := fx.createParent(t, "todo")
	s1 := fx.createChild(t, parent.ID, 1, "backlog")
	s1Active := fx.createChild(t, parent.ID, 1, "in_progress")
	s2 := fx.createChild(t, parent.ID, 2, "backlog")
	historical := fx.createIssue(t, parent.ID, pgtype.Int4{}, "done", "historical child")

	got, err := startWorkflow(t, fx, def.ID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "running" || got.Run.CurrentStage != 1 {
		t.Fatalf("run = %+v", got.Run)
	}
	assertWorkflowIssueStatus(t, fx.pool, s1.ID, "todo")
	assertWorkflowIssueStatus(t, fx.pool, s1Active.ID, "in_progress")
	assertWorkflowIssueStatus(t, fx.pool, s2.ID, "backlog")
	assertWorkflowIssueStatus(t, fx.pool, historical.ID, "done")
	if len(got.Changes) != 1 || got.Changes[0].Before.Status != "backlog" || got.Changes[0].After.Status != "todo" {
		t.Fatalf("changes = %+v", got.Changes)
	}
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "started" || got.Outcome != "started" {
		t.Fatalf("transition/outcome = %+v / %q", got.Transitions, got.Outcome)
	}
}

func TestWorkflowStartRejectsInvalidTopology(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *workflowTestFixture, pgtype.UUID)
	}{
		{"unstaged active child", func(t *testing.T, fx *workflowTestFixture, parentID pgtype.UUID) {
			fx.createIssue(t, parentID, pgtype.Int4{}, "backlog", "unstaged")
		}},
		{"stage beyond definition", func(t *testing.T, fx *workflowTestFixture, parentID pgtype.UUID) {
			fx.createChild(t, parentID, 3, "backlog")
		}},
		{"later stage already active", func(t *testing.T, fx *workflowTestFixture, parentID pgtype.UUID) {
			fx.createChild(t, parentID, 1, "backlog")
			fx.createChild(t, parentID, 2, "todo")
		}},
		{"stage one has no active work", func(t *testing.T, fx *workflowTestFixture, parentID pgtype.UUID) {
			fx.createChild(t, parentID, 2, "backlog")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newWorkflowTestFixture(t)
			def := fx.createDefinition(t, "Release", twoStageWorkflow)
			parent := fx.createParent(t, "todo")
			tc.setup(t, fx, parent.ID)
			_, err := startWorkflow(t, fx, def.ID, parent.ID)
			if !errors.Is(err, ErrWorkflowConflict) {
				t.Fatalf("err = %v, want ErrWorkflowConflict", err)
			}
		})
	}
}

func TestWorkflowStartRejectsParentBacklogOrTerminal(t *testing.T) {
	for _, status := range []string{"backlog", "done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			fx := newWorkflowTestFixture(t)
			def := fx.createDefinition(t, "Release", twoStageWorkflow)
			parent := fx.createParent(t, status)
			fx.createChild(t, parent.ID, 1, "backlog")
			_, err := startWorkflow(t, fx, def.ID, parent.ID)
			if !errors.Is(err, ErrWorkflowConflict) {
				t.Fatalf("err = %v, want ErrWorkflowConflict", err)
			}
		})
	}
}

func TestWorkflowStartRejectsSecondActiveRun(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Release", twoStageWorkflow)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "backlog")
	if _, err := startWorkflow(t, fx, def.ID, parent.ID); err != nil {
		t.Fatal(err)
	}
	_, err := startWorkflow(t, fx, def.ID, parent.ID)
	if !errors.Is(err, ErrActiveWorkflowRun) {
		t.Fatalf("err = %v, want ErrActiveWorkflowRun", err)
	}
}

func TestWorkflowStartConcurrentOnlyOneWins(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Release", twoStageWorkflow)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "backlog")

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = startWorkflow(t, fx, def.ID, parent.ID)
		}(i)
	}
	close(start)
	wg.Wait()

	successes, activeConflicts := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrActiveWorkflowRun):
			activeConflicts++
		default:
			t.Fatalf("unexpected concurrent start error: %v", err)
		}
	}
	if successes != 1 || activeConflicts != 1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/1; errs=%v", successes, activeConflicts, errs)
	}
}

func TestWorkflowStartPinsDefinitionSnapshot(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	v1 := fx.createDefinition(t, "Release", twoStageWorkflow)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "backlog")
	got, err := startWorkflow(t, fx, v1.ID, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	fx.createDefinition(t, "Release", `{"schema_version":1,"stages":[{"key":"only","name":"Only"}]}`)
	spec, err := ValidateWorkflowDefinition(got.Run.DefinitionSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Stages) != 2 || spec.Stages[0].Key != "build" || spec.Stages[1].Key != "test" {
		t.Fatalf("snapshot = %+v", spec)
	}
}
