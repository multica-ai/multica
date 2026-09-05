package service

import (
	"context"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkflowOrderGuardRejectsFutureStageActivation(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "ordered-guard", `{"schema_version":1,"stages":[{"key":"build","name":"Build"},{"key":"test","name":"Test"}]}`)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "todo")
	future := fx.createChild(t, parent.ID, 2, "backlog")

	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{
		WorkspaceID:  fx.workspaceID,
		IssueID:      parent.ID,
		DefinitionID: def.ID,
		Actor:        WorkflowActor{Type: "member", ID: fx.userID},
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := db.New(fx.pool).UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID: future.ID, Status: "todo", WorkspaceID: fx.workspaceID,
	})
	if err == nil {
		t.Fatal("future workflow stage activation succeeded; want backend rejection")
	}
	assertWorkflowIssueStatus(t, fx.pool, future.ID, "backlog")
}

func TestWorkflowOrderGuardRejectsActiveWorkWhileMaterializationBlocked(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "Blocked", `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)
	parent := fx.createParent(t, "todo")
	stage1 := fx.createChild(t, parent.ID, 1, "backlog")
	if _, err := fx.service.Start(context.Background(), StartWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, DefinitionID: def.ID, Actor: WorkflowActor{Type: "member", ID: fx.userID}}); err != nil {
		t.Fatal(err)
	}
	q := db.New(fx.pool)
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: stage1.ID, Status: "done", WorkspaceID: fx.workspaceID}); err != nil {
		t.Fatal(err)
	}
	blocked, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{WorkspaceID: fx.workspaceID, IssueID: parent.ID, ClosedStage: 1, Actor: systemWorkflowActor()})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Run.Status != "blocked_materialization" || blocked.Run.CurrentStage != 2 {
		t.Fatalf("run = %+v", blocked.Run)
	}
	stage2 := fx.createChild(t, parent.ID, 2, "backlog")
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{ID: stage2.ID, Status: "todo", WorkspaceID: fx.workspaceID}); err == nil {
		t.Fatal("blocked materialization child activated without Resume")
	}
	assertWorkflowIssueStatus(t, fx.pool, stage2.ID, "backlog")
}

func TestWorkflowOrderGuardAllowsUnrelatedEditOnCompletedPriorStage(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	stage1 := fx.children[1][0]
	fx.finishStage(t, 1)
	if _, err := fx.service.AdvanceFromClosedStage(context.Background(), AdvanceWorkflowParams{
		WorkspaceID: fx.workspaceID, IssueID: fx.parent.ID, ClosedStage: 1, Actor: systemWorkflowActor(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := fx.pool.Exec(context.Background(), `
		UPDATE issue
		SET title = title || ' edited', status = status, stage = stage, parent_issue_id = parent_issue_id
		WHERE id = $1 AND workspace_id = $2`, stage1.ID, fx.workspaceID); err != nil {
		t.Fatalf("unrelated edit on prior stage: %v", err)
	}
}

func TestWorkflowOrderGuardRejectsParkingRunningCurrentStage(t *testing.T) {
	fx := newStartedWorkflowFixture(t, 2)
	q := db.New(fx.pool)
	stage1 := fx.children[1][0]
	if _, err := q.UpdateIssueStatus(context.Background(), db.UpdateIssueStatusParams{
		ID: stage1.ID, Status: "backlog", WorkspaceID: fx.workspaceID,
	}); err == nil {
		t.Fatal("running current stage was parked back in backlog")
	}
	assertWorkflowIssueStatus(t, fx.pool, stage1.ID, "todo")
}

func TestWorkflowOrderGuardSeesRunCommittedWhileIssueUpdateWaits(t *testing.T) {
	fx := newWorkflowTestFixture(t)
	def := fx.createDefinition(t, "start-race", `{"schema_version":1,"stages":[{"key":"one","name":"One"},{"key":"two","name":"Two"}]}`)
	parent := fx.createParent(t, "todo")
	fx.createChild(t, parent.ID, 1, "backlog")
	future := fx.createChild(t, parent.ID, 2, "backlog")
	ctx := context.Background()
	tx, err := fx.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT id FROM issue WHERE id=$1 FOR UPDATE`, future.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO workflow_run (workspace_id,issue_id,workflow_definition_id,definition_snapshot,status,current_stage,started_by_type,started_by_id) VALUES ($1,$2,$3,$4,'running',1,'member',$5)`, fx.workspaceID, parent.ID, def.ID, def.Definition, fx.userID); err != nil {
		t.Fatal(err)
	}

	writer, err := fx.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Release()
	pid := writer.Conn().PgConn().PID()
	result := make(chan error, 1)
	go func() {
		_, err := db.New(writer).UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: future.ID, Status: "todo", WorkspaceID: fx.workspaceID})
		result <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err := fx.pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("writer did not block on workflow-start child lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("future stage activation committed after workflow start; want order rejection")
	}
	assertWorkflowIssueStatus(t, fx.pool, future.ID, "backlog")
}
