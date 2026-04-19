package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	gitlabapi "github.com/multica-ai/multica/server/pkg/gitlab"
)

func TestReconciler_PicksUpDriftAndAdvancesCursor(t *testing.T) {
	pool := connectTestPool(t)

	// This test is the only one that drives tickOne(), which lists ALL
	// connected workspaces. Orphaned connection rows from a previous crashed
	// or interrupted test run survive (their workspace-owner cleanup never
	// fired) and show up here as "unexpected path" errors against the mock.
	// Clean them up so the mock only ever serves this test's own workspace.
	// Other reconciler tests call reconcileOne/sweepDeletions directly and
	// are immune.
	if _, err := pool.Exec(context.Background(), `TRUNCATE workspace_gitlab_connection`); err != nil {
		t.Fatalf("truncate connections: %v", err)
	}

	wsID := makeWorkspace(t, pool)
	queries := db.New(pool)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/7/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{ID: 5001, IID: 11, Title: "from reconciler", State: "opened",
					Labels: []string{}, UpdatedAt: "2026-04-17T15:00:00Z"},
			})
		case "/api/v4/projects/7/labels":
			json.NewEncoder(w).Encode([]gitlabapi.Label{})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	// Seed a connection in the past.
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status,
			last_sync_cursor
		) VALUES ($1, 7, 'g/a', $2, 1, 'connected', '2026-04-17T14:00:00Z')
	`, wsID, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	// Inject a static decrypter so we don't have to wire the cipher.
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil })
	if err := r.tickOne(context.Background()); err != nil {
		t.Fatalf("tickOne: %v", err)
	}

	row, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: mustPGUUID(t, wsID),
		GitlabIid:   pgtype.Int4{Int32: 11, Valid: true},
	})
	if err != nil {
		t.Fatalf("issue not picked up: %v", err)
	}
	if row.Title != "from reconciler" {
		t.Errorf("title = %q", row.Title)
	}

	// Cursor should have advanced to the issue's UpdatedAt.
	conn, _ := queries.GetWorkspaceGitlabConnection(context.Background(), mustPGUUID(t, wsID))
	expected, _ := time.Parse(time.RFC3339, "2026-04-17T15:00:00Z")
	if !conn.LastSyncCursor.Valid || !conn.LastSyncCursor.Time.Equal(expected) {
		t.Errorf("last_sync_cursor = %+v, want %v", conn.LastSyncCursor, expected)
	}
}

// TestUpsertIssueFromGitlab_AssignsSequentialNumbers asserts the number-
// collision fix: previously the sqlc upsert relied on issue.number's DEFAULT 0,
// so the second GitLab-synced issue in any workspace collided on
// uq_issue_workspace_number (workspace_id, number). The query now allocates
// via workspace.issue_counter in a CTE, mirroring the local CreateIssue path.
//
// Verifies:
// - Two fresh inserts get distinct, monotonically increasing numbers.
// - Re-upserting the same issue (IID) preserves its number and does NOT
//   consume additional counter values.
func TestUpsertIssueFromGitlab_AssignsSequentialNumbers(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Record starting counter so the assertions are independent of whatever
	// state other tests leave behind in the shared workspace counter column.
	var startCounter int32
	if err := pool.QueryRow(context.Background(),
		`SELECT issue_counter FROM workspace WHERE id = $1`, wsID).Scan(&startCounter); err != nil {
		t.Fatalf("read start counter: %v", err)
	}

	row1, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 1001, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 900, Valid: true},
		Title:             "first",
		Status:            "todo",
		Priority:          "none",
		ExternalUpdatedAt: parseTS("2026-04-17T09:00:00Z"),
	})
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if row1.Number != startCounter+1 {
		t.Errorf("row1.number = %d, want %d", row1.Number, startCounter+1)
	}

	row2, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 1002, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 900, Valid: true},
		Title:             "second",
		Status:            "todo",
		Priority:          "none",
		ExternalUpdatedAt: parseTS("2026-04-17T09:01:00Z"),
	})
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if row2.Number != startCounter+2 {
		t.Errorf("row2.number = %d, want %d (distinct from row1)", row2.Number, startCounter+2)
	}

	// Re-upsert row1 with a newer external_updated_at. The UPDATE branch must
	// preserve the existing number and NOT allocate a new counter value.
	row1Again, err := queries.UpsertIssueFromGitlab(context.Background(), db.UpsertIssueFromGitlabParams{
		WorkspaceID:       wsUUID,
		GitlabIid:         pgtype.Int4{Int32: 1001, Valid: true},
		GitlabProjectID:   pgtype.Int8{Int64: 900, Valid: true},
		Title:             "first edited",
		Status:            "in_progress",
		Priority:          "none",
		ExternalUpdatedAt: parseTS("2026-04-17T10:00:00Z"),
	})
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if row1Again.Number != row1.Number {
		t.Errorf("re-upsert changed number: got %d, want %d (existing number must be preserved)",
			row1Again.Number, row1.Number)
	}

	// Counter must have advanced by exactly 2 (one per unique IID), not 3.
	var endCounter int32
	if err := pool.QueryRow(context.Background(),
		`SELECT issue_counter FROM workspace WHERE id = $1`, wsID).Scan(&endCounter); err != nil {
		t.Fatalf("read end counter: %v", err)
	}
	if endCounter != startCounter+2 {
		t.Errorf("workspace.issue_counter = %d, want %d (re-upsert must not consume a value)",
			endCounter, startCounter+2)
	}
}

// TestReconciler_SweepDeletesOrphanedCacheRow asserts the deletion sweep
// tears down cache rows whose GitLab counterpart has been destroyed (project
// webhooks don't fire on destroy, so without this sweep deleted issues
// linger indefinitely). Two cached issues, GitLab list only returns one, a
// per-issue GET on the missing one returns 404 → the orphan gets swept.
func TestReconciler_SweepDeletesOrphanedCacheRow(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	// Seed two cache rows. IID=100 will survive; IID=200 is the orphan.
	// Direct INSERT (not UpsertIssueFromGitlab) so we can set number
	// explicitly — the sqlc upsert doesn't take number and relies on the
	// DB default of 0, which collides on the second row under
	// uq_issue_workspace_number.
	for i, iid := range []int32{100, 200} {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO issue (workspace_id, title, status, priority, number,
			                   gitlab_iid, gitlab_project_id, external_updated_at)
			VALUES ($1, 'seeded', 'todo', 'none', $2, $3, 700, '2026-04-17T09:00:00Z')
		`, wsID, i+1, iid); err != nil {
			t.Fatalf("seed iid=%d: %v", iid, err)
		}
	}

	// GitLab list returns only IID=100. A per-issue GET on IID=200 returns
	// 404 → confirmed destroyed, sweep deletes it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/700/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{
				{ID: 1, IID: 100, Title: "still exists", State: "opened", UpdatedAt: "2026-04-17T10:00:00Z"},
			})
		case "/api/v4/projects/700/issues/200":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Not found"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	del := &recordingDeleter{}
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil }).
		WithIssueDeleter(del)

	if err := r.sweepDeletions(context.Background(), db.WorkspaceGitlabConnection{
		WorkspaceID:     wsUUID,
		GitlabProjectID: 700,
	}, "tok"); err != nil {
		t.Fatalf("sweepDeletions: %v", err)
	}

	if got := del.callCount(); got != 1 {
		t.Fatalf("CleanupAndDeleteIssue calls = %d, want 1", got)
	}
	orphan, err := queries.GetIssueByGitlabIID(context.Background(), db.GetIssueByGitlabIIDParams{
		WorkspaceID: wsUUID,
		GitlabIid:   pgtype.Int4{Int32: 200, Valid: true},
	})
	if err != nil {
		t.Fatalf("orphan row lookup: %v", err)
	}
	if del.calls[0] != uuidString(orphan.ID) {
		t.Errorf("swept issue_id = %q, want %q (the orphan)", del.calls[0], uuidString(orphan.ID))
	}
}

// TestReconciler_SweepWipesGenuinelyEmptyProject asserts that an empty
// project whose cache still holds rows DOES get swept — provided each orphan
// is confirmed destroyed via a per-issue 404. This is the case we hit when
// a user deletes every test issue on gitlab.com: the earlier list-length
// guard used to refuse this and strand stale rows forever.
func TestReconciler_SweepWipesGenuinelyEmptyProject(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	for i, iid := range []int32{401, 402} {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO issue (workspace_id, title, status, priority, number,
			                   gitlab_iid, gitlab_project_id, external_updated_at)
			VALUES ($1, 'seeded', 'todo', 'none', $2, $3, 702, '2026-04-17T09:00:00Z')
		`, wsID, i+1, iid); err != nil {
			t.Fatalf("seed iid=%d: %v", iid, err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/702/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		case "/api/v4/projects/702/issues/401", "/api/v4/projects/702/issues/402":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"404 Not found"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	del := &recordingDeleter{}
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil }).
		WithIssueDeleter(del)

	if err := r.sweepDeletions(context.Background(), db.WorkspaceGitlabConnection{
		WorkspaceID:     wsUUID,
		GitlabProjectID: 702,
	}, "tok"); err != nil {
		t.Fatalf("sweepDeletions: %v", err)
	}

	if got := del.callCount(); got != 2 {
		t.Fatalf("CleanupAndDeleteIssue calls = %d, want 2 (both orphans confirmed 404)", got)
	}
}

// TestReconciler_SweepSkipsOrphanWhenGetStillExists asserts the self-healing
// guard: the list response can be stale/inconsistent (GitLab's search index
// catching up), but a targeted GET is authoritative. If the per-issue check
// returns the issue, we do NOT delete — just skip this tick and try again.
func TestReconciler_SweepSkipsOrphanWhenGetStillExists(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, number,
		                   gitlab_iid, gitlab_project_id, external_updated_at)
		VALUES ($1, 'seeded', 'todo', 'none', 1, 500, 703, '2026-04-17T09:00:00Z')
	`, wsID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/703/issues":
			// List says empty — but the issue actually exists.
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		case "/api/v4/projects/703/issues/500":
			json.NewEncoder(w).Encode(gitlabapi.Issue{
				ID: 500, IID: 500, Title: "still here", State: "opened", UpdatedAt: "2026-04-17T10:00:00Z",
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	del := &recordingDeleter{}
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil }).
		WithIssueDeleter(del)

	if err := r.sweepDeletions(context.Background(), db.WorkspaceGitlabConnection{
		WorkspaceID:     wsUUID,
		GitlabProjectID: 703,
	}, "tok"); err != nil {
		t.Fatalf("sweepDeletions: %v", err)
	}

	if got := del.callCount(); got != 0 {
		t.Fatalf("CleanupAndDeleteIssue calls = %d, want 0 (GET confirmed existence)", got)
	}
}

// TestReconciler_SweepSkipsOrphanOnGetTransientError asserts that a
// non-404 error from the per-issue GET (403, 5xx, etc.) does NOT delete the
// row. We only act on an authoritative 404.
func TestReconciler_SweepSkipsOrphanOnGetTransientError(t *testing.T) {
	pool := connectTestPool(t)
	wsID := makeWorkspace(t, pool)
	wsUUID := mustPGUUID(t, wsID)
	queries := db.New(pool)

	if _, err := pool.Exec(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, number,
		                   gitlab_iid, gitlab_project_id, external_updated_at)
		VALUES ($1, 'seeded', 'todo', 'none', 1, 600, 704, '2026-04-17T09:00:00Z')
	`, wsID); err != nil {
		t.Fatalf("seed: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/704/issues":
			json.NewEncoder(w).Encode([]gitlabapi.Issue{})
		case "/api/v4/projects/704/issues/600":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"gitlab boom"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	del := &recordingDeleter{}
	r := NewReconciler(queries, gitlabapi.NewClient(srv.URL, srv.Client()),
		func(ctx context.Context, encrypted []byte) (string, error) { return "tok", nil }).
		WithIssueDeleter(del)

	if err := r.sweepDeletions(context.Background(), db.WorkspaceGitlabConnection{
		WorkspaceID:     wsUUID,
		GitlabProjectID: 704,
	}, "tok"); err != nil {
		t.Fatalf("sweepDeletions: %v", err)
	}

	if got := del.callCount(); got != 0 {
		t.Fatalf("CleanupAndDeleteIssue calls = %d, want 0 (non-404 must skip)", got)
	}
}
