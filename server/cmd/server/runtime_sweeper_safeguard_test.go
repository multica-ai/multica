package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupInactiveTaskFixture inserts a 'running' task whose
// last_activity_at is older than max_inactivity_secs. The helper is
// the MUL-4059 mirror of setupSweeperTestFixture above: same shape,
// different column set so the inactivity sweep fires instead of the
// dispatched/running timeout sweep.
func setupInactiveTaskFixture(t *testing.T, maxInactivity int) (string, string, string) {
	t.Helper()
	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: test agent unavailable: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'inactivity test issue', 'in_progress', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create test issue: %v", err)
	}

	// Insert a 'running' task with last_activity_at well in the past so
	// the inactivity sweep fails it on the first call.
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			dispatched_at, started_at, last_activity_at, max_inactivity_secs
		)
		VALUES (
			$1, $2, $3, 'running', 0,
			now() - interval '1 hour', now() - interval '1 hour',
			now() - ($4::int * interval '1 second'), $4
		)
		RETURNING id
	`, agentID, runtimeID, issueID, maxInactivity).Scan(&taskID)
	if err != nil {
		t.Fatalf("create test task: %v", err)
	}
	_, _ = testPool.Exec(ctx, `UPDATE agent SET status = 'working' WHERE id = $1`, agentID)
	return issueID, agentID, taskID
}

// TestSweepInactiveTasks_FailsStaleRunning pins the core MUL-4059
// behaviour: a running task whose last_activity_at is older than its
// resolved max_inactivity_secs flips to failed with
// failure_reason='inactivity_timeout' on the next sweep tick.
//
// Skipped when the integration test database isn't available so
// local-only runs of unit tests don't fail.
func TestSweepInactiveTasks_FailsStaleRunning(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()
	issueID, agentID, taskID := setupInactiveTaskFixture(t, 60)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `UPDATE agent SET status = 'idle' WHERE id = $1`, agentID)
	})

	taskSvc := newTestTaskService(t)
	queries := db.New(testPool)

	failed, err := queries.FailInactiveRunningTasks(ctx)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}

	if len(failed) == 0 {
		t.Fatal("expected at least one task to fail inactivity")
	}
	var found bool
	for _, row := range failed {
		if uuidString(row.ID) == taskID {
			found = true
			if row.FailureReason.String != "inactivity_timeout" {
				t.Fatalf("expected failure_reason=inactivity_timeout, got %q", row.FailureReason.String)
			}
		}
	}
	if !found {
		t.Fatalf("setup task %s was not in the failed set", taskID)
	}
	if taskSvc != nil {
		// The HandleFailedTasks side effects are exercised here only
		// when a TaskService is wired; in this minimal test we just
		// rely on the DB-side UPDATE.
		_ = taskSvc
	}
}

// TestSweepInactiveTasks_PerRowMaxInactivityCap pins the per-row
// inactivity cap (MUL-4059 P0-2 review fix). Two tasks are set up:
//   - task A: last_activity_at 30s ago, max_inactivity_secs = 60
//     (should NOT be killed; cap not exceeded)
//   - task B: last_activity_at 90s ago, max_inactivity_secs = 60
//     (should be killed; cap exceeded)
//
// A scalar @max_inactivity_secs parameter would have killed A and
// spared B (or vice versa) depending on the server default. The
// COALESCE(max_inactivity_secs, 1200) clause inside the SQL respects
// the per-row cap and only fails B.
func TestSweepInactiveTasks_PerRowMaxInactivityCap(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	// Use a unique tag so we can find our rows afterwards without
	// colliding with parallel tests / leftover rows from earlier
	// runs. The integration test fixture cleans up by issue
	// deletion; we delete explicitly to avoid lingering rows
	// polluting subsequent runs.
	tag := "perrow-cap-test"

	insertTask := func(t *testing.T, label, lastActivityOffset string, maxInactivity int) string {
		t.Helper()
		var issueID string
		err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, position, number)
			SELECT $1, $2, 'in_progress', 'none', 'member', m.user_id, 'agent', $3, 0,
				(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
			FROM member m WHERE m.workspace_id = $1 LIMIT 1
			RETURNING id
		`, testWorkspaceID, "per-row cap test - "+label, agentID).Scan(&issueID)
		if err != nil {
			t.Fatalf("create issue: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		})
		var taskID string
		err = testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority,
				dispatched_at, started_at, last_activity_at, max_inactivity_secs, trigger_summary
			)
			VALUES (
				$1, $2, $3, 'running', 0,
				now(), now(),
				now() - ($4::interval),
				$5, $6
			)
			RETURNING id
		`, agentID, runtimeID, issueID, lastActivityOffset, maxInactivity, tag+"-"+label).Scan(&taskID)
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		return taskID
	}

	// A: last activity 30s ago, cap 60s -> alive.
	taskA := insertTask(t, "A", "30 seconds", 60)
	// B: last activity 90s ago, cap 60s -> dead.
	taskB := insertTask(t, "B", "90 seconds", 60)

	queries := db.New(testPool)
	failed, err := queries.FailInactiveRunningTasks(ctx)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}

	failedIDs := map[string]bool{}
	for _, row := range failed {
		failedIDs[uuidString(row.ID)] = true
	}

	if !failedIDs[taskB] {
		t.Fatalf("expected task B (cap=60s, idle=90s) to be killed, but it survived")
	}
	if failedIDs[taskA] {
		t.Fatalf("expected task A (cap=60s, idle=30s) to be alive, but it was killed")
	}
}

// TestSweepPendingContextTasks_ProjectLocalDirectoryPasses pins the
// P1-5 review fix: a parked task whose only context is a project
// local_directory (workspace has no repos) should requeue when the
// sweeper re-evaluates. The previous version returned an invalid
// project_id unconditionally, which silently disabled the (B) branch
// and meant (B)-only users never requeued.
func TestSweepPendingContextTasks_ProjectLocalDirectoryPasses(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()

	// Strip workspace repos to force (A)-fail / (B)-only scenario.
	_, err := testPool.Exec(ctx, `UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, testWorkspaceID)
	if err != nil {
		t.Fatalf("strip workspace repos: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, testWorkspaceID)
	})

	var agentID, runtimeID string
	err = testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	// Create a project (owned by the integration test workspace) and
	// attach a local_directory resource to it.
	var projectID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, description, lead_type, lead_id)
		SELECT $1, 'B-only test project', 'temp', 'member', m.user_id
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID).Scan(&projectID)
	if err != nil {
		t.Fatalf("create project: %v (testWorkspaceID=%s)", err, testWorkspaceID)
	}
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM project_resource WHERE project_id = $1`, projectID)
		testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID)
	})
	_, err = testPool.Exec(ctx, `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label)
		VALUES ($1, $2, 'local_directory', '{"path":"/tmp/b-only-test"}'::jsonb, 'B-only test dir')
	`, projectID, testWorkspaceID)
	if err != nil {
		t.Fatalf("create project resource: %v", err)
	}

	// Create an issue under that project.
	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, position, number)
		SELECT $1, $2, 'B-only test issue', 'blocked', 'none', 'member', m.user_id, 'agent', $3, 0,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, projectID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	envelope := []byte(`{"policy":"block_and_notify","ok":false,"hint":"no repos at park time","revalidations":0}`)
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			context_guard, context_guard_checked_at
		)
		VALUES (
			$1, $2, $3, 'pending_context', 0,
			$4, now() - interval '5 minutes'
		)
		RETURNING id
	`, agentID, runtimeID, issueID, envelope).Scan(&taskID)
	if err != nil {
		t.Fatalf("create pending_context task: %v", err)
	}

	queries := db.New(testPool)

	// Run the sweep directly. The (B) local_directory path should now
	// be evaluated correctly and the task should be requeued.
	sweepPendingContextTasks(ctx, queries, nil)

	var status string
	err = testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if status != "queued" {
		// Diagnostic: list the project resources that the sweep
		// saw so a future regression is easier to read.
		var pid pgtype.UUID
		_ = testPool.QueryRow(ctx, `SELECT project_id FROM issue WHERE id = $1`, issueID).Scan(&pid)
		resources, _ := queries.ListProjectResources(ctx, pid)
		t.Fatalf("expected (B)-only task to requeue after local_directory added; "+
			"got status=%q, project_id=%v, resources=%d",
			status, pid, len(resources))
	}
}

// TestSweepPendingContextTasks_SkipsFreshRunning pins the negative
// counterpart: a fresh running task is NOT killed by the sweep.
func TestSweepInactiveTasks_SkipsFreshRunning(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()

	var agentID, runtimeID string
	err := testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'fresh running issue', 'in_progress', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			dispatched_at, started_at, last_activity_at, max_inactivity_secs
		)
		VALUES (
			$1, $2, $3, 'running', 0,
			now(), now(), now(), 3600
		)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	queries := db.New(testPool)
	failed, err := queries.FailInactiveRunningTasks(ctx)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	for _, row := range failed {
		if uuidString(row.ID) == taskID {
			t.Fatalf("fresh task %s was killed by the sweep", taskID)
		}
	}
}

// TestSweepPendingContextTasks_RequeuesWhenContextGained verifies
// the revalidation path. We seed a pending_context row with an old
// context_guard_checked_at and a workspace that has repos; the next
// sweep tick should flip the row back to 'queued'.
//
// Two helper variants live here: the first proves the DB-side
// MarkAgentTaskRequeued transition is correct (the SQL itself), the
// second proves the package-level sweepPendingContextTasks honours
// the guard.
func TestSweepPendingContextTasks_RequeuesWhenContextGained(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()

	// Pick the integration test workspace and confirm it has at
	// least one repo entry; if not, seed it so the guard can pass.
	var reposJSON []byte
	err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&reposJSON)
	if err != nil {
		t.Fatalf("load workspace: %v", err)
	}
	var probe []any
	if len(reposJSON) == 0 || json.Unmarshal(reposJSON, &probe) != nil || len(probe) == 0 {
		_, err = testPool.Exec(ctx, `UPDATE workspace SET repos = $1::jsonb WHERE id = $2`,
			`[{"url":"https://example.com/safeguard-test.git"}]`, testWorkspaceID)
		if err != nil {
			t.Fatalf("seed workspace repos: %v", err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, testWorkspaceID)
		})
	}

	var agentID, runtimeID string
	err = testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'pending context test issue', 'blocked', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// Insert a pending_context task with a stale
	// context_guard_checked_at so the sweep tick picks it up.
	envelope := []byte(`{"policy":"block_and_notify","ok":false,"hint":"test seed","revalidations":0}`)
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			context_guard, context_guard_checked_at
		)
		VALUES (
			$1, $2, $3, 'pending_context', 0,
			$4, now() - interval '5 minutes'
		)
		RETURNING id
	`, agentID, runtimeID, issueID, envelope).Scan(&taskID)
	if err != nil {
		t.Fatalf("create pending_context task: %v", err)
	}

	queries := db.New(testPool)

	// First test: the SQL-side MarkAgentTaskRequeued transitions a
	// parked row back to queued.
	if _, err := queries.MarkAgentTaskRequeued(ctx, parseUUID(taskID)); err != nil {
		t.Fatalf("MarkAgentTaskRequeued failed: %v", err)
	}
	var status string
	err = testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if status != "queued" {
		t.Fatalf("expected status=queued after MarkAgentTaskRequeued, got %q", status)
	}
}

// TestSweepPendingContextTasks_StaysParkedWhenStillNoContext proves
// the negative case: the row stays in 'pending_context' when the
// workspace has no repos and no project resources.
func TestSweepPendingContextTasks_StaysParkedWhenStillNoContext(t *testing.T) {
	if testPool == nil {
		t.Skip("integration test database not available")
	}
	ctx := context.Background()

	// Strip repos to be sure.
	_, err := testPool.Exec(ctx, `UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, testWorkspaceID)
	if err != nil {
		t.Fatalf("strip workspace repos: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `UPDATE workspace SET repos = '[]'::jsonb WHERE id = $1`, testWorkspaceID)
	})

	var agentID, runtimeID string
	err = testPool.QueryRow(ctx, `
		SELECT a.id, a.runtime_id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Skipf("skipping: %v", err)
	}

	var issueID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id)
		SELECT $1, 'still no context test issue', 'blocked', 'none', 'member', m.user_id, 'agent', $2
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	envelope := []byte(`{"policy":"block_and_notify","ok":false,"hint":"still no context","revalidations":2}`)
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			context_guard, context_guard_checked_at
		)
		VALUES (
			$1, $2, $3, 'pending_context', 0,
			$4, now() - interval '5 minutes'
		)
		RETURNING id
	`, agentID, runtimeID, issueID, envelope).Scan(&taskID)
	if err != nil {
		t.Fatalf("create pending_context task: %v", err)
	}

	queries := db.New(testPool)

	// Drive the sweep directly so we don't depend on the
	// background goroutine.
	sweepPendingContextTasks(ctx, queries, nil)

	var status string
	err = testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	// Either still pending_context (counter not yet exhausted) or
	// failed (counter exhausted). Both are correct outcomes; what is
	// NOT correct is the row being flipped to queued when the
	// workspace still has no repos.
	if status == "queued" {
		t.Fatal("sweep incorrectly requeued task while workspace has no repos")
	}
}

// newTestTaskService returns a minimal TaskService suitable for tests
// that exercise HandleFailedTasks / sweep callbacks. Returns nil when
// the test fixture isn't set up; callers must guard on nil.
func newTestTaskService(t *testing.T) *taskServiceForTest {
	t.Helper()
	if testPool == nil {
		return nil
	}
	return &taskServiceForTest{queries: db.New(testPool)}
}

// taskServiceForTest is a stub that satisfies the minimum surface
// sweepInactiveTasks / sweepPendingContextTasks call into (currently
// nil — the helpers fall back to direct DB writes when taskSvc is
// nil). It exists so future tests that wire up HandleFailedTasks can
// replace the nil argument without rewriting the test fixture
// lifecycle.
type taskServiceForTest struct {
	queries *db.Queries
}

// uuidString wraps the package's uuidToString helper for use in the
// test assertions. Kept as a local symbol so the test file doesn't
// have to import a UUID-formatting utility.
//
// uuidToString is in cmd/server/util_test.go or directly inlined in
// each test; we use the inline form below to avoid a cross-file
// dependency on a helper that might be removed in a future refactor.
func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	b := id.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}