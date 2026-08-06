package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "run", "--pure", "--help")
	cmd.Env = opencodeContractEnv()
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opencode run --help failed: %v\n%s", err, out)
	}
	return string(out)
}

func opencodeContractEnv() []string {
	return append(os.Environ(),
		"OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_DEFAULT_PLUGINS=true",
		"OPENCODE_DISABLE_MODELS_FETCH=true",
	)
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
	// Use the permission flag this binary actually advertises, otherwise the
	// non-zero exit below could come from the permission flag rather than
	// from --prompt, and the assertion would pass for the wrong reason.
	cmd := exec.Command(path, "run", "--pure", "--format", "json",
		opencodePermissionFlag(path, slog.Default()), "--prompt", "you are a bot", "say hi")
	cmd.Env = opencodeContractEnv()
	cmd.Dir = t.TempDir()
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected opencode to reject --prompt, but it exited 0:\n%s", out)
	}
}

// Every flag the opencode backend can emit must exist in the installed CLI.
// This is the generalised guard: it would have caught --prompt on day one, and
// catches the next flag an OpenCode upgrade removes.
func TestOpencodeEmittedFlagsExistInInstalledCLI(t *testing.T) {
	help := lookupOpencodeHelp(t)

	// CEREBRO-PATCH(opencode-permission-flag): keep daemon argv aligned with
	// the exact installed CLI rather than a provider-version assumption.
	// The flags opencode.go builds in Execute(). The permission flag is not
	// listed literally — it is whichever spelling this binary advertises, and
	// opencodePermissionFlag() is what Execute() asks too.
	path, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("opencode not installed")
	}
	emitted := []string{
		"--format",
		opencodePermissionFlag(path, slog.Default()),
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
