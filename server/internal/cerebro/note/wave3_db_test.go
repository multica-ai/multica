package note

// Integration tests for the Wave 3 note backend (TECH-3556): comments +
// suggestions (G1), version history with session coalescing (G2), and the
// interim edit lock (G3). These exercise the cerebro query layer plus the
// Handler's snapshot / suggestion-apply logic against a real database.
//
// They skip cleanly when no test DB is reachable, same pattern as
// note_types/apply_db_test.go. Run against a migrated DB, e.g.:
//   DATABASE_URL=postgres://multica:multica@127.0.0.1:5432/multica_t3556?sslmode=disable \
//     go test ./internal/cerebro/note/ -run TestWave3 -v

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	w3Email  = "wave3-notes-test@multica.ai"
	w3EmailB = "wave3-notes-test-b@multica.ai"
	w3Name   = "Wave3 Notes Test"
	w3Slug   = "wave3-notes-tests"
)

var (
	w3Pool  *pgxpool.Pool
	w3WsID  pgtype.UUID
	w3UserA pgtype.UUID
	w3UserB pgtype.UUID
	w3H     *Handler
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@127.0.0.1:5432/multica_t3556?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping wave3 note integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping wave3 note integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	_ = cleanupW3(ctx, pool)
	if err := setupW3(ctx, pool); err != nil {
		fmt.Printf("Failed to set up wave3 fixture: %v\n", err)
		_ = cleanupW3(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	w3Pool = pool
	w3H = &Handler{Upstream: db.New(pool), Cerebro: cerebrodb.New(pool)}
	code := m.Run()
	if err := cleanupW3(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean wave3 fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupW3(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, w3Name, w3Email).Scan(&w3UserA); err != nil {
		return fmt.Errorf("create user A: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`, w3Name+" B", w3EmailB).Scan(&w3UserB); err != nil {
		return fmt.Errorf("create user B: %w", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1, $2, $3, $4) RETURNING id`,
		"Wave3 Notes Tests", w3Slug, "Temporary", "W3N").Scan(&w3WsID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	for _, u := range []pgtype.UUID{w3UserA, w3UserB} {
		if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, w3WsID, u); err != nil {
			return fmt.Errorf("create member: %w", err)
		}
	}
	return nil
}

func cleanupW3(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_note_comment WHERE note_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM cerebro_note_version WHERE note_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM cerebro_note WHERE artifact_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, w3Slug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM "user" WHERE email IN ($1, $2)`, w3Email, w3EmailB)
	return nil
}

// makeNote creates an artifact(kind=note) + cerebro_note row owned by userA and
// returns its id. Each test gets its own note.
func makeNote(t *testing.T, ctx context.Context, title, body string) pgtype.UUID {
	t.Helper()
	id, _ := uuid.NewV7()
	art, err := w3H.Upstream.CreateArtifact(ctx, db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: w3WsID,
		Kind:        "note",
		Format:      "md",
		Title:       title,
		Body:        body,
		Metadata:    []byte("{}"),
		AuthorType:  "member",
		AuthorID:    w3UserA,
	})
	if err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if _, err := w3H.Cerebro.UpsertNote(ctx, cerebrodb.UpsertNoteParams{
		ArtifactID: art.ID,
		OwnerID:    w3UserA,
		Visibility: "private",
		Pinned:     false,
	}); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	return art.ID
}

func TestWave3Comments(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "C", "hello world body")

	// Root comment anchored to "world".
	root, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID:      noteID,
		Kind:        "comment",
		Body:        "is this the right word?",
		AnchorQuote: pgtype.Text{String: "world", Valid: true},
		AuthorType:  "member",
		AuthorID:    w3UserA,
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	// Reply by user B.
	if _, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID:       noteID,
		ThreadRootID: root.ID,
		Kind:         "comment",
		Body:         "yes it is",
		AuthorType:   "member",
		AuthorID:     w3UserB,
	}); err != nil {
		t.Fatalf("create reply: %v", err)
	}

	list, err := w3H.Cerebro.ListNoteComments(ctx, noteID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %d comments, err=%v; want 2", len(list), err)
	}
	open, _ := w3H.Cerebro.CountOpenNoteComments(ctx, noteID)
	if open != 1 {
		t.Fatalf("open root count = %d, want 1", open)
	}

	// Resolve the thread → open count drops to 0.
	if _, err := w3H.Cerebro.SetNoteCommentResolved(ctx, cerebrodb.SetNoteCommentResolvedParams{
		ID: root.ID, Resolved: true, ResolvedBy: w3UserA,
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	open, _ = w3H.Cerebro.CountOpenNoteComments(ctx, noteID)
	if open != 0 {
		t.Fatalf("open root count after resolve = %d, want 0", open)
	}
}

func TestWave3SuggestionApply(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "S", "The qiuck brown fox jumps.")

	// Suggestion: replace the typo "qiuck" with "quick".
	sug, err := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID:         noteID,
		Kind:           "suggestion",
		Body:           "typo",
		AnchorQuote:    pgtype.Text{String: "qiuck", Valid: true},
		AnchorPrefix:   pgtype.Text{String: "The ", Valid: true},
		AnchorSuffix:   pgtype.Text{String: " brown", Valid: true},
		SuggestionText: pgtype.Text{String: "quick", Valid: true},
		AuthorType:     "member",
		AuthorID:       w3UserB,
	})
	if err != nil {
		t.Fatalf("create suggestion: %v", err)
	}

	// Accept → body spliced.
	if err := w3H.applySuggestion(ctx, w3WsID, noteID, w3UserA, sug); err != nil {
		t.Fatalf("applySuggestion: %v", err)
	}
	art, _ := w3H.Upstream.GetArtifact(ctx, db.GetArtifactParams{ID: noteID, WorkspaceID: w3WsID})
	if art.Body != "The quick brown fox jumps." {
		t.Fatalf("body after accept = %q, want corrected", art.Body)
	}
	// The accept also recorded version history (pre + post snapshots).
	vers, _ := w3H.Cerebro.ListNoteVersions(ctx, noteID)
	if len(vers) < 2 {
		t.Fatalf("expected >=2 versions after suggestion apply, got %d", len(vers))
	}

	// A suggestion whose anchor no longer exists must refuse, not corrupt.
	bad, _ := w3H.Cerebro.CreateNoteComment(ctx, cerebrodb.CreateNoteCommentParams{
		NoteID:         noteID,
		Kind:           "suggestion",
		Body:           "stale",
		AnchorQuote:    pgtype.Text{String: "nonexistent text", Valid: true},
		SuggestionText: pgtype.Text{String: "x", Valid: true},
		AuthorType:     "member",
		AuthorID:       w3UserB,
	})
	if err := w3H.applySuggestion(ctx, w3WsID, noteID, w3UserA, bad); err == nil {
		t.Fatal("expected applySuggestion to refuse a missing anchor")
	}
}

func TestWave3VersionCoalescing(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "V", "v1")

	// Two quick edits by the SAME author coalesce into one version.
	w3H.snapshotVersion(ctx, noteID, w3UserA, "V", "v1-edit", "edit", "", true)
	w3H.snapshotVersion(ctx, noteID, w3UserA, "V", "v1-edit-more", "edit", "", true)
	vers, _ := w3H.Cerebro.ListNoteVersions(ctx, noteID)
	if len(vers) != 1 {
		t.Fatalf("same-author burst = %d versions, want 1 (coalesced)", len(vers))
	}
	if vers[0].Body != "v1-edit-more" {
		t.Fatalf("coalesced version body = %q, want rolled forward", vers[0].Body)
	}

	// An edit by a DIFFERENT author starts a new version entry.
	w3H.snapshotVersion(ctx, noteID, w3UserB, "V", "v2-by-b", "edit", "", true)
	vers, _ = w3H.Cerebro.ListNoteVersions(ctx, noteID)
	if len(vers) != 2 {
		t.Fatalf("after different-author edit = %d versions, want 2", len(vers))
	}

	// A manual save is always its own row, never coalesced.
	w3H.snapshotVersion(ctx, noteID, w3UserB, "V", "v2-by-b", "manual", "checkpoint", false)
	vers, _ = w3H.Cerebro.ListNoteVersions(ctx, noteID)
	if len(vers) != 3 {
		t.Fatalf("after manual save = %d versions, want 3", len(vers))
	}
	if vers[0].VersionNo != 3 || vers[0].Reason != "manual" || vers[0].Label.String != "checkpoint" {
		t.Fatalf("latest version = no:%d reason:%s label:%s; want 3/manual/checkpoint",
			vers[0].VersionNo, vers[0].Reason, vers[0].Label.String)
	}
}

func TestWave3Lock(t *testing.T) {
	if w3Pool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	noteID := makeNote(t, ctx, "L", "lock me")
	cutoffNow := func() pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: time.Now().Add(-lockTimeout), Valid: true}
	}

	// A acquires.
	if _, err := w3H.Cerebro.AcquireNoteLock(ctx, cerebrodb.AcquireNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserA, LockHeartbeatAt: cutoffNow(),
	}); err != nil {
		t.Fatalf("A acquire: %v", err)
	}
	// B cannot acquire a live lock (returns no rows).
	if _, err := w3H.Cerebro.AcquireNoteLock(ctx, cerebrodb.AcquireNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserB, LockHeartbeatAt: cutoffNow(),
	}); err == nil {
		t.Fatal("B should NOT acquire a live lock held by A")
	}
	// A heartbeats fine; B's heartbeat is a no-op (not the holder).
	if _, err := w3H.Cerebro.HeartbeatNoteLock(ctx, cerebrodb.HeartbeatNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserA,
	}); err != nil {
		t.Fatalf("A heartbeat: %v", err)
	}
	if _, err := w3H.Cerebro.HeartbeatNoteLock(ctx, cerebrodb.HeartbeatNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserB,
	}); err == nil {
		t.Fatal("B heartbeat should be a no-op (no rows)")
	}

	// Simulate A going stale: push the heartbeat into the past.
	if _, err := w3Pool.Exec(ctx, `UPDATE cerebro_note SET lock_heartbeat_at = now() - interval '5 minutes' WHERE artifact_id = $1`, noteID); err != nil {
		t.Fatalf("age lock: %v", err)
	}
	// Now B can acquire the stale lock.
	if _, err := w3H.Cerebro.AcquireNoteLock(ctx, cerebrodb.AcquireNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserB, LockHeartbeatAt: cutoffNow(),
	}); err != nil {
		t.Fatalf("B acquire stale lock: %v", err)
	}

	// A takes over (force) regardless of B holding it.
	row, err := w3H.Cerebro.ForceAcquireNoteLock(ctx, cerebrodb.ForceAcquireNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserA,
	})
	if err != nil || uuidStr(row.LockedBy) != uuidStr(w3UserA) {
		t.Fatalf("A takeover failed: holder=%s err=%v", uuidStr(row.LockedBy), err)
	}
	// lockResponse computes a live, held-by-me lock for A.
	lr := lockResponse(noteID, w3UserA, row.LockedBy, row.LockedAt, row.LockHeartbeatAt)
	if !lr.Live || !lr.HeldByMe {
		t.Fatalf("lockResponse = live:%v heldByMe:%v; want both true", lr.Live, lr.HeldByMe)
	}

	// A releases; lock is then free.
	if err := w3H.Cerebro.ReleaseNoteLock(ctx, cerebrodb.ReleaseNoteLockParams{
		ArtifactID: noteID, LockedBy: w3UserA,
	}); err != nil {
		t.Fatalf("release: %v", err)
	}
	got, _ := w3H.Cerebro.GetNoteLock(ctx, noteID)
	if got.LockedBy.Valid {
		t.Fatalf("lock should be free after release, held by %s", uuidStr(got.LockedBy))
	}
}
