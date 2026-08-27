package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// TestFailTaskAutoRerun_TransientExhaustionCreatesRerun proves that when a
// model-failure task has exhausted its regular retry budget, FailTask
// auto-schedules EXACTLY ONE rerun (resolved to the fallback model) instead of
// terminating for a manual rerun.
func TestFailTaskAutoRerun_TransientExhaustionCreatesRerun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)

	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(ctx, `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Simulate exhaustion: the task is running and already at the attempt ceiling
	// so the in-budget auto-retry will NOT fire — only the auto-rerun should.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now(), attempt=3, max_attempts=3, concrete_model='primary-model', requested_concrete_model='balanced' WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("set running/exhausted: %v", err)
	}
	// Primary is healthy now so that, after FailTask marks it unhealthy in-tx, the
	// resolver falls through to the fallback for the rerun.
	q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"})

	failed, err := svc.FailTask(ctx, task.ID, "connection closed mid-response", "", "", "", string(taskfailure.ReasonAgentProviderNetwork), false, "", "")
	if err != nil {
		t.Fatalf("FailTask: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("failed status %q", failed.Status)
	}

	var (
		count     int
		rerunID   pgtype.UUID
		rerunCnt  int32
		rerunConc pgtype.Text
	)
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, task.ID).Scan(&count); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if count != 1 {
		t.Fatalf("auto-rerun child count = %d, want 1", count)
	}
	if err := pool.QueryRow(ctx, `SELECT id, auto_rerun_count, concrete_model FROM agent_task_queue WHERE parent_task_id=$1`, task.ID).Scan(&rerunID, &rerunCnt, &rerunConc); err != nil {
		t.Fatalf("load rerun child: %v", err)
	}
	if rerunCnt != 1 {
		t.Fatalf("rerun auto_rerun_count = %d, want 1", rerunCnt)
	}
	if !rerunConc.Valid || rerunConc.String != "fallback-a" {
		t.Fatalf("rerun concrete = %q, want fallback-a", rerunConc.String)
	}
}

// TestFailTaskAutoRerun_SecondExhaustionNoRerun is the loop guard: a rerun that
// itself exhausts on a transient reason must NOT spawn yet another auto-rerun
// (auto_rerun_count has already been incremented to 1).
func TestFailTaskAutoRerun_SecondExhaustionNoRerun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)

	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	if _, err := q.UpsertGlobalModelTier(ctx, db.UpsertGlobalModelTierParams{Tier: "balanced", Concrete: "primary-model", FallbackConcrete: []string{"fallback-a"}}); err != nil {
		t.Fatalf("upsert tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(ctx, `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now(), attempt=3, max_attempts=3, concrete_model='primary-model', requested_concrete_model='balanced' WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("set running/exhausted: %v", err)
	}
	q.MarkModelHealthy(ctx, db.MarkModelHealthyParams{WorkspaceID: wsUUID, Concrete: "primary-model"})

	// First exhaustion → auto-rerun created (auto_rerun_count becomes 1).
	if _, err := svc.FailTask(ctx, task.ID, "connection closed mid-response", "", "", "", string(taskfailure.ReasonAgentProviderNetwork), false, "", ""); err != nil {
		t.Fatalf("FailTask (first): %v", err)
	}
	var rerunID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE parent_task_id=$1`, task.ID).Scan(&rerunID); err != nil {
		t.Fatalf("load rerun child: %v", err)
	}

	// Second exhaustion: fail the auto-rerun child itself.
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, rerunID); err != nil {
		t.Fatalf("set rerun running: %v", err)
	}
	if _, err := svc.FailTask(ctx, rerunID, "connection closed mid-response again", "", "", "", string(taskfailure.ReasonAgentProviderNetwork), false, "", ""); err != nil {
		t.Fatalf("FailTask (second): %v", err)
	}

	var grandchildren int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, rerunID).Scan(&grandchildren); err != nil {
		t.Fatalf("count grandchildren: %v", err)
	}
	if grandchildren != 0 {
		t.Fatalf("second-exhaustion auto-rerun count = %d, want 0 (loop guard)", grandchildren)
	}
}

// TestFailTaskAutoRerun_NonTransientNoRerun proves that a NON-transient failure
// reason (even at the attempt ceiling) does NOT trigger the auto-rerun; the task
// fails finally as before.
func TestFailTaskAutoRerun_NonTransientNoRerun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	wsUUID := util.MustParseUUID(workspaceID)

	if _, err := pool.Exec(ctx, `UPDATE agent SET service_tier='balanced' WHERE id=$1`, agentID); err != nil {
		t.Fatalf("set tier: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM model_tier_map WHERE tier='balanced' AND workspace_id IS NULL`)
		pool.Exec(ctx, `DELETE FROM model_health WHERE concrete IN ('primary-model','fallback-a')`)
	})

	svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
	issue := db.Issue{
		ID:           util.MustParseUUID(issueID),
		AssigneeID:   util.MustParseUUID(agentID),
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(userID),
		WorkspaceID:  wsUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
	}
	task, err := svc.EnqueueTaskForIssue(ctx, issue)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now(), attempt=3, max_attempts=3 WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("set running/exhausted: %v", err)
	}

	// A genuine agent-side failure (not a transient provider error).
	if _, err := svc.FailTask(ctx, task.ID, "process crashed", "", "", "", string(taskfailure.ReasonAgentProcessFailure), false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var children int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1`, task.ID).Scan(&children); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if children != 0 {
		t.Fatalf("non-transient auto-rerun child count = %d, want 0", children)
	}
}
