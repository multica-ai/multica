package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A private runtime is named in the Runtime view only to the users it is
// shared with. Everyone else, a workspace admin included, gets it folded onto
// restrictedRuntimesRowID with the totals intact.
func TestDashboardRuntimeViewFoldsUnreadableRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	marker := fmt.Sprintf("rtvis%d", time.Now().UnixNano())
	publicRT := dbfx.Runtime(t, marker+" public", testutil.Cols{"visibility": "public"})
	privateRT := dbfx.Runtime(t, marker+" private")
	member := createPlainMember(t, marker+"-member@multica.test")
	admin := createWorkspaceAdmin(t, marker+"-admin@multica.test")
	// The project keeps the restricted bucket free of other tests' runs.
	projectID := dbfx.Project(t, marker+" project")
	issueID := dbfx.Issue(t, marker+" issue", testutil.Cols{"project_id": projectID})
	dbfx.Cleanup(t, `DELETE FROM task_usage_hourly WHERE runtime_id IN ($1, $2)`, publicRT, privateRT)

	var agentIDs []string
	for i, runtimeID := range []string{publicRT, privateRT} {
		agentID := dbfx.Agent(t, fmt.Sprintf("%s agent %d", marker, i), runtimeID)
		agentIDs = append(agentIDs, agentID)
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id": runtimeID, "issue_id": issueID, "status": "completed",
			"started_at":   testutil.Raw("now() - interval '3 minutes'"),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
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

	for _, ep := range []struct {
		path  string
		h     http.HandlerFunc
		count string
		pub   float64
		priv  float64
	}{
		{"/api/dashboard/usage/by-runtime", testHandler.GetDashboardUsageByRuntime, "input_tokens", 100, 200},
		{"/api/dashboard/runtime-duration", testHandler.GetDashboardRuntimeDuration, "task_count", 1, 1},
	} {
		t.Run(ep.path, func(t *testing.T) {
			owner := byRuntime(get(t, ep.h, ep.path, testUserID), ep.count)
			if owner[publicRT] != ep.pub || owner[privateRT] != ep.priv || len(owner) != 2 {
				t.Fatalf("runtime owner: %v, want %s=%v and %s=%v only", owner, publicRT, ep.pub, privateRT, ep.priv)
			}
			for name, userID := range map[string]string{"member": member, "workspace admin": admin} {
				got := byRuntime(get(t, ep.h, ep.path, userID), ep.count)
				want := map[string]float64{publicRT: ep.pub, restrictedRuntimesRowID: ep.priv}
				if len(got) != len(want) || got[publicRT] != want[publicRT] || got[restrictedRuntimesRowID] != want[restrictedRuntimesRowID] {
					t.Fatalf("%s: %v, want %v (private runtime folded, totals kept)", name, got, want)
				}
			}
		})
	}
}
