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
