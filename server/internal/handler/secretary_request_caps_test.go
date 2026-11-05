package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The Secretary's request caps (spec 13.7.1), on the server: eight open requests
// per workspace, twenty addressees per request, and two hops below a request for
// any creator. The refusal is a 429 that names the limit. A fresh workspace keeps
// the desk and the counts to this test.
func TestSecretaryRequestCaps_OpenRequestsAddresseesAndHops(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newDigestEnv(t)
	e.setSecretary(t, e.AgentID)
	desk := dbfx.Project(t, "Secretary desk", testutil.Cols{"workspace_id": e.WorkspaceID})
	taskID := dbfx.Task(t, e.AgentID, testutil.Cols{
		"runtime_id":          e.RuntimeID,
		"status":              "running",
		"originator_user_id":  testUserID,
		"accountable_user_id": testUserID,
	})

	// create files an issue as the Secretary (asSecretary) or as the workspace's
	// owner, and returns the response.
	create := func(asSecretary bool, body map[string]any) *httptest.ResponseRecorder {
		req := newRequest("POST", "/api/issues?workspace_id="+e.WorkspaceID, body)
		req.Header.Set("X-Workspace-ID", e.WorkspaceID)
		if asSecretary {
			req.Header.Set("X-Agent-ID", e.AgentID)
			req.Header.Set("X-Task-ID", taskID)
		}
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, req)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) IssueResponse {
		t.Helper()
		var out IssueResponse
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode issue: %v", err)
		}
		return out
	}
	mustCreate := func(w *httptest.ResponseRecorder) IssueResponse {
		t.Helper()
		if w.Code != http.StatusCreated {
			t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		return decode(w)
	}
	mustRefuse := func(w *httptest.ResponseRecorder, what string) {
		t.Helper()
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s: expected 429, got %d: %s", what, w.Code, w.Body.String())
		}
	}

	// Open requests: eight in the desk, the ninth refused; an issue outside the
	// desk is never a request.
	var first IssueResponse
	for i := 1; i <= 8; i++ {
		got := mustCreate(create(true, map[string]any{"title": fmt.Sprintf("Request %d", i), "project_id": desk}))
		if i == 1 {
			first = got
		}
	}
	mustRefuse(create(true, map[string]any{"title": "Request nine", "project_id": desk}), "ninth open request")
	outside := dbfx.Project(t, "Not the desk", testutil.Cols{"workspace_id": e.WorkspaceID})
	mustCreate(create(true, map[string]any{"title": "Outside the desk", "project_id": outside}))

	// Addressees: twenty children of the request; the twenty-first is refused.
	var addressee IssueResponse
	for i := 1; i <= 20; i++ {
		got := mustCreate(create(true, map[string]any{"title": fmt.Sprintf("Addressee %02d", i), "parent_issue_id": first.ID}))
		if i == 1 {
			addressee = got
		}
	}
	mustRefuse(create(true, map[string]any{"title": "Addressee twenty-one", "parent_issue_id": first.ID}), "twenty-first addressee")

	// Hops: the addressee's child (chief to leader) is two hops and allowed; one
	// hop further is refused, for the Secretary and for any other creator.
	leader := mustCreate(create(true, map[string]any{"title": "Leader sub-issue", "parent_issue_id": addressee.ID}))
	mustRefuse(create(true, map[string]any{"title": "Too deep", "parent_issue_id": leader.ID}), "third hop, Secretary")
	mustRefuse(create(false, map[string]any{"title": "Too deep, member", "parent_issue_id": leader.ID}), "third hop, member")
}
