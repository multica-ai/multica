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

func membersGate(action Action, limit int) wireGate {
	gate := wireGate{Action: string(action)}
	if action != ActionOff {
		gate.Limit = &limit
	}
	return gate
}

func TestMembersGateIsAPeriodlessCeiling(t *testing.T) {
	for _, action := range []Action{ActionOff, ActionObserve, ActionEnforce} {
		t.Run(string(action), func(t *testing.T) {
			server := httptest.NewServer(policyHandler(func(p *wirePolicy) {
				p.Gates[string(GateMembers)] = membersGate(action, 5)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)

			decision := client.Gate(context.Background(), uuid.New(), GateMembers)
			if decision.Gate.Action != action || decision.Reason != ReasonRefreshed {
				t.Fatalf("decision = %+v", decision)
			}
			if action == ActionOff {
				return
			}
			if decision.Gate.Limit == nil || *decision.Gate.Limit != 5 || decision.Gate.PeriodStart != nil {
				t.Fatalf("gate = %+v, want limit 5 without a period", decision.Gate)
			}
		})
	}
}

// Cloud and Multica deploy independently. A policy from a Cloud that does not
// send members yet, or sends it malformed, must leave members off without
// failing the autopilot quota open with it.
func TestMembersGateFailsOpenAlone(t *testing.T) {
	negative := -1
	tests := []struct {
		name   string
		mutate func(*wirePolicy)
		reason Reason
	}{
		{"missing", func(*wirePolicy) {}, ReasonGateMissing},
		{"unknown action", func(p *wirePolicy) {
			p.Gates[string(GateMembers)] = wireGate{Action: "block"}
		}, ReasonGateInvalid},
		{"enforce without limit", func(p *wirePolicy) {
			p.Gates[string(GateMembers)] = wireGate{Action: string(ActionEnforce)}
		}, ReasonGateInvalid},
		{"negative limit", func(p *wirePolicy) {
			p.Gates[string(GateMembers)] = wireGate{Action: string(ActionEnforce), Limit: &negative}
		}, ReasonGateInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(policyHandler(tt.mutate))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			members := client.Gate(context.Background(), workspaceID, GateMembers)
			if members.Gate.Action != ActionOff || members.Reason != tt.reason {
				t.Fatalf("members = %+v, want off/%s", members, tt.reason)
			}
			autopilot := client.Gate(context.Background(), workspaceID, GateAutopilotRuns)
			if autopilot.Gate.Action != ActionEnforce || autopilot.Reason != ReasonCacheFresh {
				t.Fatalf("autopilot = %+v, want the enforced quota from the same policy", autopilot)
			}
		})
	}
}

// A members gate of the wrong JSON shape must not fail the decode of the
// whole policy: on a cold cache that would turn an enforced autopilot quota
// off. A malformed autopilot_runs gate still fails the policy open.
func TestMembersGateOfTheWrongShapeFailsOpenAlone(t *testing.T) {
	const autopilot = `"autopilot_runs": {"action": "enforce", "limit": 23,
		"period_start": "2026-08-01T00:00:00Z", "period_end": "2026-09-01T00:00:00Z",
		"reset_at": "2026-09-01T00:00:00Z"}`
	policy := func(gates string) string {
		return `{"schema_version": 1, "policy_revision": 5, "subscription_version": 0,
			"valid_until": "2030-01-01T00:00:00Z", "valid_for_seconds": 60,
			"gates": {` + gates + `}}`
	}
	for _, members := range []string{
		`"members": {"action": "enforce", "limit": "five"}`,
		`"members": ["enforce"]`,
		`"members": "enforce"`,
	} {
		t.Run(members, func(t *testing.T) {
			server := httptest.NewServer(bodyHandler(http.StatusOK, policy(autopilot+", "+members)))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			if got := client.Gate(context.Background(), workspaceID, GateMembers); got.Gate.Action != ActionOff || got.Reason != ReasonGateInvalid {
				t.Fatalf("members = %+v, want off/gate_invalid", got)
			}
			if got := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); got.Gate.Action != ActionEnforce {
				t.Fatalf("autopilot = %+v, want the enforced quota", got)
			}
		})
	}

	server := httptest.NewServer(bodyHandler(http.StatusOK,
		policy(`"autopilot_runs": {"action": "enforce", "limit": "23"}, "members": {"action": "enforce", "limit": 5}`)))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	if got := client.Gate(context.Background(), uuid.New(), GateAutopilotRuns); got.Gate.Action != ActionOff || got.Reason != ReasonInvalidPolicy {
		t.Fatalf("malformed autopilot = %+v, want the whole policy failed open", got)
	}
}

func TestRefreshGateFetchesPastACurrentCache(t *testing.T) {
	clock := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	var action atomic.Value
	action.Store(ActionEnforce)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		policy := samplePolicy(5, calls.Load(), 60, ActionEnforce)
		policy.Gates[string(GateMembers)] = membersGate(action.Load().(Action), 5)
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	if first := client.Gate(context.Background(), workspaceID, GateMembers); first.Gate.Action != ActionEnforce {
		t.Fatalf("first = %+v", first)
	}

	// The workspace upgrades; Cloud now turns the gate off.
	action.Store(ActionOff)
	clock = clock.Add(time.Second)
	recent := client.RefreshGate(context.Background(), workspaceID, GateMembers)
	if recent.Gate.Action != ActionEnforce || recent.Reason != ReasonCacheFresh || calls.Load() != 1 {
		t.Fatalf("recent = %+v, calls = %d; a policy younger than the refresh bound is reused", recent, calls.Load())
	}

	clock = clock.Add(minForcedRefreshAge)
	refreshed := client.RefreshGate(context.Background(), workspaceID, GateMembers)
	if refreshed.Gate.Action != ActionOff || refreshed.Reason != ReasonRefreshed || calls.Load() != 2 {
		t.Fatalf("refreshed = %+v, calls = %d", refreshed, calls.Load())
	}
	if cached := client.Gate(context.Background(), workspaceID, GateMembers); cached.Gate.Action != ActionOff || calls.Load() != 2 {
		t.Fatalf("cached = %+v, calls = %d; the refresh must replace the cache", cached, calls.Load())
	}
}

func TestRefreshGateFailureAnswersLikeGate(t *testing.T) {
	clock := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		policy := samplePolicy(5, 1, 60, ActionEnforce)
		policy.Gates[string(GateMembers)] = membersGate(ActionEnforce, 5)
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	client.Gate(context.Background(), workspaceID, GateMembers)
	fail.Store(true)
	clock = clock.Add(minForcedRefreshAge)
	decision := client.RefreshGate(context.Background(), workspaceID, GateMembers)
	if decision.Gate.Action != ActionEnforce || decision.Reason != ReasonCacheFresh {
		t.Fatalf("decision = %+v, want the still-fresh cached enforce", decision)
	}
}

func TestRefreshGateWithoutCloudIsOff(t *testing.T) {
	client, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision := client.RefreshGate(context.Background(), uuid.New(), GateMembers)
	if decision.Gate.Action != ActionOff || decision.Reason != ReasonDisabled {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestStorageBytesGateIsAPeriodlessCeiling(t *testing.T) {
	server := httptest.NewServer(policyHandler(func(p *wirePolicy) {
		p.Gates[string(GateStorageBytes)] = membersGate(ActionEnforce, 1_000_000_000)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)

	decision := client.Gate(context.Background(), uuid.New(), GateStorageBytes)
	if decision.Gate.Action != ActionEnforce || decision.Gate.Limit == nil ||
		*decision.Gate.Limit != 1_000_000_000 || decision.Gate.PeriodStart != nil {
		t.Fatalf("decision = %+v, want enforce 1e9 without a period", decision)
	}
}

// A Cloud that does not send storage_bytes yet, or sends it malformed, leaves
// storage off without touching the other gates.
func TestStorageBytesGateFailsOpenAlone(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*wirePolicy)
		reason Reason
	}{
		{"missing", func(*wirePolicy) {}, ReasonGateMissing},
		{"enforce without limit", func(p *wirePolicy) {
			p.Gates[string(GateStorageBytes)] = wireGate{Action: string(ActionEnforce)}
		}, ReasonGateInvalid},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(policyHandler(tt.mutate))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			storage := client.Gate(context.Background(), workspaceID, GateStorageBytes)
			if storage.Gate.Action != ActionOff || storage.Reason != tt.reason {
				t.Fatalf("storage = %+v, want off/%s", storage, tt.reason)
			}
			if autopilot := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); autopilot.Gate.Action != ActionEnforce {
				t.Fatalf("autopilot = %+v, want the enforced quota from the same policy", autopilot)
			}
		})
	}
}

func TestRefreshGateNowSkipsTheReuseWindow(t *testing.T) {
	clock := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	var limit atomic.Int64
	limit.Store(10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		policy := samplePolicy(6, 1, 60, ActionEnforce)
		gate := policy.Gates[string(GateAutopilotRuns)]
		value := int(limit.Load())
		gate.Limit = &value
		policy.Gates[string(GateAutopilotRuns)] = gate
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	if first := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); first.Gate.Limit == nil || *first.Gate.Limit != 10 {
		t.Fatalf("first = %+v; want limit 10", first)
	}

	// Cloud raises the limit one second later (an add-on confirmed by
	// Stripe's webhook), inside RefreshGate's reuse window.
	limit.Store(1010)
	clock = clock.Add(time.Second)
	if reused := client.RefreshGate(context.Background(), workspaceID, GateAutopilotRuns); *reused.Gate.Limit != 10 || calls.Load() != 1 {
		t.Fatalf("RefreshGate = %+v, calls = %d; it reuses a policy this young", reused, calls.Load())
	}
	refreshed := client.RefreshGateNow(context.Background(), workspaceID, GateAutopilotRuns)
	if refreshed.Gate.Limit == nil || *refreshed.Gate.Limit != 1010 || refreshed.Reason != ReasonRefreshed || calls.Load() != 2 {
		t.Fatalf("RefreshGateNow = %+v, calls = %d; want the new limit", refreshed, calls.Load())
	}
	// The refreshed policy is cached for everyone else.
	if cached := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); *cached.Gate.Limit != 1010 || calls.Load() != 2 {
		t.Fatalf("Gate after refresh = %+v, calls = %d", cached, calls.Load())
	}
}
