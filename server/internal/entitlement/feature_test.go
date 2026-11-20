package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func featurePolicyHandler(t *testing.T, features string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		policy := samplePolicy(4, 2, 60, ActionEnforce)
		if features != "" {
			policy.Features = json.RawMessage(features)
		}
		writePolicy(t, w, policy)
	}
}

func TestDisabledClientLeavesFeaturesUsableWithoutIO(t *testing.T) {
	var calls atomic.Int64
	client, err := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	decision := client.Feature(context.Background(), uuid.New(), FeaturePrivateProjects)
	if decision.Action != ActionOff || decision.Reason != ReasonDisabled || decision.Restricted() || calls.Load() != 0 {
		t.Fatalf("decision = %+v, calls = %d", decision, calls.Load())
	}
}

func TestCloudFeatureActionsAreDeliveredUnchanged(t *testing.T) {
	for _, action := range []Action{ActionOff, ActionObserve, ActionEnforce} {
		t.Run(string(action), func(t *testing.T) {
			server := httptest.NewServer(featurePolicyHandler(t, `{"private_projects":{"action":"`+string(action)+`"}}`))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			decision := client.Feature(context.Background(), uuid.New(), FeaturePrivateProjects)
			if decision.Action != action || decision.Reason != ReasonRefreshed ||
				decision.PolicyRevision != 4 || decision.SubscriptionVersion != 2 {
				t.Fatalf("decision = %+v", decision)
			}
			if decision.Restricted() != (action == ActionEnforce) {
				t.Fatalf("Restricted() = %v for %s", decision.Restricted(), action)
			}
		})
	}
}

// Revision 7 adds three features. Each is read on its own: a policy from an
// older Cloud that lacks them leaves them off without touching the features
// it does carry.
func TestRevisionSevenFeaturesAreReadIndependently(t *testing.T) {
	server := httptest.NewServer(featurePolicyHandler(t, `{
		"private_projects":{"action":"observe"},
		"agent_invocation_members":{"action":"enforce"},
		"runtime_member_sharing":{"action":"observe"},
		"team_app_accounts":{"action":"off"}
	}`))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	workspaceID := uuid.New()
	for name, want := range map[FeatureName]Action{
		FeaturePrivateProjects:        ActionObserve,
		FeatureAgentInvocationMembers: ActionEnforce,
		FeatureRuntimeMemberSharing:   ActionObserve,
		FeatureTeamAppAccounts:        ActionOff,
	} {
		if got := client.Feature(context.Background(), workspaceID, name); got.Action != want || got.Reason != ReasonRefreshed && got.Reason != ReasonCacheFresh {
			t.Fatalf("%s = %+v, want %s", name, got, want)
		}
	}

	older := httptest.NewServer(featurePolicyHandler(t, `{"private_projects":{"action":"enforce"}}`))
	defer older.Close()
	client = newTestClient(t, older.URL, nil)
	for _, name := range []FeatureName{FeatureAgentInvocationMembers, FeatureRuntimeMemberSharing, FeatureTeamAppAccounts} {
		if got := client.Feature(context.Background(), workspaceID, name); got.Action != ActionOff || got.Reason != ReasonFeatureMissing {
			t.Fatalf("%s from an older policy = %+v, want off/feature_missing", name, got)
		}
	}
	if got := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects); got.Action != ActionEnforce {
		t.Fatalf("private_projects = %+v, want enforce", got)
	}
}

// One feature that cannot be read falls back to off with a reason, and the
// metered gate from the same response still enforces.
func TestMalformedFeatureIsIsolatedFromGatesAndOtherFeatures(t *testing.T) {
	cases := []struct {
		name     string
		features string
		reason   Reason
	}{
		{"features absent", "", ReasonFeatureMissing},
		{"features null", `null`, ReasonFeatureMissing},
		{"features is an array", `[]`, ReasonFeatureInvalid},
		{"features is a string", `"enforce"`, ReasonFeatureInvalid},
		{"feature missing", `{"unknown_feature":{"action":"enforce"}}`, ReasonFeatureMissing},
		{"action is a number", `{"private_projects":{"action":123}}`, ReasonFeatureInvalid},
		{"action is unknown", `{"private_projects":{"action":"block"}}`, ReasonFeatureInvalid},
		{"feature is not an object", `{"private_projects":"enforce"}`, ReasonFeatureInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(featurePolicyHandler(t, tc.features))
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			workspaceID := uuid.New()

			feature := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects)
			if feature.Action != ActionOff || feature.Reason != tc.reason || feature.PolicyRevision != 4 {
				t.Fatalf("feature = %+v, want off with %s", feature, tc.reason)
			}
			gate := client.Gate(context.Background(), workspaceID, GateAutopilotRuns)
			if gate.Gate.Action != ActionEnforce || gate.Reason != ReasonCacheFresh {
				t.Fatalf("gate must keep enforcing from the same policy: %+v", gate)
			}
		})
	}
}

// Gates and features share one cached policy: reading both costs one request.
func TestFeatureAndGateShareOnePolicyFetch(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		featurePolicyHandler(t, `{"private_projects":{"action":"enforce"}}`)(w, r)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	workspaceID := uuid.New()

	if got := client.Gate(context.Background(), workspaceID, GateAutopilotRuns); got.Gate.Action != ActionEnforce {
		t.Fatalf("gate = %+v", got)
	}
	feature := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects)
	if feature.Action != ActionEnforce || feature.Reason != ReasonCacheFresh || calls.Load() != 1 {
		t.Fatalf("feature = %+v, calls = %d", feature, calls.Load())
	}
}

// A stale enforce is downgraded to observe with Reason stale, so recorded
// attempts can tell a Cloud outage from a configured observe rollout, and the
// expired policy stops restricting entirely.
func TestStaleFeatureEnforceDowngradesThenFailsOpen(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		policy := samplePolicy(4, 0, 5, ActionEnforce)
		policy.Features = json.RawMessage(`{"private_projects":{"action":"enforce"}}`)
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.staleGrace = 10 * time.Second
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	if fresh := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects); fresh.Action != ActionEnforce {
		t.Fatalf("fresh = %+v", fresh)
	}
	fail.Store(true)
	clock = clock.Add(6 * time.Second)
	stale := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects)
	if stale.Action != ActionObserve || stale.Reason != ReasonStale || !stale.Downgraded || stale.Restricted() {
		t.Fatalf("stale = %+v", stale)
	}
	clock = clock.Add(10 * time.Second)
	expired := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects)
	if expired.Action != ActionOff || expired.Reason != ReasonUnavailable {
		t.Fatalf("expired = %+v", expired)
	}
}

// A feature Cloud already sent as observe stays an ordinary observe when the
// cache goes stale: nothing was downgraded, so attempts count as observed.
func TestStaleFeatureObserveIsNotDowngraded(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		policy := samplePolicy(4, 0, 5, ActionEnforce)
		policy.Features = json.RawMessage(`{"private_projects":{"action":"observe"}}`)
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.staleGrace = 10 * time.Second
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	if fresh := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects); fresh.Action != ActionObserve || fresh.Downgraded {
		t.Fatalf("fresh = %+v", fresh)
	}
	fail.Store(true)
	clock = clock.Add(6 * time.Second)
	stale := client.Feature(context.Background(), workspaceID, FeaturePrivateProjects)
	if stale.Action != ActionObserve || stale.Reason != ReasonStale || stale.Downgraded {
		t.Fatalf("stale observe = %+v, want observe without Downgraded", stale)
	}
}

func TestColdFailureLeavesFeaturesUsable(t *testing.T) {
	server := httptest.NewServer(statusHandler(http.StatusServiceUnavailable))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	decision := client.Feature(context.Background(), uuid.New(), FeaturePrivateProjects)
	if decision.Action != ActionOff || decision.Reason != ReasonUnavailable {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestInvalidWorkspaceAndUnknownFeatureDoNotFetch(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	observer := &recordingObserver{}
	client := newTestClient(t, server.URL, func(c *Config) { c.Observer = observer })

	if got := client.Feature(context.Background(), uuid.Nil, FeaturePrivateProjects); got.Reason != ReasonInvalidWorkspace {
		t.Fatalf("nil workspace = %+v", got)
	}
	if got := client.Feature(context.Background(), uuid.New(), FeatureName("custom")); got.Reason != ReasonUnknownGate {
		t.Fatalf("unknown feature = %+v", got)
	}
	if calls.Load() != 0 {
		t.Fatalf("calls = %d, want 0", calls.Load())
	}
	_, _, decisions, _ := observer.snapshot()
	if len(decisions) != 2 || decisions[0] != "feature:private_projects/off/invalid_workspace" ||
		decisions[1] != "feature:unknown/off/unknown_gate" {
		t.Fatalf("decision labels = %v", decisions)
	}
}
