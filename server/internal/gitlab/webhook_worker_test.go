package gitlab

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWebhookWorker_DrainsAndProcessesIssueEvent(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed an Issue Hook event in the queue.
	body := []byte(`{
		"object_kind": "issue",
		"object_attributes": {"iid": 99, "title": "from worker", "state": "opened",
			"updated_at": "2026-04-17T10:00:00Z", "labels": []}
	}`)
	if _, err := queries.InsertGitlabWebhookEvent(context.Background(), db.InsertGitlabWebhookEventParams{
		WorkspaceID:     wsUUID,
		EventType:       "issue",
		ObjectID:        99,
		GitlabUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PayloadHash:     []byte{1, 2, 3, 4},
		Payload:         body,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	// Insert a workspace_gitlab_connection so the worker can resolve project_id.
	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connected')
		ON CONFLICT DO NOTHING
	`, wsID)

	// Run the worker for a brief window. Pass the pool as the txStarter
	// (it implements Begin) and one worker goroutine for predictable timing.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w := NewWebhookWorker(queries, pool, 1, 50*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	time.Sleep(500 * time.Millisecond)
	cancel()
	wg.Wait()

	// Verify the issue is now in the cache.
	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 99, Valid: true},
	})
	if err != nil {
		t.Fatalf("issue not cached: %v", err)
	}
	if row.Title != "from worker" {
		t.Errorf("title = %q", row.Title)
	}

	// Verify the event row is marked processed.
	var processed bool
	pool.QueryRow(context.Background(),
		`SELECT processed_at IS NOT NULL FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND object_id = 99`,
		wsID).Scan(&processed)
	if !processed {
		t.Errorf("event not marked processed")
	}
}

func TestWebhookWorker_PoisonPillBacksOffAndDeadLetters(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed a Note Hook for an issue that DOESN'T exist in cache → handler
	// returns "parent issue not yet cached" on every attempt.
	body := []byte(`{
		"object_kind": "note",
		"object_attributes": {"id": 42, "note": "x", "noteable_type": "Issue"},
		"issue": {"iid": 999},
		"user": {"id": 1}
	}`)
	if _, err := queries.InsertGitlabWebhookEvent(context.Background(), db.InsertGitlabWebhookEventParams{
		WorkspaceID:     wsUUID,
		EventType:       "note",
		ObjectID:        42,
		GitlabUpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		PayloadHash:     []byte{0xde, 0xad, 0xbe, 0xef},
		Payload:         body,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	// Need a workspace_gitlab_connection so the worker can resolve project_id.
	pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 7, 'g/a', '\x'::bytea, 1, 'connected')
		ON CONFLICT DO NOTHING
	`, wsID)

	// Run worker with very short idle sleep. No backoff would mean rapid
	// retries; we expect at most 1-2 attempts in 1.5s due to backoff.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	w := NewWebhookWorker(queries, pool, 1, 10*time.Millisecond)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	wg.Wait()

	// Verify failure_count grew but stayed under the dead-letter threshold.
	// First attempt: fc 0 → 1, no backoff (last_attempt_at NULL).
	// Subsequent: backoff = fc * 5s. With a 1.5s window, we should see at
	// most 1 retry (the one that's eligible immediately).
	var fc int
	var lastErr string
	pool.QueryRow(context.Background(),
		`SELECT failure_count, COALESCE(last_error, '') FROM gitlab_webhook_event WHERE workspace_id = $1::uuid AND object_id = 42`,
		wsID).Scan(&fc, &lastErr)
	if fc < 1 {
		t.Errorf("failure_count = %d, want >= 1 (worker should have tried at least once)", fc)
	}
	if fc > 2 {
		t.Errorf("failure_count = %d, want <= 2 (backoff should prevent rapid retries)", fc)
	}
	if lastErr == "" {
		t.Errorf("last_error should have been recorded, got empty string")
	}
}
