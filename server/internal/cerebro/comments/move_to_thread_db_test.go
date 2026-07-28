package comments

// Integration tests for POST /api/comments/move-to-thread (FIR-3880): the move
// re-parents the original comments instead of copying them, so nothing is left
// behind at the old location and no breadcrumb comment is written.
//
// They skip cleanly when no test DB is reachable, same pattern as the note
// package's *_db_test.go. Run against a migrated DB, e.g.:
//
//	DATABASE_URL=postgres://multica:multica@127.0.0.1:5432/multica_fir3880?sslmode=disable \
//	  go test ./internal/cerebro/comments/ -run TestMoveToThread -v

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	mtEmail = "move-to-thread-test@multica.ai"
	mtSlug  = "move-to-thread-tests"
)

var (
	mtPool *pgxpool.Pool
	mtWsID pgtype.UUID
	mtUser pgtype.UUID
	mtH    *Handler
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@127.0.0.1:5432/multica_fir3880?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping move-to-thread integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping move-to-thread integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	_ = cleanupMT(ctx, pool)
	if err := setupMT(ctx, pool); err != nil {
		fmt.Printf("Failed to set up move-to-thread fixture: %v\n", err)
		_ = cleanupMT(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	mtPool = pool
	mtH = &Handler{Queries: db.New(pool), Tx: pool}
	code := m.Run()
	if err := cleanupMT(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean move-to-thread fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupMT(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Move To Thread Test", mtEmail).Scan(&mtUser); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Move To Thread Tests", mtSlug, "Temporary", "MTT").Scan(&mtWsID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	// Owner: the handler's admin override is what lets the caller move comments
	// authored by anyone, which is the surface the UI exposes.
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, mtWsID, mtUser); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	return nil
}

func cleanupMT(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM comment WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM issue WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, mtSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, mtEmail)
	return nil
}

// makeIssue inserts a minimal issue. A raw insert bypasses the CreateIssue
// counter, so it picks the next free number itself.
func makeIssue(t *testing.T, ctx context.Context) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := mtPool.QueryRow(ctx,
		`INSERT INTO issue (workspace_id, title, creator_type, creator_id, number)
		 VALUES ($1, 'Move to thread', 'member', $2, (SELECT COALESCE(MAX(number),0)+1 FROM issue WHERE workspace_id = $1))
		 RETURNING id`,
		mtWsID, mtUser).Scan(&id); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return id
}

func makeComment(t *testing.T, ctx context.Context, issueID pgtype.UUID, content string, parent pgtype.UUID) db.Comment {
	t.Helper()
	c, err := mtH.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issueID,
		WorkspaceID: mtWsID,
		AuthorType:  "member",
		AuthorID:    mtUser,
		Content:     content,
		Type:        "comment",
		ParentID:    parent,
	})
	if err != nil {
		t.Fatalf("create comment %q: %v", content, err)
	}
	return c
}

// postMove drives the handler with the given picks, injecting the workspace
// + user the handler reads off the request.
func postMove(t *testing.T, ids ...pgtype.UUID) *httptest.ResponseRecorder {
	t.Helper()
	raw := make([]string, 0, len(ids))
	for _, id := range ids {
		raw = append(raw, `"`+util.UUIDToString(id)+`"`)
	}
	body := `{"comment_ids":[` + strings.Join(raw, ",") + `]}`
	r := httptest.NewRequest(http.MethodPost, "/api/comments/move-to-thread", strings.NewReader(body))
	r.Header.Set("X-User-ID", util.UUIDToString(mtUser))
	r = r.WithContext(middleware.SetMemberContext(r.Context(), util.UUIDToString(mtWsID), db.Member{}))
	w := httptest.NewRecorder()
	mtH.MoveToThread(w, r)
	return w
}

// readTimeline returns every comment on the issue keyed by id, so a test can
// assert on parent wiring and content without caring about row order.
func readTimeline(t *testing.T, ctx context.Context, issueID pgtype.UUID) map[string]db.Comment {
	t.Helper()
	rows, err := mtH.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issueID,
		WorkspaceID: mtWsID,
		Limit:       issueCommentCap,
	})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	out := make(map[string]db.Comment, len(rows))
	for _, c := range rows {
		out[util.UUIDToString(c.ID)] = c
	}
	return out
}

func parentOf(t *testing.T, timeline map[string]db.Comment, id pgtype.UUID) string {
	t.Helper()
	c, ok := timeline[util.UUIDToString(id)]
	if !ok {
		t.Fatalf("comment %s is gone from the issue", util.UUIDToString(id))
	}
	if !c.ParentID.Valid {
		return ""
	}
	return util.UUIDToString(c.ParentID)
}

func TestMoveToThreadMovesInsteadOfCopying(t *testing.T) {
	if mtPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx)

	root := makeComment(t, ctx, issueID, "root", pgtype.UUID{})
	keep := makeComment(t, ctx, issueID, "stays behind", root.ID)
	pick1 := makeComment(t, ctx, issueID, "new topic", root.ID)
	pick2 := makeComment(t, ctx, issueID, "more on the new topic", root.ID)

	res := postMove(t, pick1.ID, pick2.ID)
	if res.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", res.Code, res.Body.String())
	}
	var got moveToThreadResponse
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.RootCommentID != util.UUIDToString(pick1.ID) {
		t.Fatalf("want the oldest pick %s as the new root, got %s", util.UUIDToString(pick1.ID), got.RootCommentID)
	}
	if got.MovedCount != 2 {
		t.Fatalf("want moved_count 2, got %d", got.MovedCount)
	}

	timeline := readTimeline(t, ctx, issueID)

	// Nothing was copied: the issue still holds exactly the four comments.
	if len(timeline) != 4 {
		t.Fatalf("want 4 comments on the issue, got %d", len(timeline))
	}

	// The picks form a new thread — same rows, new wiring.
	if p := parentOf(t, timeline, pick1.ID); p != "" {
		t.Fatalf("want the oldest pick promoted to a root, got parent %s", p)
	}
	if p := parentOf(t, timeline, pick2.ID); p != util.UUIDToString(pick1.ID) {
		t.Fatalf("want the second pick under the new root, got parent %q", p)
	}

	// Nothing is left behind: no breadcrumb, content untouched, and the old
	// thread keeps its root and its unpicked reply.
	for _, c := range timeline {
		if strings.Contains(c.Content, "mention://comment/") || strings.Contains(c.Content, "Moved to new thread") {
			t.Fatalf("comment %s was rewritten into a breadcrumb: %q", util.UUIDToString(c.ID), c.Content)
		}
	}
	if body := timeline[util.UUIDToString(pick1.ID)].Content; body != "new topic" {
		t.Fatalf("moved comment lost its content: %q", body)
	}
	if p := parentOf(t, timeline, root.ID); p != "" {
		t.Fatalf("the old root moved: parent %s", p)
	}
	if p := parentOf(t, timeline, keep.ID); p != util.UUIDToString(root.ID) {
		t.Fatalf("the unpicked reply moved: parent %q", p)
	}
}

func TestMoveToThreadRehomesRepliesLeftBehind(t *testing.T) {
	if mtPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	issueID := makeIssue(t, ctx)

	root := makeComment(t, ctx, issueID, "root", pgtype.UUID{})
	pick := makeComment(t, ctx, issueID, "moves", root.ID)
	orphan := makeComment(t, ctx, issueID, "hangs off the moved comment", pick.ID)

	if res := postMove(t, pick.ID); res.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d: %s", res.Code, res.Body.String())
	}

	timeline := readTimeline(t, ctx, issueID)
	if len(timeline) != 3 {
		t.Fatalf("want 3 comments on the issue, got %d", len(timeline))
	}
	if p := parentOf(t, timeline, pick.ID); p != "" {
		t.Fatalf("want the pick promoted to a root, got parent %s", p)
	}
	// The unpicked reply was not dragged along — it re-homes to the moved
	// comment's old parent so the original thread stays intact.
	if p := parentOf(t, timeline, orphan.ID); p != util.UUIDToString(root.ID) {
		t.Fatalf("want the reply left behind re-homed to the old root, got parent %q", p)
	}
}
