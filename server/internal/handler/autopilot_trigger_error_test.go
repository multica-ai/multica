package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestTriggerAutopilot_InternalFailureDoesNotEchoError pins MUL-6472: an
// unclassified dispatch failure answers with a FIXED 500 string. The chain
// behind it names internal machinery (here "create run: load idempotent quota
// run: no rows in result set", elsewhere pgx table/constraint names), and every
// workspace member can reach "run now" — so none of it may reach the body. The
// 429 quota branch above it is deliberately structured and stays covered by
// TestAutopilotQuotaManualAndWebhookEnforcement.
func TestTriggerAutopilot_InternalFailureDoesNotEchoError(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// A valid, far-from-exhausted policy: the request has to travel past the
	// 429 branch and into the idempotency lookup this test breaks.
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	end := start.Add(24 * time.Hour)
	limit := 1000
	stub := entitlementtest.New()
	stub.Set(uuid.MustParse(testWorkspaceID), entitlement.GateAutopilotRuns, entitlement.Decision{
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
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id = $1`, testWorkspaceID)
	})

	agentID := createWebhookTestAgent(t, "MUL-6472 Trigger Agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")

	// A settled reservation with no run row pointing back at it. Reusing its
	// key sends dispatch down the idempotent-reuse path, where the missing run
	// is an internal error rather than the recoverable "reserved" orphan case.
	// The stored key is the one the manual entry point composes, not the raw
	// header value.
	const idempotencyKey = "mul-6472-orphaned-reservation"
	dbfx.Insert(t, "autopilot_quota_reservation", testutil.Cols{
		"workspace_id":         testWorkspaceID,
		"period_start":         start,
		"period_end":           end,
		"policy_revision":      3,
		"subscription_version": 5,
		"source":               "manual",
		"idempotency_key":      "manual:" + autopilotID + ":" + idempotencyKey,
		"state":                "consumed",
	})

	req := testutil.WithHeaders(
		newRequest("POST", "/api/autopilots/"+autopilotID+"/trigger", nil),
		"Idempotency-Key", idempotencyKey,
	)
	res := testutil.Call(t, testHandler.TriggerAutopilot, withURLParam(req, "id", autopilotID)).
		Want(http.StatusInternalServerError)

	if got := res.Map()["error"]; got != "failed to trigger autopilot" {
		t.Fatalf("error = %#v, want the fixed string with no error chain appended", got)
	}
	// Substrings of the chain this failure actually produces, plus the pgx
	// marker every DB-level failure on this path would carry.
	for _, leak := range []string{"create run", "idempotent", "quota", "no rows", "sqlstate"} {
		if strings.Contains(strings.ToLower(res.Text()), leak) {
			t.Fatalf("500 body leaks internal detail %q: %s", leak, res.Text())
		}
	}
}
