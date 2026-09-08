package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	testIssueLimit     = 137
	testAutopilotLimit = 23
)

func TestDefaultConfigIsDisabledAndPerformsNoIO(t *testing.T) {
	var calls atomic.Int64
	client, err := New(Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if client.Enabled() {
		t.Fatal("zero Config must be disabled")
	}
	decision := client.Gate(context.Background(), uuid.New(), GateIssueCount)
	if decision.Gate.Action != ActionOff || decision.Reason != ReasonDisabled || calls.Load() != 0 {
		t.Fatalf("decision = %+v, calls = %d", decision, calls.Load())
	}
}

func TestConnectedConfigValidation(t *testing.T) {
	valid := Config{BaseURL: "https://cloud.internal"}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"unsupported base URL scheme", func(c *Config) { c.BaseURL = "ftp://cloud.internal" }},
		{"base URL credentials", func(c *Config) { c.BaseURL = "https://user:pass@cloud.internal" }},
		{"base URL query", func(c *Config) { c.BaseURL = "https://cloud.internal?secret=value" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			if _, err := New(cfg); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := New(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}
}

func TestGateFetchesMachinePolicyWithoutHumanIdentity(t *testing.T) {
	workspaceID := uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/internal/entitlement-policies/"+workspaceID.String() {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization must be absent, got %q", got)
		}
		if got := r.Header.Get("X-User-ID"); got != "" {
			t.Errorf("X-User-ID must be absent, got %q", got)
		}
		writePolicy(t, w, samplePolicy(7, 3, 60, ActionEnforce))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL, nil)
	decision := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if decision.Gate.Action != ActionEnforce || decision.Gate.Limit == nil || *decision.Gate.Limit != testIssueLimit ||
		decision.Reason != ReasonRefreshed || decision.PolicyRevision != 7 || decision.SubscriptionVersion != 3 {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestGateCachesByWorkspaceAndClonesResults(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writePolicy(t, w, samplePolicy(1, 0, 60, ActionEnforce))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	workspaceID := uuid.New()

	first := client.Gate(context.Background(), workspaceID, GateIssueCount)
	*first.Gate.Limit = 999
	second := client.Gate(context.Background(), workspaceID, GateIssueCount)
	autopilot := client.Gate(context.Background(), workspaceID, GateAutopilotRuns)
	if second.Reason != ReasonCacheFresh || second.Gate.Limit == nil || *second.Gate.Limit != testIssueLimit {
		t.Fatalf("cached decision = %+v", second)
	}
	if autopilot.Gate.Action != ActionEnforce || calls.Load() != 1 {
		t.Fatalf("autopilot = %+v, calls = %d", autopilot, calls.Load())
	}
	if autopilot.Gate.Notifications == nil ||
		autopilot.Gate.Notifications.OnRejection != NotificationFirstRejectionPerPeriod {
		t.Fatalf("autopilot notifications = %+v", autopilot.Gate.Notifications)
	}
	autopilot.Gate.Notifications.OnRejection = "mutated"
	cloned := client.Gate(context.Background(), workspaceID, GateAutopilotRuns)
	if cloned.Gate.Notifications == nil ||
		cloned.Gate.Notifications.OnRejection != NotificationFirstRejectionPerPeriod {
		t.Fatalf("cloned notifications = %+v", cloned.Gate.Notifications)
	}
}

func TestNotificationPolicyDoesNotControlQuotaValidity(t *testing.T) {
	limit := 100
	start := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	base := wireGate{
		Action: string(ActionEnforce), Limit: &limit,
		PeriodStart: &start, PeriodEnd: &end, ResetAt: &end,
	}

	valid := base
	valid.Notifications = &wireNotificationPolicy{
		OnRejection: NotificationFirstRejectionPerPeriod,
	}
	gate, err := normalizeGate(GateAutopilotRuns, valid)
	if err != nil || gate.Notifications == nil ||
		gate.Notifications.OnRejection != NotificationFirstRejectionPerPeriod {
		t.Fatalf("valid notification gate = %+v, err = %v", gate, err)
	}

	malformed := base
	malformed.Notifications = &wireNotificationPolicy{
		OnRejection: "future_mode",
	}
	gate, err = normalizeGate(GateAutopilotRuns, malformed)
	if err != nil || gate.Action != ActionEnforce || gate.Notifications != nil {
		t.Fatalf("malformed notification gate = %+v, err = %v; want enforced quota without notices", gate, err)
	}

}

func TestConcurrentWorkspaceMissesUseSingleflight(t *testing.T) {
	var calls atomic.Int64
	requestStarted := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(requestStarted)
		}
		<-release
		writePolicy(t, w, samplePolicy(1, 0, 60, ActionEnforce))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	workspaceID := uuid.New()

	const workers = 32
	start := make(chan struct{})
	results := make(chan Decision, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- client.Gate(context.Background(), workspaceID, GateIssueCount)
		}()
	}
	close(start)
	<-requestStarted
	close(release)
	wg.Wait()
	close(results)
	for decision := range results {
		if decision.Gate.Action != ActionEnforce {
			t.Errorf("decision = %+v", decision)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP calls = %d, want 1", calls.Load())
	}
}

func TestCancelledLeaderDoesNotCancelSharedRefresh(t *testing.T) {
	requestStarted := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int64
	policyBody, err := json.Marshal(samplePolicy(1, 0, 60, ActionEnforce))
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	client := newTestClient(t, "https://cloud.internal", func(cfg *Config) {
		cfg.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				close(requestStarted)
			}
			<-release
			if err := req.Context().Err(); err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(string(policyBody))),
				Request:    req,
			}, nil
		})}
	})
	workspaceID := uuid.New()
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderResult := make(chan Decision, 1)
	go func() {
		leaderResult <- client.Gate(leaderCtx, workspaceID, GateIssueCount)
	}()
	<-requestStarted

	cancelLeader()
	followerResult := make(chan Decision, 1)
	go func() {
		followerResult <- client.Gate(context.Background(), workspaceID, GateIssueCount)
	}()
	close(release)

	leader, follower := <-leaderResult, <-followerResult
	if leader.Gate.Action != ActionEnforce || follower.Gate.Action != ActionEnforce {
		t.Fatalf("leader = %+v, follower = %+v", leader, follower)
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP calls = %d, want 1", calls.Load())
	}
}

func TestWorkspaceIsolationAndBoundedLRUEviction(t *testing.T) {
	workspaceA, workspaceB := uuid.New(), uuid.New()
	limits := map[string]int{workspaceA.String(): 11, workspaceB.String(): 22}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		workspaceID := strings.TrimPrefix(r.URL.Path, "/api/v1/internal/entitlement-policies/")
		policy := samplePolicy(1, 0, 60, ActionEnforce)
		limit := limits[workspaceID]
		policy.Gates[string(GateIssueCount)] = wireGate{Action: string(ActionEnforce), Limit: &limit}
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.cache = newPolicyCache(1)
	for _, tc := range []struct {
		workspaceID uuid.UUID
		wantLimit   int
	}{{workspaceA, 11}, {workspaceB, 22}, {workspaceA, 11}} {
		decision := client.Gate(context.Background(), tc.workspaceID, GateIssueCount)
		if decision.Gate.Limit == nil || *decision.Gate.Limit != tc.wantLimit {
			t.Fatalf("workspace %s decision = %+v", tc.workspaceID, decision)
		}
	}
	if calls.Load() != 3 || client.cache.len() != 1 {
		t.Fatalf("calls = %d, cache entries = %d", calls.Load(), client.cache.len())
	}
}

func TestColdFailuresFailOpen(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantReason Reason
	}{
		{"unauthorized", statusHandler(http.StatusUnauthorized), ReasonUnavailable},
		{"not found", statusHandler(http.StatusNotFound), ReasonUnavailable},
		{"server error", statusHandler(http.StatusServiceUnavailable), ReasonUnavailable},
		{"malformed JSON", bodyHandler(http.StatusOK, `{`), ReasonInvalidPolicy},
		{"oversized response", bodyHandler(http.StatusOK, strings.Repeat("x", maxResponseBodySize+1)), ReasonInvalidPolicy},
		{"unknown schema", policyHandler(func(p *wirePolicy) { p.SchemaVersion = 2 }), ReasonInvalidPolicy},
		{"unknown action", policyHandler(func(p *wirePolicy) {
			gate := p.Gates[string(GateIssueCount)]
			gate.Action = "future"
			p.Gates[string(GateIssueCount)] = gate
		}), ReasonInvalidPolicy},
		{"cloud observe action", policyHandler(func(p *wirePolicy) {
			gate := p.Gates[string(GateIssueCount)]
			gate.Action = string(ActionObserve)
			p.Gates[string(GateIssueCount)] = gate
		}), ReasonInvalidPolicy},
		{"missing gate", policyHandler(func(p *wirePolicy) { delete(p.Gates, string(GateAutopilotRuns)) }), ReasonInvalidPolicy},
		{"excessive TTL", policyHandler(func(p *wirePolicy) { p.ValidForSeconds = 301 }), ReasonInvalidPolicy},
		{"missing autopilot period", policyHandler(func(p *wirePolicy) {
			limit := testAutopilotLimit
			p.Gates[string(GateAutopilotRuns)] = wireGate{Action: string(ActionEnforce), Limit: &limit}
		}), ReasonInvalidPolicy},
		{"autopilot reset is not after period start", policyHandler(func(p *wirePolicy) {
			gate := p.Gates[string(GateAutopilotRuns)]
			resetAt := *gate.PeriodStart
			gate.ResetAt = &resetAt
			p.Gates[string(GateAutopilotRuns)] = gate
		}), ReasonInvalidPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			client := newTestClient(t, server.URL, nil)
			decision := client.Gate(context.Background(), uuid.New(), GateIssueCount)
			if decision.Gate.Action != ActionOff || decision.Reason != tt.wantReason {
				t.Fatalf("decision = %+v", decision)
			}
		})
	}
}

func TestAutopilotResetMayFollowPeriodEnd(t *testing.T) {
	policy := samplePolicy(1, 0, 60, ActionEnforce)
	gate := policy.Gates[string(GateAutopilotRuns)]
	resetAt := gate.PeriodEnd.Add(time.Hour)
	gate.ResetAt = &resetAt
	policy.Gates[string(GateAutopilotRuns)] = gate

	if _, err := normalizePolicy(policy); err != nil {
		t.Fatalf("normalizePolicy: %v", err)
	}
}

func TestTimeoutFailsOpenWithinIndependentBound(t *testing.T) {
	requestCancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(requestCancelled)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.timeout = 20 * time.Millisecond

	started := time.Now()
	decision := client.Gate(context.Background(), uuid.New(), GateIssueCount)
	if decision.Gate.Action != ActionOff || decision.Reason != ReasonUnavailable {
		t.Fatalf("decision = %+v", decision)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("request took %s", elapsed)
	}
	<-requestCancelled
}

func TestInternalPolicyClientDoesNotFollowRedirect(t *testing.T) {
	var redirectedCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	client := newTestClient(t, origin.URL, nil)

	decision := client.Gate(context.Background(), uuid.New(), GateIssueCount)
	if decision.Gate.Action != ActionOff || decision.Reason != ReasonUnavailable {
		t.Fatalf("decision = %+v", decision)
	}
	if redirectedCalls.Load() != 0 {
		t.Fatalf("redirect target received %d request(s)", redirectedCalls.Load())
	}
}

func TestFastFailuresAreRateLimitedPerWorkspace(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	for range 2 {
		decision := client.Gate(context.Background(), workspaceID, GateIssueCount)
		if decision.Gate.Action != ActionOff || decision.Reason != ReasonUnavailable {
			t.Fatalf("decision = %+v", decision)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("HTTP calls during retry suppression = %d, want 1", calls.Load())
	}
	clock = clock.Add(defaultFailureRetry + time.Millisecond)
	_ = client.Gate(context.Background(), workspaceID, GateIssueCount)
	if calls.Load() != 2 {
		t.Fatalf("HTTP calls after retry suppression = %d, want 2", calls.Load())
	}
}

func TestObserverRecordsCacheRefreshDecisionAndDegradation(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	observer := &recordingObserver{}
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writePolicy(t, w, samplePolicy(1, 0, 5, ActionEnforce))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, func(cfg *Config) { cfg.Observer = observer })
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	_ = client.Gate(context.Background(), workspaceID, GateIssueCount)
	_ = client.Gate(context.Background(), workspaceID, GateIssueCount)
	fail.Store(true)
	clock = clock.Add(6 * time.Second)
	_ = client.Gate(context.Background(), workspaceID, GateIssueCount)

	cache, refresh, decisions, _ := observer.snapshot()
	if strings.Join(cache, ",") != "miss,hit,expired" {
		t.Fatalf("cache observations = %v", cache)
	}
	if strings.Join(refresh, ",") != "ok,5xx" {
		t.Fatalf("refresh observations = %v", refresh)
	}
	if strings.Join(decisions, ",") != "issue_count/enforce/refreshed,issue_count/enforce/cache_fresh,issue_count/observe/stale" {
		t.Fatalf("decision observations = %v", decisions)
	}
}

func TestExpiredPolicyNeverEnforcesAndIgnoresCloudClock(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var fail atomic.Bool
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		policy := samplePolicy(1, 0, 5, ActionEnforce)
		policy.ValidUntil = clock.Add(24 * time.Hour) // diagnostic only; intentionally skewed
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.staleGrace = 10 * time.Second
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	fresh := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if fresh.Gate.Action != ActionEnforce {
		t.Fatalf("fresh = %+v", fresh)
	}
	fail.Store(true)
	clock = clock.Add(6 * time.Second)
	stale := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if stale.Gate.Action != ActionObserve || stale.Gate.Limit == nil || *stale.Gate.Limit != testIssueLimit || stale.Reason != ReasonStale {
		t.Fatalf("stale = %+v", stale)
	}
	clock = clock.Add(10 * time.Second)
	expired := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if expired.Gate.Action != ActionOff || expired.Reason != ReasonUnavailable {
		t.Fatalf("expired = %+v", expired)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestSubscriptionVersionSwitchReplacesExpiredSnapshot(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var subscriptionVersion atomic.Int64
	subscriptionVersion.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		action := ActionEnforce
		if subscriptionVersion.Load() == 2 {
			action = ActionOff
		}
		writePolicy(t, w, samplePolicy(1, subscriptionVersion.Load(), 5, action))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	if got := client.Gate(context.Background(), workspaceID, GateIssueCount); got.Gate.Action != ActionEnforce {
		t.Fatalf("initial = %+v", got)
	}
	subscriptionVersion.Store(2)
	clock = clock.Add(6 * time.Second)
	got := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if got.Gate.Action != ActionOff || got.PolicyRevision != 1 || got.SubscriptionVersion != 2 || got.Reason != ReasonRefreshed {
		t.Fatalf("switched = %+v", got)
	}
}

func TestSubscriptionVersionRegressionCannotOverwriteWorkspaceCache(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	observer := &recordingObserver{}
	var mu sync.Mutex
	policy := samplePolicy(1, 4, 5, ActionEnforce)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		writePolicy(t, w, policy)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, func(cfg *Config) { cfg.Observer = observer })
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	initial := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if initial.Gate.Action != ActionEnforce {
		t.Fatalf("initial = %+v", initial)
	}
	mu.Lock()
	policy = samplePolicy(1, 3, 5, ActionOff)
	mu.Unlock()
	clock = clock.Add(6 * time.Second)
	regressed := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if regressed.Gate.Action != ActionObserve || regressed.PolicyRevision != 1 || regressed.SubscriptionVersion != 4 {
		t.Fatalf("regressed = %+v", regressed)
	}
	if got := observer.regressions(); got != 1 {
		t.Fatalf("regressions = %d", got)
	}
}

func TestSubscriptionVersionRegressionIsAcceptedAfterStaleGrace(t *testing.T) {
	clock := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	var subscriptionVersion atomic.Int64
	subscriptionVersion.Store(2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writePolicy(t, w, samplePolicy(1, subscriptionVersion.Load(), 60, ActionOff))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	client.staleGrace = 10 * time.Second
	client.now = func() time.Time { return clock }
	workspaceID := uuid.New()

	initial := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if initial.SubscriptionVersion != 2 || initial.Reason != ReasonRefreshed {
		t.Fatalf("initial = %+v", initial)
	}
	subscriptionVersion.Store(1)
	clock = clock.Add(71 * time.Second)
	recovered := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if recovered.SubscriptionVersion != 1 || recovered.Reason != ReasonRefreshed {
		t.Fatalf("recovered = %+v", recovered)
	}

	clock = clock.Add(defaultFailureRetry + time.Millisecond)
	cached := client.Gate(context.Background(), workspaceID, GateIssueCount)
	if cached.SubscriptionVersion != 1 || cached.Reason != ReasonCacheFresh || calls.Load() != 2 {
		t.Fatalf("cached = %+v, HTTP calls = %d", cached, calls.Load())
	}
}

func TestInvalidWorkspaceAndUnknownGateDoNotFetch(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, nil)
	tests := []struct {
		workspaceID uuid.UUID
		gate        GateName
		reason      Reason
	}{{uuid.Nil, GateIssueCount, ReasonInvalidWorkspace}, {uuid.New(), GateName("future_gate"), ReasonUnknownGate}}
	for _, tt := range tests {
		decision := client.Gate(context.Background(), tt.workspaceID, tt.gate)
		if decision.Gate.Action != ActionOff || decision.Reason != tt.reason {
			t.Fatalf("decision = %+v", decision)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("HTTP calls = %d", calls.Load())
	}
}

func newTestClient(t *testing.T, baseURL string, mutate func(*Config)) *Client {
	t.Helper()
	cfg := Config{
		BaseURL: baseURL,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func samplePolicy(policyRevision, subscriptionVersion, validForSeconds int64, issueAction Action) wirePolicy {
	issueLimit := testIssueLimit
	autopilotLimit := testAutopilotLimit
	periodStart := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)
	issueGate := wireGate{Action: string(issueAction)}
	if issueAction != ActionOff {
		issueGate.Limit = &issueLimit
	}
	return wirePolicy{
		SchemaVersion:       SchemaVersion,
		PolicyRevision:      policyRevision,
		SubscriptionVersion: subscriptionVersion,
		ValidUntil:          time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidForSeconds:     validForSeconds,
		Gates: map[string]wireGate{
			string(GateIssueCount): issueGate,
			string(GateAutopilotRuns): {
				Action: string(ActionEnforce), Limit: &autopilotLimit,
				PeriodStart: &periodStart, PeriodEnd: &periodEnd, ResetAt: &periodEnd,
				Notifications: &wireNotificationPolicy{
					OnRejection: NotificationFirstRejectionPerPeriod,
				},
			},
		},
	}
}

func writePolicy(t *testing.T, w http.ResponseWriter, policy wirePolicy) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(policy); err != nil {
		t.Errorf("encode policy: %v", err)
	}
}

func statusHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }
}

func bodyHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func policyHandler(mutate func(*wirePolicy)) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		policy := samplePolicy(1, 0, 60, ActionEnforce)
		mutate(&policy)
		data, _ := json.Marshal(policy)
		_, _ = w.Write(data)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type recordingObserver struct {
	mu                 sync.Mutex
	cache              []string
	refresh            []string
	decisions          []string
	versionRegressions int
}

func (o *recordingObserver) RecordEntitlementCache(outcome string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cache = append(o.cache, outcome)
}

func (o *recordingObserver) RecordEntitlementRefresh(outcome string, _ float64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.refresh = append(o.refresh, outcome)
}

func (o *recordingObserver) RecordEntitlementDecision(gate, action, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.decisions = append(o.decisions, gate+"/"+action+"/"+reason)
}

func (o *recordingObserver) RecordEntitlementVersionRegression() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.versionRegressions++
}

func (o *recordingObserver) regressions() int {
	_, _, _, regressions := o.snapshot()
	return regressions
}

func (o *recordingObserver) snapshot() (cache, refresh, decisions []string, regressions int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.cache...), append([]string(nil), o.refresh...),
		append([]string(nil), o.decisions...), o.versionRegressions
}
