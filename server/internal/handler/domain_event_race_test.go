package handler

import (
	"context"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/domainevent"
)

// A status-only update that races a concurrent reassignment must not roll the
// assignee back to its pre-tx snapshot (MUL-4332 review point 1). UpdateIssue
// writes the nullable columns as bare narg, so before the fix a status-only
// write whose lock landed AFTER the reassignment committed would overwrite the
// assignee with the stale value it read before the tx — silently undoing the
// reassignment, and emitting no issue.assigned event to signal the reversal.
// After the fix every untouched column is rebuilt from the locked row, so the
// reassignment survives regardless of who wins the lock, and its assigned event
// is always present. Looped over fresh issues to exercise both interleavings.
func TestUpdateIssueConcurrentReassignNotRolledBack(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}
	const iterations = 6
	for i := 0; i < iterations; i++ {
		issueID := createTestIssue(t, fmt.Sprintf("reassign-race %s #%d", t.Name(), i), "todo", "none")
		func() {
			defer func() {
				deleteTestIssue(t, issueID)
				testPool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id = $1`, issueID)
			}()

			var wg sync.WaitGroup
			wg.Add(2)
			// A: status-only update — must NOT touch the assignee.
			go func() {
				defer wg.Done()
				req := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{"status": "in_progress"})
				req = withURLParam(req, "id", issueID)
				testHandler.UpdateIssue(httptest.NewRecorder(), req)
			}()
			// B: reassign to the member — must survive the concurrent status write.
			go func() {
				defer wg.Done()
				req := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
					"assignee_type": "member",
					"assignee_id":   testUserID,
				})
				req = withURLParam(req, "id", issueID)
				testHandler.UpdateIssue(httptest.NewRecorder(), req)
			}()
			wg.Wait()

			var assigneeType, assigneeID string
			if err := testPool.QueryRow(context.Background(),
				`SELECT COALESCE(assignee_type, ''), COALESCE(assignee_id::text, '') FROM issue WHERE id = $1`, issueID).
				Scan(&assigneeType, &assigneeID); err != nil {
				t.Fatalf("iter %d: read issue assignee: %v", i, err)
			}
			if assigneeType != "member" || assigneeID != testUserID {
				t.Fatalf("iter %d: assignee rolled back to (%q, %q), want (member, %s) — a status-only write clobbered the reassignment (review point 1)",
					i, assigneeType, assigneeID, testUserID)
			}

			var sawAssigned bool
			for _, e := range eventsForSubject(t, domainevent.SubjectIssue, issueID) {
				if e.Type == domainevent.TypeIssueAssigned && payloadField(t, e.Payload, "to_assignee_id") == testUserID {
					sawAssigned = true
				}
			}
			if !sawAssigned {
				t.Fatalf("iter %d: missing issue.assigned event for the reassignment", i)
			}
		}()
	}
}

// advanceIssueToDone (the merged-PR close path) must treat an already-done issue
// as a genuine no-op: the locked pre-image shows no transition, so it emits no
// event AND fires no parent child-done comment / realtime status_changed. A
// stale in_progress snapshot from a duplicate webhook delivery must not re-drive
// those side effects (MUL-4332 review point 4).
func TestAdvanceIssueToDoneNoOpSuppressesDuplicate(t *testing.T) {
	if testHandler == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	parentID := createTestIssue(t, "no-op parent "+t.Name(), "in_progress", "none")
	childID := createTestIssue(t, "no-op child "+t.Name(), "in_progress", "none")
	// Link child to parent and complete it directly (bypassing the handler), so
	// no child-done comment has fired yet and the parent starts clean.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET parent_issue_id = $1, status = 'done' WHERE id = $2`, parentID, childID); err != nil {
		t.Fatalf("link + complete child: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id IN ($1, $2)`, parentID, childID)
		testPool.Exec(context.Background(), `DELETE FROM domain_event WHERE subject_id IN ($1, $2)`, parentID, childID)
		deleteTestIssue(t, childID)
		deleteTestIssue(t, parentID)
	})

	childRow, err := testHandler.Queries.GetIssue(ctx, parseUUID(childID))
	if err != nil {
		t.Fatalf("load child: %v", err)
	}
	// Simulate the stale webhook view: the child is already done in the DB, but
	// the caller still holds an in_progress snapshot.
	stale := childRow
	stale.Status = "in_progress"

	before := issueCommentCount(t, parentID)
	testHandler.advanceIssueToDone(ctx, stale, testWorkspaceID)

	if after := issueCommentCount(t, parentID); after != before {
		t.Fatalf("parent comment count changed %d→%d — a no-op transition must not re-post the child-done comment (review point 4)", before, after)
	}
	for _, e := range eventsForSubject(t, domainevent.SubjectIssue, childID) {
		if e.Type == domainevent.TypeIssueStatusChanged {
			t.Fatalf("a no-op advanceIssueToDone must emit no issue.status_changed event, got %+v", e)
		}
	}
}

func issueCommentCount(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	return n
}
