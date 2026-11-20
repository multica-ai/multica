package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// A member pressing "Run now" is a run a person started, which is never
// metered. The same press made by an agent on a member's behalf, and every
// webhook delivery, count and are refused once the period is full.
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
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`, autopilotID)
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)

	memberReq := newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger", nil)
	memberReq.Header.Set("Idempotency-Key", "member-over-limit")
	memberReq = withURLParam(memberReq, "id", autopilotID)
	var memberRun AutopilotRunResponse
	testutil.Call(t, testHandler.TriggerAutopilot, memberReq).Want(http.StatusOK).JSON(&memberRun)
	if memberRun.Status == "skipped" {
		t.Fatalf("member run now was skipped (reason_code %v), want it admitted past a full quota", derefStr(memberRun.ReasonCode))
	}
	var reservationID pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `SELECT quota_reservation_id FROM autopilot_run WHERE id = $1`, memberRun.ID).Scan(&reservationID); err != nil {
		t.Fatalf("load member run: %v", err)
	}
	if reservationID.Valid {
		t.Fatal("member run now reserved quota, want an unmetered run")
	}
	// Retrying the same request reuses the run instead of executing twice.
	retryReq := newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger", nil)
	retryReq.Header.Set("Idempotency-Key", "member-over-limit")
	retryReq = withURLParam(retryReq, "id", autopilotID)
	var retriedRun AutopilotRunResponse
	testutil.Call(t, testHandler.TriggerAutopilot, retryReq).Want(http.StatusOK).JSON(&retriedRun)
	if retriedRun.ID != memberRun.ID {
		t.Fatalf("retried run now = %s, want the original run %s", retriedRun.ID, memberRun.ID)
	}
	var taskCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`, memberRun.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count member run tasks: %v", err)
	}
	if taskCount != 1 {
		t.Fatalf("member run has %d tasks after a retry, want 1", taskCount)
	}

	agentResp := triggerAsAgent(t, autopilotID, agentID, callerTask(t, agentID, testUserID))
	agentResp.Want(http.StatusTooManyRequests)
	if agentResp.Header().Get("Retry-After") == "" {
		t.Fatal("agent quota refusal must include Retry-After")
	}
	var agentBody map[string]any
	agentResp.JSON(&agentBody)
	if agentBody["reason_code"] != "quota_exceeded" {
		t.Fatalf("agent body = %#v, want quota_exceeded", agentBody)
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
	if runCount != 1 {
		t.Fatalf("autopilot has %d runs, want only the member's unmetered run", runCount)
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
		t.Fatalf("blocked counts = %#v, want the agent's manual run and one webhook", usage.BlockedCounts)
	}
	if usage.Used == nil || usage.Reserved == nil || *usage.Used != 0 || *usage.Reserved != 0 {
		t.Fatalf("usage used=%v reserved=%v, want nothing counted", usage.Used, usage.Reserved)
	}
}

// A request key is one request whichever path admitted it. Before member presses
// were unmetered, every "Run now" went through the metered path and kept its key
// only on the quota reservation, so a retry that reaches an upgraded server must
// still find it. The reverse holds too: an agent retry of a key a member's
// unmetered run already used must not start a second run.
func TestAutopilotQuotaRequestKeySharedAcrossMeteredAndUnmeteredRuns(t *testing.T) {
	for _, action := range []entitlement.Action{entitlement.ActionObserve, entitlement.ActionEnforce} {
		t.Run(string(action), func(t *testing.T) {
			workspaceUUID := uuid.MustParse(testWorkspaceID)
			start := time.Now().UTC().Truncate(time.Second)
			end := start.Add(17 * time.Hour)
			limit := 5
			stub := entitlementtest.New()
			stub.Set(workspaceUUID, entitlement.GateAutopilotRuns, entitlement.Decision{
				Gate: entitlement.Gate{
					Action: action, Limit: &limit,
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

			agentID := createWebhookTestAgent(t, "Quota Key Agent "+string(action))
			autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE autopilot_id = $1)`, autopilotID)
			dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
			taskID := callerTask(t, agentID, testUserID)

			asMember := func(key string) AutopilotRunResponse {
				t.Helper()
				r := newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger", nil)
				r.Header.Set("Idempotency-Key", key)
				r = withURLParam(r, "id", autopilotID)
				var run AutopilotRunResponse
				testutil.Call(t, testHandler.TriggerAutopilot, r).Want(http.StatusOK).JSON(&run)
				return run
			}
			asAgent := func(key string) AutopilotRunResponse {
				t.Helper()
				r := newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger?workspace_id="+testWorkspaceID, nil)
				r.Header.Set("Idempotency-Key", key)
				r.Header.Set("X-Actor-Source", "task_token")
				r.Header.Set("X-Agent-ID", agentID)
				r.Header.Set("X-Task-ID", taskID)
				r = withURLParam(r, "id", autopilotID)
				var run AutopilotRunResponse
				testutil.Call(t, testHandler.TriggerAutopilot, r).Want(http.StatusOK).JSON(&run)
				return run
			}
			tasksFor := func(runID string) int {
				t.Helper()
				var n int
				if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id = $1`, runID).Scan(&n); err != nil {
					t.Fatalf("count tasks: %v", err)
				}
				return n
			}

			metered := asAgent("metered-first")
			if retried := asMember("metered-first"); retried.ID != metered.ID {
				t.Fatalf("member retry of a metered request = run %s, want the original run %s", retried.ID, metered.ID)
			}
			if n := tasksFor(metered.ID); n != 1 {
				t.Fatalf("metered request has %d tasks after a member retry, want 1", n)
			}

			unmetered := asMember("unmetered-first")
			if retried := asAgent("unmetered-first"); retried.ID != unmetered.ID {
				t.Fatalf("agent retry of an unmetered request = run %s, want the original run %s", retried.ID, unmetered.ID)
			}
			if n := tasksFor(unmetered.ID); n != 1 {
				t.Fatalf("unmetered request has %d tasks after an agent retry, want 1", n)
			}

			var runs int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runs); err != nil {
				t.Fatalf("count runs: %v", err)
			}
			if runs != 2 {
				t.Fatalf("autopilot has %d runs, want 2", runs)
			}
			var reserved int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM autopilot_quota_reservation WHERE workspace_id = $1 AND state <> 'released'`, testWorkspaceID).Scan(&reserved); err != nil {
				t.Fatalf("count reservations: %v", err)
			}
			if reserved != 1 {
				t.Fatalf("workspace holds %d reservations, want only the agent's metered run", reserved)
			}
		})
	}
}
