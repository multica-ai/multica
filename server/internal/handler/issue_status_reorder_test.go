package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// insertCustomStatus adds one custom status directly, returning its id.
func insertCustomStatus(t *testing.T, key, category string, position int, archived bool) string {
	t.Helper()
	var id string
	category, ok := issuestatus.ParseCategory(category)
	if !ok {
		t.Fatalf("invalid fixture category %q", category)
	}
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

	rec := reorderVia(t, "started", []string{second, first})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status = %d: %s", rec.Code, rec.Body.String())
	}

	positions := positionsByID(t, first, second)
	// Legacy custom-only requests reuse these custom slots (initially 1 and 2).
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

	rec := reorderVia(t, "started", []string{second, archived, first})
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
		{"legacy requests must opt in to built-in ordering", "started", []string{builtInID}, http.StatusForbidden},
		{"ids must belong to the named category", "started", []string{inReview, inTodo}, http.StatusBadRequest},
		{"duplicate ids are rejected", "started", []string{inReview, inReview}, http.StatusBadRequest},
		{"an empty order is rejected", "started", nil, http.StatusBadRequest},
		{"the category must be one of the four", "nope", []string{inReview}, http.StatusBadRequest},
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
// Without the LockIssueStatusCatalog call, the reorder
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
	go func() { done <- reorderVia(t, "started", []string{second, first}).Code }()

	select {
	case code := <-done:
		t.Fatalf("reorder completed (%d) before the archive released the catalog lock", code)
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

	rec := reorderVia(t, "started", []string{second})
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

func TestReorderIssueStatusesIncludesBuiltInsAndPreservesLegacySlots(t *testing.T) {
	ws := dbfx.Workspace(t, "Status ordering", fmt.Sprintf("status-order-%d", time.Now().UnixNano()))
	dbfx.Member(t, ws, testUserID, "owner")
	insert := func(key string, system bool, position int) string {
		return dbfx.Insert(t, "issue_status", testutil.Cols{
			"workspace_id": ws, "key": key, "name": key, "description": "",
			"category": "started", "color": "#123456", "is_system": system, "position": position,
		})
	}
	progress := insert("in_progress", true, 0)
	review := insert("in_review", true, 0)
	qa := insert("qa", false, 1)
	uat := insert("uat", false, 2)
	call := func(ids []string, includeSystem bool, want int) {
		req := newRequest(http.MethodPatch, "/api/issue-statuses/reorder", ReorderIssueStatusesRequest{
			Category: "started", IDs: ids, IncludeSystem: includeSystem,
		})
		req.Header.Set("X-Workspace-ID", ws)
		testutil.Call(t, testHandler.ReorderIssueStatuses, req).Want(want)
	}
	order := []string{review, qa, progress, uat}
	call(order, true, http.StatusOK)
	for i, id := range order {
		if got := positionsByID(t, id)[id]; got != i+1 {
			t.Fatalf("position of %s = %d, want %d", id, got, i+1)
		}
	}
	// A fresh catalog read must return the same order as the write.
	entries, err := testHandler.Queries.ListIssueStatusEntries(context.Background(), db.ListIssueStatusEntriesParams{WorkspaceID: parseUUID(ws)})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.Key
	}
	if !reflect.DeepEqual(keys, []string{"in_review", "qa", "in_progress", "uat"}) {
		t.Fatalf("catalog order = %v", keys)
	}
	// Installed clients omit include_system. Their custom-only writes continue
	// to succeed, using the existing slots 2 and 4 rather than moving built-ins.
	call([]string{uat, qa}, false, http.StatusOK)
	want := map[string]int{review: 1, uat: 2, progress: 3, qa: 4}
	if got := positionsByID(t, review, uat, progress, qa); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy reorder = %v, want %v", got, want)
	}
	call([]string{review, qa}, true, http.StatusConflict)
	if got := positionsByID(t, review, uat, progress, qa); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial full-catalog reorder changed positions: %v", got)
	}
	// Reordering is not permission to edit or archive a built-in definition.
	edit := newRequest(http.MethodPatch, "/api/issue-statuses/"+review, UpdateIssueStatusRequest{Name: ptr("Changed")})
	edit.Header.Set("X-Workspace-ID", ws)
	edit = withURLParam(edit, "id", review)
	testutil.Call(t, testHandler.UpdateIssueStatus, edit).Want(http.StatusForbidden)
	archive := withURLParam(newRequest(http.MethodDelete, "/api/issue-statuses/"+review, nil), "id", review)
	archive.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.ArchiveIssueStatus, archive).Want(http.StatusForbidden)

	foreign := dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": testWorkspaceID, "key": "foreign_order", "name": "Foreign",
		"category": "started", "color": "#123456", "position": 1,
	})
	call([]string{review, uat, progress, foreign}, true, http.StatusNotFound)
	archived := dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": ws, "key": "archived_order", "name": "Archived",
		"category": "started", "color": "#123456", "position": 5, "archived_at": time.Now(),
	})
	call([]string{review, uat, progress, qa, archived}, true, http.StatusConflict)
	otherCategory := dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": ws, "key": "other_category", "name": "Other category",
		"category": "done", "color": "#123456", "position": 1,
	})
	call([]string{review, uat, progress, qa, otherCategory}, true, http.StatusBadRequest)
	if got := positionsByID(t, review, uat, progress, qa); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected reorder changed positions: %v", got)
	}

	memberWS := dbfx.Workspace(t, "Member ordering", fmt.Sprintf("member-order-%d", time.Now().UnixNano()))
	dbfx.Member(t, memberWS, testUserID, "member")
	req := newRequest(http.MethodPatch, "/api/issue-statuses/reorder", ReorderIssueStatusesRequest{
		Category: "started", IDs: order, IncludeSystem: true,
	})
	req.Header.Set("X-Workspace-ID", memberWS)
	testutil.Call(t, testHandler.ReorderIssueStatuses, req).Want(http.StatusForbidden)
}
