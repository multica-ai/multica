package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A private runtime is named in the Runtime view only to the users it is
// shared with. Everyone else, a workspace admin included, gets it folded onto
// restrictedRuntimesRowID with the totals intact. A run whose runtime was
// deleted (runtime_id set NULL, migration 251) names no runtime and comes back
// as runtime_id "" for everyone.
func TestDashboardRuntimeViewFoldsUnreadableRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	marker := fmt.Sprintf("rtvis%d", time.Now().UnixNano())
	member := createPlainMember(t, marker+"-member@multica.test")
	admin := createWorkspaceAdmin(t, marker+"-admin@multica.test")
	publicRT := dbfx.Runtime(t, marker+" public", testutil.Cols{"visibility": "public"})
	privateRT := dbfx.Runtime(t, marker+" private")
	sharedRT := dbfx.Runtime(t, marker+" shared", testutil.Cols{"shared_with": []string{member}})
	// The project keeps the restricted bucket free of other tests' runs.
	projectID := dbfx.Project(t, marker+" project")
	issueID := dbfx.Issue(t, marker+" issue", testutil.Cols{"project_id": projectID})
	dbfx.Cleanup(t, `DELETE FROM task_usage_hourly WHERE runtime_id IN ($1, $2, $3)`, publicRT, privateRT, sharedRT)

	var agentIDs []string
	for i, runtimeID := range []string{publicRT, privateRT, sharedRT, ""} {
		boundTo := runtimeID
		if boundTo == "" {
			boundTo = publicRT
		}
		agentID := dbfx.Agent(t, fmt.Sprintf("%s agent %d", marker, i), boundTo)
		agentIDs = append(agentIDs, agentID)
		var taskRuntime any = runtimeID
		if runtimeID == "" {
			taskRuntime = nil
		}
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": taskRuntime, "issue_id": issueID, "status": "completed",
			"started_at":   testutil.Raw("now() - interval '3 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
		if runtimeID == "" {
			// The hourly rollup never aggregates a NULL runtime.
			continue
		}
		dbfx.Exec(t, `
			INSERT INTO task_usage_hourly (
				bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model, input_tokens
			) VALUES (date_trunc('hour', now()), $1, $2, $3, $4, 'anthropic', $5, $6)
		`, testWorkspaceID, runtimeID, agentID, projectID, marker, 100*(i+1))
	}

	get := func(t *testing.T, h http.HandlerFunc, path, userID string) []map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		h(rec, asUser(newRequest(http.MethodGet, path+"?days=7&project_id="+projectID, nil), userID))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s as %s = %d: %s", path, userID, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		for _, agentID := range agentIDs {
			if strings.Contains(body, agentID) {
				t.Fatalf("%s: body names agent %s: %s", path, agentID, body)
			}
		}
		var rows []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return rows
	}
	// byRuntime sums `count` per runtime_id.
	byRuntime := func(rows []map[string]any, count string) map[string]float64 {
		out := map[string]float64{}
		for _, row := range rows {
			id, _ := row["runtime_id"].(string)
			v, _ := row[count].(float64)
			out[id] += v
		}
		return out
	}

	const folded = restrictedRuntimesRowID
	for _, ep := range []struct {
		path  string
		h     http.HandlerFunc
		count string
		want  map[string]map[string]float64 // viewer -> runtime_id -> count
	}{
		{"/api/dashboard/usage/by-runtime", testHandler.GetDashboardUsageByRuntime, "input_tokens", map[string]map[string]float64{
			"runtime owner":   {publicRT: 100, privateRT: 200, sharedRT: 300},
			"shared member":   {publicRT: 100, sharedRT: 300, folded: 200},
			"workspace admin": {publicRT: 100, folded: 500},
		}},
		{"/api/dashboard/runtime-duration", testHandler.GetDashboardRuntimeDuration, "task_count", map[string]map[string]float64{
			"runtime owner":   {publicRT: 1, privateRT: 1, sharedRT: 1, "": 1},
			"shared member":   {publicRT: 1, sharedRT: 1, folded: 1, "": 1},
			"workspace admin": {publicRT: 1, folded: 2, "": 1},
		}},
	} {
		t.Run(ep.path, func(t *testing.T) {
			for name, userID := range map[string]string{"runtime owner": testUserID, "shared member": member, "workspace admin": admin} {
				if got, want := byRuntime(get(t, ep.h, ep.path, userID), ep.count), ep.want[name]; !reflect.DeepEqual(got, want) {
					t.Errorf("%s: %v, want %v", name, got, want)
				}
			}
		})
	}
}
