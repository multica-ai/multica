package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The request usage sums runs and tokens over the request and its direct
// children only. An unrelated issue's run is not counted.
func TestGetCompanyRequestUsage_SumsRequestAndChildrenOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	e := newInquiryEnv(t)
	secretary := e.Secretary
	parent := e.issue(t, e.SquadID, "Usage request", "todo")
	child := e.issue(t, e.SquadID, "Usage child", "in_progress")
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $1 WHERE id = $2`, parent, child)
	unrelated := e.issue(t, e.SquadID, "Unrelated", "in_progress")

	addRun := func(issueID string, input, output, cost int64) {
		taskID := dbfx.Task(t, secretary, testutil.Cols{"status": "completed", "runtime_id": e.RuntimeID, "issue_id": issueID})
		dbfx.Insert(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "claude", "model": "m",
			"input_tokens": input, "output_tokens": output, "cache_read_tokens": 0, "cache_write_tokens": 0,
			"cost_usd_ticks": cost,
		})
	}
	addRun(parent, 10, 5, 100)
	addRun(child, 20, 7, 300)
	addRun(unrelated, 999, 999, 9999)

	req := newRequest("GET", "/api/company/requests/"+parent+"/usage", nil)
	req.Header.Set("X-Workspace-ID", e.WorkspaceID)
	req = withURLParam(req, "id", parent)
	var out struct {
		RunCount     int64 `json:"run_count"`
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
		CostUsdTicks int64 `json:"cost_usd_ticks"`
	}
	testutil.Call(t, testHandler.GetCompanyRequestUsage, req).Want(http.StatusOK).JSON(&out)

	if out.RunCount != 2 || out.InputTokens != 30 || out.OutputTokens != 12 || out.CostUsdTicks != 400 {
		t.Fatalf("usage = %+v, want runs 2, input 30, output 12, cost 400", out)
	}
}
