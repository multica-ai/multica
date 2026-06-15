package notetypes

// Integration tests for the note-type materialisation engine (TECH-3529 review
// gap #1 + #2 + #4): running_doc must be idempotent per period, new_note must
// not duplicate a period, every materialised note must join the Notes feature
// with workspace visibility, and manual cadence must append on every run.
//
// Tests skip cleanly when no test DB is reachable, same pattern as
// sprints/sweeper_db_test.go and feature_flags/store_test.go.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

const (
	applyTestEmail         = "note-types-apply-test@multica.ai"
	applyTestName          = "Note Types Apply Test"
	applyTestWorkspaceSlug = "note-types-apply-tests"
)

var (
	applyTestPool        *pgxpool.Pool
	applyTestWorkspaceID pgtype.UUID
	applyTestUserID      pgtype.UUID
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("Skipping note-types apply integration tests: %v\n", err)
		os.Exit(m.Run())
	}
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("Skipping note-types apply integration tests: db not reachable: %v\n", err)
		pool.Close()
		os.Exit(m.Run())
	}
	if err := cleanupApplyFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to clean apply fixture: %v\n", err)
		pool.Close()
		os.Exit(1)
	}
	if err := setupApplyFixture(ctx, pool); err != nil {
		fmt.Printf("Failed to set up apply fixture: %v\n", err)
		_ = cleanupApplyFixture(ctx, pool)
		pool.Close()
		os.Exit(1)
	}
	applyTestPool = pool
	code := m.Run()
	if err := cleanupApplyFixture(context.Background(), pool); err != nil {
		fmt.Printf("Failed to clean apply fixture: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	pool.Close()
	os.Exit(code)
}

func setupApplyFixture(ctx context.Context, pool *pgxpool.Pool) error {
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, applyTestName, applyTestEmail).Scan(&applyTestUserID); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Note Types Apply Tests", applyTestWorkspaceSlug, "Temporary workspace", "NTA").Scan(&applyTestWorkspaceID); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, applyTestWorkspaceID, applyTestUserID); err != nil {
		return fmt.Errorf("create member: %w", err)
	}
	return nil
}

func cleanupApplyFixture(ctx context.Context, pool *pgxpool.Pool) error {
	stmts := []string{
		`DELETE FROM cerebro_note WHERE artifact_id IN (SELECT id FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1))`,
		`DELETE FROM cerebro_note_type WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM artifact WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM member WHERE workspace_id IN (SELECT id FROM workspace WHERE slug = $1)`,
		`DELETE FROM workspace WHERE slug = $1`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt, applyTestWorkspaceSlug); err != nil {
			return fmt.Errorf("cleanup %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, applyTestEmail); err != nil {
		return fmt.Errorf("cleanup user: %w", err)
	}
	return nil
}

// resetNoteTypeState wipes every note type + note + artifact in the test
// workspace so each scenario starts clean.
func resetNoteTypeState(t *testing.T, ctx context.Context) {
	t.Helper()
	for _, stmt := range []string{
		`DELETE FROM cerebro_note WHERE artifact_id IN (SELECT id FROM artifact WHERE workspace_id = $1)`,
		`DELETE FROM cerebro_note_type WHERE workspace_id = $1`,
		`DELETE FROM artifact WHERE workspace_id = $1`,
	} {
		if _, err := applyTestPool.Exec(ctx, stmt, applyTestWorkspaceID); err != nil {
			t.Fatalf("reset %q: %v", stmt, err)
		}
	}
}

func createNoteType(t *testing.T, ctx context.Context, mode, cadence string) cerebrodb.CerebroNoteType {
	t.Helper()
	return createNoteTypeFull(t, ctx, mode, cadence, 1, false, 1, "## Review {{periode}}\n- Tal:")
}

func createNoteTypeFull(t *testing.T, ctx context.Context, mode, cadence string, count int32, numbering bool, nextNumber int32, template string) cerebrodb.CerebroNoteType {
	t.Helper()
	q := cerebrodb.New(applyTestPool)
	nt, err := q.CreateCerebroNoteType(ctx, cerebrodb.CreateCerebroNoteTypeParams{
		WorkspaceID:      applyTestWorkspaceID,
		Name:             "Business Review",
		Icon:             "",
		TemplateBody:     template,
		RecurrenceMode:   mode,
		CadenceUnit:      cadence,
		CadenceCount:     count,
		Enabled:          true,
		CreatedBy:        applyTestUserID,
		NumberingEnabled: numbering,
		NextNumber:       nextNumber,
	})
	if err != nil {
		t.Fatalf("create note type: %v", err)
	}
	return nt
}

// applyAt runs Apply in its own transaction (as production does) and re-fetches
// the note type so the caller sees the mutated running_doc / last_period_key.
func applyAt(t *testing.T, ctx context.Context, ntID pgtype.UUID, now time.Time) (bool, pgtype.UUID, cerebrodb.CerebroNoteType) {
	t.Helper()
	q := cerebrodb.New(applyTestPool)
	nt, err := q.GetCerebroNoteType(ctx, ntID)
	if err != nil {
		t.Fatalf("get note type: %v", err)
	}
	tx, err := applyTestPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	created, artID, err := Apply(ctx, q.WithTx(tx), nt, now)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("apply: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	refreshed, err := q.GetCerebroNoteType(ctx, ntID)
	if err != nil {
		t.Fatalf("re-get note type: %v", err)
	}
	return created, artID, refreshed
}

func bodyOf(t *testing.T, ctx context.Context, artifactID pgtype.UUID) string {
	t.Helper()
	row, err := cerebrodb.New(applyTestPool).GetNoteTypeArtifactBody(ctx, artifactID)
	if err != nil {
		t.Fatalf("get artifact body: %v", err)
	}
	return row.Body
}

func TestApply_RunningDoc_Idempotent(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	nt := createNoteType(t, ctx, ModeRunningDoc, CadenceMonth)
	now := time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC)

	created1, art1, _ := applyAt(t, ctx, nt.ID, now)
	if !created1 {
		t.Fatal("first run should create the rolling document")
	}
	body1 := bodyOf(t, ctx, art1)

	// Same period again (e.g. a double "Start ny") must be a no-op, not a
	// duplicate prepend.
	created2, art2, _ := applyAt(t, ctx, nt.ID, now.Add(2*time.Hour))
	if created2 {
		t.Error("second run in same period should be idempotent (created=false)")
	}
	if art2 != art1 {
		t.Error("idempotent run should return the same rolling document id")
	}
	if got := bodyOf(t, ctx, art1); got != body1 {
		t.Errorf("body changed on idempotent run:\nbefore=%q\nafter=%q", body1, got)
	}
	if n := strings.Count(bodyOf(t, ctx, art1), "## Review"); n != 1 {
		t.Errorf("expected 1 section after idempotent run, got %d", n)
	}
}

func TestApply_RunningDoc_NewPeriodPrepends(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	nt := createNoteType(t, ctx, ModeRunningDoc, CadenceMonth)
	feb := time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC)
	mar := time.Date(2026, time.March, 11, 9, 0, 0, 0, time.UTC)

	_, art1, _ := applyAt(t, ctx, nt.ID, feb)
	created2, art2, _ := applyAt(t, ctx, nt.ID, mar)
	if !created2 {
		t.Fatal("new period should append to the rolling document")
	}
	if art2 != art1 {
		t.Error("running_doc should reuse one document across periods")
	}
	body := bodyOf(t, ctx, art1)
	if n := strings.Count(body, "## Review"); n != 2 {
		t.Errorf("expected 2 sections after a new period, got %d", n)
	}
	// Newest (March) section is prepended above the older (February) one.
	if !strings.Contains(body, "Marts 2026") || !strings.Contains(body, "Februar 2026") {
		t.Errorf("both period labels should be present: %q", body)
	}
	if strings.Index(body, "Marts 2026") > strings.Index(body, "Februar 2026") {
		t.Error("newest section should be prepended above the older one")
	}
}

func TestApply_NewNote_Idempotent(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	nt := createNoteType(t, ctx, ModeNewNote, CadenceMonth)
	now := time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC)

	created1, art1, _ := applyAt(t, ctx, nt.ID, now)
	if !created1 {
		t.Fatal("first run should create a note for the period")
	}
	created2, art2, _ := applyAt(t, ctx, nt.ID, now.Add(3*time.Hour))
	if created2 {
		t.Error("second run in same period should be idempotent (created=false)")
	}
	if art2 != art1 {
		t.Error("idempotent run should return the existing note id")
	}
	if n := countArtifacts(t, ctx, nt.ID); n != 1 {
		t.Errorf("expected exactly 1 note for the period, got %d", n)
	}
}

// Gap #2: every materialised note joins the Notes feature with workspace
// visibility and the note type's creator as owner.
func TestApply_RegistersAsWorkspaceNote(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	q := cerebrodb.New(applyTestPool)

	for _, mode := range []string{ModeRunningDoc, ModeNewNote} {
		resetNoteTypeState(t, ctx)
		nt := createNoteType(t, ctx, mode, CadenceMonth)
		_, art, _ := applyAt(t, ctx, nt.ID, time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC))

		note, err := q.GetNote(ctx, art)
		if err != nil {
			t.Fatalf("[%s] expected a cerebro_note row, got: %v", mode, err)
		}
		if note.Visibility != "workspace" {
			t.Errorf("[%s] visibility = %q, want workspace", mode, note.Visibility)
		}
		if note.OwnerID != applyTestUserID {
			t.Errorf("[%s] owner mismatch", mode)
		}
	}
}

// Manual cadence is timestamp-keyed, so each off-cycle run appends a fresh
// section rather than being suppressed by the idempotency check.
func TestApply_Manual_AppendsEachRun(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	nt := createNoteType(t, ctx, ModeRunningDoc, CadenceManual)

	_, art1, _ := applyAt(t, ctx, nt.ID, time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC))
	created2, art2, _ := applyAt(t, ctx, nt.ID, time.Date(2026, time.February, 11, 11, 0, 0, 0, time.UTC))
	if !created2 {
		t.Fatal("manual run should always append (created=true)")
	}
	if art2 != art1 {
		t.Error("manual running_doc should reuse one document")
	}
	if n := strings.Count(bodyOf(t, ctx, art1), "## Review"); n != 2 {
		t.Errorf("expected 2 sections after two manual runs, got %d", n)
	}
}

// Numbering: each materialised note is stamped with an incrementing number
// starting from the chosen next_number, and the counter advances.
func TestApply_Numbering(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	// new_note, manual cadence (always creates), numbering from 5.
	nt := createNoteTypeFull(t, ctx, ModeNewNote, CadenceManual, 1, true, 5, "## Review #{{nummer}} – {{periode}}")

	_, art1, _ := applyAt(t, ctx, nt.ID, time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC))
	_, art2, refreshed := applyAt(t, ctx, nt.ID, time.Date(2026, time.February, 11, 11, 0, 0, 0, time.UTC))

	if t1 := titleOf(t, ctx, art1); !strings.Contains(t1, "#5") {
		t.Errorf("first note title should carry #5, got %q", t1)
	}
	if t2 := titleOf(t, ctx, art2); !strings.Contains(t2, "#6") {
		t.Errorf("second note title should carry #6, got %q", t2)
	}
	if b1 := bodyOf(t, ctx, art1); !strings.Contains(b1, "Review #5") {
		t.Errorf("first note body should render {{nummer}}=5, got %q", b1)
	}
	if refreshed.NextNumber != 7 {
		t.Errorf("next_number should have advanced to 7, got %d", refreshed.NextNumber)
	}
}

// An idempotent no-op must NOT burn a number.
func TestApply_Numbering_NoOpDoesNotBurn(t *testing.T) {
	if applyTestPool == nil {
		t.Skip("no test DB")
	}
	ctx := context.Background()
	resetNoteTypeState(t, ctx)
	nt := createNoteTypeFull(t, ctx, ModeRunningDoc, CadenceMonth, 1, true, 1, "## Review #{{nummer}}")
	now := time.Date(2026, time.February, 11, 9, 0, 0, 0, time.UTC)

	applyAt(t, ctx, nt.ID, now)                                   // creates, assigns #1, next→2
	_, _, refreshed := applyAt(t, ctx, nt.ID, now.Add(time.Hour)) // idempotent no-op
	if refreshed.NextNumber != 2 {
		t.Errorf("no-op should not burn a number; next_number = %d, want 2", refreshed.NextNumber)
	}
}

func titleOf(t *testing.T, ctx context.Context, artifactID pgtype.UUID) string {
	t.Helper()
	var title string
	if err := applyTestPool.QueryRow(ctx,
		`SELECT title FROM artifact WHERE id = $1`, artifactID,
	).Scan(&title); err != nil {
		t.Fatalf("get title: %v", err)
	}
	return title
}

func countArtifacts(t *testing.T, ctx context.Context, ntID pgtype.UUID) int {
	t.Helper()
	var n int
	if err := applyTestPool.QueryRow(ctx,
		`SELECT count(*) FROM artifact WHERE note_type_id = $1`, ntID,
	).Scan(&n); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	return n
}
