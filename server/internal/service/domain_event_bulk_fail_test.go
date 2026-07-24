package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A single unresolvable "poison" row must not fail-close the whole bulk sweep
// (MUL-4332 review point 2): the resolvable tasks still commit their fact + event
// atomically, and the poison row is skipped (left for ops), not failed.
func TestFailBulkTasksIsolatesPoisonRow(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	seedTask := func() string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0)
			RETURNING id`, agentID, issueID).Scan(&id); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		return id
	}
	goodID := seedTask()
	poisonID := seedTask()
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = ANY($1::uuid[])`, []string{goodID, poisonID})
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = ANY($1::uuid[])`, []string{goodID, poisonID})
	})

	failed, err := svc.FailBulkTasksWithEvents(ctx,
		func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
			good, e := qtx.GetAgentTask(ctx, util.MustParseUUID(goodID))
			if e != nil {
				return nil, e
			}
			poison, e := qtx.GetAgentTask(ctx, util.MustParseUUID(poisonID))
			if e != nil {
				return nil, e
			}
			// A real agent_task_queue row always resolves (agent_id is NOT NULL +
			// FK), so we present the second candidate as a VIEW with its resolvable
			// links stripped — exercising the defensive isolation path directly. The
			// DB row (poisonID) is untouched and must be left un-failed.
			poison.AgentID = pgtype.UUID{}
			poison.IssueID = pgtype.UUID{}
			poison.ChatSessionID = pgtype.UUID{}
			poison.AutopilotRunID = pgtype.UUID{}
			return []db.AgentTaskQueue{good, poison}, nil
		},
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("FailBulkTasksWithEvents returned an error — a poison row must not fail the batch: %v", err)
	}

	// Only the resolvable task is failed and returned.
	if len(failed) != 1 || util.UUIDToString(failed[0].ID) != goodID {
		t.Fatalf("expected only the resolvable task returned as failed, got %+v", failed)
	}
	if s := taskStatusForTest(t, pool, goodID); s != "failed" {
		t.Errorf("resolvable task status = %q, want failed", s)
	}
	if n := subjectEventCount(t, pool, goodID); n != 1 {
		t.Errorf("resolvable task events = %d, want 1", n)
	}
	// The poison row is untouched: not failed, no event.
	if s := taskStatusForTest(t, pool, poisonID); s != "running" {
		t.Errorf("poison task status = %q, want running (must be left for ops, not failed)", s)
	}
	if n := subjectEventCount(t, pool, poisonID); n != 0 {
		t.Errorf("poison task events = %d, want 0 (no valid event is possible without a workspace)", n)
	}
}

// A transient failure inside the fail transaction must roll the whole batch back
// — no fact is committed without its event — so the task stays selectable and the
// next sweep tick reclaims it (MUL-4332 review point 2).
func TestFailBulkTasksTransientFailureRecoversNextTick(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)

	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'running', 0)
		RETURNING id`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, taskID)
	})

	sel := func(qtx *db.Queries) ([]db.AgentTaskQueue, error) {
		row, e := qtx.GetAgentTask(ctx, util.MustParseUUID(taskID))
		if e != nil {
			return nil, e
		}
		return []db.AgentTaskQueue{row}, nil
	}

	// Tick 1: the fail step errors (a transient DB blip). The whole batch rolls
	// back — the task is NOT failed and no event is written.
	boom := errors.New("transient DB blip")
	if _, err := svc.FailBulkTasksWithEvents(ctx, sel,
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return nil, boom
		}); !errors.Is(err, boom) {
		t.Fatalf("expected the transient error to surface, got %v", err)
	}
	if s := taskStatusForTest(t, pool, taskID); s != "running" {
		t.Fatalf("after rollback task status = %q, want running (no fact may commit)", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 0 {
		t.Fatalf("after rollback events = %d, want 0", n)
	}

	// Tick 2: the same still-selectable task fails cleanly.
	failed, err := svc.FailBulkTasksWithEvents(ctx, sel,
		func(qtx *db.Queries, ids []pgtype.UUID) ([]db.AgentTaskQueue, error) {
			return qtx.FailAgentTasksByIDs(ctx, db.FailAgentTasksByIDsParams{
				Ids:           ids,
				Error:         pgtype.Text{String: "runtime went offline", Valid: true},
				FailureReason: pgtype.Text{String: "runtime_offline", Valid: true},
			})
		})
	if err != nil {
		t.Fatalf("retry FailBulkTasksWithEvents: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("retry expected 1 failed task, got %d", len(failed))
	}
	if s := taskStatusForTest(t, pool, taskID); s != "failed" {
		t.Errorf("after retry task status = %q, want failed", s)
	}
	if n := subjectEventCount(t, pool, taskID); n != 1 {
		t.Errorf("after retry events = %d, want 1", n)
	}
}

// The stuck-issue reset must re-decide UNDER the row lock: if a user moves the
// issue to a terminal status in the window between the pre-tx status read and the
// reset lock, the sweeper must not reopen it (MUL-4332 review point 4). We force
// exactly that interleaving by holding the row lock with an uncommitted move to
// 'done' until the reset is blocked on the lock.
func TestHandleFailedTasksDoesNotReopenUserCompletedIssue(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	_, _, agentID, issueID := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("set issue in_progress: %v", err)
	}
	// A non-retryable failure so no auto-retry short-circuits the reset branch.
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, failure_reason, priority, completed_at)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'failed', 'agent_error', 0, now())
		RETURNING id`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed failed task: %v", err)
	}
	failedTask, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load failed task: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		pool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, issueID)
	})

	// Holder: lock the issue row and move it to 'done' (uncommitted). A plain
	// read (HandleFailedTasks' pre-tx GetIssue) still sees the committed
	// 'in_progress', so it enters the reset branch — then blocks on this lock.
	locked := make(chan struct{})
	release := make(chan struct{})
	go func() {
		tx, e := pool.Begin(context.Background())
		if e != nil {
			t.Errorf("holder begin: %v", e)
			close(locked)
			return
		}
		defer tx.Rollback(context.Background())
		if _, e := tx.Exec(context.Background(), `SELECT status FROM issue WHERE id = $1 FOR UPDATE`, issueID); e != nil {
			t.Errorf("holder lock: %v", e)
			close(locked)
			return
		}
		if _, e := tx.Exec(context.Background(), `UPDATE issue SET status = 'done' WHERE id = $1`, issueID); e != nil {
			t.Errorf("holder update: %v", e)
			close(locked)
			return
		}
		close(locked)
		<-release
		tx.Commit(context.Background())
	}()

	<-locked
	done := make(chan struct{})
	go func() {
		svc.HandleFailedTasks(ctx, []db.AgentTaskQueue{failedTask})
		close(done)
	}()
	// Let HandleFailedTasks read the in_progress snapshot and block on the reset
	// lock, then let the user's 'done' commit win the row.
	time.Sleep(400 * time.Millisecond)
	close(release)
	<-done

	if s := issueStatusForTest(t, pool, issueID); s != "done" {
		t.Fatalf("issue status = %q, want done — a user-completed issue must not be reopened by the stuck-issue reset (review point 4)", s)
	}
}

func taskStatusForTest(t *testing.T, pool *pgxpool.Pool, taskID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&s); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	return s
}

func issueStatusForTest(t *testing.T, pool *pgxpool.Pool, issueID string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&s); err != nil {
		t.Fatalf("query issue status: %v", err)
	}
	return s
}

func subjectEventCount(t *testing.T, pool *pgxpool.Pool, subjectID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM domain_event WHERE subject_id = $1`, subjectID).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}
