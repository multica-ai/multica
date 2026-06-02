package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dwickyfp/wallts/server/internal/session"
)

// testDB returns a connection pool for the test database.
// Skips the test if TEST_DATABASE_URL is not set.
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test DB: %v", err)
	}

	return pool
}

// sessionTestFixtures holds the IDs of parent rows needed to satisfy
// agent_sessions FK constraints.
type sessionTestFixtures struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	RuntimeID   uuid.UUID
	AgentID     uuid.UUID
	IssueID     uuid.UUID
}

// setupSessionTestFixtures creates the full FK parent chain required by
// agent_sessions: workspace -> user -> agent_runtime -> agent, plus an issue.
func setupSessionTestFixtures(t *testing.T, pool *pgxpool.Pool) sessionTestFixtures {
	t.Helper()
	ctx := context.Background()

	suffix := uuid.New().String()[:8]
	var f sessionTestFixtures

	// Workspace
	err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Session Test WS "+suffix, "session-test-"+suffix, "fixture").Scan(&f.WorkspaceID)
	if err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	// User (for issue.creator_id and agent.owner_id)
	err = pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Session Tester "+suffix, "session-test-"+suffix+"@wallts.ai").Scan(&f.UserID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	// Agent runtime (required by agent.runtime_id FK)
	err = pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'test_provider', 'online', '', '{}'::jsonb, now())
		RETURNING id
	`, f.WorkspaceID, "Session Test Runtime "+suffix).Scan(&f.RuntimeID)
	if err != nil {
		t.Fatalf("insert agent_runtime: %v", err)
	}

	// Agent
	err = pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, f.WorkspaceID, "Session Test Agent "+suffix, f.RuntimeID, f.UserID).Scan(&f.AgentID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	// Issue (number must be unique per workspace)
	err = pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority,
			creator_type, creator_id, number
		)
		VALUES ($1, $2, 'todo', 'none', 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, f.WorkspaceID, "Session Test Issue "+suffix, f.UserID).Scan(&f.IssueID)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}

	t.Cleanup(func() {
		cleanupSessionTestFixtures(context.Background(), pool, f)
	})

	return f
}

// cleanupSessionTestFixtures removes all fixture data. Deleting the workspace
// cascades to agent_runtime, agent, and issue. The user is deleted separately.
func cleanupSessionTestFixtures(ctx context.Context, pool *pgxpool.Pool, f sessionTestFixtures) {
	pool.Exec(ctx, `DELETE FROM agent_sessions WHERE agent_id = $1 AND issue_id = $2`, f.AgentID, f.IssueID)
	pool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, f.WorkspaceID)
	pool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, f.UserID)
}

func TestIntegration_RepositoryCreateAndGet(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	repo := session.NewRepository(pool)

	f := setupSessionTestFixtures(t, pool)

	// Create a session via the repository
	s := session.NewSession(f.IssueID, f.AgentID, json.RawMessage(`{}`))
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Retrieve
	got, err := repo.GetActiveSession(ctx, f.IssueID, f.AgentID)
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if got.IssueID != f.IssueID {
		t.Errorf("IssueID = %s, want %s", got.IssueID, f.IssueID)
	}
	if got.AgentID != f.AgentID {
		t.Errorf("AgentID = %s, want %s", got.AgentID, f.AgentID)
	}
	if got.RunNumber != 1 {
		t.Errorf("RunNumber = %d, want 1", got.RunNumber)
	}
	if !got.IsActive {
		t.Error("expected IsActive = true")
	}
}

func TestIntegration_UniqueConstraint(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	f := setupSessionTestFixtures(t, pool)

	// Insert first
	_, err := pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 5, '{}', true, 1)
	`, f.IssueID, f.AgentID)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Duplicate run_number should fail
	_, err = pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 5, '{}', true, 1)
	`, f.IssueID, f.AgentID)
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestIntegration_VersionConflict(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	repo := session.NewRepository(pool)

	f := setupSessionTestFixtures(t, pool)

	var sid uuid.UUID
	err := pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 1, '{}', true, 1)
		RETURNING id
	`, f.IssueID, f.AgentID).Scan(&sid)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Update with correct version
	newState := json.RawMessage(`{"step": "done"}`)
	newVersion, err := repo.UpdateState(ctx, sid, newState, 1)
	if err != nil {
		t.Fatalf("update state: %v", err)
	}
	if newVersion != 2 {
		t.Errorf("newVersion = %d, want 2", newVersion)
	}

	// Stale version should conflict
	_, err = repo.UpdateState(ctx, sid, newState, 1)
	if err != session.ErrVersionConflict {
		t.Errorf("expected ErrVersionConflict, got: %v", err)
	}
}

func TestIntegration_LastActiveAtTrigger(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()

	f := setupSessionTestFixtures(t, pool)

	var sid uuid.UUID
	var initialTime time.Time
	err := pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 1, '{}', true, 1)
		RETURNING id, last_active_at
	`, f.IssueID, f.AgentID).Scan(&sid, &initialTime)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Sleep briefly, then update to trigger the function
	time.Sleep(50 * time.Millisecond)
	pool.Exec(ctx, `UPDATE agent_sessions SET state = $1 WHERE id = $2`,
		fmt.Sprintf(`{"updated": true}`), sid)

	var updatedTime time.Time
	pool.QueryRow(ctx, `SELECT last_active_at FROM agent_sessions WHERE id = $1`, sid).Scan(&updatedTime)

	if !updatedTime.After(initialTime) {
		t.Errorf("last_active_at not auto-updated: initial=%v, updated=%v", initialTime, updatedTime)
	}
}
