package migrations_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}
	testPool = pool
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// ---------- agent_runtime.owner_id ----------

func TestAgentRuntimeHasOwnerID(t *testing.T) {
	ctx := context.Background()
	var exists bool
	err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'agent_runtime' AND column_name = 'owner_id'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("agent_runtime.owner_id column does not exist")
	}
}

func TestAgentRuntimeOwnerIDForeignKey(t *testing.T) {
	ctx := context.Background()

	// Create a test user
	var userID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('migration-test', 'migration-032-test@test.ai') RETURNING id
	`).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	// Create a workspace
	var wsID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('mig-test', 'mig-032-test', 'MIG') RETURNING id
	`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	// Insert runtime with valid owner_id
	var runtimeID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'test-daemon-032', 'Test Runtime', 'local', 'claude', 'online', '', '{}'::jsonb, $2)
		RETURNING id
	`, wsID, userID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("insert runtime with owner_id: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	// Verify it's stored
	var ownerID *string
	err = testPool.QueryRow(ctx, `SELECT owner_id::text FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&ownerID)
	if err != nil {
		t.Fatalf("read owner_id: %v", err)
	}
	if ownerID == nil || *ownerID != userID {
		t.Fatalf("expected owner_id=%s, got %v", userID, ownerID)
	}

	// owner_id should accept NULL (backwards compat)
	var runtimeID2 string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'test-daemon-032-null', 'Test Runtime Null', 'local', 'codex', 'online', '', '{}'::jsonb, NULL)
		RETURNING id
	`, wsID).Scan(&runtimeID2)
	if err != nil {
		t.Fatalf("insert runtime with NULL owner_id: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID2)
	})

	// FK constraint: invalid user ID should fail
	_, err = testPool.Exec(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id)
		VALUES ($1, 'test-daemon-032-bad', 'Bad', 'local', 'claude', 'online', '', '{}'::jsonb, '00000000-0000-0000-0000-000000000099')
	`, wsID)
	if err == nil {
		t.Fatal("expected FK violation for invalid owner_id, got nil")
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE daemon_id = 'test-daemon-032-bad' AND workspace_id = $1`, wsID)
	}
}

// ---------- agent_runtime.visibility ----------

func TestAgentRuntimeHasVisibility(t *testing.T) {
	ctx := context.Background()
	var exists bool
	err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'agent_runtime' AND column_name = 'visibility'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("agent_runtime.visibility column does not exist")
	}
}

func TestAgentRuntimeVisibilityDefault(t *testing.T) {
	ctx := context.Background()

	var wsID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('vis-test', 'vis-032-test', 'VIS') RETURNING id
	`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	var runtimeID, visibility string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, 'vis-daemon', 'Vis Runtime', 'local', 'claude', 'online', '', '{}'::jsonb)
		RETURNING id, visibility
	`, wsID).Scan(&runtimeID, &visibility)
	if err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	if visibility != "workspace" {
		t.Fatalf("expected default visibility 'workspace', got '%s'", visibility)
	}
}

func TestAgentRuntimeVisibilityCheckConstraint(t *testing.T) {
	ctx := context.Background()

	var wsID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('chk-test', 'chk-032-test', 'CHK') RETURNING id
	`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	// Valid values: 'workspace' and 'private'
	for _, vis := range []string{"workspace", "private"} {
		daemon := fmt.Sprintf("chk-daemon-%s", vis)
		_, err := testPool.Exec(ctx, `
			INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, visibility)
			VALUES ($1, $2, 'Check', 'local', 'claude', 'online', '', '{}'::jsonb, $3)
		`, wsID, daemon, vis)
		if err != nil {
			t.Fatalf("visibility=%q should be valid, got: %v", vis, err)
		}
		t.Cleanup(func() {
			testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE daemon_id = $1 AND workspace_id = $2`, daemon, wsID)
		})
	}

	// Invalid value should fail
	_, err = testPool.Exec(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, visibility)
		VALUES ($1, 'chk-daemon-bad', 'Bad', 'local', 'claude', 'online', '', '{}'::jsonb, 'invalid')
	`, wsID)
	if err == nil {
		t.Fatal("expected CHECK violation for visibility='invalid', got nil")
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE daemon_id = 'chk-daemon-bad' AND workspace_id = $1`, wsID)
	}
}

// ---------- agent.approval_required ----------

func TestAgentHasApprovalRequired(t *testing.T) {
	ctx := context.Background()
	var exists bool
	err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'agent' AND column_name = 'approval_required'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("agent.approval_required column does not exist")
	}
}

func TestAgentApprovalRequiredDefault(t *testing.T) {
	ctx := context.Background()

	var wsID, runtimeID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix) VALUES ('appr-test', 'appr-032-test', 'APR') RETURNING id
	`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, 'appr-daemon', 'Appr Runtime', 'local', 'claude', 'online', '', '{}'::jsonb)
		RETURNING id
	`, wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	var approvalRequired bool
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, tools, triggers)
		VALUES ($1, 'Approval Test Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, '[]'::jsonb, '[]'::jsonb)
		RETURNING id, approval_required
	`, wsID, runtimeID).Scan(&agentID, &approvalRequired)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	if approvalRequired != false {
		t.Fatalf("expected approval_required default false, got %v", approvalRequired)
	}
}

// ---------- agent_task_queue.requested_by ----------

func TestAgentTaskQueueHasRequestedBy(t *testing.T) {
	ctx := context.Background()
	var exists bool
	err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'agent_task_queue' AND column_name = 'requested_by'
		)
	`).Scan(&exists)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if !exists {
		t.Fatal("agent_task_queue.requested_by column does not exist")
	}
}

// ---------- agent_task_queue.status includes pending_approval ----------

func TestAgentTaskQueuePendingApprovalStatus(t *testing.T) {
	ctx := context.Background()

	// Set up the full chain: workspace -> runtime -> agent -> issue -> task
	var wsID, userID, runtimeID, agentID, issueID string

	err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('pa-test', 'pa-032-test@test.ai') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID) })

	err = testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, issue_prefix) VALUES ('pa-test', 'pa-032-test', 'PAT') RETURNING id`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID) })

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, 'pa-daemon', 'PA Runtime', 'local', 'claude', 'online', '', '{}'::jsonb) RETURNING id
	`, wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, tools, triggers)
		VALUES ($1, 'PA Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, '[]'::jsonb, '[]'::jsonb) RETURNING id
	`, wsID, runtimeID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'PA Test Issue', 'todo', 'none', 'member', $2, 9900, 0) RETURNING id
	`, wsID, userID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Insert task with status 'pending_approval'
	var taskID, taskStatus string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, requested_by)
		VALUES ($1, $2, $3, 'pending_approval', 0, $4)
		RETURNING id, status
	`, agentID, runtimeID, issueID, userID).Scan(&taskID, &taskStatus)
	if err != nil {
		t.Fatalf("insert task with pending_approval: %v", err)
	}

	if taskStatus != "pending_approval" {
		t.Fatalf("expected status 'pending_approval', got '%s'", taskStatus)
	}

	// Verify it's not claimable (ClaimAgentTask only picks 'queued')
	var claimableCount int
	err = testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND status = 'queued'
	`, agentID).Scan(&claimableCount)
	if err != nil {
		t.Fatalf("count claimable: %v", err)
	}
	if claimableCount != 0 {
		t.Fatalf("pending_approval task should not be claimable, got %d queued tasks", claimableCount)
	}

	// Transition to queued (approve)
	_, err = testPool.Exec(ctx, `
		UPDATE agent_task_queue SET status = 'queued' WHERE id = $1 AND status = 'pending_approval'
	`, taskID)
	if err != nil {
		t.Fatalf("approve task: %v", err)
	}

	// Now it should be claimable
	err = testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND status = 'queued'
	`, agentID).Scan(&claimableCount)
	if err != nil {
		t.Fatalf("count claimable after approve: %v", err)
	}
	if claimableCount != 1 {
		t.Fatalf("after approval, expected 1 queued task, got %d", claimableCount)
	}
}

// ---------- Unique index covers pending_approval ----------

func TestUniqueIndexIncludesPendingApproval(t *testing.T) {
	ctx := context.Background()

	var wsID, userID, runtimeID, agentID, issueID string

	err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('idx-test', 'idx-032-test@test.ai') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID) })

	err = testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, issue_prefix) VALUES ('idx-test', 'idx-032-test', 'IDX') RETURNING id`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID) })

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, 'idx-daemon', 'IDX Runtime', 'local', 'claude', 'online', '', '{}'::jsonb) RETURNING id
	`, wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, tools, triggers)
		VALUES ($1, 'IDX Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, '[]'::jsonb, '[]'::jsonb) RETURNING id
	`, wsID, runtimeID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'IDX Test Issue', 'todo', 'none', 'member', $2, 9901, 0) RETURNING id
	`, wsID, userID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Insert a pending_approval task
	_, err = testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'pending_approval', 0)
	`, agentID, runtimeID, issueID)
	if err != nil {
		t.Fatalf("insert first pending_approval task: %v", err)
	}

	// Inserting another pending_approval or queued task for the same issue should fail (unique index)
	_, err = testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, issueID)
	if err == nil {
		t.Fatal("expected unique violation: should not allow both pending_approval and queued for same issue")
	}

	_, err = testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'pending_approval', 0)
	`, agentID, runtimeID, issueID)
	if err == nil {
		t.Fatal("expected unique violation: should not allow two pending_approval for same issue")
	}
}

// ---------- Cancel queries include pending_approval ----------

func TestCancelIncludesPendingApproval(t *testing.T) {
	ctx := context.Background()

	var wsID, userID, runtimeID, agentID, issueID string

	err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('cancel-test', 'cancel-032-test@test.ai') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, userID) })

	err = testPool.QueryRow(ctx, `INSERT INTO workspace (name, slug, issue_prefix) VALUES ('cancel-test', 'cancel-032-test', 'CAN') RETURNING id`).Scan(&wsID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID) })

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata)
		VALUES ($1, 'cancel-daemon', 'Cancel Runtime', 'local', 'claude', 'online', '', '{}'::jsonb) RETURNING id
	`, wsID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, tools, triggers)
		VALUES ($1, 'Cancel Agent', 'local', '{}'::jsonb, $2, 'workspace', 1, '[]'::jsonb, '[]'::jsonb) RETURNING id
	`, wsID, runtimeID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, 'Cancel Test Issue', 'todo', 'none', 'member', $2, 9902, 0) RETURNING id
	`, wsID, userID).Scan(&issueID)
	if err != nil {
		t.Fatalf("create issue: %v", err)
	}

	// Insert a pending_approval task
	var taskID string
	err = testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'pending_approval', 0) RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// CancelAgentTask should be able to cancel pending_approval tasks
	var cancelledStatus string
	err = testPool.QueryRow(ctx, `
		UPDATE agent_task_queue SET status = 'cancelled', completed_at = now()
		WHERE id = $1 AND status IN ('pending_approval', 'queued', 'dispatched', 'running')
		RETURNING status
	`, taskID).Scan(&cancelledStatus)
	if err != nil {
		t.Fatalf("cancel pending_approval task: %v", err)
	}
	if cancelledStatus != "cancelled" {
		t.Fatalf("expected cancelled status, got %s", cancelledStatus)
	}
}
