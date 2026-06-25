package note

// Integration test for the FIR-2022 note/document FTS search against a real
// Postgres (skips when DATABASE_URL / the default test DB is unreachable). It
// proves the three things the unit test cannot: that FTS actually matches on
// title, body AND comment, that the kind filter narrows, and — most important —
// that the visibility access rule never leaks a private note to another user.

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrosearch "github.com/multica-ai/multica/server/internal/cerebro/search"
)

func searchTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@127.0.0.1:5432/multica_t2022?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping note search integration test: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping note search integration test: db not reachable: %v", err)
	}
	return pool
}

// runSearchTitles runs the real search SQL as userID and returns a map of
// matched title -> match_source.
func runSearchTitles(t *testing.T, pool *pgxpool.Pool, ws, user pgtype.UUID, text, kind string) map[string]string {
	t.Helper()
	ctx := context.Background()
	sql, args := buildNoteSearchSQL(noteSearchInput{
		WorkspaceID: ws, UserID: user, Text: text, Kind: kind, Limit: 50, Offset: 0,
	})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL pg_trgm.word_similarity_threshold = "+cerebrosearch.TrigramThreshold); err != nil {
		t.Fatalf("set threshold: %v", err)
	}
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var (
			id, wsID, folderID, issueID, projectID, ownerID  pgtype.UUID
			kindVal, title, visibility, matchSource, comment pgtype.Text
			updatedAt                                        pgtype.Timestamptz
			body                                             pgtype.Text
			totalCount                                       int64
		)
		if err := rows.Scan(&id, &wsID, &kindVal, &title, &body, &folderID, &issueID, &projectID,
			&ownerID, &visibility, &updatedAt, &totalCount, &matchSource, &comment); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[title.String] = matchSource.String
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func TestNoteSearch_Integration(t *testing.T) {
	pool := searchTestPool(t)
	defer pool.Close()
	ctx := context.Background()

	const slug = "fir2022-search-it"
	// Clean any leftovers from a prior run.
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_note_comment WHERE note_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug=$1))`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM cerebro_note WHERE artifact_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug=$1))`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug=$1)`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug=$1)`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM workspace WHERE slug=$1`, slug)
		_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE email IN ('fir2022a@test.local','fir2022b@test.local')`)
	}
	cleanup()
	defer cleanup()

	var userA, userB, ws pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('FIR2022 A','fir2022a@test.local') RETURNING id`).Scan(&userA); err != nil {
		t.Fatalf("user A: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('FIR2022 B','fir2022b@test.local') RETURNING id`).Scan(&userB); err != nil {
		t.Fatalf("user B: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name,slug,description,issue_prefix) VALUES ('FIR2022 Search',$1,'t','F22') RETURNING id`, slug).Scan(&ws); err != nil {
		t.Fatalf("workspace: %v", err)
	}
	for _, u := range []pgtype.UUID{userA, userB} {
		if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, ws, u); err != nil {
			t.Fatalf("member: %v", err)
		}
	}

	// Helper: insert an artifact, optionally with a cerebro_note row.
	mkArtifact := func(kind, title, body string, asNote bool, visibility string) pgtype.UUID {
		var id pgtype.UUID
		if err := pool.QueryRow(ctx,
			`INSERT INTO artifact (workspace_id,kind,title,body,author_type,author_id) VALUES ($1,$2,$3,$4,'member',$5) RETURNING id`,
			ws, kind, title, body, userA).Scan(&id); err != nil {
			t.Fatalf("artifact %q: %v", title, err)
		}
		if asNote {
			if _, err := pool.Exec(ctx, `INSERT INTO cerebro_note (artifact_id,owner_id,visibility) VALUES ($1,$2,$3)`, id, userA, visibility); err != nil {
				t.Fatalf("cerebro_note %q: %v", title, err)
			}
		}
		return id
	}

	mkArtifact("note", "Deployment runbook", "How we ship to staging from main.", true, "workspace")
	mkArtifact("note", "Secret salary memo", "Confidential compensation figures.", true, "private")
	mkArtifact("report", "Q3 revenue report", "Revenue growth across brands.", false, "")
	onboarding := mkArtifact("note", "Onboarding guide", "Welcome to the team.", true, "workspace")
	if _, err := pool.Exec(ctx,
		`INSERT INTO cerebro_note_comment (note_id,body,author_type,author_id) VALUES ($1,'Remember the kubernetes deploy step.','member',$2)`,
		onboarding, userA); err != nil {
		t.Fatalf("comment: %v", err)
	}

	// a) title match, visible to userB (workspace visibility).
	got := runSearchTitles(t, pool, ws, userB, "deployment", "")
	if got["Deployment runbook"] != "title" {
		t.Errorf("expected title match for Deployment runbook, got %v", got)
	}

	// b) comment match — onboarding surfaces via its comment.
	got = runSearchTitles(t, pool, ws, userB, "kubernetes", "")
	if src, ok := got["Onboarding guide"]; !ok || src != "comment" {
		t.Errorf("expected comment match for Onboarding guide, got %v", got)
	}

	// c) ACCESS CONTROL: userB must NOT see the private note.
	got = runSearchTitles(t, pool, ws, userB, "confidential", "")
	if _, leaked := got["Secret salary memo"]; leaked {
		t.Errorf("PRIVATE NOTE LEAKED to non-owner: %v", got)
	}

	// d) owner (userA) DOES see the private note (body match).
	got = runSearchTitles(t, pool, ws, userA, "confidential", "")
	if got["Secret salary memo"] != "body" {
		t.Errorf("expected owner to find private note by body, got %v", got)
	}

	// e) plain document (no cerebro_note row) matches by body — "brands" is only
	// in the body, not the title, so match_source must be "body".
	got = runSearchTitles(t, pool, ws, userB, "brands", "")
	if got["Q3 revenue report"] != "body" {
		t.Errorf("expected document body match for Q3 revenue report, got %v", got)
	}

	// f) kind filter narrows to reports only.
	got = runSearchTitles(t, pool, ws, userB, "deployment", "report")
	if _, ok := got["Deployment runbook"]; ok {
		t.Errorf("kind=report should exclude the note, got %v", got)
	}
}
