package daemon

import (
	"context"
	"io"
	"log/slog"
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

			got, _ := resolveTaskModelSelection(context.Background(), tt.provider, agent.Command{}, tt.in, quietTaskLog())
			if got != tt.want {
				t.Errorf("resolveTaskModelSelection(%s, %+v) = %+v, want %+v", tt.provider, tt.in, got, tt.want)
			}
			if reads() != tt.wantReads {
				t.Errorf("catalog reads = %d, want %d", reads(), tt.wantReads)
			}
		})
	}
}

// TestResolveTaskModelSelectionDropsCrossProviderIncompatibleModel guards the
// runtime-level quota fallback (SE-37711 / SE-37664, invariants I10/I15): when a
// quota circuit routes an execution onto a runtime whose provider differs from
// the one the persisted model pin was made for, a known-incompatible pin must be
// dropped for this execution so the runtime launches with its own default model
// — the gpt-5.6-sol → Claude pin must never reach the CLI. The static-catalog
// classification does this without any discovery subprocess, so a dropped pin
// costs zero catalog reads; unknown/custom ids the server cannot confidently
// classify pass through untouched.
func TestResolveTaskModelSelectionDropsCrossProviderIncompatibleModel(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		in        taskModelSelection
		want      taskModelSelection
		wantReads int
	}{
		{
			// The canonical failure the fallback must prevent: an OpenAI/Codex
			// model pinned on an agent whose fallback runtime speaks Claude.
			name:      "codex model on a claude runtime is dropped",
			provider:  "claude",
			in:        taskModelSelection{Model: "gpt-5.6-sol"},
			want:      taskModelSelection{Model: ""},
			wantReads: 0,
		},
		{
			name:      "claude model on a codex runtime is dropped",
			provider:  "codex",
			in:        taskModelSelection{Model: "claude-opus-5"},
			want:      taskModelSelection{Model: ""},
			wantReads: 0,
		},
		{
			// A context-window variant is the same Claude model; it stays.
			name:      "claude context-window variant on a claude runtime is kept",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5[1m]"},
			want:      taskModelSelection{Model: "claude-opus-5[1m]"},
			wantReads: 0,
		},
		{
			name:      "native model on its own provider is kept",
			provider:  "claude",
			in:        taskModelSelection{Model: "claude-opus-5"},
			want:      taskModelSelection{Model: "claude-opus-5"},
			wantReads: 0,
		},
		{
			// A manual/custom id the server cannot classify against a static
			// catalog is left alone rather than erased.
			name:      "unknown custom pin passes through untouched",
			provider:  "claude",
			in:        taskModelSelection{Model: "my-org-tuned-model"},
			want:      taskModelSelection{Model: "my-org-tuned-model"},
			wantReads: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := stubModelDiscovery(t, thinkingCatalogs())

			got, _ := resolveTaskModelSelection(context.Background(), tt.provider, agent.Command{}, tt.in, quietTaskLog())
			if got != tt.want {
				t.Errorf("resolveTaskModelSelection(%s, %+v) = %+v, want %+v", tt.provider, tt.in, got, tt.want)
			}
			if reads() != tt.wantReads {
				t.Errorf("catalog reads = %d, want %d", reads(), tt.wantReads)
			}
		})
	}
}

// TestResolveTaskModelSelectionClearsPinUnresolvableAgainstLiveCatalog is the
// SE-37741 F8 fail-safe: when a quota/auth circuit routes an execution onto a
// fallback runtime whose provider differs from the one a pin was made for, the
// pin — and the capability overrides keyed on it — must be cleared to the
// runtime default rather than reaching the CLI and failing the launch. The
// clearing is driven by the runtime's authoritative live catalog (reusing the
// read qualification already took, never an extra one) and records a model_action
// for the audit. The agent's saved configuration is never mutated: this operates
// on the by-value selector only.
func TestResolveTaskModelSelectionClearsPinUnresolvableAgainstLiveCatalog(t *testing.T) {
	gatewayOpus := agent.Model{ID: "multica-anthropic/claude/claude-opus-5", Provider: "multica-anthropic"}
	tests := []struct {
		name       string
		provider   string
		catalogs   map[string]agent.Catalog
		in         taskModelSelection
		want       taskModelSelection
		wantAction string
		wantReads  int
	}{
		{
			// The canonical failure: an OpenAI/Codex model pinned on an agent
			// whose fallback runtime speaks Claude. Static classification catches
			// it with no catalog read, and clears the thinking level with it.
			name:       "gpt-5.6-sol on a claude fallback is cleared statically with its overrides",
			provider:   "claude",
			catalogs:   map[string]agent.Catalog{},
			in:         taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high"},
			want:       taskModelSelection{},
			wantAction: modelActionClearedIncompatible,
			wantReads:  0,
		},
		{
			// opencode cannot launch an unqualified selector, so it reads the
			// catalog even with no override; a pin absent from that catalog is
			// unknown to this runtime and is cleared.
			name:       "unknown pin on opencode is cleared against the live catalog",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Models: []agent.Model{gatewayOpus}}},
			in:         taskModelSelection{Model: "gpt-5.6-sol"},
			want:       taskModelSelection{},
			wantAction: modelActionClearedUnresolved,
			wantReads:  1,
		},
		{
			name:       "cleared unresolved pin drops thinking and service tier too",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Models: []agent.Model{gatewayOpus}}},
			in:         taskModelSelection{Model: "gpt-5.6-sol", ThinkingLevel: "high", ServiceTier: "priority"},
			want:       taskModelSelection{},
			wantAction: modelActionClearedUnresolved,
			wantReads:  1,
		},
		{
			// A bare id two providers both expose cannot be placed; qualification
			// declines to guess, so it stays unresolved and is cleared.
			name:     "ambiguous bare id is cleared",
			provider: "deveco",
			catalogs: map[string]agent.Catalog{"deveco": {Models: []agent.Model{
				{ID: "prov-a/shared-model", Provider: "prov-a"},
				{ID: "prov-b/shared-model", Provider: "prov-b"},
			}}},
			in:         taskModelSelection{Model: "shared-model"},
			want:       taskModelSelection{},
			wantAction: modelActionClearedUnresolved,
			wantReads:  1,
		},
		{
			// An empty (but successful) catalog read means the runtime advertises
			// nothing we can honour; fail safe to the runtime default.
			name:       "empty live catalog clears the pin",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Models: []agent.Model{}}},
			in:         taskModelSelection{Model: "some-model"},
			want:       taskModelSelection{},
			wantAction: modelActionClearedUnresolved,
			wantReads:  1,
		},
		{
			// A uniquely-qualified pin is exactly the fallback we want to keep:
			// the catalog names one owner, so it is promoted and kept.
			name:       "uniquely qualified pin is kept and reported qualified",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Models: []agent.Model{gatewayOpus}}},
			in:         taskModelSelection{Model: "claude/claude-opus-5"},
			want:       taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5"},
			wantAction: modelActionQualified,
			wantReads:  1,
		},
		{
			name:       "exact catalog id is kept",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Models: []agent.Model{gatewayOpus}}},
			in:         taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5"},
			want:       taskModelSelection{Model: "multica-anthropic/claude/claude-opus-5"},
			wantAction: modelActionKept,
			wantReads:  1,
		},
		{
			// A static fallback catalog is a stand-in, not the runtime's real
			// list — it must never be the basis for erasing a pin.
			name:       "fallback catalog does not clear the pin",
			provider:   "opencode",
			catalogs:   map[string]agent.Catalog{"opencode": {Fallback: true, Models: []agent.Model{gatewayOpus}}},
			in:         taskModelSelection{Model: "some-unlisted-model"},
			want:       taskModelSelection{Model: "some-unlisted-model"},
			wantAction: modelActionKept,
			wantReads:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := stubModelDiscovery(t, tt.catalogs)

			got, action := resolveTaskModelSelection(context.Background(), tt.provider, agent.Command{}, tt.in, quietTaskLog())
			if got != tt.want {
				t.Errorf("selection = %+v, want %+v", got, tt.want)
			}
			if action != tt.wantAction {
				t.Errorf("model_action = %q, want %q", action, tt.wantAction)
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
	got, action := resolveTaskModelSelection(context.Background(), "opencode", agent.Command{}, in, quietTaskLog())
	if got != in {
		t.Errorf("resolveTaskModelSelection on discovery error = %+v, want %+v unchanged", got, in)
	}
	if action != modelActionKept {
		t.Errorf("model_action on discovery error = %q, want %q — a failed read must not clear the pin", action, modelActionKept)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("catalog reads = %d, want 1 — a failed read must not be retried within the task", calls)
	}
}
