package transportretry_test

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/transportretry"
)

func TestMatchCursorWritableIterable(t *testing.T) {
	t.Parallel()
	err := "RetriableError: WritableIterable is closed (result_seen=false, exit_code=1, scanner_error=false, event_count=428, invalid_event_count=0, last_event_type=thinking)"
	if !transportretry.DefaultPolicies()[0].MatchError(err) {
		t.Fatal("expected cursor writable iterable match")
	}
}

func TestExecuteWithRetryRecoversOnThirdLaunch(t *testing.T) {
	t.Parallel()

	cfg := transportretry.DefaultConfig()
	cfg.Policies = []transportretry.Policy{
		{
			ID:               "cursor_writable_iterable",
			Providers:        []string{"cursor"},
			MatchError:       transportretry.DefaultPolicies()[0].MatchError,
			MaxExtraAttempts: 2,
			DelaysMs:         []int{0, 0, 0},
			SessionStrategy: []transportretry.SessionRetryMode{
				transportretry.SessionRetrySame,
				transportretry.SessionRetrySame,
				transportretry.SessionRetryFresh,
			},
			Enabled: true,
		},
	}

	var calls atomic.Int32
	failErr := "RetriableError: WritableIterable is closed (result_seen=false, exit_code=1, scanner_error=false, event_count=428, invalid_event_count=0, last_event_type=thinking)"
	execute := func(_ string, opts transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		i := calls.Add(1)
		if i < 3 {
			return transportretry.ResultView{
				Status:    "failed",
				Error:     failErr,
				SessionID: "sess-stream",
			}, 0, nil
		}
		return transportretry.ResultView{
			Status: "completed",
			Usage: map[string]transportretry.TokenUsageView{
				"m": {CacheReadTokens: 467},
			},
		}, 0, nil
	}

	result, tools, stats, retired, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		transportretry.RetryHooks{},
		nil,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if tools != 0 {
		t.Fatalf("tools = %d", tools)
	}
	if stats.RecoveredOnAttempt != 3 {
		t.Fatalf("recovered_on_attempt = %d, want 3", stats.RecoveredOnAttempt)
	}
	if stats.CacheReadTokensRecovered != 467 {
		t.Fatalf("cache_read_tokens_recovered = %d, want 467", stats.CacheReadTokensRecovered)
	}
	if stats.SurfacedToServer {
		t.Fatal("surfaced_to_server should be false when recovered")
	}
	if retired != "" {
		t.Fatalf("retired = %q, want empty", retired)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestExecuteWithRetrySkipsWhenToolsUsed(t *testing.T) {
	t.Parallel()

	cfg := transportretry.DefaultConfig()
	cfg.Policies = []transportretry.Policy{cfg.Policies[0]}
	failErr := "WritableIterable is closed (result_seen=false)"
	var calls atomic.Int32
	execute := func(_ string, _ transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		calls.Add(1)
		return transportretry.ResultView{Status: "failed", Error: failErr}, 1, nil
	}

	_, _, stats, _, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		transportretry.RetryHooks{},
		nil,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if !stats.SurfacedToServer {
		t.Fatal("expected surfaced_to_server when tools > 0")
	}
}

func TestExecuteWithRetryDisabledMatchesProduction(t *testing.T) {
	t.Parallel()

	cfg := transportretry.Config{Enabled: false}
	var calls atomic.Int32
	execute := func(_ string, _ transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		calls.Add(1)
		return transportretry.ResultView{Status: "failed", Error: "WritableIterable is closed"}, 0, nil
	}

	_, _, stats, _, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		transportretry.RetryHooks{},
		nil,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if stats.Attempts != 0 && stats.PolicyID != "" {
		t.Fatalf("unexpected stats when disabled: %+v", stats)
	}
}

func TestExecuteWithRetryStubPolicyNotBoundToCursor(t *testing.T) {
	t.Parallel()

	cfg := transportretry.Config{
		Enabled: true,
		Policies: []transportretry.Policy{
			{
				ID:               "stub_transport",
				Providers:        []string{"stub"},
				MatchError:       func(err string) bool { return strings.Contains(err, "stub-transport-fail") },
				MaxExtraAttempts: 1,
				DelaysMs:         []int{0, 0},
				SessionStrategy:  []transportretry.SessionRetryMode{transportretry.SessionRetrySame, transportretry.SessionRetrySame},
				Enabled:          true,
			},
		},
	}

	var calls atomic.Int32
	execute := func(_ string, opts transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		i := calls.Add(1)
		if i == 1 {
			return transportretry.ResultView{Status: "failed", Error: "stub-transport-fail", SessionID: "sess-1"}, 0, nil
		}
		if opts.ResumeSessionID != "sess-1" {
			t.Fatalf("resume = %q, want sess-1", opts.ResumeSessionID)
		}
		return transportretry.ResultView{Status: "completed"}, 0, nil
	}

	result, _, stats, _, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"stub",
		"",
		transportretry.RetryHooks{},
		nil,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q", result.Status)
	}
	if stats.PolicyID != "stub_transport" {
		t.Fatalf("policy = %q", stats.PolicyID)
	}
}

func TestExecuteWithRetrySameSessionUsesStreamSessionID(t *testing.T) {
	t.Parallel()

	cfg := transportretry.Config{
		Enabled: true,
		Policies: []transportretry.Policy{
			{
				ID:               "cursor_writable_iterable",
				Providers:        []string{"cursor"},
				MatchError:       transportretry.DefaultPolicies()[0].MatchError,
				MaxExtraAttempts: 1,
				DelaysMs:         []int{0, 0},
				SessionStrategy: []transportretry.SessionRetryMode{
					transportretry.SessionRetrySame,
					transportretry.SessionRetrySame,
				},
				Enabled: true,
			},
		},
	}

	failErr := "WritableIterable is closed (result_seen=false)"
	var calls atomic.Int32
	execute := func(_ string, opts transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		i := calls.Add(1)
		if i == 1 {
			return transportretry.ResultView{
				Status:    "failed",
				Error:     failErr,
				SessionID: "sess-from-stream",
			}, 0, nil
		}
		if opts.ResumeSessionID != "sess-from-stream" {
			t.Fatalf("resume session = %q, want sess-from-stream", opts.ResumeSessionID)
		}
		return transportretry.ResultView{Status: "completed"}, 0, nil
	}

	result, _, _, _, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"",
		transportretry.RetryHooks{},
		nil,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestExecuteWithRetryExhaustedAfterFreshSession(t *testing.T) {
	t.Parallel()

	failErr := "WritableIterable is closed (result_seen=false)"
	cfg := transportretry.Config{
		Enabled: true,
		Policies: []transportretry.Policy{
			{
				ID:               "cursor_writable_iterable",
				Providers:        []string{"cursor"},
				MatchError:       transportretry.DefaultPolicies()[0].MatchError,
				MaxExtraAttempts: 2,
				DelaysMs:         []int{0, 0, 0, 0},
				SessionStrategy: []transportretry.SessionRetryMode{
					transportretry.SessionRetrySame,
					transportretry.SessionRetrySame,
					transportretry.SessionRetryFresh,
				},
				Enabled: true,
			},
		},
	}

	var calls atomic.Int32
	execute := func(_ string, _ transportretry.ExecOptionsView) (transportretry.ResultView, int32, error) {
		calls.Add(1)
		return transportretry.ResultView{Status: "failed", Error: failErr, SessionID: "sess-1"}, 0, nil
	}

	recorder := &testObserver{}
	_, _, stats, _, err := transportretry.ExecuteWithRetry(
		context.Background(),
		cfg,
		"cursor",
		"sess-prior",
		transportretry.RetryHooks{
			OnFreshSession: func(view *transportretry.ExecOptionsView) (string, string) {
				view.ResumeSessionID = ""
				return "fresh-prompt", "sess-prior"
			},
		},
		recorder,
		slog.Default(),
		execute,
		"prompt",
		transportretry.ExecOptionsView{ResumeSessionID: "sess-prior"},
	)
	if err != nil {
		t.Fatalf("ExecuteWithRetry: %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("calls = %d, want 4 launches before exhaustion", calls.Load())
	}
	if !stats.SurfacedToServer {
		t.Fatal("expected surfaced_to_server=true after exhausting fresh_session")
	}
	if stats.Attempts != 4 {
		t.Fatalf("attempts = %d, want 4", stats.Attempts)
	}
	if !recorder.exhausted {
		t.Fatal("expected exhausted outcome metric")
	}
	modes := []string{stats.SessionModes[0].String(), stats.SessionModes[1].String(), stats.SessionModes[2].String()}
	wantModes := []string{"same_session", "same_session", "fresh_session"}
	for i := range wantModes {
		if modes[i] != wantModes[i] {
			t.Fatalf("session_modes[%d] = %q, want %q", i, modes[i], wantModes[i])
		}
	}
}

type testObserver struct {
	exhausted bool
}

func (o *testObserver) RecordAttempt(_, _, _, outcome string) {
	if outcome == "exhausted" {
		o.exhausted = true
	}
}

func (o *testObserver) RecordRecovered(_, _, _ string) {}
func (o *testObserver) RecordCacheReadTokens(_, _ string, _ int64) {}
func (o *testObserver) RecordWallSeconds(_, _ string, _ float64) {}

func TestResolveConfigAgentCustomEnvOverridesEnv(t *testing.T) {
	t.Setenv("MULTICA_TRANSPORT_RETRY_CONFIG", `{"enabled":true}`)
	cfg := transportretry.ResolveConfig(map[string]string{
		"MULTICA_TRANSPORT_RETRY_CONFIG": `{"enabled":false}`,
	})
	if cfg.Enabled {
		t.Fatal("agent custom_env should override process env")
	}
}

func TestMetricsRecordRecovered(t *testing.T) {
	m := transportretry.GlobalMetrics()
	m.RecordRecovered("cursor", "cursor_writable_iterable", "same_session")
	m.RecordAttempt("cursor", "cursor_writable_iterable", "same_session", "recovered")
}
