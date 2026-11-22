package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestInterruptedRunRecoveryPreservesEvidenceAndStops(t *testing.T) {
	for _, raw := range []string{
		"workspace routing discovery timed out",
		"codex thread/start failed: codex app-server handshake timeout: thread/start did not respond after 1m0s",
		"codex thread/resume failed: codex app-server handshake timeout: thread/resume did not respond after 1m0s",
	} {
		t.Run(raw, func(t *testing.T) {
			f, _, issue, agent := wakeFixture(t)
			ctx := context.Background()
			agentRow, err := f.q.GetAgent(ctx, parseTestUUID(t, agent))
			if err != nil {
				t.Fatal(err)
			}
			parentID := f.Task(t, agent, testutil.Cols{
				"issue_id": issue, "runtime_id": agentRow.RuntimeID, "status": "running", "attempt": 1, "max_attempts": 2,
				"session_id": "retained-session", "work_dir": t.TempDir(),
			})
			parent, err := f.q.GetAgentTask(ctx, parseTestUUID(t, parentID))
			if err != nil {
				t.Fatal(err)
			}
			f.Cleanup(t, "DELETE FROM agent_task_queue WHERE parent_task_id=$1", parentID)
			svc := f.svc.TaskSvc
			failed, err := svc.FailTask(ctx, parent.ID, raw, parent.SessionID.String, parent.WorkDir.String, "", "agent_error.unknown", false, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if failed.Error.String != raw || failed.FailureReason.String != "timeout" {
				t.Fatalf("failure evidence changed: %+v", failed)
			}
			var childID pgtype.UUID
			if err := f.Pool.QueryRow(ctx, "SELECT id FROM agent_task_queue WHERE parent_task_id=$1", parent.ID).Scan(&childID); err != nil {
				t.Fatal(err)
			}
			child, err := f.q.GetAgentTask(ctx, childID)
			if err != nil {
				t.Fatal(err)
			}
			if child.Attempt != 2 || child.MaxAttempts != 2 || child.WorkDir != parent.WorkDir || child.SessionID != parent.SessionID || child.ForceFreshSession {
				t.Fatalf("retry lost continuity/budget: %+v", child)
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='running' WHERE id=$1", child.ID)
			if _, err := svc.FailTask(ctx, child.ID, raw, child.SessionID.String, child.WorkDir.String, "", "agent_error.unknown", false, "", ""); err != nil {
				t.Fatal(err)
			}
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1", child.ID); n != 0 {
				t.Fatalf("retry limit exceeded: %d", n)
			}
			if retry, err := svc.MaybeRetryFailedTask(ctx, *failed); err != nil || retry != nil {
				t.Fatalf("replayed parent failure restarted exhausted recovery: %+v %v", retry, err)
			}
			var comment string
			if err := f.Pool.QueryRow(ctx, "SELECT content FROM comment WHERE source_task_id=$1 AND type='system'", child.ID).Scan(&comment); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(comment, raw) || !strings.Contains(comment, "Automatic recovery stopped after 2 of 2 attempts") {
				t.Fatalf("missing terminal diagnostic: %q", comment)
			}
			retained, err := f.q.GetAgentTask(ctx, parent.ID)
			if err != nil || retained.Error.String != raw || retained.WorkDir != parent.WorkDir {
				t.Fatalf("original evidence not retained: %+v %v", retained, err)
			}
		})
	}
}

func TestInterruptedRunDisabledRecoveryBudget(t *testing.T) {
	f, _, issue, agent := wakeFixture(t)
	ctx := context.Background()
	agentRow, err := f.q.GetAgent(ctx, parseTestUUID(t, agent))
	if err != nil {
		t.Fatal(err)
	}
	parent := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": agentRow.RuntimeID, "status": "running", "attempt": 1, "max_attempts": 1})
	if _, err := f.svc.TaskSvc.FailTask(ctx, parseTestUUID(t, parent), "workspace routing discovery timed out", "", "", "", "agent_error.unknown", false, "", ""); err != nil {
		t.Fatal(err)
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1", parent); n != 0 {
		t.Fatalf("disabled recovery produced %d children", n)
	}
}

func TestInterruptedRunRetryDoesNotCompeteWithLiveOwner(t *testing.T) {
	for _, status := range []string{"queued", "dispatched", "running", "waiting_local_directory"} {
		t.Run(status, func(t *testing.T) {
			f, _, issue, agent := wakeFixture(t)
			agentRow, err := f.q.GetAgent(context.Background(), parseTestUUID(t, agent))
			if err != nil {
				t.Fatal(err)
			}
			parent := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": agentRow.RuntimeID, "status": "failed", "attempt": 1, "max_attempts": 2, "failure_reason": "timeout", "error": "workspace routing discovery timed out"})
			owner := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": agentRow.RuntimeID, "status": status})
			_, err = f.q.CreateRetryTask(context.Background(), db.CreateRetryTaskParams{ID: parseTestUUID(t, parent)})
			if err != pgx.ErrNoRows {
				t.Fatalf("live owner allowed a retry: %v", err)
			}
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1", parent); n != 0 {
				t.Fatalf("competing child created: %d", n)
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", owner)
			child, err := f.q.CreateRetryTask(context.Background(), db.CreateRetryTaskParams{ID: parseTestUUID(t, parent)})
			if err != nil {
				t.Fatalf("settled owner still blocks recovery: %v", err)
			}
			f.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", child.ID)
		})
	}
}
