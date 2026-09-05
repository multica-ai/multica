package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type startedWorkflowFixture struct {
	*workflowTestFixture
	definition db.WorkflowDefinition
	parent     db.Issue
	children   map[int32][]db.Issue
}

func newStartedWorkflowFixture(t *testing.T, stageCount int) *startedWorkflowFixture {
	t.Helper()
	fx := newWorkflowTestFixture(t)
	stages := make([]WorkflowStageSpec, stageCount)
	for i := range stages {
		stages[i] = WorkflowStageSpec{Key: fmt.Sprintf("stage_%d", i+1), Name: fmt.Sprintf("Stage %d", i+1)}
	}
	raw, err := json.Marshal(WorkflowDefinitionSpec{SchemaVersion: 1, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	def := fx.createDefinition(t, "Started", string(raw))
	parent := fx.createParent(t, "todo")
	children := make(map[int32][]db.Issue, stageCount)
	for stage := int32(1); stage <= int32(stageCount); stage++ {
		children[stage] = append(children[stage], fx.createChild(t, parent.ID, stage, "backlog"))
	}
	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID:  fx.workspaceID,
		IssueID:      parent.ID,
		DefinitionID: def.ID,
		Actor:        WorkflowActor{Type: "member", ID: fx.userID},
	}); err != nil {
		t.Fatalf("start workflow: %v", err)
	}
	return &startedWorkflowFixture{workflowTestFixture: fx, definition: def, parent: parent, children: children}
}

func (f *startedWorkflowFixture) finishStage(t *testing.T, stage int32) {
	t.Helper()
	q := db.New(f.pool)
	for _, child := range f.children[stage] {
		if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
			ID: child.ID, Status: "done", WorkspaceID: f.workspaceID,
		}); err != nil {
			t.Fatalf("finish stage %d: %v", stage, err)
		}
	}
}

func systemWorkflowActor() WorkflowActor {
	return WorkflowActor{Type: "system"}
}

func TestWorkflowAdvancePromotesNextStage(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	fx.finishStage(t, 1)

	got, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "running" || got.Run.CurrentStage != 2 || got.Outcome != "stage_advanced" {
		t.Fatalf("result = %+v", got)
	}
	assertWorkflowIssueStatus(t, fx.pool, fx.children[2][0].ID, "todo")
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "stage_advanced" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
}

func TestWorkflowAdvanceDuplicateCallbackIsNoop(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	fx.finishStage(t, 1)
	params := AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()}
	first, err := fx.service.AdvanceFromClosedStage(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fx.service.AdvanceFromClosedStage(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if second.Outcome != "noop" || len(second.Transitions) != 0 || len(second.Changes) != 0 {
		t.Fatalf("second result = %+v", second)
	}
	if second.Run.Revision != first.Run.Revision {
		t.Fatalf("revision changed on duplicate callback: first=%d second=%d", first.Run.Revision, second.Run.Revision)
	}
	var count int
	if err := fx.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM workflow_transition WHERE workflow_run_id = $1 AND kind = 'stage_advanced'
	`, first.Run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stage_advanced transitions = %d, want 1", count)
	}
}

func TestWorkflowAdvanceBlocksWhenDeclaredStageIsNotMaterialized(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Missing", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	parent := fx.createParent(t, "todo")
	s1 := fx.createChild(t, parent.ID, 1, "backlog")
	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID,
		Actor: WorkflowActor{Type: "member", ID: fx.userID},
	}); err != nil {
		t.Fatal(err)
	}
	q := db.New(fx.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: s1.ID, Status: "done", WorkspaceID: fx.workspaceID}); err != nil {
		t.Fatal(err)
	}
	got, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: parent.ID, ClosedStage: 1, Actor: systemWorkflowActor(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "blocked_materialization" || got.Run.CurrentStage != 2 || got.Outcome != "blocked_materialization" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "materialization_blocked" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
}

func TestWorkflowResumeRemainsBlockedWithoutMaterializedChildren(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Resume Missing", `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)
	parent := fx.createParent(t, "todo")
	s1 := fx.createChild(t, parent.ID, 1, "backlog")
	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID, Actor: WorkflowActor{Type: "member", ID: fx.userID}}); err != nil {
		t.Fatal(err)
	}
	q := db.New(fx.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: s1.ID, Status: "done", WorkspaceID: fx.workspaceID}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()}); err != nil {
		t.Fatal(err)
	}
	got, err := fx.service.Resume(context.Background(), ResumeWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, Actor: WorkflowActor{Type: "member", ID: fx.userID}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "blocked_materialization" || got.Run.CurrentStage != 2 || got.Outcome != "noop" {
		t.Fatalf("result = %+v", got)
	}
	if len(got.Transitions) != 0 || len(got.Changes) != 0 {
		t.Fatalf("resume should be durable no-op: %+v", got)
	}
}

func TestWorkflowResumePromotesMaterializedStage(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Resume Materialized", `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)
	parent := fx.createParent(t, "todo")
	s1 := fx.createChild(t, parent.ID, 1, "backlog")
	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID, Actor: WorkflowActor{Type: "member", ID: fx.userID}}); err != nil {
		t.Fatal(err)
	}
	q := db.New(fx.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: s1.ID, Status: "done", WorkspaceID: fx.workspaceID}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()}); err != nil {
		t.Fatal(err)
	}
	s2 := fx.createChild(t, parent.ID, 2, "backlog")
	got, err := fx.service.Resume(context.Background(), ResumeWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, Actor: WorkflowActor{Type: "member", ID: fx.userID}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "running" || got.Run.CurrentStage != 2 || got.Outcome != "materialized" {
		t.Fatalf("result = %+v", got)
	}
	assertWorkflowIssueStatus(t, fx.pool, s2.ID, "todo")
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "materialized" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
}

func TestWorkflowAdvanceSkipsAlreadyTerminalDeclaredStage(t *testing.T) {
	base := newWorkflowTestFixture(t)
	stages := []WorkflowStageSpec{{Key: "stage_1", Name: "Stage 1"}, {Key: "stage_2", Name: "Stage 2"}, {Key: "stage_3", Name: "Stage 3"}}
	raw, err := json.Marshal(WorkflowDefinitionSpec{SchemaVersion: 1, Stages: stages})
	if err != nil {
		t.Fatal(err)
	}
	def := base.createDefinition(t, "Skip terminal", string(raw))
	parent := base.createParent(t, "todo")
	s1 := base.createChild(t, parent.ID, 1, "backlog")
	base.createChild(t, parent.ID, 2, "done")
	s3 := base.createChild(t, parent.ID, 3, "backlog")
	if _, err := base.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID: base.workspaceID, IssueID: parent.ID, DefinitionID: def.ID,
		Actor: WorkflowActor{Type: "member", ID: base.userID},
	}); err != nil {
		t.Fatal(err)
	}
	q := db.New(base.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: s1.ID, Status: "done", WorkspaceID: base.workspaceID}); err != nil {
		t.Fatal(err)
	}
	got, err := base.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: base.workspaceID, IssueID: parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.CurrentStage != 3 || got.Run.Status != "running" {
		t.Fatalf("run = %+v", got.Run)
	}
	assertWorkflowIssueStatus(t, base.pool, s3.ID, "todo")
	if len(got.Transitions) != 2 || got.Transitions[0].Kind != "stage_satisfied" || got.Transitions[1].Kind != "stage_advanced" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
}

func TestWorkflowAdvanceFinalStageCompletesPendingReview(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 1)
	fx.finishStage(t, 1)
	got, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "completed_pending_review" || got.Outcome != "completed_pending_review" {
		t.Fatalf("result = %+v", got)
	}
	assertWorkflowIssueStatus(t, fx.pool, fx.parent.ID, "in_review")
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "completed_pending_review" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
	if len(got.Changes) != 1 || got.Changes[0].Before.ID != fx.parent.ID || got.Changes[0].After.Status != "in_review" {
		t.Fatalf("changes = %+v", got.Changes)
	}
}

func TestWorkflowOrderGuardRejectsLaterStageActivationWithoutChangingRun(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	q := db.New(fx.pool)
	beforeRun, err := q.GetActiveWorkflowRunForIssue(context.Background(), db.GetActiveWorkflowRunForIssueParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: fx.children[2][0].ID, Status: "in_progress", WorkspaceID: fx.workspaceID}); err == nil {
		t.Fatal("future stage activation succeeded; want workflow order rejection")
	}
	afterRun, err := q.GetActiveWorkflowRunForIssue(context.Background(), db.GetActiveWorkflowRunForIssueParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if afterRun.Revision != beforeRun.Revision || afterRun.CurrentStage != beforeRun.CurrentStage || afterRun.Status != beforeRun.Status {
		t.Fatalf("run changed on rejected mutation: before=%+v after=%+v", beforeRun, afterRun)
	}
	assertWorkflowIssueStatus(t, fx.pool, fx.children[2][0].ID, "backlog")
}

func TestWorkflowParentTerminalCancelsRun(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	q := db.New(fx.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: fx.parent.ID, Status: "done", WorkspaceID: fx.workspaceID}); err != nil {
		t.Fatal(err)
	}
	got, err := fx.service.Resume(context.Background(), ResumeWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, Actor: systemWorkflowActor()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Run.Status != "cancelled" || got.Outcome != "parent_terminal" {
		t.Fatalf("result = %+v", got)
	}
	assertWorkflowIssueStatus(t, fx.pool, fx.parent.ID, "done")
	assertWorkflowIssueStatus(t, fx.pool, fx.children[2][0].ID, "backlog")
	if len(got.Transitions) != 1 || got.Transitions[0].Kind != "parent_terminal" {
		t.Fatalf("transitions = %+v", got.Transitions)
	}
}

func TestWorkflowConcurrentAdvancePersistsOnePromotion(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	fx.finishStage(t, 1)
	q := db.New(fx.pool)
	beforeRun, err := q.GetActiveWorkflowRunForIssue(context.Background(), db.GetActiveWorkflowRunForIssueParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	beforeChild, err := q.LockWorkflowParent(context.Background(), db.LockWorkflowParentParams{ID: fx.children[2][0].ID, WorkspaceID: fx.workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	params := AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()}
	errs := make([]error, 2)
	results := make([]WorkflowMutationResult, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = fx.service.AdvanceFromClosedStage(context.Background(), params)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
	}
	var durable int
	for _, result := range results {
		if result.Outcome == "stage_advanced" {
			durable++
		}
	}
	if durable != 1 {
		t.Fatalf("durable advance results = %d, results=%+v", durable, results)
	}
	var count int
	if err := fx.pool.QueryRow(context.Background(), `SELECT count(*) FROM workflow_transition WHERE workflow_run_id = $1 AND kind = 'stage_advanced'`, results[0].Run.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("stage_advanced transition count = %d, want 1", count)
	}
	afterRun, err := q.GetActiveWorkflowRunForIssue(context.Background(), db.GetActiveWorkflowRunForIssueParams{WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if afterRun.Revision != beforeRun.Revision+1 {
		t.Fatalf("run revision = %d, want %d", afterRun.Revision, beforeRun.Revision+1)
	}
	var childStatus string
	var childRevision int64
	if err := fx.pool.QueryRow(context.Background(), `SELECT status, revision FROM issue WHERE id = $1`, fx.children[2][0].ID).Scan(&childStatus, &childRevision); err != nil {
		t.Fatal(err)
	}
	if childStatus != "todo" || childRevision != beforeChild.Revision+1 {
		t.Fatalf("stage 2 child status/revision = %s/%d, want todo/%d", childStatus, childRevision, beforeChild.Revision+1)
	}
}
