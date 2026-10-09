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
