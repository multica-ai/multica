package execenv

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// TestRuntimeConfigPath pins the provider→filename mapping the daemon
// log line relies on. If the mapping ever changes, the test catches it
// — operators expect to know exactly which file to `cat`.
func TestRuntimeConfigPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		want     string
	}{
		{"claude", "/work/CLAUDE.md"},
		{"codebuddy", "/work/CLAUDE.md"},
		{"codex", "/work/AGENTS.md"},
		{"copilot", "/work/AGENTS.md"},
		{"opencode", "/work/AGENTS.md"},
		{"openclaw", "/work/AGENTS.md"},
		{"hermes", "/work/AGENTS.md"},
		{"pi", "/work/AGENTS.md"},
		{"cursor", "/work/AGENTS.md"},
		{"kimi", "/work/AGENTS.md"},
		{"kiro", "/work/AGENTS.md"},
		{"qoder", "/work/AGENTS.md"},
		{"antigravity", "/work/AGENTS.md"},
		{"gemini", "/work/GEMINI.md"},
		{"totally-unknown", ""},
	}
	for _, tc := range cases {
		if got := RuntimeConfigPath("/work", tc.provider); got != tc.want {
			t.Errorf("RuntimeConfigPath(/work, %q) = %q, want %q", tc.provider, got, tc.want)
		}
	}
}

// TestBriefMode verifies the label flips with the feature flag. Nil-safe
// path returns "legacy" so a daemon that forgot to wire SetFeatureFlags
// emits a meaningful label, not panics.
func TestBriefMode(t *testing.T) {
	// Not t.Parallel-safe because we mutate the package-level flag pointer.
	saved := runtimeFlags.Load()
	t.Cleanup(func() { runtimeFlags.Store(saved) })

	// Nil service → legacy.
	runtimeFlags.Store(nil)
	if got := BriefMode(); got != "legacy" {
		t.Errorf("BriefMode with nil service = %q, want %q", got, "legacy")
	}

	// Flag off → legacy.
	off := featureflag.NewStaticProvider()
	off.Set(runtimeBriefSlimFlag, featureflag.Rule{Default: false})
	runtimeFlags.Store(featureflag.NewService(off))
	if got := BriefMode(); got != "legacy" {
		t.Errorf("BriefMode with flag off = %q, want %q", got, "legacy")
	}

	// Flag on → slim.
	on := featureflag.NewStaticProvider()
	on.Set(runtimeBriefSlimFlag, featureflag.Rule{Default: true})
	runtimeFlags.Store(featureflag.NewService(on))
	if got := BriefMode(); got != "slim" {
		t.Errorf("BriefMode with flag on = %q, want %q", got, "slim")
	}
}
