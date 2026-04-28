package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// crossWSFixture sets up a second workspace owned by a different user, plus a
// pair of "stranger" issues the test user must NEVER see. Returns a cleanup
// function the test calls via t.Cleanup to drop everything.
type crossWSFixture struct {
	StrangerUserID         string
	StrangerWorkspaceID    string
	StrangerWorkspaceSlug  string
	StrangerIssueID        string
	SecondWorkspaceID      string
	SecondWorkspaceSlug    string
	SecondWorkspaceMemberID string
}

func setupCrossWSFixture(t *testing.T, pool *pgxpool.Pool, ownerUserID string) *crossWSFixture {
	t.Helper()
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	f := &crossWSFixture{
		StrangerWorkspaceSlug: "xws-stranger-" + uniq,
		SecondWorkspaceSlug:   "xws-second-" + uniq,
	}

	// Workspace #2 — same owner. Will hold issues the user SHOULD see.
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "X-Second", f.SecondWorkspaceSlug, "second", "SEC").Scan(&f.SecondWorkspaceID); err != nil {
		t.Fatalf("create second workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, f.SecondWorkspaceID, ownerUserID); err != nil {
		t.Fatalf("add owner to second workspace: %v", err)
	}

	// Stranger user owning a third workspace the test user is NOT a member of.
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "X-Stranger "+uniq, "x-stranger-"+uniq+"@multica.ai").Scan(&f.StrangerUserID); err != nil {
		t.Fatalf("create stranger user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, "X-Stranger", f.StrangerWorkspaceSlug, "stranger", "STR").Scan(&f.StrangerWorkspaceID); err != nil {
		t.Fatalf("create stranger workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, f.StrangerWorkspaceID, f.StrangerUserID); err != nil {
		t.Fatalf("add stranger member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, 'stranger only', 'todo', 'medium', 'member', $2, 1) RETURNING id
	`, f.StrangerWorkspaceID, f.StrangerUserID).Scan(&f.StrangerIssueID); err != nil {
		t.Fatalf("create stranger issue: %v", err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		pool.Exec(bg, `DELETE FROM workspace WHERE slug IN ($1, $2)`, f.StrangerWorkspaceSlug, f.SecondWorkspaceSlug)
		pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, f.StrangerUserID)
	})
	return f
}

func insertIssue(t *testing.T, pool *pgxpool.Pool, workspaceID, creatorID, title, status, priority string, number int, createdAt time.Time) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'member', $5, $6, $7, $7) RETURNING id
	`, workspaceID, title, status, priority, creatorID, number, createdAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	return id
}

func decodeCrossWSBody(t *testing.T, body []byte) (issues []map[string]any, hasMore bool, nextCursor *string, totalReturned int) {
	t.Helper()
	var resp struct {
		Issues        []map[string]any `json:"issues"`
		HasMore       bool             `json:"has_more"`
		NextCursor    *string          `json:"next_cursor"`
		TotalReturned int              `json:"total_returned"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, string(body))
	}
	return resp.Issues, resp.HasMore, resp.NextCursor, resp.TotalReturned
}

func TestListCrossWorkspaceIssues_Unauthenticated(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/issues/cross-workspace", nil)
	// Intentionally no X-User-ID header.
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListCrossWorkspaceIssues_HappyPathCrossesWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	f := setupCrossWSFixture(t, testPool, testUserID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	idA := insertIssue(t, testPool, testWorkspaceID, testUserID, "alpha-A", "todo", "high", 9001, now.Add(-3*time.Hour))
	idB := insertIssue(t, testPool, f.SecondWorkspaceID, testUserID, "beta-B", "in_progress", "medium", 9002, now.Add(-2*time.Hour))
	idC := insertIssue(t, testPool, testWorkspaceID, testUserID, "alpha-C", "todo", "high", 9003, now.Add(-1*time.Hour))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = ANY($1::uuid[])`, []string{idA, idB, idC})
	})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/cross-workspace?limit=2", nil)
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues, hasMore, nextCursor, total := decodeCrossWSBody(t, w.Body.Bytes())

	if total != 2 {
		t.Fatalf("total_returned = %d, want 2", total)
	}
	if !hasMore || nextCursor == nil {
		t.Fatalf("expected has_more=true with cursor, got has_more=%v cursor=%v", hasMore, nextCursor)
	}
	// Newest first: idC, then idB.
	if issues[0]["id"] != idC {
		t.Fatalf("first issue = %v, want %s", issues[0]["id"], idC)
	}
	if issues[1]["id"] != idB {
		t.Fatalf("second issue = %v, want %s", issues[1]["id"], idB)
	}

	// Workspace block must include server-derived color.
	ws0, _ := issues[0]["workspace"].(map[string]any)
	if ws0 == nil || ws0["color"] == "" || ws0["id"] == "" || ws0["issue_prefix"] == "" {
		t.Fatalf("workspace block missing fields: %v", issues[0]["workspace"])
	}

	// Stranger issue must never appear.
	for _, it := range issues {
		if it["id"] == f.StrangerIssueID {
			t.Fatalf("stranger issue leaked into result: %v", it)
		}
	}

	// Page 2 with the cursor: should return idA and finish.
	w2 := httptest.NewRecorder()
	req2 := newRequest("GET", "/api/issues/cross-workspace?limit=2&after="+*nextCursor, nil)
	testHandler.ListCrossWorkspaceIssues(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	page2, hasMore2, _, total2 := decodeCrossWSBody(t, w2.Body.Bytes())
	if total2 != 1 || hasMore2 {
		t.Fatalf("page 2: total=%d has_more=%v, want 1, false", total2, hasMore2)
	}
	if page2[0]["id"] != idA {
		t.Fatalf("page 2: first = %v, want %s", page2[0]["id"], idA)
	}
}

func TestListCrossWorkspaceIssues_StrangerWorkspaceFilteredSilently(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	f := setupCrossWSFixture(t, testPool, testUserID)

	// Create one visible issue in our own workspace so the result is non-empty.
	visible := insertIssue(t, testPool, testWorkspaceID, testUserID, "visible-x", "todo", "medium", 9100, time.Now().UTC())
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1::uuid`, visible)
	})

	w := httptest.NewRecorder()
	url := fmt.Sprintf("/api/issues/cross-workspace?workspace_ids=%s,%s", testWorkspaceID, f.StrangerWorkspaceID)
	req := newRequest("GET", url, nil)
	testHandler.ListCrossWorkspaceIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (silent filter), got %d: %s", w.Code, w.Body.String())
	}
	issues, _, _, _ := decodeCrossWSBody(t, w.Body.Bytes())
	for _, it := range issues {
		ws := it["workspace"].(map[string]any)
		if ws["id"] == f.StrangerWorkspaceID {
			t.Fatalf("stranger workspace appeared in result: %v", it)
		}
	}
}

func TestListCrossWorkspaceIssues_EmptyForUserWithNoWorkspaces(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())
	var orphanID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
	`, "X-Orphan "+uniq, "x-orphan-"+uniq+"@multica.ai").Scan(&orphanID); err != nil {
		t.Fatalf("create orphan user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, orphanID)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/issues/cross-workspace", nil)
	req.Header.Set("X-User-ID", orphanID)
	testHandler.ListCrossWorkspaceIssues(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues, hasMore, nextCursor, total := decodeCrossWSBody(t, w.Body.Bytes())
	if total != 0 || hasMore || nextCursor != nil || len(issues) != 0 {
		t.Fatalf("expected empty result, got total=%d hasMore=%v cursor=%v issues=%d",
			total, hasMore, nextCursor, len(issues))
	}
}

func TestListCrossWorkspaceIssues_BadCursor(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/cross-workspace?after=not-base64!", nil)
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListCrossWorkspaceIssues_BadStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/cross-workspace?status=not-a-status", nil)
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListCrossWorkspaceIssues_OpenOnlyExcludesDoneAndCancelled(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	now := time.Now().UTC()
	open := insertIssue(t, testPool, testWorkspaceID, testUserID, "open-1", "todo", "medium", 9201, now.Add(-2*time.Hour))
	done := insertIssue(t, testPool, testWorkspaceID, testUserID, "done-1", "done", "medium", 9202, now.Add(-1*time.Hour))
	cancelled := insertIssue(t, testPool, testWorkspaceID, testUserID, "cancelled-1", "cancelled", "medium", 9203, now)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = ANY($1::uuid[])`, []string{open, done, cancelled})
	})

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/cross-workspace?open_only=true", nil)
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	issues, _, _, _ := decodeCrossWSBody(t, w.Body.Bytes())
	for _, it := range issues {
		if it["id"] == done || it["id"] == cancelled {
			t.Fatalf("open_only leaked closed issue: %v", it)
		}
	}
	// The open one must be present.
	found := false
	for _, it := range issues {
		if it["id"] == open {
			found = true
		}
	}
	if !found {
		t.Fatalf("open_only dropped open issue %s", open)
	}
}

func TestListCrossWorkspaceIssues_LimitClamp(t *testing.T) {
	if testHandler == nil {
		t.Skip("no test handler")
	}
	// We don't need to verify count — only that limit > 100 is accepted, not
	// rejected, and the handler returns 200.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/cross-workspace?limit=99999", nil)
	testHandler.ListCrossWorkspaceIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// Sanity check that the cursor encode/decode round-trips. Pure helper test —
// independent of the DB.
func TestCrossWorkspaceCursorRoundTrip(t *testing.T) {
	c := crossWorkspaceCursor{
		CreatedAt: time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
		ID:        "11111111-1111-4111-8111-111111111111",
	}
	got, err := decodeCursor(encodeCursor(c))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.CreatedAt.Equal(c.CreatedAt) || got.ID != c.ID {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, c)
	}
}

