package handler

import (
	"context"
	"net/http"
	"os"
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

// runsResponse drives the handler the way the router does: path params carry
// the issue, the raw query string carries the scope. Headers come back too —
// the truncation signal rides one.
func runsResponse(t *testing.T, issueID, query string) *testutil.Response {
	t.Helper()
	path := "/api/issues/" + issueID + "/task-runs"
	if query != "" {
		path += "?" + query
	}
	return testutil.Call(t, testHandler.ListTasksByIssue,
		withURLParam(newRequest(http.MethodGet, path, nil), "id", issueID),
	)
}

func runsRequest(t *testing.T, issueID, query string) []AgentTaskResponse {
	t.Helper()
	var out []AgentTaskResponse
	runsResponse(t, issueID, query).Want(http.StatusOK).JSON(&out)
	return out
}

// familyRequest decodes the family read's own payload. It is a different,
// deliberately smaller type than the execution log's row, so the tests decode
// it as one rather than through AgentTaskResponse — a struct that silently
// accepted both would stop noticing if the two shapes merged again.
func familyRequest(t *testing.T, issueID, query string) []ActiveRunSummary {
	t.Helper()
	var out []ActiveRunSummary
	runsResponse(t, issueID, query).Want(http.StatusOK).JSON(&out)
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

func summaryIDs(runs []ActiveRunSummary) []string {
	ids := make([]string, len(runs))
	for i, r := range runs {
		ids[i] = r.TaskID
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

func TestTaskHistoryOmitsUnusedAssigneeFallbacks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "FallbackHistoryAgent", []byte("[]"))
	issueID := dbfx.Issue(t, "fallback history")
	runtimeID := handlerTestRuntimeID(t)
	primaryID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "status": "completed", "runtime_id": runtimeID})
	want := []string{primaryID}
	for _, tc := range []struct {
		name       string
		status     string
		fallback   bool
		dispatched bool
		started    bool
		visible    bool
	}{
		{name: "inert fallback", status: "deferred", fallback: true},
		{name: "unused cancelled fallback", status: "cancelled", fallback: true},
		{name: "ordinary cancellation", status: "cancelled", visible: true},
		{name: "ordinary deferred task", status: "deferred", visible: true},
		{name: "promoted fallback", status: "queued", fallback: true, visible: true},
		{name: "dispatched unused cancellation", status: "cancelled", fallback: true, dispatched: true},
		{name: "ordinary dispatched cancellation", status: "cancelled", dispatched: true, visible: true},
		{name: "started cancellation", status: "cancelled", fallback: true, started: true, visible: true},
		{name: "failed fallback", status: "failed", fallback: true, visible: true},
		{name: "completed fallback", status: "completed", fallback: true, visible: true},
	} {
		cols := testutil.Cols{"issue_id": issueID, "status": tc.status, "trigger_summary": tc.name, "runtime_id": runtimeID}
		if tc.fallback {
			cols["escalation_for_task_id"] = primaryID
		}
		if tc.dispatched {
			cols["dispatched_at"] = testutil.Raw("now()")
		}
		if tc.started {
			cols["started_at"] = testutil.Raw("now()")
		}
		id := dbfx.Task(t, agentID, cols)
		if tc.visible {
			want = append(want, id)
		}
	}
	sort.Strings(want)
	if got := taskIDs(runsRequest(t, issueID, "")); !sameIDs(got, want) {
		t.Errorf("issue history = %v, want %v (omit only unused assignee fallbacks)", got, want)
	}
	var agentRuns []AgentTaskResponse
	testutil.Call(t, testHandler.ListAgentTasks, withURLParam(
		newRequest(http.MethodGet, "/api/agents/"+agentID+"/tasks?include_usage=true", nil), "id", agentID,
	)).Want(http.StatusOK).JSON(&agentRuns)
	if got := taskIDs(agentRuns); !sameIDs(got, want) {
		t.Errorf("agent history = %v, want %v (same visibility as issue history)", got, want)
	}
}

func TestCancelCommentAssigneeFallbacksMigration(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "RetiredFallbackAgent", []byte("[]"))
	idleAgentID := createHandlerTestAgent(t, "UnusedFallbackAgent", []byte("[]"))
	idleIssueID := dbfx.Issue(t, "only unused fallback")
	idlePrimaryID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": idleIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed"})
	idleFallbackID := dbfx.Task(t, idleAgentID, testutil.Cols{
		"issue_id": idleIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "dispatched",
		"dispatched_at": testutil.Raw("now()"), "escalation_for_task_id": idlePrimaryID,
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET status = 'working' WHERE id IN ($1, $2)`, agentID, idleAgentID); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	want[idleFallbackID] = "cancelled"
	retryIssueID := dbfx.Issue(t, "unused fallback retry lineage")
	retryPrimaryID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": retryIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed",
	})
	startedFallbackID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": retryIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "failed",
		"started_at": testutil.Raw("now() - interval '2 minutes'"), "completed_at": testutil.Raw("now() - interval '1 minute'"),
		"escalation_for_task_id": retryPrimaryID,
	})
	startedRetryID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": retryIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "failed",
		"started_at": testutil.Raw("now() - interval '1 minute'"), "completed_at": testutil.Raw("now()"),
		"parent_task_id": startedFallbackID, "retry_of_task_id": startedFallbackID,
	})
	pendingRetryID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": retryIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "deferred",
		"fire_at":        testutil.Raw("now() + interval '1 minute'"),
		"parent_task_id": startedRetryID, "retry_of_task_id": startedRetryID,
	})
	manualRerunID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": retryIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "queued",
		"rerun_of_task_id": startedFallbackID,
	})
	mergedIssueID := dbfx.Issue(t, "fallback with merged comment")
	mergedPrimaryID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": mergedIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed",
	})
	mergedOriginalCommentID := dbfx.Comment(t, mergedIssueID, "original fallback trigger")
	mergedTriggerCommentID := dbfx.Comment(t, mergedIssueID, "explicit assignee mention")
	mergedFallbackID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": mergedIssueID, "runtime_id": handlerTestRuntimeID(t), "status": "queued",
		"escalation_for_task_id": mergedPrimaryID, "trigger_comment_id": mergedTriggerCommentID,
	})
	dbfx.Exec(t, `UPDATE agent_task_queue SET coalesced_comment_ids = ARRAY[$2::uuid] WHERE id = $1`,
		mergedFallbackID, mergedOriginalCommentID)
	want[retryPrimaryID] = "completed"
	want[startedFallbackID] = "failed"
	want[startedRetryID] = "failed"
	want[pendingRetryID] = "cancelled"
	want[manualRerunID] = "queued"
	want[mergedPrimaryID] = "completed"
	want[mergedFallbackID] = "queued"
	for _, tc := range []struct {
		status   string
		fallback bool
		started  bool
		want     string
	}{
		{status: "deferred", fallback: true, want: "cancelled"},
		{status: "queued", fallback: true, want: "cancelled"},
		{status: "dispatched", fallback: true, want: "cancelled"},
		{status: "waiting_local_directory", fallback: true, want: "cancelled"},
		{status: "running", fallback: true, started: true, want: "running"},
		{status: "completed", fallback: true, started: true, want: "completed"},
		{status: "failed", fallback: true, want: "failed"},
		{status: "cancelled", fallback: true, want: "cancelled"},
		{status: "deferred", want: "deferred"},
		{status: "queued", want: "queued"},
	} {
		issueID := dbfx.Issue(t, "retired fallback")
		primaryID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": "completed"})
		want[primaryID] = "completed"
		cols := testutil.Cols{"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": tc.status}
		if tc.fallback {
			cols["escalation_for_task_id"] = primaryID
		}
		if tc.started {
			cols["started_at"] = testutil.Raw("now()")
		}
		want[dbfx.Task(t, agentID, cols)] = tc.want
	}
	sql, err := os.ReadFile("../../migrations/456_cancel_comment_assignee_fallbacks.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := testPool.Exec(context.Background(), string(sql)); err != nil {
			t.Fatal(err)
		}
		for id, status := range want {
			if got := taskStatusByID(t, id); got != status {
				t.Errorf("task %s status = %s, want %s", id, got, status)
			}
		}
		var retryFallbackID *string
		if err := testPool.QueryRow(context.Background(),
			`SELECT escalation_for_task_id FROM agent_task_queue WHERE id = $1`, pendingRetryID,
		).Scan(&retryFallbackID); err != nil {
			t.Fatal(err)
		}
		if retryFallbackID == nil || *retryFallbackID != retryPrimaryID {
			t.Errorf("pending fallback retry escalation_for_task_id = %v, want %s", retryFallbackID, retryPrimaryID)
		}
		for _, id := range taskIDs(runsRequest(t, retryIssueID, "")) {
			if id == pendingRetryID {
				t.Errorf("cancelled fallback retry %s must be hidden from issue history", id)
			}
		}
		for id, wantStatus := range map[string]string{agentID: "working", idleAgentID: "idle"} {
			agent, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			if agent.Status != wantStatus {
				t.Errorf("agent %s status = %s, want %s after retiring unused fallbacks", id, agent.Status, wantStatus)
			}
		}
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

	runs := familyRequest(t, f.childA, "scope=family&active=true")
	if want := sortedCopy(onSelf, onSibling, onParent); !sameIDs(summaryIDs(runs), want) {
		t.Fatalf("family runs = %v, want %v (self + sibling + parent, no terminal, no unrelated issue)",
			summaryIDs(runs), want)
	}

	for _, run := range runs {
		// Issue identity labels the row; agent identity IS the answer, since
		// this read exists to find the agent working next to you.
		if run.IssueIdentifier == "" || run.IssueTitle == "" || run.AgentID == "" {
			t.Fatalf("run %s carries no issue/agent identity (%q / %q / %q); a family row cannot be attributed from the task alone",
				run.TaskID, run.IssueIdentifier, run.IssueTitle, run.AgentID)
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

	got := summaryIDs(familyRequest(t, f.parentID, "scope=family"))
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

	got := summaryIDs(familyRequest(t, f.unrelated, "scope=family"))
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

// The whole point of --active is a cheap read. ListIssueTaskUsage returns a row
// per (task, provider, model) for EVERY task the issue ever ran, so hydrating
// it here would keep paying the full-history cost the filter exists to remove.
// Usage is an execution-log concern; a coordination read must not carry it even
// when the active task happens to have some recorded.
func TestListTasksByIssueActiveOmitsUsage(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	running := f.task(t, f.childA, "running")
	dbfx.Exec(t, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks)
		VALUES ($1, 'anthropic', 'claude-opus-5', 1000, 500, 0, 0, NULL)
	`, running)

	active := runsRequest(t, f.childA, "active=true")
	if len(active) != 1 {
		t.Fatalf("active runs = %d, want 1", len(active))
	}
	if len(active[0].Usage) != 0 {
		t.Fatalf("active read carried usage %+v; the coordination path must not query it", active[0].Usage)
	}

	// The execution log still gets it — this is a per-path decision, not a
	// removal of the feature.
	history := runsRequest(t, f.childA, "")
	if len(history) != 1 || len(history[0].Usage) != 1 {
		t.Fatalf("history read lost its usage: %+v", history)
	}
}

// A boolean param that only recognises the exact string "true" turns every
// typo into a silent downgrade: `active=tru` would answer "who is here now?"
// with the whole execution history under a 200, and the caller reads a pile of
// finished runs as the live ones. Same reason the unknown-scope check exists.
func TestListTasksByIssueRejectsInvalidActive(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	f.task(t, f.childA, "completed")

	for _, bad := range []string{"tru", "TRUE", "1", "yes", ""} {
		query := "active=" + bad
		if bad == "" {
			// The empty value is the one non-rejection in the set: it means the
			// caller did not ask, so the read stays the full history.
			runsRequest(t, f.childA, query)
			continue
		}
		runsResponse(t, f.childA, query).Want(http.StatusBadRequest)
	}

	// false is a real value and must not 400.
	runsRequest(t, f.childA, "active=false")
}

// A truncated coordination read and a complete one are indistinguishable in the
// body, and reading the first as the second produces exactly the wrong
// conclusion — "no run on that sibling" when the answer was cut off. The cap
// therefore has to announce itself.
func TestListTasksByIssueFamilyScopeReportsTruncation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)

	// Exactly at the cap: a full page is not a truncated one.
	for i := 0; i < familyActiveRunCap; i++ {
		f.task(t, f.childA, "running")
	}
	full := runsResponse(t, f.childA, "scope=family").Want(http.StatusOK)
	if got := full.Header().Get(HeaderActiveRunsTruncated); got != "" {
		t.Fatalf("truncation header = %q on an exactly-full page, want empty", got)
	}
	var atCap []ActiveRunSummary
	full.JSON(&atCap)
	if len(atCap) != familyActiveRunCap {
		t.Fatalf("rows at cap = %d, want %d", len(atCap), familyActiveRunCap)
	}

	// One past it: the response is capped AND says so.
	f.task(t, f.childB, "running")
	over := runsResponse(t, f.childA, "scope=family").Want(http.StatusOK)
	if got := over.Header().Get(HeaderActiveRunsTruncated); got != "true" {
		t.Fatalf("truncation header = %q past the cap, want \"true\"", got)
	}
	var truncated []ActiveRunSummary
	over.JSON(&truncated)
	if len(truncated) != familyActiveRunCap {
		t.Fatalf("rows past cap = %d, want them trimmed to %d", len(truncated), familyActiveRunCap)
	}
}

// The family read's payload is its product, not an implementation detail: it is
// an agent spending its own context on the answer. The execution-log row costs
// roughly 5x the bytes — attribution alone was the largest field on it and
// needed a second query — and a caller asking "who else is here?" reads none of
// it. Pinning the exact key set is what stops the two shapes from quietly
// merging back together the next time someone reuses taskToResponse here.
func TestListTasksByIssueFamilyScopePayloadStaysLean(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	f := newFamilyFixture(t)
	f.task(t, f.childB, "running")

	var raw []map[string]any
	runsResponse(t, f.childA, "scope=family").Want(http.StatusOK).JSON(&raw)
	if len(raw) != 1 {
		t.Fatalf("family rows = %d, want 1", len(raw))
	}

	want := map[string]bool{
		"task_id": true, "issue_id": true, "issue_identifier": true,
		"issue_title": true, "agent_id": true, "status": true,
		"created_at": true, "started_at": true,
	}
	for key := range raw[0] {
		if !want[key] {
			t.Errorf("family row carries execution-log field %q; this read is a coordination payload", key)
		}
	}
	// started_at is omitempty (a queued run has none), so only the rest are
	// guaranteed present on a running row.
	for key := range want {
		if _, ok := raw[0][key]; !ok && key != "started_at" {
			t.Errorf("family row missing %q", key)
		}
	}
}
