package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type runningIssueTaskFixture struct {
	pool      *pgxpool.Pool
	queries   *db.Queries
	service   *TaskService
	workspace pgtype.UUID
	user      pgtype.UUID
	agent     pgtype.UUID
	issue     db.Issue
	task      db.AgentTaskQueue
	input     db.Comment
}

func seedRunningIssueTask(t *testing.T) runningIssueTaskFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)
	issueUUID := util.MustParseUUID(issueID)
	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	createdInput, err := queries.CreateComment(ctx, db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issueUUID, WorkspaceID: workspaceUUID,
		AuthorType: "member", AuthorID: userUUID, Content: "Please finish this", Type: "comment",
	})
	if err != nil {
		t.Fatalf("create trigger comment: %v", err)
	}
	bus := events.New()
	service := &TaskService{Queries: queries, TxStarter: pool, Bus: bus}
	task, err := service.EnqueueTaskForIssue(ctx, issue, createdInput.ID)
	if err != nil {
		t.Fatalf("enqueue task: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status='running', dispatched_at=now(), started_at=now()
		WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("start task: %v", err)
	}
	task, err = queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("reload running task: %v", err)
	}
	return runningIssueTaskFixture{
		pool: pool, queries: queries, service: service, workspace: workspaceUUID,
		user: userUUID, agent: agentUUID, issue: issue, task: task, input: createdInput.Comment(),
	}
}

func completionPayload(t *testing.T, taskID pgtype.UUID, output string) []byte {
	t.Helper()
	data, err := json.Marshal(protocol.TaskCompletedPayload{TaskID: util.UUIDToString(taskID), Output: output})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIssueCompletionProgressCannotSuppressFinalOutcome(t *testing.T) {
	f := seedRunningIssueTask(t)
	ctx := context.Background()
	if _, err := f.queries.CreateComment(ctx, db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: f.issue.ID, WorkspaceID: f.workspace,
		AuthorType: "agent", AuthorID: f.agent, Content: "Still working", Type: "progress_update",
		SourceTaskID: f.task.ID,
	}); err != nil {
		t.Fatalf("create progress comment: %v", err)
	}

	commentEvents := 0
	f.service.Bus.Subscribe(protocol.EventCommentCreated, func(events.Event) { commentEvents++ })
	result := completionPayload(t, f.task.ID, "Final answer with evidence")
	completed, transitioned, err := f.service.CompleteTaskWithTransition(ctx, f.task.ID, result, "", "", "", false, "", "")
	if err != nil || !transitioned || completed.Status != "completed" {
		t.Fatalf("complete = task %+v transitioned %v err %v", completed, transitioned, err)
	}

	outcome, err := f.queries.GetAgentTaskCompletionOutcome(ctx, f.task.ID)
	if err != nil {
		t.Fatalf("load outcome: %v", err)
	}
	if outcome.OutcomeKind != "final" || outcome.Content != "Final answer with evidence" || !outcome.FinalCommentID.Valid {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(outcome.AnsweredCommentIds) != 1 || outcome.AnsweredCommentIds[0] != f.input.ID {
		t.Fatalf("answered comments = %v, want trigger %s", outcome.AnsweredCommentIds, util.UUIDToString(f.input.ID))
	}
	var progressCount, finalCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND type='progress_update'`, f.task.ID).Scan(&progressCount); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND type='comment'`, f.task.ID).Scan(&finalCount); err != nil {
		t.Fatal(err)
	}
	if progressCount != 1 || finalCount != 1 || commentEvents != 1 {
		t.Fatalf("progress=%d final=%d final events=%d; want 1/1/1", progressCount, finalCount, commentEvents)
	}
	var outboxState string
	if err := f.pool.QueryRow(ctx, `SELECT state FROM task_completion_outbox WHERE task_id=$1`, f.task.ID).Scan(&outboxState); err != nil {
		t.Fatal(err)
	}
	if outboxState != "published" {
		t.Fatalf("outbox state = %q, want published", outboxState)
	}

	replayed, transitioned, err := f.service.CompleteTaskWithTransition(ctx, f.task.ID, result, "", "", "", false, "", "")
	if err != nil || transitioned || replayed.ID != f.task.ID {
		t.Fatalf("replay = task %+v transitioned %v err %v", replayed, transitioned, err)
	}
	if commentEvents != 1 {
		t.Fatalf("replayed completion emitted %d comment events, want one total", commentEvents)
	}
}

func TestIssueCompletionOutcomeFailureRollsBackTerminalTransition(t *testing.T) {
	f := seedRunningIssueTask(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	functionName := "test_fail_completion_" + suffix
	triggerName := "test_fail_completion_trigger_" + suffix
	ddl := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.task_id = '%s'::uuid THEN
				RAISE EXCEPTION 'injected outcome failure';
			END IF;
			RETURN NEW;
		END $$;
		CREATE TRIGGER %s BEFORE INSERT ON agent_task_completion_outcome
		FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, util.UUIDToString(f.task.ID), triggerName, functionName)
	if _, err := f.pool.Exec(ctx, ddl); err != nil {
		t.Fatalf("install failure trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON agent_task_completion_outcome; DROP FUNCTION IF EXISTS %s()", triggerName, functionName))
	})

	result := completionPayload(t, f.task.ID, "This must roll back")
	if _, transitioned, err := f.service.CompleteTaskWithTransition(ctx, f.task.ID, result, "", "", "", false, "", ""); err == nil || transitioned {
		t.Fatalf("completion with injected failure = transitioned %v err %v", transitioned, err)
	}
	current, err := f.queries.GetAgentTask(ctx, f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "running" {
		t.Fatalf("task status = %q, want running after rollback", current.Status)
	}
	var outcomes, finalComments int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_completion_outcome WHERE task_id=$1`, f.task.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND type='comment'`, f.task.ID).Scan(&finalComments); err != nil {
		t.Fatal(err)
	}
	if outcomes != 0 || finalComments != 0 {
		t.Fatalf("rolled-back outcomes=%d final comments=%d, want 0/0", outcomes, finalComments)
	}
}
