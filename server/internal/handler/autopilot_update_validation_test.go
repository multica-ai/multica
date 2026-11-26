package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// An edit must reject the values a create rejects. Unchecked status and
// execution_mode values only failed at the table's CHECK constraint (500),
// and a blank title was stored as an autopilot with no name.
func TestUpdateAutopilot_RejectsInvalidFields(t *testing.T) {
	agentID := createWebhookTestAgent(t, "UpdateValidation Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	for name, body := range map[string]map[string]any{
		"unknown status":         {"status": "pause"},
		"unknown execution mode": {"execution_mode": "bogus"},
		"empty title":            {"title": ""},
		"blank title":            {"title": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			req := newRequest("PATCH", "/api/autopilots/"+apID+"?workspace_id="+testWorkspaceID, body)
			testutil.Call(t, testHandler.UpdateAutopilot, withURLParam(req, "id", apID)).Want(http.StatusBadRequest)
		})
	}

	var title, status, mode string
	if err := testPool.QueryRow(context.Background(),
		`SELECT title, status, execution_mode FROM autopilot WHERE id = $1`, apID).Scan(&title, &status, &mode); err != nil {
		t.Fatalf("read autopilot: %v", err)
	}
	if strings.TrimSpace(title) == "" || status != "active" || mode != "run_only" {
		t.Fatalf("autopilot changed by rejected edits: title=%q status=%q mode=%q", title, status, mode)
	}
}

func TestCreateAutopilot_RejectsBlankTitle(t *testing.T) {
	agentID := createWebhookTestAgent(t, "CreateBlankTitle Agent")
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "   ",
		"assignee_id":    agentID,
		"execution_mode": "run_only",
	})
	testutil.Call(t, testHandler.CreateAutopilot, req).Want(http.StatusBadRequest)
}

// Clearing a schedule trigger's cron left it enabled with its old
// next_run_at: the list kept showing a next run the scheduler would never
// pick up, since it skips triggers without a cron.
func TestUpdateAutopilotTrigger_RejectsEmptyCronOnSchedule(t *testing.T) {
	agentID := createWebhookTestAgent(t, "EmptyCron Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createScheduleTriggerViaHandler(t, apID, "0 * * * *")

	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{"cron_expression": ""})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusBadRequest)

	if storedTriggerNextRunIsNull(t, trig.ID) {
		t.Fatal("rejected edit cleared the trigger's next run")
	}
}
