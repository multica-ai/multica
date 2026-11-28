package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// model-runtime keys rows by (provider, model) and counts a task once per row:
// the same model under two providers is two rows, a task that used two models
// counts in full on both, and case variants of one provider are one row.
func TestDashboardModelRunTimeDedup(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	marker := fmt.Sprintf("mrt%d", time.Now().UnixNano())
	agentID := dbfx.Agent(t, marker+" agent", dbfx.Runtime(t, marker+" runtime"))
	member := createPlainMember(t, marker+"-member@multica.test")
	projectID := dbfx.Project(t, marker+" project")
	issueID := dbfx.Issue(t, marker+" issue", testutil.Cols{"project_id": projectID})

	// minutes is each task's duration; usage lists its (provider, model) rows.
	for _, task := range []struct {
		status  string
		minutes int
		usage   [][2]string
	}{
		{"completed", 1, [][2]string{{"cursor", "auto"}, {"openai", "auto"}}},
		{"failed", 2, [][2]string{{"anthropic", marker + "-x"}, {"anthropic", marker + "-y"}}},
		{"cancelled", 3, [][2]string{{"Anthropic", marker + "-y"}, {"anthropic", marker + "-y"}}},
	} {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"issue_id": issueID, "status": task.status,
			"started_at":   testutil.Raw(fmt.Sprintf("now() - interval '%d minutes' - interval '1 minute'", task.minutes)),
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
		})
		for _, u := range task.usage {
			dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": taskID, "provider": u[0], "model": u[1], "input_tokens": 1})
		}
	}

	rec := httptest.NewRecorder()
	testHandler.GetDashboardModelRunTime(rec, asUser(newRequest(http.MethodGet,
		"/api/dashboard/model-runtime?days=7&project_id="+projectID, nil), member))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), agentID) {
		t.Fatalf("body names the private agent %s: %s", agentID, rec.Body.String())
	}
	var got []DashboardModelRunTimeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sort.Slice(got, func(i, j int) bool {
		return got[i].Provider+"/"+got[i].Model < got[j].Provider+"/"+got[j].Model
	})
	want := []DashboardModelRunTimeResponse{
		{Provider: "anthropic", Model: marker + "-x", TotalSeconds: 120, TaskCount: 1, FailedCount: 1},
		{Provider: "anthropic", Model: marker + "-y", TotalSeconds: 300, TaskCount: 2, FailedCount: 1, CancelledCount: 1},
		{Provider: "cursor", Model: "auto", TotalSeconds: 60, TaskCount: 1},
		{Provider: "openai", Model: "auto", TotalSeconds: 60, TaskCount: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model-runtime =\n%+v\nwant\n%+v", got, want)
	}
}
