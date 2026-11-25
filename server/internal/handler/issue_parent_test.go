package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// issueChain inserts n issues where each one is the parent of the next and
// returns them root first.
func issueChain(t *testing.T, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		cols := testutil.Cols{}
		if i > 0 {
			cols["parent_issue_id"] = ids[i-1]
		}
		ids[i] = dbfx.Issue(t, fmt.Sprintf("parent chain %d", i), cols)
	}
	return ids
}

func issueParentOf(t *testing.T, issueID string) string {
	t.Helper()
	var parent *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT parent_issue_id::text FROM issue WHERE id = $1`, issueID,
	).Scan(&parent); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	if parent == nil {
		return ""
	}
	return *parent
}

func putIssueParent(issueID, parentID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID, map[string]any{
		"parent_issue_id": parentID,
	}), "id", issueID))
	return w
}

// The cycle check must see the whole ancestor chain, not only the nearest
// few parents: a loop through a deep chain makes recursive subscriber
// queries over the tree run forever.
func TestUpdateIssueRejectsParentCycleThroughDeepChain(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	chain := issueChain(t, 12)
	root, leaf := chain[0], chain[len(chain)-1]

	w := putIssueParent(root, leaf)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("set root parent to its 11th-level descendant = %d %s, want 400", w.Code, w.Body.String())
	}
	if got := issueParentOf(t, root); got != "" {
		t.Fatalf("root parent = %s, want none", got)
	}
}

func TestBatchUpdateSkipsParentCycleThroughDeepChain(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	chain := issueChain(t, 12)
	root, leaf := chain[0], chain[len(chain)-1]

	code, resp, raw := batchUpdate(t, map[string]any{
		"issue_ids": []string{root},
		"updates":   map[string]any{"parent_issue_id": leaf},
	})
	if code != http.StatusOK {
		t.Fatalf("batch = %d %s, want 200", code, raw)
	}
	if got := resultFor(t, resp, root); got.Outcome != batchOutcomeSkipped || got.Code != "parent_cycle" {
		t.Fatalf("root result = %+v, want skipped/parent_cycle", got)
	}
	if got := issueParentOf(t, root); got != "" {
		t.Fatalf("root parent = %s, want none", got)
	}
}

// A -> B and B -> A land at once. Each passes the cycle check against the
// committed tree, so both used to commit and leave the two issues as each
// other's parent. The competing write here holds the workspace's parent lock
// the way a concurrent parent change does; the request has to validate
// against what it wrote.
func TestUpdateIssueParentRacingReverseParentDoesNotCycle(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	a := dbfx.Issue(t, "parent race A")
	b := dbfx.Issue(t, "parent race B")

	other, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin competing write: %v", err)
	}
	defer other.Rollback(ctx)
	var otherPID int
	if err := other.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&otherPID); err != nil {
		t.Fatalf("competing pid: %v", err)
	}
	if _, err := other.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::uuid::text || ':issue_parent', 0))`, testWorkspaceID,
	); err != nil {
		t.Fatalf("competing parent lock: %v", err)
	}
	if _, err := other.Exec(ctx, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, a, b); err != nil {
		t.Fatalf("competing B -> A: %v", err)
	}

	var w *httptest.ResponseRecorder
	done := make(chan struct{})
	go func() {
		defer close(done)
		w = putIssueParent(a, b)
	}()
	// Before the fix the request never waits, so it may finish first.
	waitUntilDoneOrBlockedBy(t, otherPID, done)
	if err := other.Commit(ctx); err != nil {
		t.Fatalf("commit competing write: %v", err)
	}
	<-done

	if issueParentOf(t, a) == b && issueParentOf(t, b) == a {
		t.Fatalf("A and B are each other's parent (request returned %d %s)", w.Code, w.Body.String())
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("A -> B after B -> A = %d %s, want 400", w.Code, w.Body.String())
	}
}

// Both directions through the handler at once: one wins, the other is told
// it would make a cycle. Neither may fail on a deadlock between the parent
// lock and the parent foreign key's lock on the other issue's row.
func TestUpdateIssueReverseParentsAtOnceOneWins(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for round := 0; round < 20; round++ {
		a := dbfx.Issue(t, fmt.Sprintf("parent pair %d A", round))
		b := dbfx.Issue(t, fmt.Sprintf("parent pair %d B", round))
		var wa, wb *httptest.ResponseRecorder
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; wa = putIssueParent(a, b) }()
		go func() { defer wg.Done(); <-start; wb = putIssueParent(b, a) }()
		close(start)
		wg.Wait()

		codes := []int{wa.Code, wb.Code}
		sort.Ints(codes)
		if codes[0] != http.StatusOK || codes[1] != http.StatusBadRequest {
			t.Fatalf("round %d: A -> B = %d %s, B -> A = %d %s; want one 200 and one 400",
				round, wa.Code, wa.Body.String(), wb.Code, wb.Body.String())
		}
	}
}

func TestUpdateIssueLocksParentTreeBeforeDuplicateRows(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issue := dbfx.Issue(t, "issue to mark and move")
	original := dbfx.Issue(t, "duplicate target")
	parent := dbfx.Issue(t, "new parent")
	req := updateIssueRequest(issue, map[string]any{
		"duplicate_of_issue_id": original,
		"parent_issue_id":       parent,
	})
	ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin parent lock holder: %v", err)
	}
	defer holder.Rollback(context.Background())
	if err := db.New(holder).LockIssueParentTree(ctx, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("hold parent tree lock: %v", err)
	}
	holderPID := holderBackendPID(t, ctx, holder)

	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		testHandler.UpdateIssue(w, req.WithContext(ctx))
	}()
	defer func() {
		cancel()
		<-done
	}()
	waitUntilDoneOrBlockedBy(t, holderPID, done)
	select {
	case <-done:
		t.Fatalf("update finished while the parent tree was locked: %d %s", w.Code, w.Body.String())
	default:
	}

	// Waiting for the parent tree must leave both rows available to foreign-key checks.
	for _, id := range []string{issue, original} {
		if _, err := testPool.Exec(ctx, `SELECT id FROM issue WHERE id = $1 FOR KEY SHARE NOWAIT`, id); err != nil {
			t.Fatalf("issue %s was already locked while the update waited for the parent tree: %v", id, err)
		}
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("release parent tree lock: %v", err)
	}
	<-done
	if w.Code != http.StatusOK {
		t.Fatalf("mark and move issue = %d %s, want 200", w.Code, w.Body.String())
	}
	status, duplicateOf, _ := duplicateState(t, issue)
	if status != "cancelled" || duplicateOf == nil || *duplicateOf != original {
		t.Fatalf("mark and move stored (status, duplicate_of) = (%q, %v), want (cancelled, %s)", status, duplicateOf, original)
	}
	if got := issueParentOf(t, issue); got != parent {
		t.Fatalf("mark and move stored parent = %s, want %s", got, parent)
	}
}

// Single and batch parent changes wait for the parent tree before they lock
// the issue row, the order every other parent write takes them in.
func TestParentChangeLocksParentTreeBeforeIssueRow(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	writes := map[string]func(issue, parent string) *httptest.ResponseRecorder{
		"single": putIssueParent,
		"batch": func(issue, parent string) *httptest.ResponseRecorder {
			w := httptest.NewRecorder()
			testHandler.BatchUpdateIssues(w, newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
				"issue_ids": []string{issue},
				"updates":   map[string]any{"parent_issue_id": parent},
			}))
			return w
		},
	}
	for name, write := range writes {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			issue := dbfx.Issue(t, name+" parent change")
			parent := dbfx.Issue(t, name+" new parent")

			holder, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin parent lock holder: %v", err)
			}
			defer holder.Rollback(ctx)
			if err := db.New(holder).LockIssueParentTree(ctx, parseUUID(testWorkspaceID)); err != nil {
				t.Fatalf("hold parent tree lock: %v", err)
			}
			holderPID := holderBackendPID(t, ctx, holder)

			var w *httptest.ResponseRecorder
			done := make(chan struct{})
			go func() {
				defer close(done)
				w = write(issue, parent)
			}()
			waitUntilDoneOrBlockedBy(t, holderPID, done)
			select {
			case <-done:
				t.Fatalf("parent change finished while the parent tree was locked: %d %s", w.Code, w.Body.String())
			default:
			}
			if _, err := testPool.Exec(ctx, `SELECT id FROM issue WHERE id = $1 FOR KEY SHARE NOWAIT`, issue); err != nil {
				t.Errorf("issue row was locked while the parent change waited for the parent tree: %v", err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatalf("release parent tree lock: %v", err)
			}
			<-done
			if w.Code != http.StatusOK {
				t.Fatalf("parent change = %d %s, want 200", w.Code, w.Body.String())
			}
			if got := issueParentOf(t, issue); got != parent {
				t.Fatalf("parent = %s, want %s", got, parent)
			}
		})
	}
}

func waitUntilDoneOrBlockedBy(t *testing.T, holderPID int, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return
		default:
		}
		var blocked bool
		if err := testPool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE datname = current_database()
				  AND $1 = ANY(pg_blocking_pids(pid))
			)
		`, holderPID).Scan(&blocked); err != nil {
			t.Fatalf("observe blocked request: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("request neither finished nor queued behind the competing write")
}
