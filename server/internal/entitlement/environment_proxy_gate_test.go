package entitlement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// A string where the gate object belongs: the wrong JSON shape must fail this
// gate alone, not the whole policy.
const wrongShapeEnvironmentProxyPolicy = `{"schema_version": 1, "policy_revision": 5, "subscription_version": 0,
	"valid_until": "2030-01-01T00:00:00Z", "valid_for_seconds": 60, "gates": {
	"autopilot_runs": {"action": "enforce", "limit": 23, "period_start": "2026-08-01T00:00:00Z",
		"period_end": "2026-09-01T00:00:00Z", "reset_at": "2026-09-01T00:00:00Z"},
	"app_tool_calls": {"action": "enforce", "limit": 1500, "period_start": "2026-11-01T00:00:00Z",
		"period_end": "2026-12-01T00:00:00Z", "reset_at": "2026-12-01T00:00:00Z"},
	"environment_proxy": "enforce"}}`

// Cloud and Multica deploy independently. A policy from a Cloud that does not
// send environment_proxy yet, or sends it malformed, must leave remote preview
// off without failing the metered gates open with it. The gate is read through
// Client.Gate, the entry the server calls: normalizePolicy alone cannot catch a
// gate missing from GateName.valid(), which would answer ReasonUnknownGate.
func TestEnvironmentProxyGateThroughPublicEntry(t *testing.T) {
	zero, three, negative := 0, 3, -1
	// Every policy serves the enforced autopilot quota and app_tool_calls
	// budget next to whatever environment_proxy the case adds.
	served := func(mutate func(*wirePolicy)) http.HandlerFunc {
		return policyHandler(func(p *wirePolicy) {
			p.Gates[string(GateAppToolCalls)] = appToolCallsGate(ActionEnforce, 1500)
			mutate(p)
		})
	}
	proxy := func(action Action, limit *int) func(*wirePolicy) {
		return func(p *wirePolicy) {
			p.Gates[string(GateEnvironmentProxy)] = wireGate{Action: string(action), Limit: limit}
		}
	}
	for _, tt := range []struct {
		name       string
		handler    http.HandlerFunc
		wantAction Action
		wantLimit  *int
		reason     Reason
	}{
		{"missing", served(func(*wirePolicy) {}), ActionOff, nil, ReasonGateMissing},
		{"off", served(proxy(ActionOff, nil)), ActionOff, nil, ReasonRefreshed},
		{"observe limit 3", served(proxy(ActionObserve, &three)), ActionObserve, &three, ReasonRefreshed},
		{"enforce limit 0", served(proxy(ActionEnforce, &zero)), ActionEnforce, &zero, ReasonRefreshed},
		{"enforce limit 3", served(proxy(ActionEnforce, &three)), ActionEnforce, &three, ReasonRefreshed},
		{"enforce without limit", served(proxy(ActionEnforce, nil)), ActionOff, nil, ReasonGateInvalid},
		{"enforce negative limit", served(proxy(ActionEnforce, &negative)), ActionOff, nil, ReasonGateInvalid},
		{"unknown action", served(proxy("block", nil)), ActionOff, nil, ReasonGateInvalid},
		{"wrong JSON shape", bodyHandler(http.StatusOK, wrongShapeEnvironmentProxyPolicy), ActionOff, nil, ReasonGateInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			preview := client.Gate(context.Background(), workspaceID, GateEnvironmentProxy)
			if preview.Gate.Action != tt.wantAction || preview.Reason != tt.reason {
				t.Fatalf("environment_proxy = %+v, want %s/%s", preview, tt.wantAction, tt.reason)
			}
			if (tt.wantLimit == nil) != (preview.Gate.Limit == nil) || tt.wantLimit != nil && *preview.Gate.Limit != *tt.wantLimit {
				t.Fatalf("environment_proxy limit = %v, want %v", preview.Gate.Limit, tt.wantLimit)
			}
			// One bad optional gate must never take the metered gates down with it.
			for _, name := range []GateName{GateAutopilotRuns, GateAppToolCalls} {
				if got := client.Gate(context.Background(), workspaceID, name); got.Gate.Action != ActionEnforce {
					t.Fatalf("%s = %+v, want the enforced gate from the same policy", name, got)
				}
			}
		})
	}
}
