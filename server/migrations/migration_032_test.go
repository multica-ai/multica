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
