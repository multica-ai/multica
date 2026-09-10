package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
)

// insertCustomStatus adds one custom status directly, returning its id.
func insertCustomStatus(t *testing.T, key, category string, position int, archived bool) string {
	t.Helper()
	var id string
	archivedAt := "NULL"
	if archived {
		archivedAt = "now()"
	}
	err := testPool.QueryRow(context.Background(), fmt.Sprintf(`
		INSERT INTO issue_status (workspace_id, key, name, description, category, color, position, archived_at)
		VALUES ($1, $2, $3, '', $4, '#ff0000', $5, %s)
		RETURNING id
	`, archivedAt), testWorkspaceID, key, key, category, position).Scan(&id)
	if err != nil {
		t.Fatalf("insert custom status %q: %v", key, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_status WHERE id = $1`, id)
	})
	return id
}

func reorderVia(t *testing.T, category string, ids []string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	testHandler.ReorderIssueStatuses(rec, newRequest(http.MethodPatch, "/api/issue-statuses/reorder", ReorderIssueStatusesRequest{
		Category: category,
		IDs:      ids,
	}))
	return rec
}

func positionsByID(t *testing.T, ids ...string) map[string]int {
	t.Helper()
	out := make(map[string]int, len(ids))
	for _, id := range ids {
		var position int
		if err := testPool.QueryRow(context.Background(),
			`SELECT position FROM issue_status WHERE id = $1`, id).Scan(&position); err != nil {
			t.Fatalf("read position for %s: %v", id, err)
		}
		out[id] = position
	}
	return out
}

func TestReorderIssueStatusesWritesIntraCategoryPositionsFromOne(t *testing.T) {
	suffix := time.Now().UnixNano()
	first := insertCustomStatus(t, fmt.Sprintf("qa_a_%d", suffix), "in_review", 1, false)
	second := insertCustomStatus(t, fmt.Sprintf("qa_b_%d", suffix), "in_review", 2, false)

	rec := reorderVia(t, "in_review", []string{second, first})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d: %s", rec.Code, rec.Body.String())
	}

	positions := positionsByID(t, first, second)
	// Positions start at 1: the category's built-in is seeded at 0 and never
	// moves, so it stays at the head of its column.
	if positions[second] != 1 || positions[first] != 2 {
		t.Fatalf("positions = %#v, want second=1 first=2", positions)
	}

	var response struct {
		Statuses []IssueStatusResponse `json:"statuses"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode reorder response: %v", err)
	}
	if len(response.Statuses) == 0 {
		t.Fatal("reorder response carried no catalog")
	}
}

// The regression Elon caught: with "show archived" on, the client used to hand
// the whole rendered order — archived rows included — to a PATCH-per-row loop.
// The archived row rejected AFTER the rows before it had already been written,
// so the user saw a failure toast over a database that had partly reordered.
func TestReorderIssueStatusesRejectsArchivedWithoutPartialWrite(t *testing.T) {
	suffix := time.Now().UnixNano()
	first := insertCustomStatus(t, fmt.Sprintf("qa_c_%d", suffix), "in_review", 1, false)
	second := insertCustomStatus(t, fmt.Sprintf("qa_d_%d", suffix), "in_review", 2, false)
	archived := insertCustomStatus(t, fmt.Sprintf("qa_e_%d", suffix), "in_review", 3, true)

	before := positionsByID(t, first, second, archived)

	rec := reorderVia(t, "in_review", []string{second, archived, first})
	if rec.Code != http.StatusConflict {
		t.Fatalf("reorder status = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	after := positionsByID(t, first, second, archived)
	for id, position := range before {
		if after[id] != position {
			t.Fatalf("position of %s moved from %d to %d despite the rejection: %#v",
				id, position, after[id], after)
		}
	}
}

func TestReorderIssueStatusesRejectsForeignInputs(t *testing.T) {
	suffix := time.Now().UnixNano()
	inReview := insertCustomStatus(t, fmt.Sprintf("qa_f_%d", suffix), "in_review", 1, false)
	inTodo := insertCustomStatus(t, fmt.Sprintf("qa_g_%d", suffix), "todo", 1, false)

	// The built-ins are seeded lazily on the first catalog read.
	if err := issuestatus.Ensure(context.Background(), testHandler.Queries, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("seed built-ins: %v", err)
	}
	var builtInID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM issue_status WHERE workspace_id = $1 AND key = 'in_review' AND is_system`,
		testWorkspaceID).Scan(&builtInID); err != nil {
		t.Fatalf("load built-in: %v", err)
	}

	cases := []struct {
		name     string
		category string
		ids      []string
		want     int
	}{
		{"a built-in cannot be reordered", "in_review", []string{builtInID}, http.StatusForbidden},
		{"ids must belong to the named category", "in_review", []string{inReview, inTodo}, http.StatusBadRequest},
		{"duplicate ids are rejected", "in_review", []string{inReview, inReview}, http.StatusBadRequest},
		{"an empty order is rejected", "in_review", nil, http.StatusBadRequest},
		{"the category must be one of the seven", "nope", []string{inReview}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := reorderVia(t, tc.category, tc.ids); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// TestReorderIssueStatusesSerializesAgainstConcurrentArchive is the sharp
// counterexample for "atomic".
//
// It reproduces the real interleaving rather than a sequential one: the reorder
// must be IN FLIGHT and parked on the catalog lock when the archive commits.
// A test that archives first and then calls reorder proves nothing — the
// active-set check would catch that even with the lock deleted, because the
// archive is already visible by the time the handler reads anything.
//
// Verified by deleting the LockIssueStatusCatalogShared call: the reorder then
// reads the catalog before the archive commits, sees both rows active, and
// commits a reorder that includes a row archived underneath it.
func TestReorderIssueStatusesSerializesAgainstConcurrentArchive(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	first := insertCustomStatus(t, fmt.Sprintf("qa_h_%d", suffix), "in_review", 1, false)
	second := insertCustomStatus(t, fmt.Sprintf("qa_i_%d", suffix), "in_review", 2, false)

	before := positionsByID(t, first, second)

	// Hold the archive side first so the reorder is guaranteed to park on the
	// lock BEFORE it reads the catalog.
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
	go func() { done <- reorderVia(t, "in_review", []string{second, first}).Code }()

	select {
	case code := <-done:
		t.Fatalf("reorder completed (%d) before the archive released the lock — it never took the shared lock", code)
	case <-time.After(400 * time.Millisecond):
		// Parked on the lock, as required.
	}

	// Archive inside the held lock and commit: from the reorder's point of view
	// both rows were active when it was called, and one is archived by the time
	// it can read anything.
	if _, err := tx.Exec(ctx,
		`UPDATE issue_status SET archived_at = now() WHERE id = $1`, second); err != nil {
		t.Fatalf("archive inside the window: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit archive: %v", err)
	}

	select {
	case code := <-done:
		if code != http.StatusConflict {
			t.Fatalf("reorder status = %d, want 409 against a status archived inside the race window", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reorder never completed after the archive committed")
	}

	after := positionsByID(t, first, second)
	for id, position := range before {
		if after[id] != position {
			t.Fatalf("position of %s moved from %d to %d despite the conflict: %#v",
				id, position, after[id], after)
		}
	}
}

// A payload that names only SOME of a category's active custom statuses cannot
// be applied: positions come from the array index, so reordering a subset
// writes positions that collide with the rows left out of it.
func TestReorderIssueStatusesRejectsAPartialSet(t *testing.T) {
	suffix := time.Now().UnixNano()
	first := insertCustomStatus(t, fmt.Sprintf("qa_j_%d", suffix), "in_review", 1, false)
	second := insertCustomStatus(t, fmt.Sprintf("qa_k_%d", suffix), "in_review", 2, false)

	before := positionsByID(t, first, second)

	rec := reorderVia(t, "in_review", []string{second})
	if rec.Code != http.StatusConflict {
		t.Fatalf("reorder status = %d, want 409: %s", rec.Code, rec.Body.String())
	}

	after := positionsByID(t, first, second)
	for id, position := range before {
		if after[id] != position {
			t.Fatalf("position of %s moved from %d to %d on a partial payload: %#v",
				id, position, after[id], after)
		}
	}
	// Specifically: `second` must NOT have been written to position 1, which is
	// what would have collided with `first`.
	if after[second] != before[second] {
		t.Fatalf("partial payload still rewrote a position: %#v", after)
	}
}
