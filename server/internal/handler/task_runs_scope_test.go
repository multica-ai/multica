package handler

import (
	"net/http"
	"sort"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// GET /api/issues/{id}/task-runs answers three different questions depending on
// its query params (#7768): the execution history the issue sidebar renders,
// "is anyone working on THIS issue right now", and "is anyone working anywhere
// in this sub-issue family right now". These tests pin the boundaries between
// them, because the only thing separating a cheap coordination read from a full
// history dump is a query param the caller may forget.

// familyFixture builds the shape the family read exists for: one parent, two
// children, and an unrelated issue in the same workspace that must never appear.
type familyFixture struct {
	agentID   string
	parentID  string
	childA    string
	childB    string
	unrelated string
}

func newFamilyFixture(t *testing.T) familyFixture {
	t.Helper()
	f := familyFixture{
		agentID:  createHandlerTestAgent(t, "FamilyRunsAgent", []byte("[]")),
		parentID: dbfx.Issue(t, "family-parent"),
	}
	f.childA = dbfx.Issue(t, "family-child-a", testutil.Cols{"parent_issue_id": f.parentID})
	f.childB = dbfx.Issue(t, "family-child-b", testutil.Cols{"parent_issue_id": f.parentID})
	f.unrelated = dbfx.Issue(t, "family-unrelated")
	return f
}

func (f familyFixture) task(t *testing.T, issueID, status string) string {
	t.Helper()
	return dbfx.Task(t, f.agentID, testutil.Cols{
		"issue_id":   issueID,
		"status":     status,
		"runtime_id": handlerTestRuntimeID(t),
	})
}

// runsRequest drives the handler the way the router does: path params carry the
// issue, the raw query string carries the scope.
func runsRequest(t *testing.T, issueID, query string) []AgentTaskResponse {
	t.Helper()
	path := "/api/issues/" + issueID + "/task-runs"
	if query != "" {
		path += "?" + query
	}
	var out []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue,
		withURLParam(newRequest(http.MethodGet, path, nil), "id", issueID),
	).Want(http.StatusOK).JSON(&out)
	return out
}

func taskIDs(runs []AgentTaskResponse) []string {
	ids := make([]string, len(runs))
	for i, r := range runs {
		ids[i] = r.ID
	}
	sort.Strings(ids)
	return ids
}

func sortedCopy(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The no-param response is what the issue-detail execution log and the CLI's
// short-task-ID resolver both read. Adding the coordination params must not
// have narrowed it: a completed run still comes back.
func TestListTasksByIssueDefaultsToFullHistory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	done := f.task(t, f.childA, "completed")
	live := f.task(t, f.childA, "running")

	got := taskIDs(runsRequest(t, f.childA, ""))
	if want := sortedCopy(done, live); !sameIDs(got, want) {
		t.Fatalf("history runs = %v, want %v (a completed run must survive)", got, want)
	}
}

// active=true is the cheap "is an agent on this right now" read. It must drop
// terminal runs and keep the whole in-flight set — including queued, which is
// about to touch the same code even though it cannot answer you yet.
func TestListTasksByIssueActiveDropsTerminalRuns(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	f.task(t, f.childA, "completed")
	f.task(t, f.childA, "failed")
	queued := f.task(t, f.childA, "queued")
	running := f.task(t, f.childA, "running")

	got := taskIDs(runsRequest(t, f.childA, "active=true"))
	if want := sortedCopy(queued, running); !sameIDs(got, want) {
		t.Fatalf("active runs = %v, want %v", got, want)
	}
}

// The point of #7768: a child asks the question and learns about its siblings.
// The parent's own run counts (it is the same coordination family), an
// unrelated issue's does not, and every row says which issue it belongs to —
// without that, a caller cannot tell one sibling's run from another's.
func TestListTasksByIssueFamilyScopeSpansSiblings(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	onSelf := f.task(t, f.childA, "running")
	onSibling := f.task(t, f.childB, "dispatched")
	onParent := f.task(t, f.parentID, "running")
	f.task(t, f.childB, "completed")
	f.task(t, f.unrelated, "running")

	runs := runsRequest(t, f.childA, "scope=family&active=true")
	if want := sortedCopy(onSelf, onSibling, onParent); !sameIDs(taskIDs(runs), want) {
		t.Fatalf("family runs = %v, want %v (self + sibling + parent, no terminal, no unrelated issue)",
			taskIDs(runs), want)
	}

	for _, run := range runs {
		if run.IssueIdentifier == "" || run.IssueTitle == "" {
			t.Fatalf("run %s carries no issue identity (%q / %q); a family row cannot be labelled from the task alone",
				run.ID, run.IssueIdentifier, run.IssueTitle)
		}
	}
}

// Asked from the parent instead of a child, the same flag has to answer the
// mirror-image question — "who is running on my children?" — rather than
// returning nothing because the parent has no parent of its own.
func TestListTasksByIssueFamilyScopeFromParentSeesChildren(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	onChild := f.task(t, f.childB, "running")
	onParent := f.task(t, f.parentID, "queued")
	f.task(t, f.unrelated, "running")

	got := taskIDs(runsRequest(t, f.parentID, "scope=family"))
	if want := sortedCopy(onChild, onParent); !sameIDs(got, want) {
		t.Fatalf("family runs from parent = %v, want %v", got, want)
	}
}

// An issue with no parent and no children still has to answer, degenerating to
// its own active runs — otherwise a caller has to know the issue's shape before
// it can choose a flag.
func TestListTasksByIssueFamilyScopeOnStandaloneIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	own := f.task(t, f.unrelated, "running")
	f.task(t, f.unrelated, "completed")
	f.task(t, f.childA, "running")

	got := taskIDs(runsRequest(t, f.unrelated, "scope=family"))
	if want := sortedCopy(own); !sameIDs(got, want) {
		t.Fatalf("family runs from standalone issue = %v, want %v", got, want)
	}
}

// A misspelled scope must fail loudly. Silently falling back to full history
// would hand an agent every past run when it asked for the live ones, which
// reads as "nobody else is here" only after the caller has paid for the whole
// log.
func TestListTasksByIssueRejectsUnknownScope(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)

	testutil.Call(t, testHandler.ListTasksByIssue,
		withURLParam(newRequest(http.MethodGet,
			"/api/issues/"+f.childA+"/task-runs?scope=sibling", nil), "id", f.childA),
	).Want(http.StatusBadRequest)
}
