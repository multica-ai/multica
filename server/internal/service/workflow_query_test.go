package service

import (
	"context"
	"errors"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkflowQueryListsOnlyLatestDefinitionVersions(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	first := fx.createDefinition(t, "Release", `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`)
	second := fx.createDefinition(t, "Release", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	deploy := fx.createDefinition(t, "Deploy", `{"schema_version":1,"stages":[{"key":"ship","name":"Ship"}]}`)

	got, err := fx.service.ListLatestDefinitions(context.Background(), fx.workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("definitions = %d, want 2", len(got))
	}
	seen := map[string]int32{}
	for _, row := range got {
		seen[row.Name] = row.Version
	}
	if seen[second.Name] != second.Version || seen[deploy.Name] != deploy.Version {
		t.Fatalf("latest versions = %+v", seen)
	}
	if seen[first.Name] == first.Version {
		t.Fatalf("old release version leaked into latest list: %+v", seen)
	}
}

func TestWorkflowQueryGetsDefinitionRunAndTransitions(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Read", `{"schema_version":1,"stages":[{"key":"build","name":"Build"}]}`)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "backlog")
	started, err := fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID,
		Actor: WorkflowActor{Type: "member", ID: fx.userID},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotDef, err := fx.service.GetDefinition(context.Background(), fx.workspaceID, def.ID)
	if err != nil || gotDef.ID != def.ID {
		t.Fatalf("definition = %+v err=%v", gotDef, err)
	}
	gotRun, err := fx.service.GetCurrentOrLatestRun(context.Background(), fx.workspaceID, parent.ID)
	if err != nil || gotRun.ID != started.Run.ID {
		t.Fatalf("run = %+v err=%v", gotRun, err)
	}
	transitions, err := fx.service.ListTransitions(context.Background(), fx.workspaceID, started.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].Kind != "started" {
		t.Fatalf("transitions = %+v", transitions)
	}
}

func TestWorkflowCancelIsIdempotentAndDoesNotMutateChildren(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Cancel", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	parent := fx.createParent(t, "todo")
	s1 := fx.createChild(t, parent.ID, 1, "backlog")
	s2 := fx.createChild(t, parent.ID, 2, "backlog")
	started, err := fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID,
		Actor: WorkflowActor{Type: "member", ID: fx.userID},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := fx.service.Cancel(context.Background(), fx.workspaceID, parent.ID, WorkflowActor{Type: "member", ID: fx.userID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Run.Status != "cancelled" || first.Outcome != "cancelled" {
		t.Fatalf("first cancel = %+v", first)
	}
	second, err := fx.service.Cancel(context.Background(), fx.workspaceID, parent.ID, WorkflowActor{Type: "member", ID: fx.userID})
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != "already_cancelled" || second.Run.ID != first.Run.ID || second.Run.Revision != first.Run.Revision {
		t.Fatalf("second cancel = %+v", second)
	}
	assertWorkflowIssueStatus(t, fx.pool, s1.ID, "todo")
	assertWorkflowIssueStatus(t, fx.pool, s2.ID, "backlog")
	transitions, err := db.New(fx.pool).ListWorkflowTransitions(context.Background(), db.ListWorkflowTransitionsParams{
		WorkspaceID: fx.workspaceID, WorkflowRunID: started.Run.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	cancelCount := 0
	for _, tr := range transitions {
		if tr.Kind == "cancelled" {
			cancelCount++
		}
	}
	if cancelCount != 1 {
		t.Fatalf("cancelled transitions = %d, want 1", cancelCount)
	}
}

func TestWorkflowCancelCompletedRunConflicts(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 1)
	fx.finishStage(t, 1)
	if _, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()}); err != nil {
		t.Fatal(err)
	}
	_, err := fx.service.Cancel(context.Background(), fx.workspaceID, fx.parent.ID, WorkflowActor{Type: "member", ID: fx.userID})
	if !errors.Is(err, ErrWorkflowConflict) {
		t.Fatalf("err = %v, want ErrWorkflowConflict", err)
	}
}
