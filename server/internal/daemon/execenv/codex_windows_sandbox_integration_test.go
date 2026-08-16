package execenv

import (
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// TestWindowsSandboxHonorsShellQuotedCustomArg is the MUL-4957 round-3 must-fix
// 2 integration test. A `-c windows.sandbox=...` opt-in supplied shell-quoted
// (as users commonly type custom_args) reaches Codex normalized by
// agent.NormalizeCodexLaunchArgs; the Windows sandbox decision must consume the
// SAME normalized args, not the raw tokens, or the two drift and the user's
// isolation opt-in is silently downgraded. This locks the raw-args →
// normalized → policy chain end to end across the two packages so they cannot
// diverge again.
func TestWindowsSandboxHonorsShellQuotedCustomArg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []string
	}{
		{"both tokens single-quoted", []string{"'-c'", "'windows.sandbox=unelevated'"}},
		{"value double-quoted", []string{"-c", `"windows.sandbox=elevated"`}},
		{"inline flag single-quoted", []string{"'-c=windows.sandbox=unelevated'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The shared parser owns the same one-layer quote cleanup as launch
			// normalization, so raw and normalized classification cannot drift.
			rawState := windowsSandboxFromCustomArgs(tc.raw)
			norm := agent.NormalizeCodexLaunchArgs(nil, tc.raw, nil, testLogger())
			missing := filepath.Join(t.TempDir(), "config.toml")
			state := resolveWindowsSandboxState(missing, nil, sharedConfigAbsent, norm, testLogger())
			if rawState != windowsSandboxNative || state != rawState {
				t.Fatalf("classification drift: raw=%v normalized=%v args=%v", rawState, state, norm)
			}
			if p := codexSandboxPolicyForWindows(state); p.Mode != "workspace-write" {
				t.Fatalf("policy mode = %q, want workspace-write (isolation preserved)", p.Mode)
			}
		})
	}
}
