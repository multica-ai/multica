package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Issue status catalog tests (MUL-6243).

// seedTestCatalog makes the shared test workspace's catalog present. The
// fixture creates its workspace with raw SQL, so it has no catalog rows —
// which is itself the unseeded case covered by TestUnseededWorkspaceStillAccepts.
func seedTestCatalog(t *testing.T) {
	t.Helper()
	if err := issuestatus.Ensure(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}
}

// createTestCustomStatus inserts a custom status directly and removes it after
// the test, so catalog state cannot leak between tests in the shared workspace.
func createTestCustomStatus(t *testing.T, key, category string) db.IssueStatus {
	t.Helper()
	seedTestCatalog(t)
	entry, err := testHandler.Queries.CreateIssueStatusEntry(context.Background(), db.CreateIssueStatusEntryParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         key,
		Name:        key,
		Description: "",
		Category:    category,
		Color:       "#123456",
	})
	if err != nil {
		t.Fatalf("create custom status %q: %v", key, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE id = $1`, entry.ID)
	})
	return entry
}

// TestEnsureIsIdempotent covers the rolling-deploy case: two pods can seed the
// same workspace concurrently and the second must be a no-op, not an error.
func TestEnsureIsIdempotent(t *testing.T) {
	ctx := context.Background()
	seedTestCatalog(t)
	if err := issuestatus.Ensure(ctx, testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("second Ensure should be a no-op, got: %v", err)
	}

	entries, err := testHandler.Queries.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}

	systemByKey := map[string]db.IssueStatus{}
	for _, e := range entries {
		if e.IsSystem {
			systemByKey[e.Key] = e
		}
	}
	if len(systemByKey) != 7 {
		t.Fatalf("expected exactly 7 built-in rows after double seeding, got %d", len(systemByKey))
	}
	// Every built-in must be its own category's canonical — the invariant that
	// makes Effective an identity function on built-in keys.
	for key, entry := range systemByKey {
		if entry.Category != key {
			t.Errorf("built-in %q has category %q; a built-in must be its own category's canonical", key, entry.Category)
		}
	}
}

// TestCatalogOrderMatchesHistoricalStatusOrder pins the default board order.
// A workspace with no custom statuses must list exactly as it did before this
// feature, or every existing user's board silently rearranges.
func TestCatalogOrderMatchesHistoricalStatusOrder(t *testing.T) {
	seedTestCatalog(t)
	entries, err := testHandler.Queries.ListIssueStatusEntries(context.Background(), db.ListIssueStatusEntriesParams{
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}

	var gotSystem []string
	for _, e := range entries {
		if e.IsSystem {
			gotSystem = append(gotSystem, e.Key)
		}
	}
	want := []string{"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"}
	for i := range want {
		if i >= len(gotSystem) || gotSystem[i] != want[i] {
			t.Fatalf("built-in order = %v, want %v (frontend STATUS_ORDER)", gotSystem, want)
		}
	}
}

// TestUnseededWorkspaceStillAcceptsBuiltInStatuses is the regression guard for
// the failure mode found while wiring this up: requiring a catalog row to
// validate a status made every issue write fail in a workspace whose seed had
// not landed. The built-ins must resolve with or without a row.
func TestUnseededWorkspaceStillAcceptsBuiltInStatuses(t *testing.T) {
	ctx := context.Background()
	// Strip the catalog to simulate a workspace created by a pod that predates
	// the feature, or one the seed migration has not reached yet.
	if _, err := testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1`, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("clear catalog: %v", err)
	}
	t.Cleanup(func() { seedTestCatalog(t) })

	for _, key := range issuestatus.Canonical() {
		if _, err := issuestatus.Resolve(ctx, testHandler.Queries, parseUUID(testWorkspaceID), key); err != nil {
			t.Errorf("built-in %q must resolve in an unseeded workspace, got: %v", key, err)
		}
		if got := issuestatus.Effective(ctx, testHandler.Queries, parseUUID(testWorkspaceID), key); got != key {
			t.Errorf("Effective(%q) = %q in an unseeded workspace, want the key unchanged", key, got)
		}
	}

	// A non-built-in key still needs a catalog row, so failing open is scoped
	// exactly to the set that was valid before this feature.
	if _, err := issuestatus.Resolve(ctx, testHandler.Queries, parseUUID(testWorkspaceID), "human_review"); err == nil {
		t.Error("a custom key with no catalog row must not resolve")
	}

	// The error message must never omit a key that Resolve accepts.
	keys, err := issuestatus.ActiveKeys(ctx, testHandler.Queries, parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("ActiveKeys: %v", err)
	}
	if len(keys) != 7 {
		t.Errorf("ActiveKeys on an unseeded workspace = %v, want the 7 built-ins", keys)
	}
}

// TestCustomStatusInheritsItsCategoryBehavior is the core promise of the
// model: a custom status behaves as the canonical status it names.
func TestCustomStatusInheritsItsCategoryBehavior(t *testing.T) {
	ctx := context.Background()
	cases := []struct{ key, category string }{
		{"human_review_t", issuestatus.InReview},
		{"rework_t", issuestatus.Todo},
		{"gate_approved_t", issuestatus.Done},
		{"waiting_customer_t", issuestatus.Blocked},
		{"triage_later_t", issuestatus.Backlog},
	}
	for _, tc := range cases {
		createTestCustomStatus(t, tc.key, tc.category)
		got := issuestatus.Effective(ctx, testHandler.Queries, parseUUID(testWorkspaceID), tc.key)
		if got != tc.category {
			t.Errorf("Effective(%q) = %q, want %q", tc.key, got, tc.category)
		}
	}
}

// TestIssueWriteAcceptsCustomStatusAndRejectsUnknown exercises the real HTTP
// write path, which is where the dropped enum CHECK has to be replaced.
func TestIssueWriteAcceptsCustomStatusAndRejectsUnknown(t *testing.T) {
	createTestCustomStatus(t, "human_review_w", issuestatus.InReview)

	t.Run("custom status is accepted", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title":  "custom status write",
			"status": "human_review_w",
		})
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if created.Status != "human_review_w" {
			t.Errorf("issue.status = %q, want the custom key stored verbatim", created.Status)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parseUUID(created.ID))
		})
	})

	t.Run("unknown status is rejected", func(t *testing.T) {
		req := newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title":  "unknown status write",
			"status": "not_a_status",
		})
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for an unknown status, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("archived status is rejected", func(t *testing.T) {
		entry := createTestCustomStatus(t, "retired_w", issuestatus.InProgress)
		if _, err := testHandler.Queries.ArchiveIssueStatusEntry(context.Background(), db.ArchiveIssueStatusEntryParams{
			ID:          entry.ID,
			WorkspaceID: parseUUID(testWorkspaceID),
		}); err != nil {
			t.Fatalf("archive: %v", err)
		}
		req := newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title":  "archived status write",
			"status": "retired_w",
		})
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for an archived status, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestBuiltInStatusesAreImmutable pins the product decision that built-in
// statuses cannot be renamed, recolored, or archived, so the default workspace
// is identical for every user who never opens the settings page.
func TestBuiltInStatusesAreImmutable(t *testing.T) {
	ctx := context.Background()
	seedTestCatalog(t)
	builtIn, err := testHandler.Queries.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Key:         "in_review",
	})
	if err != nil {
		t.Fatalf("load built-in: %v", err)
	}

	t.Run("rename is refused at the API", func(t *testing.T) {
		req := withURLParam(
			newRequest(http.MethodPatch, "/api/issue-statuses/"+uuidToString(builtIn.ID), map[string]any{"name": "Renamed"}),
			"id", uuidToString(builtIn.ID))
		rec := httptest.NewRecorder()
		testHandler.UpdateIssueStatus(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 renaming a built-in, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("archive is refused at the API", func(t *testing.T) {
		req := withURLParam(
			newRequest(http.MethodDelete, "/api/issue-statuses/"+uuidToString(builtIn.ID), nil),
			"id", uuidToString(builtIn.ID))
		rec := httptest.NewRecorder()
		testHandler.ArchiveIssueStatus(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 archiving a built-in, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// Defense in depth: even a direct query cannot rename or archive a
	// built-in, because the statements carry an is_system guard.
	t.Run("storage layer refuses too", func(t *testing.T) {
		if _, err := testHandler.Queries.UpdateIssueStatusEntry(ctx, db.UpdateIssueStatusEntryParams{
			ID:          builtIn.ID,
			WorkspaceID: parseUUID(testWorkspaceID),
			Name:        pgtype.Text{String: "Renamed", Valid: true},
		}); err == nil {
			t.Error("UpdateIssueStatusEntry must not touch a built-in row")
		}
		if _, err := testHandler.Queries.ArchiveIssueStatusEntry(ctx, db.ArchiveIssueStatusEntryParams{
			ID:          builtIn.ID,
			WorkspaceID: parseUUID(testWorkspaceID),
		}); err == nil {
			t.Error("ArchiveIssueStatusEntry must not touch a built-in row")
		}
	})
}

// TestArchiveRetiresStatusWithoutTouchingExistingIssues pins the archive rule:
// archiving retires a status from FUTURE use only. Issues already on it keep it
// and keep behaving as their category prescribes — retiring a label must not
// rewrite history.
func TestArchiveRetiresStatusWithoutTouchingExistingIssues(t *testing.T) {
	ctx := context.Background()
	entry := createTestCustomStatus(t, "in_use_a", issuestatus.InProgress)
	issueID := mustCreateIssue(t, "occupies the status", "in_use_a")

	if code := archiveStatusVia(t, entry); code != http.StatusOK {
		t.Fatalf("archiving an in-use status should succeed, got %d", code)
	}

	// The existing issue keeps the archived status verbatim.
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "in_use_a" {
		t.Errorf("existing issue status = %q, want it left on the archived status", status)
	}

	// And it still resolves to its category, so its platform behavior is
	// unchanged — Effective deliberately ignores archived_at.
	if got := issuestatus.Effective(ctx, testHandler.Queries, parseUUID(testWorkspaceID), "in_use_a"); got != issuestatus.InProgress {
		t.Errorf("Effective on an archived status = %q, want %q", got, issuestatus.InProgress)
	}

	// But nothing NEW can be assigned to it.
	rec := httptest.NewRecorder()
	testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": "should be refused", "status": "in_use_a",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 assigning an archived status to a new issue, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateIssueStatusValidation covers the reserved-key and category rules.
func TestCreateIssueStatusValidation(t *testing.T) {
	seedTestCatalog(t)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"reserved built-in key", map[string]any{"name": "Mine", "key": "in_review", "category": "in_review", "color": "#123456"}, http.StatusBadRequest},
		{"name slugifying onto a built-in", map[string]any{"name": "In Review", "category": "in_review", "color": "#123456"}, http.StatusBadRequest},
		{"unknown category", map[string]any{"name": "Weird", "category": "started", "color": "#123456"}, http.StatusBadRequest},
		{"missing category", map[string]any{"name": "Weird2", "color": "#123456"}, http.StatusBadRequest},
		{"bad color", map[string]any{"name": "Weird3", "category": "todo", "color": "red"}, http.StatusBadRequest},
		{"empty name", map[string]any{"name": "  ", "category": "todo", "color": "#123456"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testHandler.CreateIssueStatus(rec, newRequest(http.MethodPost, "/api/issue-statuses", tc.body))
			if rec.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestCreateIssueStatusIsNotGatedOnRollout pins the removal of the rollout gate
// (MUL-6643). Creation used to 403 unless a feature flag was flipped on, because
// the first custom status minted a value older pods could not interpret. Every
// pod resolves categories now, so an unset flag must not be able to take the
// feature away again — creation answers to workspace role alone.
func TestCreateIssueStatusIsNotGatedOnRollout(t *testing.T) {
	seedTestCatalog(t)

	var created IssueStatusResponse
	testutil.Call(t, testHandler.CreateIssueStatus,
		newRequest(http.MethodPost, "/api/issue-statuses", map[string]any{
			"name": "Human Review", "category": issuestatus.InReview, "color": "#123456",
		})).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue_status WHERE id = $1`, parseUUID(created.ID))
}

// TestIssueWriteStoresCanonicalStatusKey guards the 500 found in review:
// resolution is case- and whitespace-insensitive, so writing the caller's raw
// string back would store a value the column's format constraint rejects.
func TestIssueWriteStoresCanonicalStatusKey(t *testing.T) {
	createTestCustomStatus(t, "human_review_c", issuestatus.InReview)

	for _, input := range []string{"HUMAN_REVIEW_C", "  human_review_c  ", "Human_Review_C"} {
		t.Run(input, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
				"title": "canonical key " + input, "status": input,
			}))
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201 for %q, got %d: %s", input, rec.Code, rec.Body.String())
			}
			var created struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			json.Unmarshal(rec.Body.Bytes(), &created)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parseUUID(created.ID))
			})
			if created.Status != "human_review_c" {
				t.Errorf("stored status = %q, want the canonical key %q", created.Status, "human_review_c")
			}
			// The stored value must satisfy the column's format constraint; a
			// non-canonical write would have failed as a 500 above, but assert
			// the persisted row too so the guarantee is explicit.
			var persisted string
			if err := testPool.QueryRow(context.Background(),
				`SELECT status FROM issue WHERE id = $1`, parseUUID(created.ID)).Scan(&persisted); err != nil {
				t.Fatalf("read back: %v", err)
			}
			if persisted != "human_review_c" {
				t.Errorf("persisted status = %q, want %q", persisted, "human_review_c")
			}
		})
	}
}

// TestCustomTerminalStatusCountsAsTerminalInSQL covers the SQL-side consumers
// the Go resolver cannot reach. Before issue_effective_status existed, each of
// these read the status literal, so a custom status in the `done` category
// still counted as open for the duplicate guard and was missed by sub-issue and
// project completion counts.
func TestCustomTerminalStatusCountsAsTerminalInSQL(t *testing.T) {
	ctx := context.Background()
	createTestCustomStatus(t, "gate_approved_s", issuestatus.Done)

	mkIssue := func(title, status string) pgtype.UUID {
		t.Helper()
		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": title, "status": status,
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %q: %d %s", title, rec.Code, rec.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		json.Unmarshal(rec.Body.Bytes(), &created)
		id := parseUUID(created.ID)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
		return id
	}

	t.Run("duplicate guard treats it as closed", func(t *testing.T) {
		title := "sql terminal duplicate probe"
		mkIssue(title, "gate_approved_s")
		if _, err := testHandler.Queries.FindActiveDuplicateIssue(ctx, db.FindActiveDuplicateIssueParams{
			WorkspaceID:     parseUUID(testWorkspaceID),
			NormalizedTitle: title,
		}); err == nil {
			t.Error("an issue on a custom done status must not count as an active duplicate")
		}
	})

	t.Run("project completion counts it as done", func(t *testing.T) {
		var projectID pgtype.UUID
		if err := testPool.QueryRow(ctx,
			`INSERT INTO project (workspace_id, title) VALUES ($1, 'Status SQL Probe') RETURNING id`,
			parseUUID(testWorkspaceID)).Scan(&projectID); err != nil {
			t.Fatalf("create project fixture: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

		for _, id := range []pgtype.UUID{
			mkIssue("sql project custom done", "gate_approved_s"),
			mkIssue("sql project open", "todo"),
		} {
			if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, id); err != nil {
				t.Fatalf("attach to project: %v", err)
			}
		}

		stats, err := testHandler.Queries.GetProjectIssueStats(ctx, []pgtype.UUID{projectID})
		if err != nil {
			t.Fatalf("GetProjectIssueStats: %v", err)
		}
		if len(stats) != 1 {
			t.Fatalf("expected stats for one project, got %d", len(stats))
		}
		if stats[0].TotalCount != 2 || stats[0].DoneCount != 1 {
			t.Errorf("project stats = %d done / %d total, want 1/2 (the custom done status must count)",
				stats[0].DoneCount, stats[0].TotalCount)
		}
	})

	t.Run("sub-issue progress counts it as done", func(t *testing.T) {
		parent := mkIssue("sql child progress parent", "in_progress")
		for _, id := range []pgtype.UUID{
			mkIssue("sql child custom done", "gate_approved_s"),
			mkIssue("sql child open", "todo"),
		} {
			if _, err := testPool.Exec(ctx, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parent, id); err != nil {
				t.Fatalf("attach child: %v", err)
			}
		}

		rows, err := testHandler.Queries.ChildIssueProgress(ctx, parseUUID(testWorkspaceID))
		if err != nil {
			t.Fatalf("ChildIssueProgress: %v", err)
		}
		for _, row := range rows {
			if row.ParentIssueID == parent {
				if row.Total != 2 || row.Done != 1 {
					t.Errorf("child progress = %d done / %d total, want 1/2 (the custom done status must count)",
						row.Done, row.Total)
				}
				return
			}
		}
		t.Error("no progress row for the parent issue")
	})
}

// archiveStatusVia runs the real archive endpoint and returns its status code.
func archiveStatusVia(t *testing.T, entry db.IssueStatus) int {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ArchiveIssueStatus(rec, withURLParam(
		newRequest(http.MethodDelete, "/api/issue-statuses/"+uuidToString(entry.ID), nil),
		"id", uuidToString(entry.ID)))
	return rec.Code
}

// holdExclusiveCatalogLock takes the archive side of the catalog lock and holds
// it until the returned release func runs, so a test can pin one ordering.
func holdExclusiveCatalogLock(t *testing.T) (release func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock holder: %v", err)
	}
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text || ':issue_status', 0))`,
		parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("take exclusive lock: %v", err)
	}
	released := false
	return func() {
		if released {
			return
		}
		released = true
		tx.Rollback(ctx)
	}
}

// TestWritesRejectAnArchivedStatus covers the SEQUENTIAL case: the status is
// already archived before the request arrives, so the pre-flight validation
// alone is enough to refuse it.
//
// This is deliberately NOT the race test — no archive is interleaved with a
// write here, and these cases would pass even without the in-transaction
// re-resolve. The interleaved counterexample is
// TestArchiveCommitsInsideTheWriteRaceWindow below.
func TestWritesRejectAnArchivedStatus(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		key   string
		write func(t *testing.T, key string) int
	}{
		{"create", "race_a_create", func(t *testing.T, key string) int {
			rec := httptest.NewRecorder()
			testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
				"title": "race create " + key, "status": key,
			}))
			return rec.Code
		}},
		{"update", "race_a_update", func(t *testing.T, key string) int {
			seed := mustCreateIssue(t, "race update seed "+key, "todo")
			rec := httptest.NewRecorder()
			testHandler.UpdateIssue(rec, withURLParam(
				newRequest(http.MethodPatch, "/api/issues/"+uuidToString(seed), map[string]any{"status": key}),
				"id", uuidToString(seed)))
			return rec.Code
		}},
		{"update with description merge", "race_a_upd_merge", func(t *testing.T, key string) int {
			seed := mustCreateIssue(t, "race merge seed "+key, "todo")
			rec := httptest.NewRecorder()
			testHandler.UpdateIssue(rec, withURLParam(
				newRequest(http.MethodPatch, "/api/issues/"+uuidToString(seed), map[string]any{
					"status": key, "description": "merged body",
				}),
				"id", uuidToString(seed)))
			return rec.Code
		}},
		{"batch", "race_a_batch", func(t *testing.T, key string) int {
			seed := mustCreateIssue(t, "race batch seed "+key, "todo")
			rec := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(rec, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
				"issue_ids": []string{uuidToString(seed)},
				"updates":   map[string]any{"status": key},
			}))
			return rec.Code
		}},
		{"batch with description merge", "race_a_batch_merge", func(t *testing.T, key string) int {
			seed := mustCreateIssue(t, "race batch merge seed "+key, "todo")
			rec := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(rec, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
				"issue_ids": []string{uuidToString(seed)},
				"updates":   map[string]any{"status": key, "description": "merged body"},
			}))
			return rec.Code
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := tc.key
			entry := createTestCustomStatus(t, key, issuestatus.InProgress)

			if code := archiveStatusVia(t, entry); code != http.StatusOK {
				t.Fatalf("archive setup returned %d", code)
			}

			code := tc.write(t, key)
			if code == http.StatusOK || code == http.StatusCreated {
				t.Fatalf("%s succeeded (%d) against an archived status", tc.name, code)
			}

			// Whatever the code, the invariant is that nothing references the
			// archived status.
			var stranded int
			if err := testPool.QueryRow(ctx,
				`SELECT count(*) FROM issue WHERE workspace_id = $1 AND status = $2`,
				parseUUID(testWorkspaceID), key).Scan(&stranded); err != nil {
				t.Fatalf("count stranded: %v", err)
			}
			if stranded != 0 {
				t.Errorf("%d issue(s) left referencing archived status %q", stranded, key)
			}
		})
	}

}

// TestArchiveSucceedsAfterACommittedWrite is the other ordering: an issue
// committed on the status does not block archiving it, because archiving only
// retires it from future use.
func TestArchiveSucceedsAfterACommittedWrite(t *testing.T) {
	ctx := context.Background()
	t.Run("writer wins: archive still succeeds and leaves the issue alone", func(t *testing.T) {
		entry := createTestCustomStatus(t, "race_b_writer", issuestatus.InProgress)

		rec := httptest.NewRecorder()
		testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
			"title": "race writer wins", "status": "race_b_writer",
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create on custom status: %d %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		json.Unmarshal(rec.Body.Bytes(), &created)
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parseUUID(created.ID)) })

		if code := archiveStatusVia(t, entry); code != http.StatusOK {
			t.Fatalf("archive after a committed write = %d, want 200", code)
		}
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`,
			parseUUID(created.ID)).Scan(&status); err != nil {
			t.Fatalf("read back: %v", err)
		}
		if status != "race_b_writer" {
			t.Errorf("issue status = %q, want it untouched by the archive", status)
		}
	})

	// The writer must genuinely BLOCK on the archive's exclusive lock rather
	// than racing past it — otherwise the interleaved case below would only be
	// closed by luck.
	t.Run("writer blocks while archive holds the exclusive lock", func(t *testing.T) {
		createTestCustomStatus(t, "race_block", issuestatus.InProgress)
		release := holdExclusiveCatalogLock(t)
		defer release()

		done := make(chan int, 1)
		go func() {
			rec := httptest.NewRecorder()
			testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
				"title": "race blocked writer", "status": "race_block",
			}))
			done <- rec.Code
		}()

		select {
		case code := <-done:
			t.Fatalf("write completed (%d) while the archive lock was held", code)
		case <-time.After(400 * time.Millisecond):
			// Blocked as required.
		}

		release()
		select {
		case code := <-done:
			if code != http.StatusCreated {
				t.Fatalf("write after the lock released = %d, want 201", code)
			}
			testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND status = 'race_block'`,
				parseUUID(testWorkspaceID))
		case <-time.After(10 * time.Second):
			t.Fatal("write never completed after the archive lock released")
		}
	})
}

// TestArchiveCommitsInsideTheWriteRaceWindow is the sharp counterexample, run
// against EVERY write endpoint that can land on a custom status.
//
// It interleaves the two operations so the writer's PRE-FLIGHT validation
// observes an active status and the archive commits before the write lands.
// That is the exact hole the first implementation had: the pre-flight Resolve
// ran outside the transaction and was never rechecked, so the writer sailed
// past the released lock and stored an archived key. Verified to fail without
// the in-transaction re-resolve (the issue is left stranded) and pass with it.
//
// Seed issues for the update/batch cases are created BEFORE the exclusive lock
// is taken, so the only thing racing the archive is the status write itself.
func TestArchiveCommitsInsideTheWriteRaceWindow(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		seed  func(t *testing.T, key string) pgtype.UUID
		write func(t *testing.T, key string, seed pgtype.UUID) int
	}{
		{
			name: "create",
			key:  "win_create",
			write: func(t *testing.T, key string, _ pgtype.UUID) int {
				rec := httptest.NewRecorder()
				testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
					"title": "race window create", "status": key,
				}))
				return rec.Code
			},
		},
		{
			name: "update",
			key:  "win_update",
			seed: func(t *testing.T, key string) pgtype.UUID {
				return mustCreateIssue(t, "race window update "+key, "todo")
			},
			write: func(t *testing.T, key string, seed pgtype.UUID) int {
				rec := httptest.NewRecorder()
				testHandler.UpdateIssue(rec, withURLParam(
					newRequest(http.MethodPatch, "/api/issues/"+uuidToString(seed), map[string]any{"status": key}),
					"id", uuidToString(seed)))
				return rec.Code
			},
		},
		{
			name: "update with description merge",
			key:  "win_upd_merge",
			seed: func(t *testing.T, key string) pgtype.UUID {
				return mustCreateIssue(t, "race window merge "+key, "todo")
			},
			write: func(t *testing.T, key string, seed pgtype.UUID) int {
				rec := httptest.NewRecorder()
				testHandler.UpdateIssue(rec, withURLParam(
					newRequest(http.MethodPatch, "/api/issues/"+uuidToString(seed), map[string]any{
						"status": key, "description": "merged body",
					}),
					"id", uuidToString(seed)))
				return rec.Code
			},
		},
		{
			name: "batch",
			key:  "win_batch",
			seed: func(t *testing.T, key string) pgtype.UUID {
				return mustCreateIssue(t, "race window batch "+key, "todo")
			},
			write: func(t *testing.T, key string, seed pgtype.UUID) int {
				rec := httptest.NewRecorder()
				testHandler.BatchUpdateIssues(rec, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
					"issue_ids": []string{uuidToString(seed)},
					"updates":   map[string]any{"status": key},
				}))
				return rec.Code
			},
		},
		{
			name: "batch with description merge",
			key:  "win_batch_merge",
			seed: func(t *testing.T, key string) pgtype.UUID {
				return mustCreateIssue(t, "race window batch merge "+key, "todo")
			},
			write: func(t *testing.T, key string, seed pgtype.UUID) int {
				rec := httptest.NewRecorder()
				testHandler.BatchUpdateIssues(rec, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
					"issue_ids": []string{uuidToString(seed)},
					"updates":   map[string]any{"status": key, "description": "merged body"},
				}))
				return rec.Code
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			entry := createTestCustomStatus(t, tc.key, issuestatus.InProgress)

			var seedID pgtype.UUID
			if tc.seed != nil {
				seedID = tc.seed(t, tc.key)
			}

			// Hold the archive side first, so the writer is guaranteed to park
			// on the lock AFTER its pre-flight validation (which runs outside
			// any transaction) and BEFORE its write.
			tx, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx,
				`SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text || ':issue_status', 0))`,
				parseUUID(testWorkspaceID)); err != nil {
				t.Fatalf("take exclusive lock: %v", err)
			}

			done := make(chan int, 1)
			go func() { done <- tc.write(t, tc.key, seedID) }()

			select {
			case code := <-done:
				t.Fatalf("write completed (%d) before the archive released the lock", code)
			case <-time.After(400 * time.Millisecond):
				// Parked on the lock, as required.
			}

			// Archive inside the held lock and commit: from the writer's point
			// of view the status was active at validation time and archived by
			// write time.
			if _, err := tx.Exec(ctx,
				`UPDATE issue_status SET archived_at = now() WHERE id = $1`, entry.ID); err != nil {
				t.Fatalf("archive inside the window: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatalf("commit archive: %v", err)
			}

			select {
			case code := <-done:
				if code == http.StatusOK || code == http.StatusCreated {
					t.Errorf("%s succeeded (%d) against a status archived inside the race window", tc.name, code)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("write never completed after the archive committed")
			}

			var stranded int
			if err := testPool.QueryRow(ctx,
				`SELECT count(*) FROM issue WHERE workspace_id = $1 AND status = $2`,
				parseUUID(testWorkspaceID), tc.key).Scan(&stranded); err != nil {
				t.Fatalf("count stranded: %v", err)
			}
			if stranded != 0 {
				t.Errorf("%d issue(s) stranded on a status archived inside the race window", stranded)
			}
		})
	}
}

// mustCreateIssue creates an issue through the real endpoint and returns its id.
func mustCreateIssue(t *testing.T, title, status string) pgtype.UUID {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": title, "status": status,
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed issue %q: %d %s", title, rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	id := parseUUID(created.ID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

// TestBatchCustomTerminalStatusEntersStageBarrier is the regression guard for
// the batch stage-barrier prefilter. It compared status literals, so a batch
// that moved the last child onto a CUSTOM done status left childDoneCompleted
// empty and skipped the parent notification entirely — the already-fixed
// barrier logic downstream never ran.
func TestBatchCustomTerminalStatusEntersStageBarrier(t *testing.T) {
	ctx := context.Background()
	createTestCustomStatus(t, "batch_done_s", issuestatus.Done)

	parent := mustCreateIssue(t, "batch barrier parent", "in_progress")
	child := mustCreateIssue(t, "batch barrier child", "todo")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parent, child); err != nil {
		t.Fatalf("attach child: %v", err)
	}

	rec := httptest.NewRecorder()
	testHandler.BatchUpdateIssues(rec, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{uuidToString(child)},
		"updates":   map[string]any{"status": "batch_done_s"},
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("batch update: %d %s", rec.Code, rec.Body.String())
	}

	// The observable effect of entering the barrier is the system comment on
	// the parent. Without the fix the batch is silent.
	var comments int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM comment WHERE issue_id = $1`, parent).Scan(&comments); err != nil {
		t.Fatalf("count parent comments: %v", err)
	}
	if comments == 0 {
		t.Error("moving the last child to a custom done status did not close the stage barrier")
	}
}

// TestChildrenResponseCarriesStatusCategory pins the field the CLI's
// `issue children` stage progress reads. Without it the CLI counts only literal
// done/cancelled and reports wrong progress to an agent.
func TestChildrenResponseCarriesStatusCategory(t *testing.T) {
	ctx := context.Background()
	createTestCustomStatus(t, "children_done_s", issuestatus.Done)

	parent := mustCreateIssue(t, "children category parent", "in_progress")
	child := mustCreateIssue(t, "children category child", "children_done_s")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parent, child); err != nil {
		t.Fatalf("attach child: %v", err)
	}

	rec := httptest.NewRecorder()
	testHandler.ListChildIssues(rec, withURLParam(
		newRequest(http.MethodGet, "/api/issues/"+uuidToString(parent)+"/children", nil),
		"id", uuidToString(parent)))
	if rec.Code != http.StatusOK {
		t.Fatalf("list children: %d %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Issues []struct {
			Status         string `json:"status"`
			StatusCategory string `json:"status_category"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Issues) != 1 {
		t.Fatalf("expected 1 child, got %d: %s", len(payload.Issues), rec.Body.String())
	}
	if payload.Issues[0].Status != "children_done_s" {
		t.Errorf("status = %q, want the custom key", payload.Issues[0].Status)
	}
	if payload.Issues[0].StatusCategory != "done" {
		t.Errorf("status_category = %q, want %q", payload.Issues[0].StatusCategory, "done")
	}
}

// TestCategoryFilterExpandsToIndexedStatusKeys is the regression guard for the
// performance fix: filtering by category must expand to concrete status keys so
// `status = ANY(...)` can use the (workspace_id, status) index. Wrapping the
// column in issue_effective_status() made that index unusable and turned a
// two-page index read into a full workspace scan.
func TestCategoryFilterExpandsToIndexedStatusKeys(t *testing.T) {
	ctx := context.Background()
	seedTestCatalog(t)
	ws := parseUUID(testWorkspaceID)

	t.Run("built-in category expands to exactly its own key", func(t *testing.T) {
		keys, err := issuestatus.ExpandCategories(ctx, testHandler.Queries, ws, []string{"blocked"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if len(keys) != 1 || keys[0] != "blocked" {
			t.Errorf("expand(blocked) = %v, want exactly [blocked] — a workspace with no "+
				"custom statuses must produce the pre-feature query", keys)
		}
	})

	t.Run("category includes its custom statuses", func(t *testing.T) {
		createTestCustomStatus(t, "human_review_x", issuestatus.InReview)
		keys, err := issuestatus.ExpandCategories(ctx, testHandler.Queries, ws, []string{"in_review"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if !slices.Contains(keys, "in_review") || !slices.Contains(keys, "human_review_x") {
			t.Errorf("expand(in_review) = %v, want both the canonical and the custom key", keys)
		}
	})

	// An issue left on an archived status still belongs in its category's
	// column, so the expansion must keep archived keys.
	t.Run("archived statuses stay in their category", func(t *testing.T) {
		entry := createTestCustomStatus(t, "retired_x", issuestatus.Done)
		if code := archiveStatusVia(t, entry); code != http.StatusOK {
			t.Fatalf("archive: %d", code)
		}
		keys, err := issuestatus.ExpandCategories(ctx, testHandler.Queries, ws, []string{"done"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if !slices.Contains(keys, "retired_x") {
			t.Errorf("expand(done) = %v, want the archived key included so issues on it "+
				"still appear in the Done column", keys)
		}
	})

	// A workspace whose seed has not landed must still filter correctly.
	t.Run("unseeded workspace falls back to the canonical keys", func(t *testing.T) {
		if _, err := testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1`, ws); err != nil {
			t.Fatalf("clear catalog: %v", err)
		}
		t.Cleanup(func() { seedTestCatalog(t) })
		keys, err := issuestatus.ExpandCategories(ctx, testHandler.Queries, ws, []string{"todo", "done"})
		if err != nil {
			t.Fatalf("expand: %v", err)
		}
		if len(keys) != 2 || !slices.Contains(keys, "todo") || !slices.Contains(keys, "done") {
			t.Errorf("expand on an unseeded workspace = %v, want [todo done]", keys)
		}
	})
}

// TestListFilterByCategoryReturnsCustomStatusIssues exercises the real endpoint.
func TestListFilterByCategoryReturnsCustomStatusIssues(t *testing.T) {
	ctx := context.Background()
	createTestCustomStatus(t, "human_review_l", issuestatus.InReview)
	customID := mustCreateIssue(t, "custom in review", "human_review_l")
	builtinID := mustCreateIssue(t, "builtin in review", "in_review")
	otherID := mustCreateIssue(t, "unrelated todo", "todo")
	_ = ctx

	rec := httptest.NewRecorder()
	testHandler.ListIssues(rec, newRequest(http.MethodGet, "/api/issues?status_category=in_review&limit=100", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Issues []struct {
			ID             string `json:"id"`
			Status         string `json:"status"`
			StatusCategory string `json:"status_category"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := map[string]string{}
	for _, i := range payload.Issues {
		got[i.ID] = i.StatusCategory
	}
	if _, ok := got[uuidToString(customID)]; !ok {
		t.Error("the in_review category must include issues on a custom in_review status")
	}
	if _, ok := got[uuidToString(builtinID)]; !ok {
		t.Error("the in_review category must include issues on the built-in status")
	}
	if _, ok := got[uuidToString(otherID)]; ok {
		t.Error("the in_review category must not include a todo issue")
	}
	// Every row carries an authoritative category, custom statuses included —
	// this is what the client buckets and caches by.
	if c := got[uuidToString(customID)]; c != "in_review" {
		t.Errorf("custom-status row status_category = %q, want %q", c, "in_review")
	}
}

// TestCreateEventCarriesCustomStatusCategory is the regression guard for the
// websocket path: filling the category only on the HTTP response is too late
// for other tabs, which receive the broadcast payload. Without it they cannot
// bucket the new issue and must fall back to a full refetch.
func TestCreateEventCarriesCustomStatusCategory(t *testing.T) {
	createTestCustomStatus(t, "human_review_ev", issuestatus.InReview)

	gotEvent := make(chan events.Event, 1)
	testHandler.Bus.Subscribe(protocol.EventIssueCreated, func(e events.Event) {
		select {
		case gotEvent <- e:
		default:
		}
	})

	rec := httptest.NewRecorder()
	testHandler.CreateIssue(rec, newRequest(http.MethodPost, "/api/issues", map[string]any{
		"title": "custom status event", "status": "human_review_ev",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID             string `json:"id"`
		StatusCategory string `json:"status_category"`
	}
	json.Unmarshal(rec.Body.Bytes(), &created)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, parseUUID(created.ID))
	})
	if created.StatusCategory != "in_review" {
		t.Errorf("HTTP response status_category = %q, want in_review", created.StatusCategory)
	}

	select {
	case e := <-gotEvent:
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Fatalf("event payload shape: %T", e.Payload)
		}
		issue, ok := payload["issue"].(IssueResponse)
		if !ok {
			t.Fatalf("event issue shape: %T", payload["issue"])
		}
		if issue.StatusCategory != "in_review" {
			t.Errorf("event status_category = %q, want in_review — other tabs cannot "+
				"bucket the new issue without it", issue.StatusCategory)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no issue:created event")
	}
}

// TestListEndpointsCarryStatusCategory covers the remaining payload exits: every
// row a list or get returns must carry an authoritative category, custom
// statuses included. It deliberately does NOT assert the per-request catalog
// read count — see the note on the resolver subtest below for why.
func TestListEndpointsCarryStatusCategory(t *testing.T) {
	createTestCustomStatus(t, "human_review_n", issuestatus.InReview)
	for i := range 3 {
		mustCreateIssue(t, fmt.Sprintf("n+1 probe %d", i), "human_review_n")
	}

	t.Run("list carries category for every custom row", func(t *testing.T) {
		rec := httptest.NewRecorder()
		testHandler.ListIssues(rec, newRequest(http.MethodGet, "/api/issues?status_category=in_review&limit=100", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		var payload struct {
			Issues []struct {
				Status         string `json:"status"`
				StatusCategory string `json:"status_category"`
			} `json:"issues"`
		}
		json.Unmarshal(rec.Body.Bytes(), &payload)
		seen := 0
		for _, i := range payload.Issues {
			if i.Status == "human_review_n" {
				seen++
				if i.StatusCategory != "in_review" {
					t.Errorf("custom row status_category = %q, want in_review", i.StatusCategory)
				}
			}
		}
		if seen < 3 {
			t.Errorf("expected the 3 custom-status rows in the in_review category, saw %d", seen)
		}
	})

	// NOTE ON COVERAGE: there is deliberately no assertion here that the handler
	// reads the catalog only once per page. pg_stat_user_tables counters lag
	// behind the queries that produced them, so a delta around one request does
	// not reliably distinguish one read from N — a threshold written against it
	// passes even with a per-row filler, which would make this test claim more
	// than it proves. Observing it properly needs a counting Querier injected
	// into the handler, which it does not support today.
	//
	// What IS pinned: the Resolver amortizes reads (below), and every list site
	// constructs its filler OUTSIDE the loop, which is what makes that
	// amortization reach the handler.
	t.Run("the resolver amortizes repeated lookups", func(t *testing.T) {
		r := issuestatus.NewResolver(parseUUID(testWorkspaceID))
		ctx := context.Background()
		for range 10 {
			if got := r.Effective(ctx, testHandler.Queries, "human_review_n"); got != "in_review" {
				t.Fatalf("Effective = %q, want in_review", got)
			}
		}
	})

	t.Run("get carries category", func(t *testing.T) {
		id := mustCreateIssue(t, "get carries category", "human_review_n")
		rec := httptest.NewRecorder()
		testHandler.GetIssue(rec, withURLParam(
			newRequest(http.MethodGet, "/api/issues/"+uuidToString(id), nil), "id", uuidToString(id)))
		if rec.Code != http.StatusOK {
			t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
		}
		var got struct {
			StatusCategory string `json:"status_category"`
		}
		json.Unmarshal(rec.Body.Bytes(), &got)
		if got.StatusCategory != "in_review" {
			t.Errorf("GetIssue status_category = %q, want in_review", got.StatusCategory)
		}
	})
}

// TestBackgroundEventCarriesCustomStatusCategory pins the background-event
// payload. IssueToMap fills a category only for built-ins; the publishers that
// can emit a CUSTOM status go through IssueToMapWithCategory so clients receive
// an authoritative one instead of a blank they would have to refetch to resolve.
func TestBackgroundEventCarriesCustomStatusCategory(t *testing.T) {
	ctx := context.Background()
	createTestCustomStatus(t, "human_review_bg", issuestatus.InReview)
	id := mustCreateIssue(t, "background event category", "human_review_bg")

	issue, err := testHandler.Queries.GetIssue(ctx, id)
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}

	// Plain IssueToMap leaves a custom status uncategorized — that is why the
	// publishers use the WithCategory form.
	plain := service.IssueToMap(issue, "MUL")
	if plain["status_category"] != "" {
		t.Errorf("IssueToMap custom status_category = %v, want empty (no catalog access)",
			plain["status_category"])
	}

	authoritative := service.IssueToMapWithCategory(ctx, testHandler.Queries, issue, "MUL")
	if authoritative["status_category"] != "in_review" {
		t.Errorf("IssueToMapWithCategory status_category = %v, want in_review",
			authoritative["status_category"])
	}
	if authoritative["status"] != "human_review_bg" {
		t.Errorf("status = %v, want the custom key verbatim", authoritative["status"])
	}
}

// TestCatalogWritesAnnounceThemselves pins the realtime contract for the status
// catalog (MUL-6458): every write that changes it publishes exactly one
// workspace-scoped event, and a write that changes nothing publishes none.
//
// One event type covers all four writes because every client answers them the
// same way — re-read the catalog. What the assertions below are really
// protecting is the routing: an event with an empty WorkspaceID is dropped by
// the SubscribeAll forwarder without a word, so the catalog would look
// synchronized in tests and silently never reach another tab.
func TestCatalogWritesAnnounceThemselves(t *testing.T) {
	ctx := context.Background()
	seedTestCatalog(t)

	changes := make(chan events.Event, 8)
	testHandler.Bus.Subscribe(protocol.EventIssueStatusChanged, func(e events.Event) {
		select {
		case changes <- e:
		default:
		}
	})

	expectChange := func(t *testing.T, action string) {
		t.Helper()
		select {
		case e := <-changes:
			if e.WorkspaceID != testWorkspaceID {
				t.Errorf("event workspace_id = %q, want %q — an event without one never "+
					"reaches a client", e.WorkspaceID, testWorkspaceID)
			}
			if e.ActorType != "member" || e.ActorID == "" {
				t.Errorf("event actor = %q/%q, want the acting member", e.ActorType, e.ActorID)
			}
			payload, ok := e.Payload.(map[string]any)
			if !ok {
				t.Fatalf("event payload shape: %T", e.Payload)
			}
			if payload["action"] != action {
				t.Errorf("event action = %v, want %q", payload["action"], action)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("no issue_status:changed event for %s — other tabs keep the stale catalog "+
				"until its 5 minute staleTime expires", action)
		}
	}
	expectNoChange := func(t *testing.T) {
		t.Helper()
		select {
		case e := <-changes:
			t.Fatalf("unexpected issue_status:changed event: %v", e.Payload)
		case <-time.After(300 * time.Millisecond):
		}
	}

	var created IssueStatusResponse
	testutil.Call(t, testHandler.CreateIssueStatus,
		newRequest(http.MethodPost, "/api/issue-statuses", map[string]any{
			"name": "Realtime Gate", "category": issuestatus.InReview, "color": "#123456",
		})).Want(http.StatusCreated).JSON(&created)
	dbfx.Cleanup(t, `DELETE FROM issue_status WHERE id = $1`, parseUUID(created.ID))
	expectChange(t, "created")

	testutil.Call(t, testHandler.UpdateIssueStatus, withURLParam(
		newRequest(http.MethodPatch, "/api/issue-statuses/"+created.ID, map[string]any{"name": "Renamed Gate"}),
		"id", created.ID)).Want(http.StatusOK)
	expectChange(t, "updated")

	// Reorder demands EVERY active custom status in the category, so the
	// payload is read back rather than assumed — a status left over from
	// another test would otherwise turn this into a 409.
	active, err := testHandler.Queries.ListActiveCustomIssueStatusEntries(ctx, db.ListActiveCustomIssueStatusEntriesParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Category:    issuestatus.InReview,
	})
	if err != nil {
		t.Fatalf("list active custom statuses: %v", err)
	}
	ids := make([]string, len(active))
	for i, entry := range active {
		ids[i] = uuidToString(entry.ID)
	}
	if code := reorderVia(t, issuestatus.InReview, ids).Code; code != http.StatusOK {
		t.Fatalf("reorder: %d", code)
	}
	expectChange(t, "reordered")

	entry, err := testHandler.Queries.GetIssueStatusEntryByID(ctx, db.GetIssueStatusEntryByIDParams{
		ID:          parseUUID(created.ID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("reload created status: %v", err)
	}
	if code := archiveStatusVia(t, entry); code != http.StatusOK {
		t.Fatalf("archive: %d", code)
	}
	expectChange(t, "archived")

	// Archiving an already-archived status is a 200 no-op. Announcing it would
	// have every other client re-read a catalog that did not move.
	if code := archiveStatusVia(t, entry); code != http.StatusOK {
		t.Fatalf("second archive: %d", code)
	}
	expectNoChange(t)
}
