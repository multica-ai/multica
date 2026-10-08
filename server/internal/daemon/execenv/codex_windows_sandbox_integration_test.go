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
			// The raw tokens still carry shell quotes, so scanning them directly
			// must NOT recognize the opt-in — this is the drift the bug was.
			if got := windowsSandboxFromCustomArgs(tc.raw); got == windowsSandboxNative {
				t.Fatalf("raw shell-quoted args unexpectedly recognized without normalization: %v", tc.raw)
			}
			// After the daemon's normalization the same args resolve to native,
			// so the policy keeps workspace-write instead of loosening.
			norm := agent.NormalizeCodexLaunchArgs(nil, tc.raw, nil, testLogger())
			missing := filepath.Join(t.TempDir(), "config.toml")
			state := resolveWindowsSandboxState(missing, nil, sharedConfigAbsent, norm, testLogger())
			if state != windowsSandboxNative {
				t.Fatalf("normalized %v -> state %v, want native", norm, state)
			}
			if p := codexSandboxPolicyForWindows(state); p.Mode != "workspace-write" {
				t.Fatalf("policy mode = %q, want workspace-write (isolation preserved)", p.Mode)
			}
		})
	}
}
