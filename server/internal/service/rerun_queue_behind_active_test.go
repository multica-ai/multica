package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// rerunQueueFixture seeds the shared (workspace, agent, issue) trio and returns
// the service under test plus the agent's runtime id, which
// agent_task_queue.runtime_id needs (NOT NULL).
func rerunQueueFixture(t *testing.T) (svc *TaskService, pool *pgxpool.Pool, creatorID, agentID, issueID, runtimeID string) {
	t.Helper()
	pool = newResolveOriginatorPool(t)
	_, creatorID, agentID, issueID = seedAttributionFixture(t, pool)

	if err := pool.QueryRow(context.Background(), `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	svc = &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	return svc, pool, creatorID, agentID, issueID, runtimeID
}

func seedRerunTask(t *testing.T, pool *pgxpool.Pool, agentID, runtimeID, issueID, status string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, $4, 0)
		RETURNING id
	`, agentID, runtimeID, issueID, status).Scan(&id); err != nil {
		t.Fatalf("insert %s task: %v", status, err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id) })
	return id
}

func taskStatus(t *testing.T, pool *pgxpool.Pool, id pgtype.UUID) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	return status
}

// TestRerunIssueQueuesBehindActiveTaskWithoutCancelling is the regression this
// change exists for. 'running' and 'waiting_local_directory' both mean an agent
// is executing, and neither appears in idx_one_pending_task_per_issue_agent_v2 —
// so a rerun enqueues a queued row alongside the active one and ClaimAgentTask's
// per-(issue, agent) serialization holds it there until the active run reaches a
// terminal state.
//
// The load-bearing assertion is that the active task is untouched. Rerun used to
// call a cancel query covering every active status, so clicking "run this again"
// silently killed the pass the agent was still working on.
func TestRerunIssueQueuesBehindActiveTaskWithoutCancelling(t *testing.T) {
	for _, activeStatus := range []string{"running", "waiting_local_directory"} {
		t.Run(activeStatus, func(t *testing.T) {
			svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
			activeID := seedRerunTask(t, pool, agentID, runtimeID, issueID, activeStatus)

			task, err := svc.RerunIssue(context.Background(), util.MustParseUUID(issueID), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creatorID), nil)
			if err != nil {
				t.Fatalf("RerunIssue: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })

			if got := taskStatus(t, pool, activeID); got != activeStatus {
				t.Fatalf("rerun must NOT cancel an executing task; %s task is now %q", activeStatus, got)
			}
			if util.UUIDToString(task.ID) == util.UUIDToString(activeID) {
				t.Fatal("rerun must enqueue its own row, not hand back the active task")
			}
			if task.Status != "queued" {
				t.Fatalf("the rerun row must wait its turn as queued, got %q", task.Status)
			}
			if !task.ForceFreshSession {
				t.Fatal("a rerun row must carry force_fresh_session (MUL-4869)")
			}
		})
	}
}

// TestRerunIssueReplacesNotYetStartedTask keeps the other half of the contract:
// a row that has not begun executing is still cancelled and replaced. Nothing is
// lost by doing so (no agent was working), the unique index leaves no choice for
// queued/dispatched, and replacing rather than reusing is what keeps the new run
// attributed to the rerunning member instead of inheriting the attribution of
// whoever created the pending row (MUL-4302 §5).
func TestRerunIssueReplacesNotYetStartedTask(t *testing.T) {
	for _, pendingStatus := range []string{"queued", "dispatched"} {
		t.Run(pendingStatus, func(t *testing.T) {
			svc, pool, creatorID, agentID, issueID, runtimeID := rerunQueueFixture(t)
			pendingID := seedRerunTask(t, pool, agentID, runtimeID, issueID, pendingStatus)

			task, err := svc.RerunIssue(context.Background(), util.MustParseUUID(issueID), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creatorID), nil)
			if err != nil {
				t.Fatalf("RerunIssue: %v", err)
			}
			t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID) })

			if util.UUIDToString(task.ID) == util.UUIDToString(pendingID) {
				t.Fatal("expected a fresh rerun task, got the pre-existing pending row")
			}
			if got := taskStatus(t, pool, pendingID); got != "cancelled" {
				t.Fatalf("a not-yet-started %s task must be cancelled to make room for the rerun, got %q", pendingStatus, got)
			}
			if task.Status != "queued" {
				t.Fatalf("rerun task status = %q, want queued", task.Status)
			}
		})
	}
}
