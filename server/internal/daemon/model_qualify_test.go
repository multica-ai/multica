package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// stubModelDiscovery replaces the daemon's listModels indirection with a
// counting fake, so a test can assert BOTH what a task resolved to and how
// many discovery rounds it took to get there. The count is the contract:
// discovery is a CLI subprocess with a 15-30s ceiling that cachedDiscovery
// does not memoize when it returns empty or fallback, so "once" and "never"
// are behaviours worth pinning, not implementation detail.
func stubModelDiscovery(t *testing.T, catalogs map[string]agent.Catalog) func() int {
	t.Helper()
	var mu sync.Mutex
	calls := 0

	orig := listModels
	listModels = func(_ context.Context, provider string, _ agent.Command) (agent.Catalog, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return catalogs[provider], nil
	}
	t.Cleanup(func() { listModels = orig })

	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func quietTaskLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// thinkingCatalogs mirrors the shapes the reporter's gateway config produces:
// a slash-shaped model id under a custom provider, advertising a reasoning
// catalog. codex carries a service tier so the codex-specific paths are real.
func thinkingCatalogs() map[string]agent.Catalog {
	gatewayOpus := agent.Model{
		ID:       "multica-anthropic/claude/claude-opus-5",
		Provider: "multica-anthropic",
		Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
			{Value: "high", Label: "High"},
		}},
	}
	return map[string]agent.Catalog{
		"pi":       {Models: []agent.Model{gatewayOpus}},
		"opencode": {Models: []agent.Model{gatewayOpus}},
		"omp":      {Models: []agent.Model{gatewayOpus}},
		"claude": {Models: []agent.Model{{
			ID:       "claude-opus-5",
			Provider: "anthropic",
			Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
				{Value: "high", Label: "High"},
			}},
		}}},
		"codex": {Models: []agent.Model{{
			ID:           "gpt-5.6-sol",
			Provider:     "openai",
			ServiceTiers: []agent.ModelServiceTier{{ID: "priority", Name: "Priority"}},
			Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{
				{Value: "high", Label: "High"},
			}},
		}}},
	}
}

// TestResolveTaskModelSelectionReadsTheCatalogAtMostOnce is the production
// path the previous round left unguarded (MUL-6471 review): a task that both
// qualifies its model and validates a capability override must not pay for
// discovery twice. It also pins the other half — the tasks that must not
// reach discovery at all.
func TestResolveTaskModelSelectionReadsTheCatalogAtMostOnce(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		in        taskModelSelection
		want      taskModelSelection
		wantReads int
	}{
		{
			// The reporter's exact configuration. One read serves both the
			// selector promotion and the thinking-level check; before the fix
			// the id never matched the catalog, so the level was dropped.
			name:      "pi qualifies and validates on a single read",
			provider:  "pi",
			in:        taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			name:      "opencode qualifies and validates on a single read",
			provider:  "opencode",
			in:        taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			// pi launches an unqualified id correctly on its own, so with no
			// capability override there is nothing to look up.
			name:      "pi without a capability override never reads the catalog",
			provider:  "pi",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "claude/claude-opus-5"},
			wantReads: 0,
		},
		{
			name:      "omp inherits pi's launch contract",
			provider:  "omp",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "claude/claude-opus-5"},
			wantReads: 0,
		},
		{
			// opencode cannot launch an unqualified selector, so it reads even
			// with no capability override — the one case that must pay.
			name:      "opencode reads even without a capability override",
			provider:  "opencode",
			in:        taskModelSelection{Model: "claude/claude-opus-5"},
			want:      taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5"},
			wantReads: 1,
		},
		{
			name:      "claude with no override never reads the catalog",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5"},
			want:      taskModelSelection{Model: "claude-opus-5"},
			wantReads: 0,
		},
		{
			name:      "claude with a thinking level reads exactly once",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5", ThinkingLevel: "high"},
			want:      taskModelSelection{Model: "claude-opus-5", ThinkingLevel: "high"},
			wantReads: 1,
		},
		{
			// codex validates both overrides; they share the one read.
			name:      "codex validates thinking and service tier on a single read",
			provider:  "codex",
			in:        taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			want:      taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			wantReads: 1,
		},
		{
			// codex with no explicit model fails both checks closed, and does
			// so without a catalog read — the guard predates this change and
			// must survive it.
			name:      "codex without a model fails closed without reading",
			provider:  "codex",
			in:        taskModelSelection{ThinkingLevel: "high", ServiceTier: "priority"},
			want:      taskModelSelection{},
			wantReads: 0,
		},
		{
			name:      "no pinned model and no overrides reads nothing",
			provider:  "opencode",
			in:        taskModelSelection{},
			want:      taskModelSelection{},
			wantReads: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := stubModelDiscovery(t, thinkingCatalogs())

			got := resolveTaskModelSelection(context.Background(), tt.provider, agent.Command{}, tt.in, quietTaskLog())
			if got != tt.want {
				t.Errorf("resolveTaskModelSelection(%s, %+v) = %+v, want %+v", tt.provider, tt.in, got, tt.want)
			}
			if reads() != tt.wantReads {
				t.Errorf("catalog reads = %d, want %d", reads(), tt.wantReads)
			}
		})
	}
}

// A runtime that cannot answer must not block the task: the persisted model
// may well be exactly what its CLI expects, and a stale-looking capability
// override is kept rather than silently dropped on a transient failure. The
// failed read is still only attempted once.
func TestResolveTaskModelSelectionFailsOpenOnDiscoveryError(t *testing.T) {
	var mu sync.Mutex
	calls := 0

	orig := listModels
	listModels = func(_ context.Context, _ string, _ agent.Command) (agent.Catalog, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return agent.Catalog{}, context.DeadlineExceeded
	}
	t.Cleanup(func() { listModels = orig })

	in := taskModelSelection{Model: "claude/claude-opus-5", ThinkingLevel: "high"}
	got := resolveTaskModelSelection(context.Background(), "opencode", agent.Command{}, in, quietTaskLog())
	if got != in {
		t.Errorf("resolveTaskModelSelection on discovery error = %+v, want %+v unchanged", got, in)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("catalog reads = %d, want 1 — a failed read must not be retried within the task", calls)
	}
}

// A catalog discovery did not verify — a static fallback or an empty list —
// cannot say what the runtime supports, so every provider launches with the
// saved selection untouched, even where the stand-in disagrees with it. The
// installed binary's own effort vocabulary is the only thing still enforced
// (MUL-7691).
func TestResolveTaskModelSelectionPassesThroughUnverifiedCatalogs(t *testing.T) {
	levels := func(values ...string) *agent.ModelThinking {
		out := &agent.ModelThinking{}
		for _, v := range values {
			out.SupportedLevels = append(out.SupportedLevels, agent.ThinkingLevel{Value: v})
		}
		return out
	}
	claudeFallback := func(cli []string) agent.Catalog {
		return agent.Catalog{
			Models: []agent.Model{
				{ID: "claude-sonnet-4-6", Default: true, Thinking: levels("low", "medium", "high")},
			},
			Fallback:          true,
			CLIThinkingLevels: cli,
		}
	}
	full := []string{"low", "medium", "high", "xhigh", "max"}
	for _, tc := range []struct {
		name     string
		provider string
		catalog  agent.Catalog
		in, want taskModelSelection
	}{
		{
			name: "claude model newer than the static list", provider: "claude", catalog: claudeFallback(full),
			in:   taskModelSelection{Model: "claude-opus-5-5", ThinkingLevel: "xhigh"},
			want: taskModelSelection{Model: "claude-opus-5-5", ThinkingLevel: "xhigh"},
		},
		{
			name: "claude listed model the stand-in narrows", provider: "claude", catalog: claudeFallback(full),
			in:   taskModelSelection{Model: "claude-sonnet-4-6", ThinkingLevel: "max"},
			want: taskModelSelection{Model: "claude-sonnet-4-6", ThinkingLevel: "max"},
		},
		{
			name: "claude empty model does not borrow the stand-in default", provider: "claude", catalog: claudeFallback(full),
			in:   taskModelSelection{ThinkingLevel: "xhigh"},
			want: taskModelSelection{ThinkingLevel: "xhigh"},
		},
		{
			name: "claude CLI without --effort still drops the level", provider: "claude", catalog: claudeFallback([]string{}),
			in:   taskModelSelection{Model: "claude-opus-5-5", ThinkingLevel: "high"},
			want: taskModelSelection{Model: "claude-opus-5-5"},
		},
		{
			name: "grok", provider: "grok",
			catalog: agent.Catalog{Models: []agent.Model{{ID: "grok-4.5", Thinking: levels("low", "medium", "high")}}, Fallback: true},
			in:      taskModelSelection{Model: "grok-4.5", ThinkingLevel: "xhigh"},
			want:    taskModelSelection{Model: "grok-4.5", ThinkingLevel: "xhigh"},
		},
		{
			name: "codex service tier", provider: "codex",
			catalog: agent.Catalog{Models: []agent.Model{{ID: "gpt-5.5", Thinking: levels("low")}}, Fallback: true},
			in:      taskModelSelection{Model: "gpt-5.5", ThinkingLevel: "xhigh", ServiceTier: "priority"},
			want:    taskModelSelection{Model: "gpt-5.5", ThinkingLevel: "xhigh", ServiceTier: "priority"},
		},
		{
			name: "empty opencode catalog", provider: "opencode", catalog: agent.Catalog{Models: []agent.Model{}},
			in:   taskModelSelection{Model: "deepseek/deepseek-v4", ThinkingLevel: "max"},
			want: taskModelSelection{Model: "deepseek/deepseek-v4", ThinkingLevel: "max"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := stubModelDiscovery(t, map[string]agent.Catalog{tc.provider: tc.catalog})
			got := resolveTaskModelSelection(context.Background(), tc.provider, agent.Command{}, tc.in, quietTaskLog())
			if got != tc.want {
				t.Errorf("launch selection = %+v, want %+v", got, tc.want)
			}
			if reads() != 1 {
				t.Errorf("catalog reads = %d, want 1", reads())
			}
		})
	}
}

// Exercise real discovery through the daemon's task-launch guard rather than
// injecting a prebuilt fallback: live discovery fails, bundled succeeds but
// lacks the saved model. Model-scoped overrides pass through; the CLI-version
// gate for explicit standard routing must still reject old Codex binaries.
func TestResolveTaskModelSelectionKeepsLiveOnlyCodexOverridesOnFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake binary requires a POSIX shell")
	}
	for _, tc := range []struct {
		name, version string
		in, want      taskModelSelection
	}{
		{
			name: "new CLI retains model-scoped overrides", version: "0.155.1",
			in:   taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			want: taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
		},
		{
			name: "old CLI rejects explicit standard despite missing model", version: "0.130.0",
			in:   taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high", ServiceTier: "default"},
			want: taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high"},
		},
		{
			name: "new CLI accepts explicit standard despite missing model", version: "0.155.1",
			in:   taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high", ServiceTier: "default"},
			want: taskModelSelection{Model: "gpt-6-sol", ThinkingLevel: "high", ServiceTier: "default"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			logFile := filepath.Join(dir, "calls")
			binary := filepath.Join(dir, "codex")
			script := "#!/bin/sh\n" +
				"printf '%s\\n' \"$*\" >> '" + logFile + "'\n" +
				"if [ \"$1\" = \"--version\" ]; then echo 'codex-cli " + tc.version + "'; exit 0; fi\n" +
				"if [ \"$3\" = \"--bundled\" ]; then echo '{\"models\":[{\"slug\":\"gpt-5.5\",\"display_name\":\"GPT-5.5\",\"visibility\":\"list\"}]}'; exit 0; fi\n" +
				"exit 1\n"
			if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}

			got := resolveTaskModelSelection(context.Background(), "codex", agent.Command{Path: binary}, tc.in, quietTaskLog())
			if got != tc.want {
				t.Fatalf("launch selection = %+v, want %+v", got, tc.want)
			}
			calls, err := os.ReadFile(logFile)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(calls), "debug models\n") || !strings.Contains(string(calls), "debug models --bundled\n") {
				t.Fatalf("expected failed live discovery and successful bundled fallback, got %q", calls)
			}
			if strings.Count(string(calls), "debug models --bundled\n") != 1 {
				t.Fatalf("task capability checks must share a single fallback discovery: %q", calls)
			}
		})
	}
}
