package session_test

import (
	"context"
	"encoding/json"
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

func TestIntegration_RepositoryCreateAndGet(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	repo := session.NewRepository(pool)

	// Use existing seeded issue+agent IDs from the test DB
	// (assumes 001_init migration data or test fixtures)
	issueID := uuid.New()
	agentID := uuid.New()

	// Create a session directly — skip FK validation by using raw SQL for setup
	pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 1, '{}', true, 1)
	`, issueID, agentID)

	// Retrieve
	got, err := repo.GetActiveSession(ctx, issueID, agentID)
	if err != nil {
		t.Fatalf("get active session: %v", err)
	}
	if got.IssueID != issueID {
		t.Errorf("IssueID = %s, want %s", got.IssueID, issueID)
	}
	if got.AgentID != agentID {
		t.Errorf("AgentID = %s, want %s", got.AgentID, agentID)
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

	issueID := uuid.New()
	agentID := uuid.New()

	// Insert first
	pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 5, '{}', true, 1)
	`, issueID, agentID)

	// Duplicate run_number should fail
	_, err := pool.Exec(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 5, '{}', true, 1)
	`, issueID, agentID)
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestIntegration_VersionConflict(t *testing.T) {
	pool := testDB(t)
	ctx := context.Background()
	repo := session.NewRepository(pool)

	issueID := uuid.New()
	agentID := uuid.New()

	var sid uuid.UUID
	pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 1, '{}', true, 1)
		RETURNING id
	`, issueID, agentID).Scan(&sid)

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

	issueID := uuid.New()
	agentID := uuid.New()

	var sid uuid.UUID
	var initialTime time.Time
	pool.QueryRow(ctx, `
		INSERT INTO agent_sessions (id, issue_id, agent_id, run_number, state, is_active, version)
		VALUES (gen_random_uuid(), $1, $2, 1, '{}', true, 1)
		RETURNING id, last_active_at
	`, issueID, agentID).Scan(&sid, &initialTime)

	// Sleep briefly, then update to trigger the function
	time.Sleep(50 * time.Millisecond)
	pool.Exec(ctx, `UPDATE agent_sessions SET state = '{"updated": true}' WHERE id = $1`, sid)

	var updatedTime time.Time
	pool.QueryRow(ctx, `SELECT last_active_at FROM agent_sessions WHERE id = $1`, sid).Scan(&updatedTime)

	if !updatedTime.After(initialTime) {
		t.Errorf("last_active_at not auto-updated: initial=%v, updated=%v", initialTime, updatedTime)
	}
}
