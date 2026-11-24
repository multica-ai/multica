package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// newAppsQuotaFixture meters the fixture's gateway with the real app call
// quota under an enforced limit.
func newAppsQuotaFixture(t *testing.T, limit int) (*appsFixture, *entitlementtest.Stub) {
	t.Helper()
	if testHandler == nil {
		t.Skip("database not available")
	}
	stub := entitlementtest.New()
	start := time.Now().UTC().Truncate(time.Second)
	end := start.AddDate(0, 1, 0)
	stub.Set(uuid.MustParse(testWorkspaceID), entitlement.GateAppToolCalls, entitlement.Decision{Gate: entitlement.Gate{
		Action: entitlement.ActionEnforce, Limit: &limit, PeriodStart: &start, PeriodEnd: &end, ResetAt: &end,
		Notifications: &entitlement.NotificationPolicy{OnRejection: entitlement.NotificationFirstRejectionPerLimit},
	}})
	f := newAppsFixture(t, func(cfg *connectedapps.Config) {
		cfg.CallQuota = service.AppToolCallQuota{Queries: testHandler.Queries, Entitlements: stub, TxStarter: testPool}
	})
	f.h.Entitlements = stub
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM app_tool_call_quota_period WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM entitlement_limit_overage WHERE workspace_id = $1 AND gate = 'app_tool_calls'`, testWorkspaceID)
	})
	return f, stub
}

func (f *appsFixture) callOutcomes(t *testing.T) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT outcome FROM app_tool_call WHERE workspace_id = $1 AND task_id = $2 ORDER BY created_at`, testWorkspaceID, f.taskID)
	if err != nil {
		t.Fatalf("list calls: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var outcome string
		rows.Scan(&outcome)
		out = append(out, outcome)
	}
	return out
}

// A call past the monthly limit never reaches Composio; the agent is told
// why, the call is logged as quota_exceeded, and meta tools stay free.
func TestConnectedApps_MonthlyQuotaRefusesGatewayCalls(t *testing.T) {
	f, _ := newAppsQuotaFixture(t, 1)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	run := map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}}

	if _, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", run); isErr {
		t.Fatalf("call within the limit = %s", text)
	}
	for _, meta := range []string{"apps_list", "apps_search_tools", "apps_get_tool"} {
		f.gateway(t, f.token(t), meta, map[string]any{"app": "gmail", "tool": "GMAIL_FETCH_EMAILS"})
	}
	_, text, isErr := f.gateway(t, f.token(t), "apps_run_tool", run)
	if !isErr || !strings.HasPrefix(text, "app call limit reached") || !strings.Contains(text, "used 1 of its 1") {
		t.Fatalf("call past the limit = %s (error %v)", text, isErr)
	}
	if n := len(f.sdk.executions()); n != 1 {
		t.Fatalf("executions = %d, want only the call within the limit", n)
	}
	if got := f.callOutcomes(t); len(got) != 2 || got[0] != "ok" || got[1] != "quota_exceeded" {
		t.Fatalf("call log = %v, want [ok quota_exceeded]", got)
	}
	// The executed call carries the period the counter charged; the refused
	// one none.
	var counted, uncounted int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE p.period_start IS NOT NULL), count(*) FILTER (WHERE c.metered_period_start IS NULL)
		FROM app_tool_call c
		LEFT JOIN app_tool_call_quota_period p ON p.workspace_id = c.workspace_id AND p.period_start = c.metered_period_start
		WHERE c.task_id = $1`, f.taskID).Scan(&counted, &uncounted); err != nil || counted != 1 || uncounted != 1 {
		t.Fatalf("metered periods: %d counted, %d uncounted (err %v); want 1 and 1", counted, uncounted, err)
	}
	if n := f.inboxCount(t, testUserID, service.AppToolCallQuotaNoticeType); n != 1 {
		t.Fatalf("owner notices = %d, want 1", n)
	}
}

// A confirmed call approved after the workspace ran out fails the request
// instead of running it.
func TestConnectedApps_MonthlyQuotaRefusesApprovedCalls(t *testing.T) {
	f, _ := newAppsQuotaFixture(t, 0)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "confirm_writes"}})
	request := f.legacyRequest(t, "personal", "gmail", "GMAIL_SEND_EMAIL", "send", map[string]any{"to": "x"}, "")

	out := f.approve(t, request.ID, nil)
	if out.Status != "failed" || out.Error == nil || !strings.HasPrefix(*out.Error, "app call limit reached") {
		t.Fatalf("approved over the limit = %+v", out)
	}
	if n := len(f.sdk.executions()); n != 0 {
		t.Fatalf("executions = %d, want none", n)
	}
	if got := f.callOutcomes(t); len(got) != 1 || got[0] != "quota_exceeded" {
		t.Fatalf("call log = %v, want [quota_exceeded]", got)
	}
}

// Billing reads the period's count and, for owners and admins, who made it.
// An approved call that fails its checks before running never reached
// Composio, so it is not in the breakdown either.
func TestGetAppCallUsage(t *testing.T) {
	f, _ := newAppsQuotaFixture(t, 5)
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal", "level": "write"}})
	f.gateway(t, f.token(t), "apps_run_tool", map[string]any{"tool": "GMAIL_FETCH_EMAILS", "arguments": map[string]any{}})
	request := f.legacyRequest(t, "personal", "gmail", "GMAIL_SEND_EMAIL", "send", map[string]any{"to": "x"}, "")
	testPool.Exec(context.Background(), `DELETE FROM user_composio_connection WHERE user_id = $1 AND toolkit_slug = 'gmail'`, testUserID)
	if out := f.approve(t, request.ID, nil); out.Status != "failed" {
		t.Fatalf("approved call without an account = %+v, want failed", out)
	}
	if got := f.callOutcomes(t); len(got) != 2 || got[1] != "error" {
		t.Fatalf("call log = %v, want the run and the failed approval", got)
	}

	path := "/api/workspaces/" + testWorkspaceID + "/app-call-usage"
	var owner AppCallUsageResponse
	testutil.Call(t, f.h.GetAppCallUsage, withURLParam(newRequestAs(testUserID, http.MethodGet, path, nil), "id", testWorkspaceID)).
		Want(http.StatusOK).JSON(&owner)
	if owner.Action != "enforce" || owner.Used == nil || *owner.Used != 1 || owner.Limit == nil || *owner.Limit != 5 ||
		owner.Reached == nil || *owner.Reached || owner.ResetAt == nil ||
		len(owner.Members) != 1 || owner.Members[0].UserID == nil || *owner.Members[0].UserID != testUserID || owner.Members[0].Calls != 1 {
		t.Fatalf("owner usage = %+v", owner)
	}

	var member AppCallUsageResponse
	testutil.Call(t, f.h.GetAppCallUsage, withURLParam(newRequestAs(f.member, http.MethodGet, path, nil), "id", testWorkspaceID)).
		Want(http.StatusOK).JSON(&member)
	if member.Used == nil || *member.Used != 1 || member.Members != nil {
		t.Fatalf("member usage = %+v, want the total without the breakdown", member)
	}

	f.h.Entitlements = nil
	var off AppCallUsageResponse
	testutil.Call(t, f.h.GetAppCallUsage, withURLParam(newRequestAs(testUserID, http.MethodGet, path, nil), "id", testWorkspaceID)).
		Want(http.StatusOK).JSON(&off)
	if off.Action != "off" || off.Used != nil || off.Limit != nil {
		t.Fatalf("self-hosted usage = %+v", off)
	}
}
