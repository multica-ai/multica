package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// newBindCASFixture creates a workspace + agent + autopilot + an issue_created
// run and returns the AutopilotService, its queries, the pool, the run, and a
// helper that inserts a real agent_task_queue row (task_id carries a legacy FK,
// so binds must reference actual tasks) and returns its id.
func newBindCASFixture(t *testing.T) (*AutopilotService, *db.Queries, *pgxpool.Pool, db.AutopilotRun, func() pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)

	suffix := time.Now().UnixNano()
	var userID, workspaceID, runtimeID, agentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('bind cas', $1) RETURNING id`,
		fmt.Sprintf("bind-cas-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ('bind cas', $1, '', 'BCS') RETURNING id`,
		fmt.Sprintf("bind-cas-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, NULL, 'bind cas rt', 'cloud', 'bind_cas', 'online', 'rt', '{}'::jsonb, now(), 'private', $2) RETURNING id
	`, workspaceID, userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'bind cas agent', '', 'cloud', '{}'::jsonb, $2, 'private', 1, $3) RETURNING id
	`, workspaceID, runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	ap, err := queries.CreateAutopilot(ctx, db.CreateAutopilotParams{
		WorkspaceID:   mustUUID(t, workspaceID),
		Title:         "bind cas",
		AssigneeType:  "agent",
		AssigneeID:    mustUUID(t, agentID),
		Status:        "active",
		ExecutionMode: "create_issue",
		CreatedByType: "member",
		CreatedByID:   mustUUID(t, userID),
	})
	if err != nil {
		t.Fatalf("CreateAutopilot: %v", err)
	}
	run, err := queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		AutopilotID: ap.ID,
		Source:      "manual",
		Status:      "issue_created",
	})
	if err != nil {
		t.Fatalf("CreateAutopilotRun: %v", err)
	}

	newTask := func() pgtype.UUID {
		var id string
		if err := pool.QueryRow(context.Background(),
			`INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority) VALUES ($1, $2, 'queued', 0) RETURNING id`,
			agentID, runtimeID).Scan(&id); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		return mustUUID(t, id)
	}

	bus := events.New()
	taskSvc := NewTaskService(queries, pool, nil, bus)
	svc := NewAutopilotService(queries, pool, bus, taskSvc)
	return svc, queries, pool, run, newTask
}

// TestBindAutopilotRunTaskIsIdempotentAndExclusive covers MUL-4809 §4.1 P0-1/P0-2:
// the first dispatched task owns the run; re-binding the same task is idempotent,
// a different task is refused, and once the run is terminal the bind reports the
// authoritative terminal run rather than a phantom running state.
func TestBindAutopilotRunTaskIsIdempotentAndExclusive(t *testing.T) {
	ctx := context.Background()
	svc, queries, _, run, newTask := newBindCASFixture(t)
	taskA := newTask()
	taskB := newTask()

	// First bind wins.
	bound, err := svc.bindAutopilotRunTask(ctx, run.ID, taskA)
	if err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if bound.Status != "running" || bound.TaskID.Bytes != taskA.Bytes {
		t.Fatalf("after first bind: status=%q task_id=%x", bound.Status, bound.TaskID.Bytes)
	}

	// Re-binding the SAME task is idempotent.
	again, err := svc.bindAutopilotRunTask(ctx, run.ID, taskA)
	if err != nil || again.TaskID.Bytes != taskA.Bytes {
		t.Fatalf("idempotent re-bind: err=%v task_id=%x", err, again.TaskID.Bytes)
	}

	// A DIFFERENT task must be refused, and the run must still point at A.
	if _, err := svc.bindAutopilotRunTask(ctx, run.ID, taskB); err == nil {
		t.Fatal("re-bind to a different task should have failed")
	}
	cur, _ := queries.GetAutopilotRun(ctx, run.ID)
	if cur.TaskID.Bytes != taskA.Bytes {
		t.Fatalf("run rebound to a different task: task_id=%x", cur.TaskID.Bytes)
	}

	// Once the run is terminal AND owned by A, a late bind by the SAME task A
	// returns the authoritative terminal run (idempotent, not a resurrection).
	if _, err := queries.UpdateAutopilotRunCompleted(ctx, db.UpdateAutopilotRunCompletedParams{ID: run.ID}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	term, err := svc.bindAutopilotRunTask(ctx, run.ID, taskA)
	if err != nil {
		t.Fatalf("bind-after-terminal by the owning task should succeed, got err: %v", err)
	}
	if term.Status != "completed" {
		t.Fatalf("bind after terminal: status=%q, want completed", term.Status)
	}

	// But a DIFFERENT task B binding after A already finalized the run must be
	// refused — otherwise B's competing dispatch would be reported as landed. The
	// run must still be owned by A.
	if _, err := svc.bindAutopilotRunTask(ctx, run.ID, taskB); err == nil {
		t.Fatal("bind-after-terminal by a different task must fail, not be reported as dispatched")
	}
	owner, _ := queries.GetAutopilotRun(ctx, run.ID)
	if owner.TaskID.Bytes != taskA.Bytes || owner.Status != "completed" {
		t.Fatalf("terminal run ownership changed: status=%q task_id=%x", owner.Status, owner.TaskID.Bytes)
	}
}

// TestBindAutopilotRunTaskFailsWhenRunGone covers the "cannot confirm authoritative
// state" case (MUL-4809 §4.1 P0-2): if the run row disappears, the bind must
// error rather than report a phantom dispatch success (which would let a
// schedule/webhook caller record the delivery as dispatched).
func TestBindAutopilotRunTaskFailsWhenRunGone(t *testing.T) {
	ctx := context.Background()
	svc, _, pool, run, newTask := newBindCASFixture(t)
	task := newTask()

	if _, err := pool.Exec(ctx, `DELETE FROM autopilot_run WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("delete run: %v", err)
	}

	if _, err := svc.bindAutopilotRunTask(ctx, run.ID, task); err == nil {
		t.Fatal("bind on a missing run must return an error, not a phantom success")
	}
}
