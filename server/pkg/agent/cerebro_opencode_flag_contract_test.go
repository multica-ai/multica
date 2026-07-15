package agent

import (
	"os/exec"
	"strings"
	"testing"
)

// FIR-3212. The existing opencode tests drive fakeOpencodeScript(), a stub that
// accepts any argv. A fake that accepts everything cannot detect a flag that
// exists nowhere — which is why `--prompt` reached production unnoticed and
// broke every OpenCode run.
//
// These tests are the missing half: they assert our flag contract against the
// REAL installed binary. They skip (never fail) when opencode is not installed,
// so CI without the binary stays green.

// lookupOpencodeHelp returns `opencode run --help` output, or skips the test.
func lookupOpencodeHelp(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed on this host; skipping real-binary flag contract test")
	}
	// `run --help` exits 0 and prints the flag table for the run subcommand.
	out, err := exec.Command(path, "run", "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("opencode run --help failed: %v\n%s", err, out)
	}
	return string(out)
}

// The premise of the daemon-side fix: --prompt is not a real OpenCode flag.
// If a future OpenCode release adds it, this test fails and tells us the
// inline-brief path may be reconsidered on purpose rather than by accident.
func TestInstalledOpencodeRejectsPromptFlag(t *testing.T) {
	help := lookupOpencodeHelp(t)
	if strings.Contains(help, "--prompt") {
		t.Fatal("installed opencode now advertises --prompt; " +
			"revisit providerNeedsInlineSystemPrompt(\"opencode\") in server/internal/daemon")
	}

	// Belt and braces: prove the CLI actually rejects it rather than ignoring it.
	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed")
	}
	out, err := exec.Command(path, "run", "--format", "json",
		"--dangerously-skip-permissions", "--prompt", "you are a bot", "say hi").CombinedOutput()
	if err == nil {
		t.Fatalf("expected opencode to reject --prompt, but it exited 0:\n%s", out)
	}
}

// Every flag the opencode backend can emit must exist in the installed CLI.
// This is the generalised guard: it would have caught --prompt on day one, and
// catches the next flag an OpenCode upgrade removes.
func TestOpencodeEmittedFlagsExistInInstalledCLI(t *testing.T) {
	help := lookupOpencodeHelp(t)

	// The flags opencode.go builds in Execute().
	emitted := []string{
		"--format",
		"--dangerously-skip-permissions",
		"--dir",
		"--model",
		"--variant",
		"--session",
	}
	for _, flag := range emitted {
		if !strings.Contains(help, flag) {
			t.Errorf("opencode backend emits %s but the installed CLI does not support it; "+
				"the run will exit 1 with a usage dump", flag)
		}
	}
}
