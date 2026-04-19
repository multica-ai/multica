package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newUUID returns a fresh random pgtype.UUID.
func newUUID() pgtype.UUID {
	var id pgtype.UUID
	if err := id.Scan(uuid.NewString()); err != nil {
		panic(err)
	}
	return id
}

var (
	testPool        *pgxpool.Pool
	testWorkspaceID pgtype.UUID
	testUserID      pgtype.UUID
)

const (
	serviceTestWorkspaceSlug = "service-tests"
	serviceTestUserEmail     = "service-test@multica.ai"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping service tests: could not connect to database: %v\n", err)
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping service tests: database not reachable: %v\n", err)
		pool.Close()
		os.Exit(0)
	}

	testPool = pool

	// Setup a persistent test workspace and user for service tests.
	if err := cleanupServiceTestWorkspace(ctx, pool); err != nil {
		fmt.Printf("Failed to pre-clean service test workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Service Test User", serviceTestUserEmail).Scan(&userID); err != nil {
		fmt.Printf("Failed to create service test user: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := testUserID.Scan(userID); err != nil {
		fmt.Printf("Failed to scan user UUID: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	var wsID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Service Tests", serviceTestWorkspaceSlug, "Temporary workspace for service tests", "SVC").Scan(&wsID); err != nil {
		fmt.Printf("Failed to create service test workspace: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	if err := testWorkspaceID.Scan(wsID); err != nil {
		fmt.Printf("Failed to scan workspace UUID: %v\n", err)
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()

	if err := cleanupServiceTestWorkspace(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean up service test workspace: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func cleanupServiceTestWorkspace(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, serviceTestWorkspaceSlug); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, serviceTestUserEmail)
	return err
}

// testContext holds per-test state and cleanup helpers.
type testContext struct {
	ctx         context.Context
	cleanup     func()
	workspaceID pgtype.UUID
}

// setupTaskServiceTest constructs a TaskService and a testContext for each test.
func setupTaskServiceTest(t *testing.T) (*TaskService, testContext) {
	t.Helper()
	ctx := context.Background()
	hub := realtime.NewHub()
	bus := events.New()
	svc := NewTaskService(db.New(testPool), hub, bus)

	tc := testContext{
		ctx:         ctx,
		workspaceID: testWorkspaceID,
	}
	tc.cleanup = func() {
		bgCtx := context.Background()
		// Delete in dependency order, scoped to test runtimes/agents in this workspace.
		testPool.Exec(bgCtx, `
			DELETE FROM task_usage
			WHERE task_id IN (
				SELECT id FROM agent_task_queue
				WHERE runtime_id IN (
					SELECT id FROM agent_runtime
					WHERE workspace_id = $1 AND name LIKE 'test-rt-%'
				)
			)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM agent_task_queue
			WHERE runtime_id IN (
				SELECT id FROM agent_runtime
				WHERE workspace_id = $1 AND name LIKE 'test-rt-%'
			)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM agent_runtime_assignment
			WHERE agent_id IN (
				SELECT id FROM agent
				WHERE workspace_id = $1 AND name LIKE 'test-agent-%'
			)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'test-agent-%'
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'test-issue-%'
		`, testWorkspaceID)
		// Runtime group tables — must run before agent_runtime to satisfy FKs.
		testPool.Exec(bgCtx, `
			DELETE FROM runtime_group_override
			WHERE group_id IN (SELECT id FROM runtime_group WHERE workspace_id = $1)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM runtime_group_member
			WHERE group_id IN (SELECT id FROM runtime_group WHERE workspace_id = $1)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM agent_runtime_group
			WHERE agent_id IN (
				SELECT id FROM agent WHERE workspace_id = $1 AND name LIKE 'test-agent-%'
			)
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM runtime_group WHERE workspace_id = $1
		`, testWorkspaceID)
		testPool.Exec(bgCtx, `
			DELETE FROM agent_runtime WHERE workspace_id = $1 AND name LIKE 'test-rt-%'
		`, testWorkspaceID)
	}

	return svc, tc
}

// createAgent inserts a test agent and returns its UUID.
func (tc *testContext) createAgent(t *testing.T) pgtype.UUID {
	t.Helper()
	name := "test-agent-" + uuid.NewString()
	var idStr string
	if err := testPool.QueryRow(tc.ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, max_concurrent_tasks
		)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, 'workspace', 1)
		RETURNING id
	`, tc.workspaceID, name).Scan(&idStr); err != nil {
		t.Fatalf("createAgent: %v", err)
	}
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		t.Fatalf("createAgent scan UUID: %v", err)
	}
	return id
}

// createRuntime inserts a test agent_runtime and returns its UUID.
func (tc *testContext) createRuntime(t *testing.T, status string) pgtype.UUID {
	t.Helper()
	name := "test-rt-" + uuid.NewString()
	var idStr string
	if err := testPool.QueryRow(tc.ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'local', 'claude', $3, '', '{}'::jsonb, now())
		RETURNING id
	`, tc.workspaceID, name, status).Scan(&idStr); err != nil {
		t.Fatalf("createRuntime: %v", err)
	}
	var id pgtype.UUID
	if err := id.Scan(idStr); err != nil {
		t.Fatalf("createRuntime scan UUID: %v", err)
	}
	return id
}

// assign inserts an agent_runtime_assignment row.
func (tc *testContext) assign(t *testing.T, agentID, runtimeID pgtype.UUID) {
	t.Helper()
	if _, err := testPool.Exec(tc.ctx, `
		INSERT INTO agent_runtime_assignment (agent_id, runtime_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, agentID, runtimeID); err != nil {
		t.Fatalf("assign: %v", err)
	}
}

// seedTaskRow creates a minimal agent+issue (for FK satisfaction) and inserts a
// completed agent_task_queue row pinned to the given runtimeID with created_at = at.
// Returns the task UUID.
func (tc *testContext) seedTaskRow(t *testing.T, runtimeID pgtype.UUID, at time.Time) pgtype.UUID {
	t.Helper()

	// We need an agent FK. Create a throw-away agent.
	agentID := tc.createAgent(t)

	// Create a minimal issue for the task FK.
	issueTitle := "test-issue-" + uuid.NewString()
	var issueIDStr string
	if err := testPool.QueryRow(tc.ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, position)
		VALUES ($1, $2, 'done', 'none', 'member', $3,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1,
		        0)
		RETURNING id
	`, tc.workspaceID, issueTitle, testUserID).Scan(&issueIDStr); err != nil {
		t.Fatalf("seedTaskRow create issue: %v", err)
	}

	var issueID pgtype.UUID
	if err := issueID.Scan(issueIDStr); err != nil {
		t.Fatalf("seedTaskRow scan issue UUID: %v", err)
	}

	var taskIDStr string
	if err := testPool.QueryRow(tc.ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, created_at)
		VALUES ($1, $2, $3, 'completed', 0, $4)
		RETURNING id
	`, agentID, runtimeID, issueID, at).Scan(&taskIDStr); err != nil {
		t.Fatalf("seedTaskRow create task: %v", err)
	}

	var taskID pgtype.UUID
	if err := taskID.Scan(taskIDStr); err != nil {
		t.Fatalf("seedTaskRow scan task UUID: %v", err)
	}
	return taskID
}

// seedUsage calls seedTaskRow then inserts a task_usage row with input_tokens = tokens.
func (tc *testContext) seedUsage(t *testing.T, runtimeID pgtype.UUID, tokens int64, at time.Time) {
	t.Helper()
	taskID := tc.seedTaskRow(t, runtimeID, at)
	if _, err := testPool.Exec(tc.ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, created_at)
		VALUES ($1, 'claude', 'test-model', $2, 0, 0, 0, $3)
	`, taskID, tokens, at); err != nil {
		t.Fatalf("seedUsage: %v", err)
	}
}

// createGroup inserts a runtime_group with the given member runtimes and returns the group UUID.
func (tc *testContext) createGroup(t *testing.T, runtimes []pgtype.UUID) pgtype.UUID {
	t.Helper()
	groupID := newUUID()
	_, err := testPool.Exec(tc.ctx,
		`INSERT INTO runtime_group (id, workspace_id, name) VALUES ($1, $2, $3)`,
		groupID, testWorkspaceID, "test-grp-"+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("createGroup: %v", err)
	}
	for _, rid := range runtimes {
		_, err := testPool.Exec(tc.ctx,
			`INSERT INTO runtime_group_member (group_id, runtime_id) VALUES ($1, $2)`,
			groupID, rid,
		)
		if err != nil {
			t.Fatalf("createGroup insert member: %v", err)
		}
	}
	return groupID
}

// linkAgentToGroup inserts an agent_runtime_group row associating the agent with the group.
func (tc *testContext) linkAgentToGroup(t *testing.T, agentID, groupID pgtype.UUID) {
	t.Helper()
	_, err := testPool.Exec(tc.ctx,
		`INSERT INTO agent_runtime_group (agent_id, group_id) VALUES ($1, $2)`,
		agentID, groupID,
	)
	if err != nil {
		t.Fatalf("linkAgentToGroup: %v", err)
	}
}

// setOverride inserts an active runtime_group_override (starts_at=now, ends_at=endsAt).
func (tc *testContext) setOverride(t *testing.T, groupID, runtimeID pgtype.UUID, endsAt time.Time) {
	t.Helper()
	_, err := testPool.Exec(tc.ctx,
		`INSERT INTO runtime_group_override (group_id, runtime_id, starts_at, ends_at) VALUES ($1, $2, now(), $3)`,
		groupID, runtimeID, endsAt,
	)
	if err != nil {
		t.Fatalf("setOverride: %v", err)
	}
}

// createRuntimeInOtherWorkspace creates a separate workspace + runtime and
// returns the runtime UUID. Used to test cross-workspace rejection.
func (tc *testContext) createRuntimeInOtherWorkspace(t *testing.T) pgtype.UUID {
	t.Helper()
	wsID := newUUID()
	_, err := testPool.Exec(tc.ctx,
		`INSERT INTO workspace (id, name, slug, description, issue_prefix)
		 VALUES ($1, $2, $3, '', 'TST')`,
		wsID,
		"service-test-other-"+uuid.NewString(),
		"svc-test-other-"+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("createRuntimeInOtherWorkspace: insert workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	rtID := newUUID()
	_, err = testPool.Exec(tc.ctx,
		`INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider,
		 status, device_info, metadata, last_seen_at)
		 VALUES ($1, $2, $3, 'local', 'claude', 'online', '', '{}'::jsonb, now())`,
		rtID,
		wsID,
		"test-rt-other-"+uuid.NewString(),
	)
	if err != nil {
		t.Fatalf("createRuntimeInOtherWorkspace: insert runtime: %v", err)
	}
	return rtID
}
