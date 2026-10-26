package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

// TestPrecedenceDBDefault: without an explicit budget, the actual DB default wins.
// Run only after PR #7250's pending migration (DEFAULT 4) is applied in isolated CI.
func TestPrecedenceDBDefault(t *testing.T) {
	svc, pool, creator, _, issue, _ := rerunQueueFixture(t)
	ctx := context.Background()
	var dbDefault string
	if err := pool.QueryRow(ctx, "SELECT column_default FROM information_schema.columns WHERE table_schema='public' AND table_name='agent_task_queue' AND column_name='max_attempts'").Scan(&dbDefault); err != nil {
		t.Fatal(err)
	}
	if dbDefault != "4" && dbDefault != "(4)" {
		t.Fatalf("expected PR7250 DB default 4, got %q", dbDefault)
	}
	task, err := svc.RerunIssue(ctx, util.MustParseUUID(issue), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creator), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE id=$1", task.ID) })
	if task.MaxAttempts != 4 {
		t.Fatalf("no explicit budget must inherit default 4, got %d", task.MaxAttempts)
	}
	t.Logf("PASS_PRECEDENCE_DEFAULT: absent task budget / no workspace -> 4")
}

func TestPrecedenceWorkspaceOverDBDefault(t *testing.T) {
	svc, pool, creator, agent, issue, _ := rerunQueueFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings=jsonb_set(COALESCE(settings,'{}'::jsonb), '{agent_task}', '{"max_attempts":3}'::jsonb, true) WHERE id=(SELECT workspace_id FROM agent WHERE id=$1)`, agent); err != nil {
		t.Fatal(err)
	}
	task, err := svc.RerunIssue(ctx, util.MustParseUUID(issue), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creator), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE id=$1", task.ID) })
	if task.MaxAttempts != 3 {
		t.Fatalf("workspace budget 3 should supersede default 4, got %d", task.MaxAttempts)
	}
	t.Logf("PASS_PRECEDENCE_WORKSPACE: absent task budget / workspace=3 -> 3")
}

func TestPrecedenceExplicitOneShotWinsWorkspace(t *testing.T) {
	svc, pool, creator, agent, issue, _ := rerunQueueFixture(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE workspace SET settings=jsonb_set(COALESCE(settings,'{}'::jsonb), '{agent_task}', '{"max_attempts":4}'::jsonb, true) WHERE id=(SELECT workspace_id FROM agent WHERE id=$1)`, agent); err != nil {
		t.Fatal(err)
	}
	var existing int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatalf("fixture must start at 0 tasks, got %d", existing)
	}
	task, err := svc.RerunIssue(ctx, util.MustParseUUID(issue), pgtype.UUID{}, pgtype.UUID{}, util.MustParseUUID(creator), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE id=$1", task.ID) })
	var persisted int32
	var contextHasMarker bool
	if err := pool.QueryRow(ctx, `SELECT max_attempts, context @> '{"explicit_single_attempt_budget": true}'::jsonb FROM agent_task_queue WHERE id=$1`, task.ID).Scan(&persisted, &contextHasMarker); err != nil {
		t.Fatal(err)
	}
	if task.MaxAttempts != 1 || persisted != 1 || !contextHasMarker {
		t.Fatalf("requested=1 but returned=%d persisted=%d marker=%v", task.MaxAttempts, persisted, contextHasMarker)
	}
	if _, err := pool.Exec(ctx, "UPDATE agent_task_queue SET status='running' WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FailTask(ctx, task.ID, "API Error: Connection closed mid-response.", "", "", "", "agent_error.provider_network", false, "", ""); err != nil {
		t.Fatal(err)
	}
	var children int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1", task.ID).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if children != 0 {
		t.Fatalf("single attempt generated %d retries", children)
	}
	t.Logf("PASS_PRECEDENCE_EXPLICIT: requested=1 / workspace=4 -> stored=1, retry_children=0")
}
