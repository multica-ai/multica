package service

import (
 "context"
 "testing"

 "github.com/jackc/pgx/v5/pgtype"
 "github.com/multica-ai/multica/server/internal/util"
)

// Integration coverage requires a real, migrated Postgres, not a skipped DB test.
func TestOneAttemptRerunPersistsBudgetAndStopsProviderNetwork(t *testing.T) {
 svc, pool, creatorID, _, issueID, _ := rerunQueueFixture(t)
 ctx := context.Background()
 task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creatorID), nil, true)
 if err != nil { t.Fatalf("RerunIssue one-shot: %v", err) }
 t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE parent_task_id = $1 OR id = $1", task.ID) })
 if task.Attempt != 1 || task.MaxAttempts != 1 {
  t.Fatalf("returned task budget attempt=%d max_attempts=%d, want 1/1",task.Attempt,task.MaxAttempts)
 }
 var attempt,maxAttempts int32
 if err := pool.QueryRow(ctx, "SELECT attempt, max_attempts FROM agent_task_queue WHERE id=$1",task.ID).Scan(&attempt,&maxAttempts); err != nil {
  t.Fatalf("read persisted task budget: %v",err)
 }
 if attempt != 1 || maxAttempts != 1 { t.Fatalf("persisted attempt=%d max_attempts=%d, want 1/1",attempt,maxAttempts) }

 if _,err := pool.Exec(ctx,"UPDATE agent_task_queue SET status='running' WHERE id=$1",task.ID); err != nil {
  t.Fatalf("set test task running: %v",err)
 }
 if _,err := svc.FailTask(ctx,task.ID,"API Error: Connection closed mid-response.","","","","agent_error.provider_network",false,"",""); err!=nil {
  t.Fatalf("FailTask provider_network: %v",err)
 }
 var children int
 if err := pool.QueryRow(ctx,"SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1",task.ID).Scan(&children); err!=nil {
  t.Fatalf("read retry children: %v",err)
 }
 if children!=0 {t.Fatalf("one-attempt task generated %d retries, want zero",children)}
}

func TestOneAttemptRerunLegacyDefaultPreserved(t *testing.T) {
 svc,pool,creatorID,_,issueID,_:=rerunQueueFixture(t)
 ctx:=context.Background()
 task,err:=svc.RerunIssue(ctx,util.MustParseUUID(issueID),pgtype.UUID{},pgtype.UUID{},util.MustParseUUID(creatorID),nil)
 if err!=nil {t.Fatalf("legacy rerun: %v",err)}
 t.Cleanup(func(){_,_ = pool.Exec(context.Background(),"DELETE FROM agent_task_queue WHERE id=$1",task.ID)})
 if task.MaxAttempts!=2 {t.Fatalf("legacy default max_attempts=%d, want 2",task.MaxAttempts)}
}

func TestOneAttemptRerunHistoricalMentionPath(t *testing.T) {
 svc,pool,creatorID,agentID,issueID,runtimeID:=rerunQueueFixture(t)
 ctx:=context.Background()
 original:=seedRerunTask(t,pool,agentID,runtimeID,issueID,"completed")
 if _,err:=pool.Exec(ctx,"UPDATE issue SET assignee_type=NULL, assignee_id=NULL WHERE id=$1",issueID);err!=nil {
  t.Fatalf("unassign in fixture: %v",err)
 }
 task,err:=svc.RerunIssue(ctx,util.MustParseUUID(issueID),original,pgtype.UUID{},util.MustParseUUID(creatorID),nil,true)
 if err!=nil {t.Fatalf("historical mention rerun: %v",err)}
 t.Cleanup(func(){_,_ = pool.Exec(context.Background(),"DELETE FROM agent_task_queue WHERE id=$1",task.ID)})
 if task.MaxAttempts!=1 || task.Attempt!=1 {t.Fatalf("mention budget=%d/%d, want 1/1",task.Attempt,task.MaxAttempts)}
}

 // TestFirstRunWithoutPriorTaskUsesOneAttemptBudget proves the manual-rerun API
 // can safely bootstrap the FIRST run after an issue was assigned with no-start.
 // Do not use a prompt-level instruction as a substitute for this database gate.
func TestFirstRunWithoutPriorTaskUsesOneAttemptBudget(t *testing.T) {
 svc, pool, creatorID, agentID, issueID, _ := rerunQueueFixture(t)
 ctx := context.Background()
 var before int
 if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2", issueID, agentID).Scan(&before); err != nil {
  t.Fatalf("read pre-dispatch task count: %v", err)
 }
 if before != 0 { t.Fatalf("fixture has %d pre-existing tasks: expected zero before first run", before) }
 task, err := svc.RerunIssue(ctx, util.MustParseUUID(issueID), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creatorID), nil, true)
 if err != nil { t.Fatalf("first run with explicit max_attempts=1: %v", err) }
 t.Cleanup(func(){_,_ = pool.Exec(context.Background(),"DELETE FROM agent_task_queue WHERE id=$1",task.ID)})
 if task.Attempt != 1 || task.MaxAttempts != 1 || task.RerunOfTaskID.Valid {
  t.Fatalf("expected initial task attempt=1, max_attempts=1, no parent rerun; got attempt=%d max=%d rerun=%v",task.Attempt,task.MaxAttempts,task.RerunOfTaskID.Valid)
 }
 var count int
 if err := pool.QueryRow(ctx,"SELECT COUNT(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2",issueID,agentID).Scan(&count);err!=nil{
  t.Fatalf("read created task count: %v",err)
 }
 if count!=1 {t.Fatalf("expected exactly one initial task row, got %d",count)}
 if _,err:=pool.Exec(ctx,"UPDATE agent_task_queue SET status='running' WHERE id=$1",task.ID);err!=nil {
  t.Fatalf("mark test run as running: %v",err)
 }
 if _,err:=svc.FailTask(ctx,task.ID,"API Error: Connection closed mid-response.","","","","agent_error.provider_network",false,"","");err!=nil {
  t.Fatalf("simulate provider_network failure: %v",err)
 }
 var children int
 if err:=pool.QueryRow(ctx,"SELECT COUNT(*) FROM agent_task_queue WHERE parent_task_id=$1",task.ID).Scan(&children);err!=nil {
  t.Fatalf("read automatic retries: %v",err)
 }
 if children!=0 {t.Fatalf("first-run max_attempts=1 unexpectedly created %d retry tasks",children)}
}
