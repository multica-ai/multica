package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateAutopilotTrigger_AcceptsCoalesceForRunOnlySchedule(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Coalesce Schedule Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "*/5 * * * *",
		"timezone":        "UTC",
		"overlap_policy":  "coalesce",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.OverlapPolicy != "coalesce" {
		t.Fatalf("overlap_policy: got %q want coalesce", resp.OverlapPolicy)
	}
}

func TestCreateAutopilotTrigger_RejectsCoalesceForCreateIssue(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Coalesce Create Issue Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "create_issue")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "*/5 * * * *",
		"overlap_policy":  "coalesce",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestCreateAutopilotTrigger_RejectsOverlapPolicyForWebhook(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Webhook Overlap Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":           "webhook",
		"overlap_policy": "coalesce",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilot_RequiresClearingCoalesceBeforeCreateIssueMode(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Coalesce Mode Guard Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	create := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "*/5 * * * *",
		"overlap_policy":  "coalesce",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(create, req)
	if create.Code != http.StatusCreated {
		t.Fatalf("create coalescing trigger: got %d body=%s", create.Code, create.Body.String())
	}
	var trigger AutopilotTriggerResponse
	if err := json.Unmarshal(create.Body.Bytes(), &trigger); err != nil {
		t.Fatalf("decode trigger: %v", err)
	}

	blocked := httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+apID, map[string]any{
		"execution_mode": "create_issue",
	})
	req = withURLParam(req, "id", apID)
	testHandler.UpdateAutopilot(blocked, req)
	if blocked.Code != http.StatusBadRequest {
		t.Fatalf("mode change with coalescing trigger: got %d body=%s", blocked.Code, blocked.Body.String())
	}

	clear := httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trigger.ID, map[string]any{
		"overlap_policy": "allow",
	})
	req = withURLParams(req, "id", apID, "triggerId", trigger.ID)
	testHandler.UpdateAutopilotTrigger(clear, req)
	if clear.Code != http.StatusOK {
		t.Fatalf("clear coalescing policy: got %d body=%s", clear.Code, clear.Body.String())
	}

	allowed := httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+apID, map[string]any{
		"execution_mode": "create_issue",
	})
	req = withURLParam(req, "id", apID)
	testHandler.UpdateAutopilot(allowed, req)
	if allowed.Code != http.StatusOK {
		t.Fatalf("mode change after clearing policy: got %d body=%s", allowed.Code, allowed.Body.String())
	}
}

func TestAutopilotSchedulePolicyMutationsPreserveCrossTableInvariant(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Coalesce Concurrent Guard Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	create := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots/"+apID+"/triggers", map[string]any{
		"kind":            "schedule",
		"cron_expression": "*/5 * * * *",
		"overlap_policy":  "allow",
	})
	req = withURLParam(req, "id", apID)
	testHandler.CreateAutopilotTrigger(create, req)
	if create.Code != http.StatusCreated {
		t.Fatalf("create allow trigger: got %d body=%s", create.Code, create.Body.String())
	}
	var trigger AutopilotTriggerResponse
	if err := json.Unmarshal(create.Body.Bytes(), &trigger); err != nil {
		t.Fatalf("decode trigger: %v", err)
	}

	// Hold the common parent lock until both requests have started. Once
	// released, the two handlers must serialize: either the mode change wins
	// and coalesce is rejected, or coalesce wins and the mode change is
	// rejected. They must never both commit.
	guard, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin guard transaction: %v", err)
	}
	defer guard.Rollback(context.Background())
	if _, err := guard.Exec(context.Background(), `SELECT id FROM autopilot WHERE id = $1 FOR UPDATE`, apID); err != nil {
		t.Fatalf("lock autopilot: %v", err)
	}

	started := make(chan struct{}, 2)
	results := make(chan int, 2)
	go func() {
		started <- struct{}{}
		w := httptest.NewRecorder()
		r := newRequest("PATCH", "/api/autopilots/"+apID, map[string]any{
			"execution_mode": "create_issue",
		})
		r = withURLParam(r, "id", apID)
		testHandler.UpdateAutopilot(w, r)
		results <- w.Code
	}()
	go func() {
		started <- struct{}{}
		w := httptest.NewRecorder()
		r := newRequest("PATCH", "/api/autopilots/"+apID+"/triggers/"+trigger.ID, map[string]any{
			"overlap_policy": "coalesce",
		})
		r = withURLParams(r, "id", apID, "triggerId", trigger.ID)
		testHandler.UpdateAutopilotTrigger(w, r)
		results <- w.Code
	}()
	<-started
	<-started
	time.Sleep(50 * time.Millisecond)
	if err := guard.Commit(context.Background()); err != nil {
		t.Fatalf("release guard lock: %v", err)
	}

	successes := 0
	rejections := 0
	for range 2 {
		switch code := <-results; code {
		case http.StatusOK:
			successes++
		case http.StatusBadRequest:
			rejections++
		default:
			t.Fatalf("unexpected concurrent mutation status: %d", code)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Fatalf("concurrent mutations: got %d success and %d rejection, want one each", successes, rejections)
	}

	var executionMode, overlapPolicy string
	if err := testPool.QueryRow(context.Background(), `
		SELECT a.execution_mode, t.overlap_policy
		FROM autopilot a
		JOIN autopilot_trigger t ON t.autopilot_id = a.id
		WHERE a.id = $1 AND t.id = $2
	`, apID, trigger.ID).Scan(&executionMode, &overlapPolicy); err != nil {
		t.Fatalf("read final invariant: %v", err)
	}
	if executionMode != "run_only" && overlapPolicy == "coalesce" {
		t.Fatalf("illegal final state: execution_mode=%s overlap_policy=%s", executionMode, overlapPolicy)
	}
}
