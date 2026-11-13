package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestReportTaskUsageStoresCreditsSeparatelyFromTokens(t *testing.T) {
	issueID := dbfx.Issue(t, "credits usage")
	agentID := dbfx.Agent(t, "credits usage agent", testRuntimeID)
	creditsTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running"})
	tokenTask := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": testRuntimeID, "status": "running"})
	report := func(taskID string, entry map[string]any) {
		t.Helper()
		req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/usage",
			map[string]any{"usage": []map[string]any{entry}}), "taskId", taskID)
		testutil.Call(t, testHandler.ReportTaskUsage, req).Want(http.StatusOK)
	}
	report(creditsTask, map[string]any{"provider": "qoder_cloud", "model": "", "credits_micros": 120000})
	// A retried terminal report replaces the figure instead of adding to it.
	report(creditsTask, map[string]any{"provider": "qoder_cloud", "model": "", "credits_micros": 350000})
	report(tokenTask, map[string]any{"provider": "anthropic", "model": "claude-opus-5", "input_tokens": 10, "credits_micros": -1})

	req := withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil), "id", issueID)
	var runs []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&runs)
	usage := map[string][]TaskUsageData{}
	for _, run := range runs {
		usage[run.ID] = run.Usage
	}
	credits := usage[creditsTask]
	if len(credits) != 1 || credits[0].Provider != "qoder_cloud" || credits[0].CreditsMicros == nil || *credits[0].CreditsMicros != 350000 {
		t.Fatalf("credits task usage = %+v, want one qoder_cloud row with 350000 credits_micros", credits)
	}
	if credits[0].InputTokens != 0 || credits[0].CostUsdTicks != nil {
		t.Fatalf("credits row gained tokens or USD cost: %+v", credits[0])
	}
	tokens := usage[tokenTask]
	if len(tokens) != 1 || tokens[0].CreditsMicros != nil {
		t.Fatalf("token task usage = %+v, want no credits", tokens)
	}
}
