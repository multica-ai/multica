package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Task Message Window contract tests (MUL-5122). These assert the externally
// observable page contract — order, cursors, totals, facets, filters, bounds
// and authorization — not the SQL or handler internals.

var execLogIssueNum int64 = 95100

// createExecLogTask stands up an issue-bound task in the test workspace so
// ResolveTaskWorkspaceID authorizes the caller, and returns its id.
func createExecLogTask(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "ExecLogAgent-"+t.Name(), []byte("[]"))

	num := atomic.AddInt64(&execLogIssueNum, 1)
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'exec-log-issue', 'todo', 'medium', $2, 'member', $3, 0)
		RETURNING id
	`, testWorkspaceID, testUserID, num).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2)
		RETURNING id
	`, agentID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// seedExecLogMessage inserts one task_message. Empty tool/content become NULL so
// the missing-optional-field path is exercised. Returns the row id.
func seedExecLogMessage(t *testing.T, taskID string, seq int, msgType, tool, content string) string {
	t.Helper()
	var toolArg, contentArg any
	if tool != "" {
		toolArg = tool
	}
	if content != "" {
		contentArg = content
	}
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO task_message (task_id, seq, type, tool, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text
	`, taskID, seq, msgType, toolArg, contentArg).Scan(&id); err != nil {
		t.Fatalf("seed task message (seq=%d): %v", seq, err)
	}
	return id
}

func execLogPageRequest(t *testing.T, taskID, query string) *http.Request {
	t.Helper()
	path := "/api/tasks/" + taskID + "/messages/page"
	if query != "" {
		path += "?" + query
	}
	req := newRequestAs(testUserID, "GET", path, nil)
	req = withURLParam(req, "taskId", taskID)
	return withChatTestWorkspaceCtx(t, req)
}

func doExecLogPage(t *testing.T, taskID, query string) ExecutionLogPageResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListTaskMessagesPage(w, execLogPageRequest(t, taskID, query))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for query %q, got %d: %s", query, w.Code, w.Body.String())
	}
	var resp ExecutionLogPageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode page response: %v", err)
	}
	return resp
}

func execLogStatus(t *testing.T, taskID, query string) int {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListTaskMessagesPage(w, execLogPageRequest(t, taskID, query))
	return w.Code
}

// traverseOldest walks every older page from the newest window back to the
// start and returns the full Run in chronological order.
func traverseOldest(t *testing.T, taskID string, limit int) []protocol.TaskMessagePayload {
	t.Helper()
	base := "limit=" + itoa(limit)
	resp := doExecLogPage(t, taskID, base)
	all := append([]protocol.TaskMessagePayload{}, resp.Messages...)
	cursor := resp.OlderCursor
	guard := 0
	for cursor != nil {
		guard++
		if guard > 1000 {
			t.Fatal("pagination did not terminate")
		}
		older := doExecLogPage(t, taskID, base+"&before="+*cursor)
		all = append(append([]protocol.TaskMessagePayload{}, older.Messages...), all...)
		cursor = older.OlderCursor
	}
	return all
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestExecutionLogPage_NewestPageAndOlderTraversal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	for seq := 1; seq <= 5; seq++ {
		seedExecLogMessage(t, taskID, seq, "text", "", "line "+itoa(seq))
	}

	// Newest window: the most recent two events, chronological within the page.
	resp := doExecLogPage(t, taskID, "limit=2")
	if len(resp.Messages) != 2 || resp.Messages[0].Seq != 4 || resp.Messages[1].Seq != 5 {
		t.Fatalf("newest page mismatch: %+v", seqsOf(resp.Messages))
	}
	if resp.RawTotal != 5 || resp.MatchedTotal != 5 {
		t.Fatalf("totals mismatch: raw=%d matched=%d", resp.RawTotal, resp.MatchedTotal)
	}
	if resp.OlderCursor == nil {
		t.Fatal("expected older_cursor on a windowed newest page")
	}
	if resp.LatestCursor == nil {
		t.Fatal("expected latest_cursor (catch-up anchor) on the newest page")
	}

	// Full traversal reconstructs the Run in order with no gaps or duplicates.
	all := traverseOldest(t, taskID, 2)
	if got := seqsOf(all); !equalInts(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("full traversal mismatch: %v", got)
	}
}

func TestExecutionLogPage_DuplicateSeqDeterministicTraversal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)

	// Historical duplicate-seq fixture: several rows share seq values, exactly
	// the shape storage does not forbid. The (seq, id) tie-break must still give
	// a terminating, gap-free, duplicate-free traversal. Each row carries unique
	// content so the traversal can be checked as an exact bijection over the
	// seeded set — catching an id-level duplicate within the same seq that a
	// seq-only check would miss.
	seqs := []int{1, 2, 2, 2, 3, 3, 4}
	seededContent := map[string]int{}
	var expectedSeqs []int
	for i, s := range seqs {
		content := "dup-" + itoa(i)
		seedExecLogMessage(t, taskID, s, "text", "", content)
		seededContent[content] = s
		expectedSeqs = append(expectedSeqs, s)
	}
	sort.Ints(expectedSeqs)

	all := traverseOldest(t, taskID, 2)
	if got := seqsOf(all); !equalInts(got, expectedSeqs) {
		t.Fatalf("seq order mismatch: got %v want %v", got, expectedSeqs)
	}
	// Exact bijection: every seeded row appears exactly once, none invented.
	got := map[string]int{}
	for _, m := range all {
		got[m.Content]++
	}
	if len(got) != len(seededContent) {
		t.Fatalf("distinct rows mismatch: got %d want %d", len(got), len(seededContent))
	}
	for content, count := range got {
		if count != 1 {
			t.Fatalf("row %q visited %d times (want 1)", content, count)
		}
		if _, ok := seededContent[content]; !ok {
			t.Fatalf("traversal invented row %q", content)
		}
	}
}

func TestExecutionLogPage_AfterCatchUp(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	for seq := 1; seq <= 3; seq++ {
		seedExecLogMessage(t, taskID, seq, "text", "", "early")
	}

	resp := doExecLogPage(t, taskID, "limit=50")
	if got := seqsOf(resp.Messages); !equalInts(got, []int{1, 2, 3}) {
		t.Fatalf("initial page mismatch: %v", got)
	}
	if resp.LatestCursor == nil {
		t.Fatal("expected latest_cursor from the newest page")
	}
	anchor := *resp.LatestCursor

	// New events land after the client caught up.
	seedExecLogMessage(t, taskID, 4, "text", "", "late")
	seedExecLogMessage(t, taskID, 5, "text", "", "late")

	caught := doExecLogPage(t, taskID, "limit=50&after="+anchor)
	if got := seqsOf(caught.Messages); !equalInts(got, []int{4, 5}) {
		t.Fatalf("catch-up page mismatch: %v", got)
	}
	if caught.OlderCursor != nil {
		t.Fatal("after-page must not return an older_cursor")
	}
	if caught.LatestCursor == nil {
		t.Fatal("after-page must advance the catch-up anchor")
	}

	// Polling again from the advanced anchor yields nothing (fully caught up).
	done := doExecLogPage(t, taskID, "limit=50&after="+*caught.LatestCursor)
	if len(done.Messages) != 0 {
		t.Fatalf("expected empty catch-up when up to date, got %v", seqsOf(done.Messages))
	}
}

func TestExecutionLogPage_FullRunFacetsAndFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	// A tool call pair, an error, an agent line, and a provider-native
	// exec_command that will sit only in older history.
	seedExecLogMessage(t, taskID, 1, "exec_command", "exec_command", "old command")
	seedExecLogMessage(t, taskID, 2, "text", "", "narration")
	seedExecLogMessage(t, taskID, 3, "tool_use", "Bash", "run")
	seedExecLogMessage(t, taskID, 4, "tool_result", "Bash", "output")
	seedExecLogMessage(t, taskID, 5, "error", "", "boom")

	// Facets describe the complete Run regardless of the loaded window.
	resp := doExecLogPage(t, taskID, "limit=2")
	if got := len(resp.Messages); got != 2 {
		t.Fatalf("expected 2 loaded, got %d", got)
	}
	if facetCount(resp.TypeFacets, "exec_command") != 1 {
		t.Fatalf("exec_command must appear in full-Run type facets even when unloaded: %+v", resp.TypeFacets)
	}
	if facetCount(resp.TypeFacets, "error") != 1 || facetCount(resp.TypeFacets, "text") != 1 {
		t.Fatalf("type facets incomplete: %+v", resp.TypeFacets)
	}
	if facetCount(resp.ToolFacets, "Bash") != 2 {
		t.Fatalf("Bash tool facet should cover tool_use + tool_result = 2: %+v", resp.ToolFacets)
	}
	if facetCount(resp.ToolFacets, "exec_command") != 1 {
		t.Fatalf("exec_command tool facet missing: %+v", resp.ToolFacets)
	}
	if resp.RawTotal != 5 {
		t.Fatalf("raw_total mismatch: %d", resp.RawTotal)
	}

	// Filter by the Bash tool chip: covers both its tool_use and tool_result.
	filtered := doExecLogPage(t, taskID, "limit=50&tools=Bash")
	if filtered.MatchedTotal != 2 {
		t.Fatalf("Bash filter matched_total should be 2, got %d", filtered.MatchedTotal)
	}
	for _, m := range filtered.Messages {
		if m.Tool != "Bash" {
			t.Fatalf("filtered page leaked a non-Bash row: %+v", m)
		}
	}

	// Type filter reaches an event only present in unloaded history.
	execFilter := doExecLogPage(t, taskID, "limit=50&types=exec_command")
	if execFilter.MatchedTotal != 1 || len(execFilter.Messages) != 1 || execFilter.Messages[0].Type != "exec_command" {
		t.Fatalf("exec_command filter mismatch: matched=%d msgs=%+v", execFilter.MatchedTotal, seqsOf(execFilter.Messages))
	}

	// OR semantics across a type chip and a tool chip.
	orFilter := doExecLogPage(t, taskID, "limit=50&types=error&tools=Bash")
	if orFilter.MatchedTotal != 3 {
		t.Fatalf("error OR Bash should match 3 (error + tool_use + tool_result), got %d", orFilter.MatchedTotal)
	}
}

func TestExecutionLogPage_EmptyRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	resp := doExecLogPage(t, taskID, "")
	if len(resp.Messages) != 0 || resp.RawTotal != 0 || resp.MatchedTotal != 0 {
		t.Fatalf("empty run should be truly empty: %+v", resp)
	}
	if resp.OlderCursor != nil || resp.LatestCursor != nil {
		t.Fatalf("empty run must expose no cursors: older=%v latest=%v", resp.OlderCursor, resp.LatestCursor)
	}
	if len(resp.TypeFacets) != 0 || len(resp.ToolFacets) != 0 {
		t.Fatalf("empty run must have empty facets: %+v %+v", resp.TypeFacets, resp.ToolFacets)
	}
}

func TestExecutionLogPage_UnknownTypeAndMissingFieldsRetained(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	// Unknown type with every optional field NULL — must survive, not be dropped.
	seedExecLogMessage(t, taskID, 1, "mystery_type", "", "")

	resp := doExecLogPage(t, taskID, "")
	if len(resp.Messages) != 1 {
		t.Fatalf("unknown-type event was dropped: %+v", resp.Messages)
	}
	m := resp.Messages[0]
	if m.Type != "mystery_type" || m.Tool != "" || m.Content != "" {
		t.Fatalf("unknown-type payload mismatch: %+v", m)
	}
	if facetCount(resp.TypeFacets, "mystery_type") != 1 {
		t.Fatalf("unknown type must appear in facets: %+v", resp.TypeFacets)
	}
}

func TestExecutionLogPage_LimitBounds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "limit=abc"} {
		if code := execLogStatus(t, taskID, q); code != http.StatusBadRequest {
			t.Fatalf("query %q should be 400, got %d", q, code)
		}
	}
	if code := execLogStatus(t, taskID, "limit=100"); code != http.StatusOK {
		t.Fatalf("limit=100 (max) should be 200, got %d", code)
	}
}

func TestExecutionLogPage_InvalidAndContradictoryCursor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := createExecLogTask(t)
	if code := execLogStatus(t, taskID, "before=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("garbage before-cursor should be 400, got %d", code)
	}
	if code := execLogStatus(t, taskID, "after=not-a-cursor"); code != http.StatusBadRequest {
		t.Fatalf("garbage after-cursor should be 400, got %d", code)
	}
	if code := execLogStatus(t, taskID, "before=abc&after=def"); code != http.StatusBadRequest {
		t.Fatalf("before+after together should be 400, got %d", code)
	}
}

func TestExecutionLogPage_CrossWorkspaceReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	foreignAgentID := createForeignWorkspaceAgent(t)

	var foreignWS string
	if err := testPool.QueryRow(ctx, `SELECT workspace_id FROM agent WHERE id = $1`, foreignAgentID).Scan(&foreignWS); err != nil {
		t.Fatalf("load foreign workspace: %v", err)
	}
	var foreignIssue string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'foreign-exec-issue', 'todo', 'medium', $2, 'agent', 96001, 0)
		RETURNING id
	`, foreignWS, foreignAgentID).Scan(&foreignIssue); err != nil {
		t.Fatalf("create foreign issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, foreignIssue) })

	var foreignTask string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
		VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), 'running', 0, $2)
		RETURNING id
	`, foreignAgentID, foreignIssue).Scan(&foreignTask); err != nil {
		t.Fatalf("create foreign task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, foreignTask) })

	seedExecLogMessage(t, foreignTask, 1, "text", "", "secret")

	if code := execLogStatus(t, foreignTask, ""); code != http.StatusNotFound {
		t.Fatalf("cross-workspace task must be 404, got %d", code)
	}
}

// --- small assertion helpers ---

func seqsOf(msgs []protocol.TaskMessagePayload) []int {
	out := make([]int, len(msgs))
	for i, m := range msgs {
		out[i] = m.Seq
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func facetCount(facets []ExecutionLogFacet, key string) int64 {
	for _, f := range facets {
		if f.Key == key {
			return f.Count
		}
	}
	return -1
}
