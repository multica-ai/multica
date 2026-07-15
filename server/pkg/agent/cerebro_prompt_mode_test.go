package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// FIR-3212. claude.go hardcoded --append-system-prompt, so an agent could never
// REPLACE a runtime's own system prompt even though the installed Claude CLI has
// supported --system-prompt for that all along. These tests pin the mode contract
// and, critically, pin it to what the installed CLI actually accepts rather than
// to a hand-written table (the same class of mistake that made every OpenCode run
// die on a --prompt flag the CLI never had).

func TestSystemPromptSupportForClaudeAllowsAppendAndReplace(t *testing.T) {
	got, ok := SystemPromptSupportFor("claude")
	if !ok {
		t.Fatal("claude must have an authoritative system-prompt support entry")
	}
	if !got.Native {
		t.Error("claude has a real system-prompt channel; Native must be true")
	}
	for _, mode := range []SystemPromptMode{SystemPromptModeAppend, SystemPromptModeReplace} {
		if !got.Supports(mode) {
			t.Errorf("claude must support %q", mode)
		}
	}
	if got.Supports(SystemPromptModePrepend) {
		t.Error("claude has a native channel; prepending into the user message would be a downgrade and must not be offered")
	}
}

func TestSystemPromptSupportForPrependOnlyProviders(t *testing.T) {
	// Verified line-by-line: opencode.go:74-75, kiro.go:272-273, kimi.go:289-290,
	// openclaw.go:197-198 all splice the brief into the user message.
	for _, provider := range []string{"opencode", "kiro", "kimi", "openclaw"} {
		got, ok := SystemPromptSupportFor(provider)
		if !ok {
			t.Fatalf("%s must have an authoritative entry", provider)
		}
		if got.Native {
			t.Errorf("%s has no native system-prompt channel; Native must be false", provider)
		}
		if !got.Supports(SystemPromptModePrepend) {
			t.Errorf("%s must support prepend", provider)
		}
		if got.Supports(SystemPromptModeReplace) {
			t.Errorf("%s cannot replace a system prompt it has no channel for; claiming otherwise is the lie FIR-3212 exists to stop", provider)
		}
	}
}

func TestSystemPromptSupportForIgnoringProviders(t *testing.T) {
	// hermes.go:72-73 deliberately discards it; copilot/gemini/cursor/antigravity
	// have no SystemPrompt reference at all.
	for _, provider := range []string{"hermes", "copilot", "gemini", "cursor", "antigravity"} {
		got, ok := SystemPromptSupportFor(provider)
		if !ok {
			t.Fatalf("%s must have an authoritative entry", provider)
		}
		if got.Native {
			t.Errorf("%s does not consume SystemPrompt; Native must be false", provider)
		}
		if len(got.Modes) != 0 {
			t.Errorf("%s ignores SystemPrompt entirely; Modes must be empty, got %v", provider, got.Modes)
		}
	}
}

func TestSystemPromptSupportForUnknownProviderIsNotAuthoritative(t *testing.T) {
	// Mirrors StaticCatalog's contract (cerebro_model_catalog.go:11-25): absence of
	// proof is never proof of absence.
	if _, ok := SystemPromptSupportFor("some-runtime-we-have-never-seen"); ok {
		t.Error("an unknown provider must report ok=false, not a confident empty answer")
	}
}

func TestClaudeSystemPromptArgs(t *testing.T) {
	tests := []struct {
		name string
		mode SystemPromptMode
		want []string
	}{
		{
			// Today's behaviour, and what an empty mode must keep doing: the
			// daemon's brief adds to Claude's own prompt rather than erasing it.
			name: "default mode appends",
			mode: SystemPromptModeDefault,
			want: []string{"--append-system-prompt", "brief"},
		},
		{
			name: "append mode appends",
			mode: SystemPromptModeAppend,
			want: []string{"--append-system-prompt", "brief"},
		},
		{
			name: "replace mode replaces",
			mode: SystemPromptModeReplace,
			want: []string{"--system-prompt", "brief"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClaudeSystemPromptArgs(tc.mode, "brief")
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Errorf("ClaudeSystemPromptArgs(%q) = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}
}

func TestClaudeSystemPromptArgsEmptyPromptEmitsNothing(t *testing.T) {
	for _, mode := range []SystemPromptMode{SystemPromptModeDefault, SystemPromptModeAppend, SystemPromptModeReplace} {
		if got := ClaudeSystemPromptArgs(mode, ""); len(got) != 0 {
			t.Errorf("empty prompt in mode %q must emit no args, got %v", mode, got)
		}
	}
}

// TestInstalledClaudeAcceptsBothSystemPromptFlags is the guard that a hand-written
// table cannot give us. It asks the REAL binary whether the flags we intend to send
// exist. A fake that accepts everything can never catch a flag that does not exist —
// that is exactly how the OpenCode --prompt bug survived to production.
func TestInstalledClaudeAcceptsBothSystemPromptFlags(t *testing.T) {
	path, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not installed; capability assertion cannot run here")
	}
	out, err := exec.Command(path, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("claude --help failed: %v", err)
	}
	help := string(out)
	for _, flag := range []string{"--system-prompt", "--append-system-prompt"} {
		if !strings.Contains(help, flag) {
			t.Errorf("installed claude at %s does not advertise %s; the mode table claims it does", path, flag)
		}
	}
}
