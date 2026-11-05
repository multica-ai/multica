package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Scale check for the Secretary's read path (spec 13.8.5): the org map of a
// fifty-squad company returns every squad, and a request with twenty addressees
// is filed by the Secretary. Timings are logged so the numbers in the plan come
// from a run, not an estimate. The assertions are about correctness only.
func TestSecretaryScale_FiftySquadOrgMapAndTwentyAddresseeRequest(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newDigestEnv(t)
	for i := 0; i < 50; i++ {
		e.squad(t, fmt.Sprintf("Scale squad %02d", i))
	}

	start := time.Now()
	req := newRequest("GET", "/api/company/org-map", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	var all service.OrgMap
	testutil.Call(t, testHandler.GetCompanyOrgMap, req).Want(http.StatusOK).JSON(&all)
	t.Logf("org map, 50 squads: %s, %d squads returned, %d floors", time.Since(start), len(all.Squads), len(all.Floors))
	if len(all.Squads) != 50 {
		t.Fatalf("org map returned %d squads, want 50", len(all.Squads))
	}

	e.setSecretary(t, e.AgentID)
	desk := dbfx.Project(t, "Secretary desk", testutil.Cols{"workspace_id": e.WorkspaceID})
	taskID := dbfx.Task(t, e.AgentID, testutil.Cols{
		"runtime_id":          e.RuntimeID,
		"status":              "running",
		"originator_user_id":  testUserID,
		"accountable_user_id": testUserID,
	})
	post := func(body map[string]any) *httptest.ResponseRecorder {
		req := newRequest("POST", "/api/issues?workspace_id="+e.WorkspaceID, body)
		req.Header.Set("X-Workspace-ID", e.WorkspaceID)
		req.Header.Set("X-Agent-ID", e.AgentID)
		req.Header.Set("X-Task-ID", taskID)
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, req)
		return w
	}

	start = time.Now()
	w := post(map[string]any{"title": "Scale request", "project_id": desk})
	if w.Code != http.StatusCreated {
		t.Fatalf("create request: %d %s", w.Code, w.Body.String())
	}
	t.Logf("request create: %s", time.Since(start))

	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	squads := all.Squads
	start = time.Now()
	for i := 0; i < 20; i++ {
		c := post(map[string]any{
			"title":           fmt.Sprintf("Addressee %02d", i),
			"parent_issue_id": parent.ID,
			"assignee_type":   "squad",
			"assignee_id":     squads[i].ID,
		})
		if c.Code != http.StatusCreated {
			t.Fatalf("addressee %d: %d %s", i, c.Code, c.Body.String())
		}
	}
	t.Logf("20 addressees filed: %s (%s each)", time.Since(start), time.Since(start)/20)
}
