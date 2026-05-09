package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service/ship"
	gh "github.com/multica-ai/multica/server/pkg/github"
)

// TestShipService_SyncProject_HappyPath drives the service end-to-end
// against the real database using an httptest-backed GitHub mock. Verifies
// the upsert lands a PR row and that re-running the sync is idempotent.
//
// Lives in the handler package (not internal/service/ship) because the
// existing testPool / testWorkspaceID fixtures live here.
func TestShipService_SyncProject_HappyPath(t *testing.T) {
	enableShipHub(t, false)
	projectID := createShipProject(t, "https://github.com/multica-ai/multica")

	body := `[{
        "number": 7, "title": "Hello PRs", "state": "open", "draft": false,
        "html_url": "https://github.com/multica-ai/multica/pull/7",
        "body": "summary",
        "user": {"login": "alice", "avatar_url": "https://example.com/a.png"},
        "base": {"ref": "main"},
        "head": {"ref": "feat/x", "sha": "abc"},
        "labels": [{"name": "feat", "color": "00ff00"}],
        "additions": 10, "deletions": 5, "changed_files": 2,
        "created_at": "2026-04-30T00:00:00Z", "updated_at": "2026-05-01T00:00:00Z"
    }]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Both /pulls?state=open and /pulls?state=closed get the same body in
		// this mock — the closed result will simply re-upsert the same row.
		// Idempotency check below proves that re-running the sync is safe.
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := gh.NewClient("test-token")
	client.BaseURL = srv.URL
	svc := &ship.Service{Q: testHandler.Queries, Github: client}

	wsUUID := parseUUID(testWorkspaceID)
	projUUID := parseUUID(projectID)
	res, err := svc.SyncProject(context.Background(), wsUUID, projUUID)
	if err != nil {
		t.Fatalf("SyncProject: %v", err)
	}
	if res.Upserted == 0 {
		t.Fatalf("expected upserts, got %+v", res)
	}

	// Verify exactly one row landed in the DB despite two API calls.
	var count int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM pull_request WHERE workspace_id = $1 AND pr_number = 7`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 PR row after sync, got %d", count)
	}

	// Idempotency: a second sync produces no duplicates.
	if _, err := svc.SyncProject(context.Background(), wsUUID, projUUID); err != nil {
		t.Fatalf("second SyncProject: %v", err)
	}
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM pull_request WHERE workspace_id = $1 AND pr_number = 7`,
		testWorkspaceID).Scan(&count); err != nil {
		t.Fatalf("count after second sync: %v", err)
	}
	if count != 1 {
		t.Fatalf("idempotency violated: %d rows", count)
	}
}
