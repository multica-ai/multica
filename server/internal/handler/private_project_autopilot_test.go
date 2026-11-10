package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// An autopilot bound to a private project is part of it: its title, prompt,
// triggers and runs answer like a missing autopilot to members outside the
// project, and it is left out of their list.
func TestPrivateProjectAutopilotsHiddenFromOutsiders(t *testing.T) {
	f := newPrivateProjectFixture(t)
	var agentID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'`, testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           f.marker + " secret autopilot",
		"description":     f.marker + " secret prompt",
		"assignee_id":     agentID,
		"execution_mode":  "run_only",
		"project_id":      f.projectID,
		"created_by_type": "member",
		"created_by_id":   f.insider,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "completed",
		"result":       testutil.Raw(`'{"output":"secret result"}'::jsonb`),
	})

	call := func(userID string, h http.HandlerFunc, path string, params map[string]string) *httptest.ResponseRecorder {
		req := asUser(newRequest(http.MethodGet, path, nil), userID)
		var kv []string
		for k, v := range params {
			kv = append(kv, k, v)
		}
		req = withURLParams(req, kv...)
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec
	}
	for _, tc := range []struct {
		name   string
		userID string
		want   int
	}{
		{"workspace member outside the project", f.outsider, http.StatusNotFound},
		{"project member", f.insider, http.StatusOK},
		{"workspace owner outside the member list", testUserID, http.StatusOK},
	} {
		list := call(tc.userID, testHandler.ListAutopilots, "/api/autopilots", nil)
		listed := strings.Contains(list.Body.String(), autopilotID)
		if list.Code != http.StatusOK || listed != (tc.want == http.StatusOK) {
			t.Errorf("%s: list = %d, autopilot listed %v", tc.name, list.Code, listed)
		}
		for _, r := range []struct {
			route string
			h     http.HandlerFunc
			path  string
			p     map[string]string
		}{
			{"detail", testHandler.GetAutopilot, "/api/autopilots/" + autopilotID, map[string]string{"id": autopilotID}},
			{"runs", testHandler.ListAutopilotRuns, "/api/autopilots/" + autopilotID + "/runs", map[string]string{"id": autopilotID}},
			{"run", testHandler.GetAutopilotRun, "/api/autopilots/" + autopilotID + "/runs/" + runID, map[string]string{"id": autopilotID, "runId": runID}},
		} {
			rec := call(tc.userID, r.h, r.path, r.p)
			if rec.Code != tc.want {
				t.Errorf("%s: %s = %d, want %d", tc.name, r.route, rec.Code, tc.want)
			}
			if tc.want == http.StatusNotFound && strings.Contains(rec.Body.String(), f.marker) {
				t.Errorf("%s: %s names the private autopilot: %s", tc.name, r.route, rec.Body.String())
			}
		}
	}
}
