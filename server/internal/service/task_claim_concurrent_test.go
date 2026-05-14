package service

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestClaimTask_ConcurrentCapacityEnforcement verifies that the advisory
// lock in ClaimTask prevents concurrent claims from exceeding an agent's
// max_concurrent_tasks. This is a regression test for the TOCTOU race
// identified in PR #2637 review.
func TestClaimTask_ConcurrentCapacityEnforcement(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database not available: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("database not reachable: %v", err)
	}

	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()

	svc := &TaskService{
		Queries:   queries,
		TxStarter: pool,
		Hub:       hub,
		Bus:       bus,
	}

	// Create a test workspace + user + agent with max_concurrent_tasks=1.
	var workspaceID, agentID, runtimeID pgtype.UUID
	err = pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ('claim-test-ws', 'claim-test-'||substr(md5(random()::text),1,8), 'concurrent claim test')
		RETURNING id
	`).Scan(&workspaceID)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	err = pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, timezone)
		VALUES ($1, 'test-daemon-claim', 'test-rt', 'local', 'claude', 'online', 'test', '{}'::jsonb, 'UTC')
		RETURNING id
	`, workspaceID).Scan(&runtimeID)
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, max_concurrent_tasks, runtime_id, visibility)
		VALUES ($1, 'claim-test-agent', 'local', '{}'::jsonb, 1, $2, 'workspace')
		RETURNING id
	`, workspaceID, runtimeID).Scan(&agentID)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	// Enqueue 5 tasks for this agent.
	for i := 0; i < 5; i++ {
		_, err = pool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
			VALUES ($1, $2, 'queued', 1)
		`, agentID, runtimeID)
		if err != nil {
			t.Fatalf("enqueue task %d: %v", i, err)
		}
	}

	// Launch 10 goroutines each attempting to claim.
	const goroutines = 10
	var wg sync.WaitGroup
	var mu sync.Mutex
	var claimed []*db.AgentTaskQueue

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			task, err := svc.ClaimTask(ctx, agentID)
			if err != nil {
				t.Errorf("ClaimTask error: %v", err)
				return
			}
			if task != nil {
				mu.Lock()
				claimed = append(claimed, task)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// With max_concurrent_tasks=1, exactly 1 claim should succeed.
	if len(claimed) != 1 {
		t.Fatalf("expected exactly 1 claim to succeed with max_concurrent_tasks=1, got %d", len(claimed))
	}

	// Verify DB state: exactly 1 task in 'dispatched' status.
	var dispatched int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE agent_id = $1 AND status = 'dispatched'
	`, agentID).Scan(&dispatched)
	if err != nil {
		t.Fatalf("count dispatched: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("expected 1 dispatched task in DB, got %d", dispatched)
	}
}
