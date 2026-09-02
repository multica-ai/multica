package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
)

func TestAutopilotQuotaManualAndWebhookEnforcement(t *testing.T) {
	workspaceUUID := uuid.MustParse(testWorkspaceID)
	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(17 * time.Hour)
	limit := 0
	stub := entitlementtest.New()
	stub.Set(workspaceUUID, entitlement.GateAutopilotRuns, entitlement.Decision{
		Gate: entitlement.Gate{
			Action: entitlement.ActionEnforce, Limit: &limit,
			PeriodStart: &start, PeriodEnd: &end, ResetAt: &end,
		},
		PolicyRevision: 3, SubscriptionVersion: 5,
	})
	priorProvider := testHandler.AutopilotService.Entitlements
	testHandler.AutopilotService.Entitlements = stub
	t.Cleanup(func() {
		testHandler.AutopilotService.Entitlements = priorProvider
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, testWorkspaceID)
	})

	agentID := createWebhookTestAgent(t, "Quota Handler Agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	manual := httptest.NewRecorder()
	manualReq := newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger", nil)
	manualReq.Header.Set("Idempotency-Key", "manual-over-limit")
	manualReq = withURLParam(manualReq, "id", autopilotID)
	testHandler.TriggerAutopilot(manual, manualReq)
	if manual.Code != http.StatusTooManyRequests {
		t.Fatalf("manual status = %d body=%s, want 429", manual.Code, manual.Body.String())
	}
	if manual.Header().Get("Retry-After") == "" {
		t.Fatal("manual quota refusal must include Retry-After")
	}
	var manualBody map[string]any
	if err := json.Unmarshal(manual.Body.Bytes(), &manualBody); err != nil || manualBody["reason_code"] != "quota_exceeded" {
		t.Fatalf("manual body = %#v, err=%v", manualBody, err)
	}

	trigger := createWebhookTriggerViaHandler(t, autopilotID)
	webhook := postWebhook(t, *trigger.WebhookToken, map[string]any{"event": "quota.test"}, nil)
	if webhook.Code != http.StatusOK {
		t.Fatalf("webhook status = %d body=%s, want 200 ignored", webhook.Code, webhook.Body.String())
	}
	var webhookBody map[string]any
	if err := json.Unmarshal(webhook.Body.Bytes(), &webhookBody); err != nil ||
		webhookBody["status"] != "ignored" || webhookBody["reason_code"] != "quota_exceeded" {
		t.Fatalf("webhook body = %#v, err=%v", webhookBody, err)
	}
	var runCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runCount); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if runCount != 0 {
		t.Fatalf("blocked manual/webhook requests created %d runs, want zero", runCount)
	}

	usageRecorder := httptest.NewRecorder()
	usageReq := newRequest("GET", "/api/autopilots/usage", nil)
	testHandler.GetAutopilotQuotaUsage(usageRecorder, usageReq)
	if usageRecorder.Code != http.StatusOK {
		t.Fatalf("usage status = %d body=%s, want 200", usageRecorder.Code, usageRecorder.Body.String())
	}
	var usage AutopilotQuotaUsageResponse
	if err := json.Unmarshal(usageRecorder.Body.Bytes(), &usage); err != nil {
		t.Fatalf("decode usage response: %v", err)
	}
	if usage.BlockedCounts["manual"] != 1 || usage.BlockedCounts["webhook"] != 1 {
		t.Fatalf("blocked counts = %#v, want one manual and one webhook", usage.BlockedCounts)
	}
}
