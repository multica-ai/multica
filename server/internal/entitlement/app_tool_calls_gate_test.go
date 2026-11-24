package entitlement

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func appToolCallsGate(action Action, limit int) wireGate {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	gate := wireGate{Action: string(action)}
	if action != ActionOff {
		gate.Limit = &limit
		gate.PeriodStart, gate.PeriodEnd, gate.ResetAt = &start, &end, &end
		gate.Notifications = &wireNotificationPolicy{OnRejection: NotificationFirstRejectionPerLimit}
	}
	return gate
}

func TestAppToolCallsGateIsAMonthlyBudget(t *testing.T) {
	for _, action := range []Action{ActionOff, ActionObserve, ActionEnforce} {
		t.Run(string(action), func(t *testing.T) {
			server := httptest.NewServer(policyHandler(func(p *wirePolicy) {
				p.Gates[string(GateAppToolCalls)] = appToolCallsGate(action, 1500)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)

			decision := client.Gate(context.Background(), uuid.New(), GateAppToolCalls)
			if decision.Gate.Action != action || decision.Reason != ReasonRefreshed {
				t.Fatalf("decision = %+v", decision)
			}
			if action == ActionOff {
				return
			}
			gate := decision.Gate
			if gate.Limit == nil || *gate.Limit != 1500 || gate.PeriodStart == nil || gate.PeriodEnd == nil ||
				gate.ResetAt == nil || gate.Notifications == nil ||
				gate.Notifications.OnRejection != NotificationFirstRejectionPerLimit {
				t.Fatalf("gate = %+v, want limit 1500 with its period and notices", gate)
			}
		})
	}
}

// Cloud and Multica deploy independently. A Cloud that does not send
// app_tool_calls yet, or sends it malformed, leaves the gate off without
// touching the autopilot quota in the same policy.
func TestAppToolCallsGateFailsOpenAlone(t *testing.T) {
	limit := 100
	for _, tt := range []struct {
		name   string
		mutate func(*wirePolicy)
		reason Reason
	}{
		{"missing", func(*wirePolicy) {}, ReasonGateMissing},
		{"enforce without limit", func(p *wirePolicy) {
			gate := appToolCallsGate(ActionEnforce, limit)
			gate.Limit = nil
			p.Gates[string(GateAppToolCalls)] = gate
		}, ReasonGateInvalid},
		{"enforce without period", func(p *wirePolicy) {
			p.Gates[string(GateAppToolCalls)] = wireGate{Action: string(ActionEnforce), Limit: &limit}
		}, ReasonGateInvalid},
		{"unknown action", func(p *wirePolicy) {
			p.Gates[string(GateAppToolCalls)] = wireGate{Action: "block"}
		}, ReasonGateInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(policyHandler(tt.mutate))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			calls := client.Gate(context.Background(), workspaceID, GateAppToolCalls)
			if calls.Gate.Action != ActionOff || calls.Reason != tt.reason {
				t.Fatalf("app_tool_calls = %+v, want off/%s", calls, tt.reason)
			}
			if autopilot := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); autopilot.Gate.Action != ActionEnforce {
				t.Fatalf("autopilot = %+v, want the enforced quota from the same policy", autopilot)
			}
		})
	}
}
