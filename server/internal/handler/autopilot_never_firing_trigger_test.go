package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A cron that parses but never fires ("0 0 30 2 *") can be saved on purpose:
// the preview endpoint answers it with an empty list instead of a 400. robfig
// reports "no next occurrence" as the zero time, and storing that as
// next_run_at made the trigger read "next run: year 0001" and, through the
// list's min(next_run_at), hid the real next run of every sibling trigger.

func createScheduleTriggerViaHandler(t *testing.T, autopilotID, cron string) AutopilotTriggerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers", map[string]any{
		"kind": "schedule", "cron_expression": cron, "timezone": "UTC",
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func storedTriggerNextRunIsNull(t *testing.T, triggerID string) bool {
	t.Helper()
	var isNull bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT next_run_at IS NULL FROM autopilot_trigger WHERE id = $1`, triggerID).Scan(&isNull); err != nil {
		t.Fatalf("read next_run_at: %v", err)
	}
	return isNull
}

func TestCreateAutopilotTrigger_NeverFiringCronHasNoNextRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "NeverFiring Create Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	never := createScheduleTriggerViaHandler(t, apID, "0 0 30 2 *")
	if never.NextRunAt != nil {
		t.Errorf("next_run_at = %q, want null for a cron that never fires", *never.NextRunAt)
	}
	if !storedTriggerNextRunIsNull(t, never.ID) {
		t.Error("stored next_run_at is not NULL for a cron that never fires")
	}

	// The list reports the autopilot's real next run, not the sibling's
	// missing one.
	hourly := createScheduleTriggerViaHandler(t, apID, "0 * * * *")
	if hourly.NextRunAt == nil {
		t.Fatal("an hourly trigger must have a next run")
	}
	w := httptest.NewRecorder()
	testHandler.ListAutopilots(w, newRequest("GET", "/api/autopilots?workspace_id="+testWorkspaceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAutopilots: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Autopilots []struct {
			ID        string  `json:"id"`
			NextRunAt *string `json:"next_run_at"`
		} `json:"autopilots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, a := range list.Autopilots {
		if a.ID != apID {
			continue
		}
		if a.NextRunAt == nil {
			t.Fatal("list next_run_at is null, want the hourly trigger's next run")
		}
		got, err := time.Parse(time.RFC3339, *a.NextRunAt)
		if err != nil {
			t.Fatalf("parse list next_run_at %q: %v", *a.NextRunAt, err)
		}
		want, err := time.Parse(time.RFC3339, *hourly.NextRunAt)
		if err != nil {
			t.Fatalf("parse trigger next_run_at %q: %v", *hourly.NextRunAt, err)
		}
		if !got.Equal(want) {
			t.Errorf("list next_run_at = %s, want the hourly trigger's %s", got, want)
		}
		return
	}
	t.Fatalf("autopilot %s missing from the list", apID)
}

func TestUpdateAutopilotTrigger_NeverFiringCronClearsNextRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "NeverFiring Update Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	trig := createScheduleTriggerViaHandler(t, apID, "0 * * * *")
	if storedTriggerNextRunIsNull(t, trig.ID) {
		t.Fatal("an hourly trigger must start with a next run")
	}

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trig.ID, map[string]any{"cron_expression": "0 0 30 2 *"})
	req = withURLParams(req, "id", apID, "triggerId", trig.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilotTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NextRunAt != nil {
		t.Errorf("next_run_at = %q, want null after switching to a cron that never fires", *resp.NextRunAt)
	}
	if !storedTriggerNextRunIsNull(t, trig.ID) {
		t.Error("stored next_run_at is not NULL after switching to a cron that never fires")
	}
}
