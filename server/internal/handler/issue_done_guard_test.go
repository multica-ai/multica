package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// withRequireHumanDone flips the human-only-done policy on for the duration of
// a test and restores it afterwards. cfg is a value field on the shared
// testHandler; handler tests run sequentially (no t.Parallel here), so the
// mutate-and-restore is safe.
func withRequireHumanDone(t *testing.T, on bool) {
	t.Helper()
	prev := testHandler.cfg.RequireHumanDone
	testHandler.cfg.RequireHumanDone = on
	t.Cleanup(func() { testHandler.cfg.RequireHumanDone = prev })
}

// createDoneGuardIssue creates an issue in the given status and registers
// cleanup. Returns the decoded response.
func createDoneGuardIssue(t *testing.T, status string) IssueResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "done-guard " + time.Now().Format(time.RFC3339Nano),
		"status": status,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue
}

// asMachineActor stamps the request as if it arrived on a mat_ task token —
// the authoritative shape the auth middleware produces for an agent. X-User-ID
// stays the owning human (set by newRequest), mirroring production.
func asMachineActor(req *http.Request, agentID string) *http.Request {
	req.Header.Set("X-Actor-Source", "task_token")
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
	return req
}

func putStatus(t *testing.T, issueID, status string, asMachine bool, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": status})
	if asMachine {
		req = asMachineActor(req, agentID)
	}
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	return w
}

func issueStatusInDB(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("read issue status: %v", err)
	}
	return status
}

func countAgentDoneBlockedInbox(t *testing.T, recipientUserID, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM inbox_item
		   WHERE recipient_id = $1 AND issue_id = $2 AND type = 'agent_done_blocked'`,
		recipientUserID, issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count agent_done_blocked inbox: %v", err)
	}
	return n
}

// TestRequireHumanDone_BlocksMachineActor — with the policy on, a machine
// actor's done transition is rejected with 403, the status is left untouched,
// and the owning human gets an agent_done_blocked inbox row.
func TestRequireHumanDone_BlocksMachineActor(t *testing.T) {
	withRequireHumanDone(t, true)
	agentID := createHandlerTestAgent(t, "done-guard agent "+time.Now().Format(time.RFC3339Nano), []byte("[]"))
	issue := createDoneGuardIssue(t, "in_progress")

	w := putStatus(t, issue.ID, "done", true, agentID)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusInDB(t, issue.ID); got != "in_progress" {
		t.Fatalf("status must stay unchanged, got %q", got)
	}
	if n := countAgentDoneBlockedInbox(t, testUserID, issue.ID); n != 1 {
		t.Fatalf("expected exactly 1 agent_done_blocked inbox row, got %d", n)
	}
}

// TestRequireHumanDone_AllowsHumanActor — with the policy on, a human (no
// X-Actor-Source) can still mark an issue done.
func TestRequireHumanDone_AllowsHumanActor(t *testing.T) {
	withRequireHumanDone(t, true)
	issue := createDoneGuardIssue(t, "in_progress")

	w := putStatus(t, issue.ID, "done", false, "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusInDB(t, issue.ID); got != "done" {
		t.Fatalf("expected status done, got %q", got)
	}
}

// TestRequireHumanDone_AllowsMachineNonDone — the gate is scoped to the `done`
// transition only; a machine actor may still move an issue to any other
// status (e.g. in_review, the natural agent handoff).
func TestRequireHumanDone_AllowsMachineNonDone(t *testing.T) {
	withRequireHumanDone(t, true)
	agentID := createHandlerTestAgent(t, "done-guard agent "+time.Now().Format(time.RFC3339Nano), []byte("[]"))
	issue := createDoneGuardIssue(t, "in_progress")

	w := putStatus(t, issue.ID, "in_review", true, agentID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusInDB(t, issue.ID); got != "in_review" {
		t.Fatalf("expected status in_review, got %q", got)
	}
}

// TestRequireHumanDone_DisabledAllowsMachine — with the policy off (default),
// a machine actor marks done exactly as before. Guards against the flag
// leaking into the default contract.
func TestRequireHumanDone_DisabledAllowsMachine(t *testing.T) {
	withRequireHumanDone(t, false)
	agentID := createHandlerTestAgent(t, "done-guard agent "+time.Now().Format(time.RFC3339Nano), []byte("[]"))
	issue := createDoneGuardIssue(t, "in_progress")

	w := putStatus(t, issue.ID, "done", true, agentID)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := issueStatusInDB(t, issue.ID); got != "done" {
		t.Fatalf("expected status done, got %q", got)
	}
}

// TestRequireHumanDone_BlocksMachineCreateAsDone — creating an issue already
// in `done` is the same loophole as transitioning into it, so it is gated too.
func TestRequireHumanDone_BlocksMachineCreateAsDone(t *testing.T) {
	withRequireHumanDone(t, true)
	agentID := createHandlerTestAgent(t, "done-guard agent "+time.Now().Format(time.RFC3339Nano), []byte("[]"))

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "done-guard create " + time.Now().Format(time.RFC3339Nano),
		"status": "done",
	})
	req = asMachineActor(req, agentID)
	testHandler.CreateIssue(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
