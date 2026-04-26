package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAutopilotCreateUpdateTriggerRunsAndDelete(t *testing.T) {
	ctx := context.Background()
	agentID := handlerTestAgentID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots", map[string]any{
		"title":                "Autopilot handler test",
		"description":          "Create the follow-up issue from the autopilot test.",
		"mode":                 "create_issue",
		"agent_id":             agentID,
		"priority":             "high",
		"issue_title_template": "{{autopilot.title}} - {{source}}",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AutopilotResponse
	mustDecodeJSON(t, w, &created)
	if created.Mode != "create_issue" {
		t.Fatalf("CreateAutopilot: expected mode create_issue, got %q", created.Mode)
	}
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, created.ID)

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID, map[string]any{
		"status":   "paused",
		"priority": "low",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated AutopilotResponse
	mustDecodeJSON(t, w, &updated)
	if updated.Status != "paused" || updated.Priority != "low" {
		t.Fatalf("UpdateAutopilot: expected paused/low, got %s/%s", updated.Status, updated.Priority)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/autopilots/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots/"+created.ID+"/trigger", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.TriggerAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("TriggerAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var run AutopilotRunResponse
	mustDecodeJSON(t, w, &run)
	if run.Status != "succeeded" {
		t.Fatalf("TriggerAutopilot: expected succeeded run, got %s", run.Status)
	}
	if run.CreatedIssueID == nil || run.CreatedTaskID == nil {
		t.Fatalf("TriggerAutopilot: expected created issue and task IDs, got issue=%v task=%v", run.CreatedIssueID, run.CreatedTaskID)
	}
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, *run.CreatedIssueID)

	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(*run.CreatedIssueID))
	if err != nil {
		t.Fatalf("created issue not found: %v", err)
	}
	if issue.Title != "Autopilot handler test - manual" {
		t.Fatalf("created issue title = %q", issue.Title)
	}
	if issue.Priority != "low" {
		t.Fatalf("created issue priority = %q", issue.Priority)
	}
	if issue.AssigneeType.String != "agent" || uuidToString(issue.AssigneeID) != agentID {
		t.Fatalf("created issue assignee = %s/%s", issue.AssigneeType.String, uuidToString(issue.AssigneeID))
	}

	var taskCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE id = $1 AND issue_id = $2 AND agent_id = $3 AND status = 'queued'
	`, *run.CreatedTaskID, *run.CreatedIssueID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count queued task: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("expected one queued task, got %d", taskCount)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/autopilots/"+created.ID+"/runs", nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.ListAutopilotRuns(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAutopilotRuns: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var runsResp struct {
		Runs []AutopilotRunResponse `json:"runs"`
	}
	mustDecodeJSON(t, w, &runsResp)
	if len(runsResp.Runs) == 0 || runsResp.Runs[0].ID != run.ID {
		t.Fatalf("ListAutopilotRuns: expected latest run %s, got %+v", run.ID, runsResp.Runs)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/autopilots/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteAutopilot(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilot: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/autopilots/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetAutopilot(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetAutopilot after delete: expected 404, got %d", w.Code)
	}
}

func TestAutopilotListEmptyWorkspace(t *testing.T) {
	ctx := context.Background()
	slug := "autopilot-empty-" + time.Now().Format("20060102150405.000000000")
	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Autopilot Empty", slug, "Temporary empty autopilot workspace", "AEL").Scan(&workspaceID); err != nil {
		t.Fatalf("create empty workspace: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/autopilots?workspace_id="+workspaceID, nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	testHandler.ListAutopilots(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAutopilots empty workspace: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Autopilots []AutopilotResponse `json:"autopilots"`
		Total      int64               `json:"total"`
		HasMore    bool                `json:"has_more"`
	}
	mustDecodeJSON(t, w, &resp)
	if len(resp.Autopilots) != 0 || resp.Total != 0 || resp.HasMore {
		t.Fatalf("ListAutopilots empty workspace: unexpected response %+v", resp)
	}
}

func TestAutopilotValidation(t *testing.T) {
	agentID := handlerTestAgentID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots", map[string]any{
		"title":    "Unsupported autopilot mode",
		"mode":     "run_only",
		"agent_id": agentID,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilot run_only: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	archivedAgentID := createArchivedHandlerTestAgent(t)
	defer testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, archivedAgentID)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots", map[string]any{
		"title":    "Archived agent autopilot",
		"mode":     "create_issue",
		"agent_id": archivedAgentID,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateAutopilot archived agent: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotTriggerCRUDAndValidation(t *testing.T) {
	created := createAutopilotForTest(t, "Autopilot trigger CRUD", "active")
	defer testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, created.ID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+created.ID+"/triggers", map[string]any{
		"type":     "schedule",
		"cron":     "not a cron",
		"timezone": "UTC",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilotTrigger invalid cron: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots/"+created.ID+"/triggers", map[string]any{
		"type":     "schedule",
		"cron":     "*/5 * * * *",
		"timezone": "Mars/Base",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateAutopilotTrigger invalid timezone: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/autopilots/"+created.ID+"/triggers", map[string]any{
		"type":     "schedule",
		"label":    "Every five minutes",
		"cron":     "*/5 * * * *",
		"timezone": "Europe/Istanbul",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var trigger AutopilotTriggerResponse
	mustDecodeJSON(t, w, &trigger)
	if trigger.NextRunAt == nil || trigger.Cron == nil || *trigger.Cron != "*/5 * * * *" {
		t.Fatalf("CreateAutopilotTrigger: unexpected trigger response: %+v", trigger)
	}

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID+"/triggers/"+trigger.ID, map[string]any{
		"cron":     "0 * * * *",
		"timezone": "UTC",
		"status":   "paused",
	})
	req = withURLParam(req, "id", created.ID)
	req = withURLParam(req, "triggerId", trigger.ID)
	testHandler.UpdateAutopilotTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAutopilotTrigger: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotTriggerResponse
	mustDecodeJSON(t, w, &updated)
	if updated.Status != "paused" || updated.Timezone != "UTC" || updated.Cron == nil || *updated.Cron != "0 * * * *" {
		t.Fatalf("UpdateAutopilotTrigger: unexpected response: %+v", updated)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/autopilots/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAutopilot with triggers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched AutopilotResponse
	mustDecodeJSON(t, w, &fetched)
	if len(fetched.Triggers) != 1 || fetched.Triggers[0].ID != trigger.ID {
		t.Fatalf("GetAutopilot: expected one trigger %s, got %+v", trigger.ID, fetched.Triggers)
	}

	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/autopilots/"+created.ID+"/triggers/"+trigger.ID, nil)
	req = withURLParam(req, "id", created.ID)
	req = withURLParam(req, "triggerId", trigger.ID)
	testHandler.DeleteAutopilotTrigger(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteAutopilotTrigger: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutopilotSchedulerDueClaimAndIdempotency(t *testing.T) {
	ctx := context.Background()
	created := createAutopilotForTest(t, "Autopilot schedule due", "active")
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, created.ID)
	trigger := createScheduleTriggerForTest(t, created.ID, "* * * * *", "UTC", "active")
	dueAt := time.Now().Add(-1 * time.Minute).UTC().Truncate(time.Microsecond)
	if _, err := testPool.Exec(ctx, `UPDATE autopilot_trigger SET next_run_at = $1 WHERE id = $2`, dueAt, trigger.ID); err != nil {
		t.Fatalf("make trigger due: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := testHandler.AutopilotService.ProcessDueSchedules(ctx, time.Now(), 10)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ProcessDueSchedules: %v", err)
		}
	}

	var runCount int
	var status string
	var createdIssueID *string
	var createdTaskID *string
	if err := testPool.QueryRow(ctx, `
		SELECT count(*), max(status), max(created_issue_id::text), max(created_task_id::text)
		FROM autopilot_run
		WHERE trigger_id = $1 AND source = 'schedule'
	`, trigger.ID).Scan(&runCount, &status, &createdIssueID, &createdTaskID); err != nil {
		t.Fatalf("count scheduled runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected one scheduled run, got %d", runCount)
	}
	if status != "succeeded" || createdIssueID == nil || createdTaskID == nil {
		t.Fatalf("expected succeeded run with issue/task, got status=%s issue=%v task=%v", status, createdIssueID, createdTaskID)
	}
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, *createdIssueID)

	var nextRunAt time.Time
	var lastRunAt time.Time
	if err := testPool.QueryRow(ctx, `
		SELECT next_run_at, last_run_at FROM autopilot_trigger WHERE id = $1
	`, trigger.ID).Scan(&nextRunAt, &lastRunAt); err != nil {
		t.Fatalf("load advanced trigger: %v", err)
	}
	if !lastRunAt.Equal(dueAt) {
		t.Fatalf("expected last_run_at %s, got %s", dueAt, lastRunAt)
	}
	if !nextRunAt.After(time.Now().Add(-5 * time.Second)) {
		t.Fatalf("expected next_run_at to advance, got %s", nextRunAt)
	}
}

func TestAutopilotSchedulerSkipsPausedAndRecordsFailure(t *testing.T) {
	ctx := context.Background()
	paused := createAutopilotForTest(t, "Paused schedule", "paused")
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, paused.ID)
	pausedTrigger := createScheduleTriggerForTest(t, paused.ID, "* * * * *", "UTC", "active")
	if _, err := testPool.Exec(ctx, `UPDATE autopilot_trigger SET next_run_at = now() - interval '1 minute' WHERE id = $1`, pausedTrigger.ID); err != nil {
		t.Fatalf("make paused trigger due: %v", err)
	}
	runs, err := testHandler.AutopilotService.ProcessDueSchedules(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("ProcessDueSchedules paused: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("expected paused autopilot to be skipped, got %d runs", len(runs))
	}

	failing := createAutopilotForTest(t, "Failing schedule", "active")
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, failing.ID)
	failTrigger := createScheduleTriggerForTest(t, failing.ID, "* * * * *", "UTC", "active")
	if _, err := testPool.Exec(ctx, `UPDATE autopilot_trigger SET next_run_at = now() - interval '1 minute' WHERE id = $1`, failTrigger.ID); err != nil {
		t.Fatalf("make failing trigger due: %v", err)
	}
	agentID := handlerTestAgentID(t)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now(), archived_by = $2 WHERE id = $1`, agentID, testUserID); err != nil {
		t.Fatalf("archive handler test agent: %v", err)
	}
	defer testPool.Exec(ctx, `UPDATE agent SET archived_at = NULL, archived_by = NULL WHERE id = $1`, agentID)

	runs, err = testHandler.AutopilotService.ProcessDueSchedules(ctx, time.Now(), 10)
	if err == nil {
		t.Fatal("expected ProcessDueSchedules to return run failure")
	}
	if len(runs) != 1 || runs[0].Status != "failed" {
		t.Fatalf("expected one failed run, got runs=%+v err=%v", runs, err)
	}
	if !runs[0].Error.Valid || runs[0].Error.String == "" {
		t.Fatalf("expected failed run error to be recorded, got %+v", runs[0].Error)
	}
	if runs[0].CreatedIssueID.Valid {
		defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, uuidToString(runs[0].CreatedIssueID))
	}
}

func createAutopilotForTest(t *testing.T, title, status string) AutopilotResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots", map[string]any{
		"title":    title,
		"status":   status,
		"mode":     "create_issue",
		"agent_id": handlerTestAgentID(t),
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot helper: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	mustDecodeJSON(t, w, &created)
	return created
}

func createScheduleTriggerForTest(t *testing.T, autopilotID, cronExpr, timezone, status string) AutopilotTriggerResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+autopilotID+"/triggers", map[string]any{
		"type":     "schedule",
		"cron":     cronExpr,
		"timezone": timezone,
		"status":   status,
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger helper: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var trigger AutopilotTriggerResponse
	mustDecodeJSON(t, w, &trigger)
	return trigger
}

func handlerTestAgentID(t *testing.T) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent
		WHERE workspace_id = $1 AND name = 'Handler Test Agent'
		LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load handler test agent: %v", err)
	}
	return agentID
}

func createArchivedHandlerTestAgent(t *testing.T) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT runtime_id FROM agent WHERE id = $1
	`, handlerTestAgentID(t)).Scan(&runtimeID); err != nil {
		t.Fatalf("load handler test runtime: %v", err)
	}
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id, archived_at, archived_by
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, now(), $4)
		RETURNING id
	`, testWorkspaceID, "Archived Handler Test Agent", runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create archived agent: %v", err)
	}
	return agentID
}

func mustDecodeJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decode JSON response: %v; body=%s", err, w.Body.String())
	}
}
