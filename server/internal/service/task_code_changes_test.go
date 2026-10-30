package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/codechanges"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func publicCodeSnapshot() *codechanges.Snapshot {
	return &codechanges.Snapshot{Version: 1, BaseRef: strings.Repeat("a", 40), HeadRef: strings.Repeat("b", 40), Files: []codechanges.File{{Path: "app.go", Status: "M", Diff: "@@ -1 +1 @@\n-old\n+private-source", Additions: 1, Deletions: 1}}}
}
func codeChangesServiceFixture(t *testing.T) (*TaskService, db.AgentTaskQueue, codechanges.ExecutionRef) {
	t.Helper()
	pool := sharedTestPool(t)
	ctx := context.Background()
	ws, user, agent, issue := seedAttributionFixture(t, pool)
	var id pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,runtime_id,issue_id,status,priority,dispatched_at,started_at,max_attempts,originator_user_id,accountable_user_id)
 SELECT id,runtime_id,$2,'running',2,now(),now(),3,$3,$3 FROM agent WHERE id=$1 RETURNING id`, agent, issue, user).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM agent_task_code_changes WHERE task_id=$1", id)
	})
	q := db.New(pool)
	if _, err := q.UpdateWorkspaceCodeChanges(ctx, db.UpdateWorkspaceCodeChangesParams{ID: util.MustParseUUID(ws), Enabled: true}); err != nil {
		t.Fatal(err)
	}
	task, err := q.GetAgentTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	return svc, task, codechanges.ExecutionRef{RuntimeID: util.UUIDToString(task.RuntimeID), DispatchedAt: task.DispatchedAt.Time.Format(time.RFC3339Nano)}
}
func TestCodeChangesAtomicTerminalAndStaleRollback(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "fail"}[failure], func(t *testing.T) {
			svc, task, ref := codeChangesServiceFixture(t)
			ctx := context.Background()
			snapshot := publicCodeSnapshot()
			stale := ref
			stale.DispatchedAt = task.DispatchedAt.Time.Add(time.Second).Format(time.RFC3339Nano)
			if failure {
				_, _, err := svc.FailTaskWithCodeChanges(ctx, task.ID, "network", "", "", "", "agent_error.provider_network", false, "", "", snapshot, stale)
				if !errors.Is(err, ErrCodeChangesExecutionStale) {
					t.Fatalf("stale fail=%v", err)
				}
			} else {
				_, _, err := svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", snapshot, stale)
				if !errors.Is(err, ErrCodeChangesExecutionStale) {
					t.Fatalf("stale completion=%v", err)
				}
			}
			current, err := svc.Queries.GetAgentTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != "running" || len(current.Result) > 0 {
				t.Fatal("stale artifact changed terminal state")
			}
			var retries int
			if err := sharedTestPool(t).QueryRow(ctx, "SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1", task.ID).Scan(&retries); err != nil || retries != 0 {
				t.Fatalf("stale report created retries=%d err=%v", retries, err)
			}
			if failure {
				_, _, err = svc.FailTaskWithCodeChanges(ctx, task.ID, "failed", "", "", "", "agent_error", false, "", "", snapshot, ref)
			} else {
				_, _, err = svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", snapshot, ref)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw, err := svc.Queries.GetTaskCodeChangesSnapshot(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := codechanges.Decode(raw)
			if err != nil || len(decoded.Files) != 1 {
				t.Fatalf("saved artifact=%+v err=%v", decoded, err)
			}
			current, err = svc.Queries.GetAgentTask(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(current)
			if strings.Contains(string(body), "private-source") {
				t.Fatal("source entered task response")
			}
		})
	}
}
func TestCodeChangesRequireTransactionAndRespectDisabledSetting(t *testing.T) {
	svc, task, ref := codeChangesServiceFixture(t)
	ctx := context.Background()
	pool := svc.TxStarter
	svc.TxStarter = nil
	if _, _, err := svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", publicCodeSnapshot(), ref); err == nil {
		t.Fatal("snapshot write without transaction accepted")
	}
	current, err := svc.Queries.GetAgentTask(ctx, task.ID)
	if err != nil || current.Status != "running" {
		t.Fatal("rejected snapshot changed task")
	}
	svc.TxStarter = pool
	issue, err := svc.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Queries.UpdateWorkspaceCodeChanges(ctx, db.UpdateWorkspaceCodeChangesParams{ID: issue.WorkspaceID, Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", publicCodeSnapshot(), ref); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Queries.GetTaskCodeChangesSnapshot(ctx, task.ID); err == nil {
		t.Fatal("disabled workspace saved an in-flight capture")
	}
}
func TestCodeChangesMatchingTerminalReplayBackfillsMissingArtifact(t *testing.T) {
	svc, task, ref := codeChangesServiceFixture(t)
	ctx := context.Background()
	if _, _, err := svc.CompleteTaskWithTransition(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, transitioned, err := svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", publicCodeSnapshot(), ref); err != nil || transitioned {
		t.Fatalf("backfill transition=%v err=%v", transitioned, err)
	}
	if _, err := svc.Queries.GetTaskCodeChangesSnapshot(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
}
func TestCodeChangesSettingSurvivesStaleGenericWorkspaceUpdate(t *testing.T) {
	svc, task, _ := codeChangesServiceFixture(t)
	ctx := context.Background()
	issue, err := svc.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.Queries.UpdateWorkspace(ctx, db.UpdateWorkspaceParams{ID: issue.WorkspaceID, Settings: []byte(`{"github_enabled":false,"code_changes_enabled":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !codechanges.Enabled(updated.Settings) {
		t.Fatal("stale generic settings replaced dedicated code flag")
	}
}

func TestCodeChangesUnboundRuntimeCannotBackfillArtifact(t *testing.T) {
	svc, task, ref := codeChangesServiceFixture(t)
	ctx := context.Background()
	if _, _, err := svc.CompleteTaskWithTransition(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := sharedTestPool(t).Exec(ctx, "UPDATE agent_task_queue SET runtime_id=NULL WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteTaskWithCodeChanges(ctx, task.ID, []byte(`{"output":"done"}`), "", "", "", false, "", "", publicCodeSnapshot(), ref); !errors.Is(err, ErrCodeChangesExecutionStale) {
		t.Fatalf("unbound backfill=%v", err)
	}
	if _, err := svc.Queries.GetTaskCodeChangesSnapshot(ctx, task.ID); err == nil {
		t.Fatal("unbound execution forged a missing artifact")
	}
}
