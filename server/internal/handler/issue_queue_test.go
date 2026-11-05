package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// queueIssue makes a backlog issue, queued and waiting on the given issues.
func queueIssue(t *testing.T, title string, waitsOn ...string) string {
	t.Helper()
	id := dbfx.Issue(t, title, testutil.Cols{"status": "backlog"})
	setRunQueue(t, id, waitsOn...)
	return id
}

func setRunQueue(t *testing.T, id string, waitsOn ...string) {
	t.Helper()
	if waitsOn == nil {
		waitsOn = []string{}
	}
	dbfx.Exec(t, `UPDATE issue SET run_queued = true, run_after_issue_ids = $2::uuid[] WHERE id = $1`, id, waitsOn)
}

func queuedIssueStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, id).Scan(&status)
	return status
}

func promoteQueue(t *testing.T) int {
	t.Helper()
	n, err := testHandler.PromoteQueuedIssues(context.Background(), parseUUID(testWorkspaceID), false)
	if err != nil {
		t.Fatalf("PromoteQueuedIssues: %v", err)
	}
	return n
}

func TestPromoteQueuedIssues_ReleasesWhenEveryWaitIsFinished(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	a := queueIssue(t, "queue a")
	b := queueIssue(t, "queue b")
	c := queueIssue(t, "queue c", a, b)

	if n := promoteQueue(t); n != 2 {
		t.Fatalf("first pass moved %d issues, want 2 (the two with no waits)", n)
	}
	if got := queuedIssueStatus(t, c); got != "backlog" {
		t.Fatalf("c status = %q, want backlog while its waits are not both promoted", got)
	}

	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, a)
	if n := promoteQueue(t); n != 0 {
		t.Fatalf("moved %d issues while b is still open", n)
	}

	// A cancelled wait counts as finished.
	dbfx.Exec(t, `UPDATE issue SET status = 'cancelled' WHERE id = $1`, b)
	if n := promoteQueue(t); n != 1 {
		t.Fatalf("moved %d issues after every wait finished, want 1", n)
	}
	if got := queuedIssueStatus(t, c); got != "todo" {
		t.Fatalf("c status = %q, want todo", got)
	}
	if n := promoteQueue(t); n != 0 {
		t.Fatalf("repeat pass moved %d issues", n)
	}
}

func TestPromoteQueuedIssuesWaitsForPredecessorRunToEnd(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := seededReadyAgentID(t)
	predecessor := dbfx.Issue(t, "queue run predecessor", testutil.Cols{"status": "done"})
	successor := queueIssue(t, "queue run successor", predecessor)
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id": predecessor, "runtime_id": runtimeID, "status": "queued", "started_at": time.Now(),
	})

	for _, status := range []string{"queued", "dispatched", "waiting_local_directory", "running"} {
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = $2 WHERE id = $1`, taskID, status)
		if n := promoteQueue(t); n != 0 {
			t.Fatalf("moved %d issues while the finished predecessor's run is %s", n, status)
		}
	}

	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID)
	if n := promoteQueue(t); n != 1 {
		t.Fatalf("moved %d issues after the predecessor's run ended, want 1", n)
	}
	if got := queuedIssueStatus(t, successor); got != "todo" {
		t.Fatalf("successor status = %q, want todo", got)
	}
}

func putRunQueue(t *testing.T, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/issues/"+id+"/run-queue", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = withURLParam(req, "id", id)
	testHandler.SetIssueRunQueue(w, req)
	return w
}

func TestSetIssueRunQueue_ValidatesWaitsAndClears(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	id := dbfx.Issue(t, "run queue endpoint", testutil.Cols{"status": "backlog"})
	other := dbfx.Issue(t, "run queue predecessor", testutil.Cols{"status": "todo"})
	chained := queueIssue(t, "run queue chained", id)

	for _, body := range []string{
		`{"queued":true,"after_issue_ids":["` + id + `"]}`,                           // waits on itself
		`{"queued":true,"after_issue_ids":["` + chained + `"]}`,                      // cycle: chained already waits on id
		`{"queued":true,"after_issue_ids":["00000000-0000-0000-0000-000000000099"]}`, // not in workspace
		`{"queued":true,"after_issue_ids":["not-a-uuid"]}`,
	} {
		if w := putRunQueue(t, id, body); w.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, w.Code)
		}
	}

	if w := putRunQueue(t, id, `{"queued":true,"after_issue_ids":["`+other+`"]}`); w.Code != http.StatusOK {
		t.Fatalf("set: status = %d: %s", w.Code, w.Body.String())
	}
	var queued bool
	var waits []string
	dbfx.QueryRow(t, `SELECT run_queued, run_after_issue_ids::text[] FROM issue WHERE id = $1`, id).Scan(&queued, &waits)
	if !queued || len(waits) != 1 || waits[0] != other {
		t.Fatalf("stored run queue = (%v, %v), want (true, [%s])", queued, waits, other)
	}

	if w := putRunQueue(t, id, `{"queued":false}`); w.Code != http.StatusOK {
		t.Fatalf("clear: status = %d: %s", w.Code, w.Body.String())
	}
	dbfx.QueryRow(t, `SELECT run_queued, run_after_issue_ids::text[] FROM issue WHERE id = $1`, id).Scan(&queued, &waits)
	if queued || len(waits) != 0 {
		t.Fatalf("after clear = (%v, %v), want (false, [])", queued, waits)
	}
}

func TestRunQueueBatchPreviewAndWaits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	open := dbfx.Issue(t, "run queue open predecessor", testutil.Cols{"status": "todo"})
	a := dbfx.Issue(t, "run queue batch a", testutil.Cols{"status": "backlog"})
	b := dbfx.Issue(t, "run queue batch b", testutil.Cols{"status": "backlog"})

	batch := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var fn http.HandlerFunc = testHandler.BatchSetRunQueue
		if strings.Contains(path, "preview") {
			fn = testHandler.PreviewRunQueue
		}
		return queuePost(t, fn, path, body)
	}

	if w := batch("/api/issues/batch-run-queue", `{"issue_ids":["`+a+`","`+b+`"],"queued":true,"after_issue_ids":["`+open+`"]}`); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"updated":2`) {
		t.Fatalf("batch set: %d %s", w.Code, w.Body.String())
	}

	// The predecessor is still open, so nothing would start now.
	w := batch("/api/issues/run-queue/preview", `{"issue_ids":["`+a+`","`+b+`"],"queued":true,"after_issue_ids":["`+open+`"]}`)
	var preview struct {
		ReleaseNow int `json:"release_now"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.ReleaseNow != 0 {
		t.Fatalf("preview with open wait: %d %s, want release_now 0", w.Code, w.Body.String())
	}

	// The waits endpoint lists both queued issues against the open predecessor.
	fw := httptest.NewRecorder()
	freq := httptest.NewRequest(http.MethodGet, "/api/issues/run-queue/waits", nil)
	freq.Header.Set("X-User-ID", testUserID)
	freq.Header.Set("X-Workspace-ID", testWorkspaceID)
	testHandler.ListRunQueueWaits(fw, freq)
	if fw.Code != http.StatusOK || strings.Count(fw.Body.String(), open) < 2 {
		t.Fatalf("waits: %d %s, want both issues waiting on %s", fw.Code, fw.Body.String(), open)
	}

	if w := batch("/api/issues/batch-run-queue", `{"issue_ids":["`+a+`"],"queued":true,"after_issue_ids":["`+a+`"]}`); w.Code != http.StatusBadRequest {
		t.Fatalf("self wait in batch: status = %d, want 400", w.Code)
	}
}

func queuePost(t *testing.T, fn http.HandlerFunc, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	fn(w, req)
	return w
}
