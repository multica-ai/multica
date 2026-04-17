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
