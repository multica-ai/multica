package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var testTaskPool *pgxpool.Pool
var testTaskSvc *TaskService
var testTaskUserID string
var testTaskWorkspaceID string
var testTaskRuntimeID string
var testTaskAgentID string

const (
	taskTestEmail = "task-service-test@multica.ai"
	taskTestName  = "Task Service Test User"
	taskTestSlug  = "task-service-tests"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		os.Exit(0)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		os.Exit(0)
	}

	testTaskPool = pool
	queries := db.New(pool)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	testTaskSvc = NewTaskService(queries, pool, hub, bus)

	testTaskUserID, testTaskWorkspaceID, testTaskRuntimeID, testTaskAgentID, err = setupTaskServiceFixture(ctx, pool)
	if err != nil {
		pool.Close()
		os.Exit(1)
	}

	code := m.Run()
	_ = cleanupTaskServiceFixture(context.Background(), pool)
	pool.Close()
	os.Exit(code)
}

func setupTaskServiceFixture(ctx context.Context, pool *pgxpool.Pool) (userID, workspaceID, runtimeID, agentID string, err error) {
	if err = cleanupTaskServiceFixture(ctx, pool); err != nil {
		return
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, taskTestName, taskTestEmail).Scan(&userID)
	if err != nil {
		return
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Task Service Tests", taskTestSlug, "Temporary workspace for task service tests", "TSK").Scan(&workspaceID)
	if err != nil {
		return
	}

	if _, err = pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		return
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, now())
		RETURNING id
	`, workspaceID, "Task Service Runtime", "task_service_runtime", "Task service runtime").Scan(&runtimeID)
	if err != nil {
		return
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
		RETURNING id
	`, workspaceID, "Task Service Agent", runtimeID, userID).Scan(&agentID)
	return
}

func mustUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil {
		t.Fatalf("scan uuid %q: %v", raw, err)
	}
	return id
}

func cleanupTaskServiceFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, taskTestSlug); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, taskTestEmail); err != nil {
		return err
	}
	return nil
}

func TestCompleteTask_AutoAdvancesIssueToInReview(t *testing.T) {
	if testTaskSvc == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var issueID string
	if err := testTaskPool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, number, title, description, status, priority,
			assignee_type, assignee_id, creator_type, creator_id, position
		)
		VALUES ($1, 9001, 'auto advance fixture', '', 'todo', 'high', 'agent', $2, 'member', $3, 0)
		RETURNING id
	`, testTaskWorkspaceID, testTaskAgentID, testTaskUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testTaskPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	var taskID string
	if err := testTaskPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, started_at
		)
		VALUES ($1, $2, $3, 'running', 3, now())
		RETURNING id
	`, testTaskAgentID, testTaskRuntimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}

	payload, err := json.Marshal(map[string]any{"output": "done"})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	if _, err := testTaskSvc.CompleteTask(ctx, mustUUID(t, taskID), payload, "session-test", "/tmp/workdir-test"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var status string
	if err := testTaskPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_review" {
		t.Fatalf("expected issue status in_review, got %q", status)
	}
}

func TestCompleteTask_DoesNotOverrideTerminalIssueStatus(t *testing.T) {
	if testTaskSvc == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	statuses := []string{"done", "blocked", "cancelled", "in_review"}
	for i, initialStatus := range statuses {
		t.Run(initialStatus, func(t *testing.T) {
			issueNumber := 9100 + i
			var issueID string
			if err := testTaskPool.QueryRow(ctx, `
				INSERT INTO issue (
					workspace_id, number, title, description, status, priority,
					assignee_type, assignee_id, creator_type, creator_id, position
				)
				VALUES ($1, $2, $3, '', $4, 'high', 'agent', $5, 'member', $6, 0)
				RETURNING id
			`, testTaskWorkspaceID, issueNumber, "terminal status fixture", initialStatus, testTaskAgentID, testTaskUserID).Scan(&issueID); err != nil {
				t.Fatalf("create issue: %v", err)
			}
			t.Cleanup(func() {
				testTaskPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
			})

			var taskID string
			if err := testTaskPool.QueryRow(ctx, `
				INSERT INTO agent_task_queue (
					agent_id, runtime_id, issue_id, status, priority, started_at
				)
				VALUES ($1, $2, $3, 'running', 3, now())
				RETURNING id
			`, testTaskAgentID, testTaskRuntimeID, issueID).Scan(&taskID); err != nil {
				t.Fatalf("create task: %v", err)
			}

			payload, err := json.Marshal(map[string]any{"output": "done"})
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}

			if _, err := testTaskSvc.CompleteTask(ctx, mustUUID(t, taskID), payload, "session-test", "/tmp/workdir-test"); err != nil {
				t.Fatalf("CompleteTask: %v", err)
			}

			var status string
			if err := testTaskPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
				t.Fatalf("load issue status: %v", err)
			}
			if status != initialStatus {
				t.Fatalf("expected issue status %q to remain unchanged, got %q", initialStatus, status)
			}
		})
	}
}
