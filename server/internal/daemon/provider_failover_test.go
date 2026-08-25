package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// GAP-5 (issue #4): opt-in provider failover chain.

func TestParseProviderFailover(t *testing.T) {
	cases := []struct {
		raw  string
		want map[string][]string
	}{
		{"", nil},
		{"  ", nil},
		{"codex:claude", map[string][]string{"codex": {"claude"}}},
		{
			"codex:claude,kimi-k2.6;claude:qwen3.7-plus",
			map[string][]string{"codex": {"claude", "kimi-k2.6"}, "claude": {"qwen3.7-plus"}},
		},
		{"codex: claude , kimi ", map[string][]string{"codex": {"claude", "kimi"}}}, // whitespace trimmed
		{"codex:claude,claude", map[string][]string{"codex": {"claude"}}},           // duplicate fallback dropped
		{"codex:codex", nil},                                                        // self-fallback → entry skipped
		{":claude;bogus", nil},                                                      // bad entries skipped, nothing left
	}
	for _, tc := range cases {
		got := parseProviderFailover(tc.raw)
		if tc.want == nil {
			if got != nil {
				t.Errorf("parseProviderFailover(%q) = %v, want nil", tc.raw, got)
			}
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("parseProviderFailover(%q) = %v, want %v", tc.raw, got, tc.want)
			continue
		}
		for k, v := range tc.want {
			if len(got[k]) != len(v) {
				t.Errorf("parseProviderFailover(%q)[%q] = %v, want %v", tc.raw, k, got[k], v)
				continue
			}
			for i := range v {
				if got[k][i] != v[i] {
					t.Errorf("parseProviderFailover(%q)[%q][%d] = %q, want %q", tc.raw, k, i, got[k][i], v[i])
				}
			}
		}
	}
}

func TestIsProviderNetworkError(t *testing.T) {
	for _, msg := range []string{
		"dial tcp 1.2.3.4:443: connect: connection refused",
		"API Error: Connection closed mid-response.",
		"API Error: 429 Too Many Requests",
	} {
		if !isProviderNetworkError(errors.New(msg)) {
			t.Errorf("isProviderNetworkError(%q) = false, want true", msg)
		}
	}
	for _, err := range []error{
		nil,
		errors.New("invalid api key: check your MULTICA key"),
		errors.New("prompt is too long: context overflow"),
		errors.New("task failed: agent_error.auth"),
	} {
		if isProviderNetworkError(err) {
			t.Errorf("isProviderNetworkError(%v) = true, want false", err)
		}
	}
}

// failoverTestDaemon wires a minimal Daemon whose fake runner records the
// providers it was asked to run, keyed off cfg.ProviderFailover.
func failoverTestDaemon(t *testing.T, failover func(provider string) (TaskResult, error)) (*Daemon, *[]string) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.HasSuffix(r.URL.Path, "/status") {
			_, _ = w.Write([]byte(`{"status":"cancelled"}`))
		}
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	tried := &[]string{}
	d := &Daemon{
		client:             NewClient(srv.URL),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		runtimeIndex:       map[string]Runtime{"rt-1": {ID: "rt-1", Provider: "codex"}},
		cancelPollInterval: time.Hour,
		cfg:                Config{ProviderFailover: map[string][]string{"codex": {"claude"}}},
	}
	d.runner = taskRunnerFunc(func(_ context.Context, _ Task, provider string, _ int, _ *slog.Logger) (TaskResult, error) {
		mu.Lock()
		*tried = append(*tried, provider)
		mu.Unlock()
		return failover(provider)
	})
	d.handleTask(context.Background(), Task{ID: "t-failover", RuntimeID: "rt-1", Agent: &AgentData{Name: "a"}}, 0)
	return d, tried
}

func TestHandleTask_ProviderFailoverOn429(t *testing.T) {
	_, tried := failoverTestDaemon(t, func(provider string) (TaskResult, error) {
		if provider == "codex" {
			return TaskResult{}, errors.New("API Error: 429 Too Many Requests")
		}
		return TaskResult{Status: "completed"}, nil
	})
	if len(*tried) != 2 || (*tried)[0] != "codex" || (*tried)[1] != "claude" {
		t.Errorf("attempts = %v, want [codex claude]", *tried)
	}
}

func TestHandleTask_NoFailoverOnNonNetworkError(t *testing.T) {
	_, tried := failoverTestDaemon(t, func(string) (TaskResult, error) {
		return TaskResult{}, errors.New("invalid api key: check your credentials")
	})
	if len(*tried) != 1 {
		t.Errorf("attempts = %v, want single attempt (no chain walk)", *tried)
	}
}
