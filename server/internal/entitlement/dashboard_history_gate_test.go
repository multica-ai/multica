package entitlement

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func dashboardHistoryGate(action Action, days int) wireGate {
	gate := wireGate{Action: string(action)}
	if action != ActionOff {
		gate.Limit = &days
	}
	return gate
}

func TestDashboardHistoryGateIsADayCount(t *testing.T) {
	for _, action := range []Action{ActionOff, ActionObserve, ActionEnforce} {
		t.Run(string(action), func(t *testing.T) {
			server := httptest.NewServer(policyHandler(func(p *wirePolicy) {
				p.Gates[string(GateDashboardHistoryDays)] = dashboardHistoryGate(action, 30)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)

			decision := client.Gate(context.Background(), uuid.New(), GateDashboardHistoryDays)
			if decision.Gate.Action != action || decision.Reason != ReasonRefreshed || decision.Downgraded {
				t.Fatalf("decision = %+v", decision)
			}
			if action == ActionOff {
				return
			}
			if decision.Gate.Limit == nil || *decision.Gate.Limit != 30 || decision.Gate.PeriodStart != nil {
				t.Fatalf("gate = %+v, want limit 30 without a period", decision.Gate)
			}
		})
	}
}

// A Cloud that does not send the gate yet, or sends it malformed, leaves the
// dashboard unlimited without failing the autopilot quota open with it.
func TestDashboardHistoryGateFailsOpenAlone(t *testing.T) {
	zero, tooMany, negative, valid := 0, MaxDashboardHistoryDays+1, -1, 30
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	tests := []struct {
		name   string
		mutate func(*wirePolicy)
		reason Reason
	}{
		{"missing", func(*wirePolicy) {}, ReasonGateMissing},
		{"unknown action", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{Action: "block", Limit: &valid}
		}, ReasonGateInvalid},
		{"enforce without limit", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{Action: string(ActionEnforce)}
		}, ReasonGateInvalid},
		{"zero days", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{Action: string(ActionEnforce), Limit: &zero}
		}, ReasonGateInvalid},
		{"negative days", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{Action: string(ActionEnforce), Limit: &negative}
		}, ReasonGateInvalid},
		{"past the product range", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{Action: string(ActionEnforce), Limit: &tooMany}
		}, ReasonGateInvalid},
		{"with a period", func(p *wirePolicy) {
			p.Gates[string(GateDashboardHistoryDays)] = wireGate{
				Action: string(ActionEnforce), Limit: &valid,
				PeriodStart: &periodStart, PeriodEnd: &periodEnd, ResetAt: &periodEnd,
			}
		}, ReasonGateInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(policyHandler(tt.mutate))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			history := client.Gate(context.Background(), workspaceID, GateDashboardHistoryDays)
			if history.Gate.Action != ActionOff || history.Reason != tt.reason {
				t.Fatalf("dashboard_history_days = %+v, want off/%s", history, tt.reason)
			}
			autopilot := client.Gate(context.Background(), workspaceID, GateAutopilotRuns)
			if autopilot.Gate.Action != ActionEnforce || autopilot.Reason != ReasonCacheFresh {
				t.Fatalf("autopilot = %+v, want the enforced quota from the same policy", autopilot)
			}
		})
	}
}

// A JSON type error inside the gate (a string day count) must not fail the
// decode of the whole policy.
func TestDashboardHistoryGateOfTheWrongShapeFailsOpenAlone(t *testing.T) {
	const autopilot = `"autopilot_runs": {"action": "enforce", "limit": 23,
		"period_start": "2026-08-01T00:00:00Z", "period_end": "2026-09-01T00:00:00Z",
		"reset_at": "2026-09-01T00:00:00Z"}`
	for _, gate := range []string{
		`"dashboard_history_days": {"action": "enforce", "limit": "30"}`,
		`"dashboard_history_days": {"action": "enforce", "limit": 30.5}`,
		`"dashboard_history_days": "enforce"`,
	} {
		t.Run(gate, func(t *testing.T) {
			body := `{"schema_version": 1, "policy_revision": 6, "subscription_version": 0,
				"valid_until": "2030-01-01T00:00:00Z", "valid_for_seconds": 60,
				"gates": {` + autopilot + `, ` + gate + `}}`
			server := httptest.NewServer(bodyHandler(http.StatusOK, body))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			if got := client.Gate(context.Background(), workspaceID, GateDashboardHistoryDays); got.Gate.Action != ActionOff || got.Reason != ReasonGateInvalid {
				t.Fatalf("dashboard_history_days = %+v, want off/gate_invalid", got)
			}
			if got := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); got.Gate.Action != ActionEnforce {
				t.Fatalf("autopilot = %+v, want the enforced quota", got)
			}
		})
	}
}

// A stale enforce is lowered to observe and marked Downgraded, so consumers
// can count Cloud outages apart from a configured observe.
func TestStaleEnforceIsMarkedDowngraded(t *testing.T) {
	for _, configured := range []Action{ActionEnforce, ActionObserve} {
		t.Run(string(configured), func(t *testing.T) {
			clock := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
			var fail atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if fail.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				policy := samplePolicy(6, 1, 5, ActionEnforce)
				policy.Gates[string(GateDashboardHistoryDays)] = dashboardHistoryGate(configured, 30)
				writePolicy(t, w, policy)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			client.now = func() time.Time { return clock }
			workspaceID := uuid.New()

			if fresh := client.Gate(context.Background(), workspaceID, GateDashboardHistoryDays); fresh.Gate.Action != configured || fresh.Downgraded {
				t.Fatalf("fresh = %+v", fresh)
			}
			fail.Store(true)
			clock = clock.Add(6 * time.Second)
			stale := client.Gate(context.Background(), workspaceID, GateDashboardHistoryDays)
			wantDowngraded := configured == ActionEnforce
			if stale.Gate.Action != ActionObserve || stale.Reason != ReasonStale || stale.Downgraded != wantDowngraded {
				t.Fatalf("stale = %+v, want observe/stale downgraded=%v", stale, wantDowngraded)
			}
			if stale.Gate.Limit == nil || *stale.Gate.Limit != 30 {
				t.Fatalf("stale gate = %+v, want the cached limit kept", stale.Gate)
			}
		})
	}
}
