package handler

import (
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Enable step for duplicate marks (MUL-7349). The column and the code that
// keeps it correct shipped in the previous release; these tests cover creating
// a mark and reading both sides of it. How a mark is maintained across writes
// lives in issue_duplicate_maintenance_test.go.

type duplicateRelations struct {
	DuplicateOf *IssueResponse  `json:"duplicate_of"`
	Duplicates  []IssueResponse `json:"duplicates"`
}

func markDuplicate(t *testing.T, issueID, targetID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateIssue, updateIssueRequest(issueID, map[string]any{
		"duplicate_of_issue_id": targetID,
	}))
}

func listDuplicates(t *testing.T, issueID string) duplicateRelations {
	t.Helper()
	var out duplicateRelations
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/duplicates", nil), "id", issueID)
	testutil.Call(t, testHandler.ListIssueDuplicates, req).Want(http.StatusOK).JSON(&out)
	return out
}

func TestMarkDuplicateCancelsAndLinksBothSides(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-original", testutil.Cols{"status": "in_progress"})
	duplicate := dbfx.Issue(t, "dup-duplicate", testutil.Cols{"status": "todo"})

	// The mark is a status write: it must emit the same status_changed event a
	// manual cancel does, or parent stage barriers never hear about it.
	var mu sync.Mutex
	var payloads []map[string]any
	testHandler.Bus.Subscribe(protocol.EventIssueUpdated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		if resp, ok := payload["issue"].(IssueResponse); ok && resp.ID == duplicate {
			mu.Lock()
			payloads = append(payloads, payload)
			mu.Unlock()
		}
	})

	var resp IssueResponse
	markDuplicate(t, duplicate, original).Want(http.StatusOK).JSON(&resp)
	if resp.Status != "cancelled" {
		t.Fatalf("marked issue status = %q, want cancelled", resp.Status)
	}
	status, pointer, _ := duplicateState(t, duplicate)
	if status != "cancelled" || pointer == nil || *pointer != original {
		t.Fatalf("stored (status, duplicate_of) = (%q, %v), want (cancelled, %s)", status, pointer, original)
	}

	mu.Lock()
	if len(payloads) != 1 {
		mu.Unlock()
		t.Fatalf("got %d issue:updated events for the marked issue, want 1", len(payloads))
	}
	event := payloads[0]
	mu.Unlock()
	if event["status_changed"] != true {
		t.Errorf("status_changed = %v, want true", event["status_changed"])
	}
	if got, _ := event["duplicate_of_issue_id"].(*string); got == nil || *got != original {
		t.Errorf("event duplicate_of_issue_id = %v, want %s", event["duplicate_of_issue_id"], original)
	}

	fromDuplicate := listDuplicates(t, duplicate)
	if fromDuplicate.DuplicateOf == nil || fromDuplicate.DuplicateOf.ID != original {
		t.Fatalf("duplicate's relations: duplicate_of = %+v, want %s", fromDuplicate.DuplicateOf, original)
	}
	fromOriginal := listDuplicates(t, original)
	if fromOriginal.DuplicateOf != nil {
		t.Fatalf("original's relations: duplicate_of = %+v, want nil", fromOriginal.DuplicateOf)
	}
	if len(fromOriginal.Duplicates) != 1 || fromOriginal.Duplicates[0].ID != duplicate {
		t.Fatalf("original's relations: duplicates = %+v, want [%s]", fromOriginal.Duplicates, duplicate)
	}
}

// Reopening is the unmark action the banner offers, so it must clear both
// sides of what the mark created.
func TestReopeningRemovesMarkFromBothSides(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-unmark-original")
	duplicate := dbfx.Issue(t, "dup-unmark-duplicate")
	markDuplicate(t, duplicate, original).Want(http.StatusOK)

	testutil.Call(t, testHandler.UpdateIssue, updateIssueRequest(duplicate, map[string]any{
		"status": "todo",
	})).Want(http.StatusOK)

	if relations := listDuplicates(t, original); len(relations.Duplicates) != 0 {
		t.Fatalf("original still lists %d duplicates after the mark was removed", len(relations.Duplicates))
	}
	if relations := listDuplicates(t, duplicate); relations.DuplicateOf != nil {
		t.Fatalf("reopened issue still reports an original: %+v", relations.DuplicateOf)
	}
}

func TestRemarkingSameTargetIsNoop(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-noop-original")
	duplicate := dbfx.Issue(t, "dup-noop-duplicate")
	markDuplicate(t, duplicate, original).Want(http.StatusOK)
	_, _, before := duplicateState(t, duplicate)

	markDuplicate(t, duplicate, original).Want(http.StatusOK)
	if _, _, after := duplicateState(t, duplicate); after != before {
		t.Fatalf("re-marking the same target bumped revision %d -> %d", before, after)
	}
}

func TestDuplicateMarkRejections(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "dup-reject-original")
	marked := dbfx.Issue(t, "dup-reject-marked")
	markDuplicate(t, marked, original).Want(http.StatusOK)
	other := dbfx.Issue(t, "dup-reject-other")

	t.Run("self", func(t *testing.T) {
		markDuplicate(t, other, other).Want(http.StatusBadRequest)
	})
	t.Run("target not in workspace", func(t *testing.T) {
		markDuplicate(t, other, "00000000-0000-0000-0000-000000000001").Want(http.StatusBadRequest)
	})
	t.Run("target is itself a duplicate", func(t *testing.T) {
		body := markDuplicate(t, other, marked).Want(http.StatusConflict).Map()
		if body["code"] != "duplicate_target_is_duplicate" {
			t.Fatalf("code = %v, want duplicate_target_is_duplicate", body["code"])
		}
	})
	t.Run("issue has duplicates", func(t *testing.T) {
		body := markDuplicate(t, original, other).Want(http.StatusConflict).Map()
		if body["code"] != "issue_has_duplicates" {
			t.Fatalf("code = %v, want issue_has_duplicates", body["code"])
		}
	})
	t.Run("explicit null", func(t *testing.T) {
		testutil.Call(t, testHandler.UpdateIssue, updateIssueRequest(marked, map[string]any{
			"duplicate_of_issue_id": nil,
		})).Want(http.StatusBadRequest)
	})
	t.Run("status other than cancelled", func(t *testing.T) {
		testutil.Call(t, testHandler.UpdateIssue, updateIssueRequest(other, map[string]any{
			"duplicate_of_issue_id": original,
			"status":                "done",
		})).Want(http.StatusBadRequest)
	})
	t.Run("batch update", func(t *testing.T) {
		req := newRequest("POST", "/api/issues/batch-update", map[string]any{
			"issue_ids": []string{other},
			"updates":   map[string]any{"duplicate_of_issue_id": original},
		})
		testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusBadRequest)
	})

	// None of the rejected writes may have touched the issue.
	if status, pointer, _ := duplicateState(t, other); status != "todo" || pointer != nil {
		t.Fatalf("rejected marks changed the issue: (status, duplicate_of) = (%q, %v)", status, pointer)
	}
}

// Two people marking A as a duplicate of B and B as a duplicate of A at the
// same moment must not leave a cycle. Both marks lock the pair in id order, so
// whichever runs second sees the first one's pointer and is refused.
func TestConcurrentCrossMarksLeaveNoCycle(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for attempt := 0; attempt < 5; attempt++ {
		a := dbfx.Issue(t, "dup-race-a")
		b := dbfx.Issue(t, "dup-race-b")

		var start, done sync.WaitGroup
		start.Add(1)
		codes := make([]int, 2)
		for i, pair := range [][2]string{{a, b}, {b, a}} {
			done.Add(1)
			go func(i int, issueID, targetID string) {
				defer done.Done()
				start.Wait()
				codes[i] = markDuplicate(t, issueID, targetID).Code
			}(i, pair[0], pair[1])
		}
		start.Done()
		done.Wait()

		succeeded := 0
		for _, code := range codes {
			switch code {
			case http.StatusOK:
				succeeded++
			case http.StatusConflict:
			default:
				t.Fatalf("attempt %d: unexpected status %d (codes %v)", attempt, code, codes)
			}
		}
		if succeeded != 1 {
			t.Fatalf("attempt %d: %d marks succeeded, want exactly 1 (codes %v)", attempt, succeeded, codes)
		}
		_, aPointer, _ := duplicateState(t, a)
		_, bPointer, _ := duplicateState(t, b)
		if aPointer != nil && bPointer != nil {
			t.Fatalf("attempt %d: cycle left behind: %s -> %s and %s -> %s", attempt, a, *aPointer, b, *bPointer)
		}
	}
}

// Defence in depth for a rollback that goes back past the release which added
// the column: such a server reopens duplicates and deletes originals without
// touching the pointer, so reads apply the validity rule themselves.
func TestDuplicateReadsIgnoreLeftoverMarks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Run("reopened without clearing", func(t *testing.T) {
		original := dbfx.Issue(t, "dup-leftover-reopen-original")
		duplicate := dbfx.Issue(t, "dup-leftover-reopen-duplicate")
		other := dbfx.Issue(t, "dup-leftover-reopen-other")
		markDuplicate(t, duplicate, original).Want(http.StatusOK)

		dbfx.Exec(t, `UPDATE issue SET status = 'todo' WHERE id = $1`, duplicate)

		if relations := listDuplicates(t, original); len(relations.Duplicates) != 0 {
			t.Fatalf("original lists a reopened issue: %+v", relations.Duplicates)
		}
		if relations := listDuplicates(t, duplicate); relations.DuplicateOf != nil {
			t.Fatalf("reopened issue reports an original: %+v", relations.DuplicateOf)
		}
		// The leftover does not make it a duplicate, so it can be an original.
		markDuplicate(t, other, duplicate).Want(http.StatusOK)
	})

	t.Run("original deleted without clearing", func(t *testing.T) {
		original := dbfx.Issue(t, "dup-leftover-delete-original")
		duplicate := dbfx.Issue(t, "dup-leftover-delete-duplicate")
		other := dbfx.Issue(t, "dup-leftover-delete-other")
		markDuplicate(t, duplicate, original).Want(http.StatusOK)

		dbfx.Exec(t, `DELETE FROM issue WHERE id = $1`, original)

		if relations := listDuplicates(t, duplicate); relations.DuplicateOf != nil {
			t.Fatalf("issue reports a deleted original: %+v", relations.DuplicateOf)
		}
		markDuplicate(t, other, duplicate).Want(http.StatusOK)
	})
}

// Responses carry the original as a validated summary (MUL-7349). An older
// server can delete an original without clearing the pointers of its
// duplicates, so detail, list and search must all check the original exists
// rather than expose the bare pointer.
func TestDuplicateOfSummaryFollowsTheOriginal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	original := dbfx.Issue(t, "summary-original", testutil.Cols{"status": "in_progress"})
	duplicate := dbfx.Issue(t, "summary-duplicate-needle", testutil.Cols{"status": "todo"})
	markDuplicate(t, duplicate, original).Want(http.StatusOK)

	type page struct {
		Issues []IssueResponse `json:"issues"`
	}
	find := func(issues []IssueResponse) *IssueResponse {
		for i := range issues {
			if issues[i].ID == duplicate {
				return &issues[i]
			}
		}
		return nil
	}
	read := func() (detail IssueResponse, listed, searched *IssueResponse) {
		t.Helper()
		testutil.Call(t, testHandler.GetIssue, withURLParam(newRequest("GET", "/api/issues/"+duplicate, nil), "id", duplicate)).
			Want(http.StatusOK).JSON(&detail)
		var list, search page
		testutil.Call(t, testHandler.ListIssues, newRequest("GET", "/api/issues?status=cancelled&limit=200", nil)).
			Want(http.StatusOK).JSON(&list)
		testutil.Call(t, testHandler.SearchIssues, newRequest("GET", "/api/issues/search?q=summary-duplicate-needle&include_closed=true", nil)).
			Want(http.StatusOK).JSON(&search)
		return detail, find(list.Issues), find(search.Issues)
	}

	var originalDetail IssueResponse
	testutil.Call(t, testHandler.GetIssue, withURLParam(newRequest("GET", "/api/issues/"+original, nil), "id", original)).
		Want(http.StatusOK).JSON(&originalDetail)

	detail, listed, searched := read()
	for name, resp := range map[string]*IssueResponse{"detail": &detail, "list": listed, "search": searched} {
		if resp == nil {
			t.Fatalf("%s: duplicate missing from response", name)
		}
		want := IssueRefResponse{ID: original, Identifier: originalDetail.Identifier, Title: "summary-original", Status: "in_progress"}
		if resp.DuplicateOf == nil || *resp.DuplicateOf != want {
			t.Fatalf("%s: duplicate_of = %+v, want %+v", name, resp.DuplicateOf, want)
		}
	}

	// An older server deleting the original leaves the pointer behind.
	dbfx.Exec(t, `DELETE FROM issue WHERE id = $1`, original)

	detail, listed, searched = read()
	for name, resp := range map[string]*IssueResponse{"detail": &detail, "list": listed, "search": searched} {
		if resp == nil {
			t.Fatalf("%s: duplicate missing from response", name)
		}
		if resp.DuplicateOf != nil {
			t.Fatalf("%s: duplicate_of = %+v after the original was deleted, want null", name, resp.DuplicateOf)
		}
	}
}
