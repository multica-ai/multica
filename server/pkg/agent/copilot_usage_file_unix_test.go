//go:build unix

package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// copilotUsageFileScript is a fake Copilot CLI that records its argv and, like
// the real one, writes the usage summary to --usage-output-file on exit.
func copilotUsageFileScript(argvPath string) string {
	return fmt.Sprintf(`#!/bin/sh
: > %[1]q
out=""
prev=""
for a in "$@"; do
  printf '%%s\n' "$a" >> %[1]q
  if [ "$prev" = "--usage-output-file" ]; then out="$a"; fi
  prev="$a"
done
cat > /dev/null
if [ -n "$out" ]; then
  cat > "$out" <<'JSON'
%[2]s
JSON
fi
`, argvPath, fixtureUsageFile) + copilotStreamTail
}

func runCopilotFakeWithConfig(t *testing.T, script string, cfg Config, opts ExecOptions) Result {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "copilot")
	writeTestExecutable(t, fakePath, []byte(script))
	cfg.ExecutablePath = fakePath
	cfg.Logger = slog.Default()

	backend, err := New("copilot", cfg)
	if err != nil {
		t.Fatalf("New(copilot): %v", err)
	}
	opts.Timeout = 30 * time.Second
	session, err := backend.Execute(t.Context(), "Reply with just OK", opts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		return result
	case <-time.After(30 * time.Second):
		t.Fatal("copilot run never produced a result")
		return Result{}
	}
}

func readArgv(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recorded argv: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

// TestCopilotExecuteReportsUsageFromUsageFile is the regression for #10501:
// a completed run on a modern CLI reported no tokens, because the JSON stream
// carries none and the --usage-output-file summary was never requested.
func TestCopilotExecuteReportsUsageFromUsageFile(t *testing.T) {
	t.Parallel()

	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	result := runCopilotFakeWithConfig(t, copilotUsageFileScript(argvPath),
		Config{BuiltinRuntime: true, CLIVersion: "1.0.109"}, ExecOptions{})

	if result.Status != "completed" {
		t.Fatalf("status = %q (%s), want completed", result.Status, result.Error)
	}
	want := TokenUsage{InputTokens: 3, OutputTokens: 5, CacheWriteTokens: 17817}
	if got := result.Usage["gpt-6.1-sol"]; got != want {
		t.Fatalf("usage = %#v, want %#v", result.Usage, want)
	}

	argv := readArgv(t, argvPath)
	var usageFile string
	for i, a := range argv {
		if a == "--usage-output-file" && i+1 < len(argv) {
			usageFile = argv[i+1]
		}
	}
	if usageFile == "" {
		t.Fatalf("expected --usage-output-file in argv, got %v", argv)
	}
	// The per-run file and its directory are gone once the run reports.
	if _, err := os.Stat(filepath.Dir(usageFile)); !os.IsNotExist(err) {
		t.Fatalf("expected the usage file directory to be removed, stat err = %v", err)
	}
}

func TestCopilotExecuteKeepsArgvForOlderCLI(t *testing.T) {
	t.Parallel()
	// A CLI that may not know the flag gets today's argv, and the run simply
	// stays unreported as before rather than failing on an unknown option.
	argvPath := filepath.Join(t.TempDir(), "argv.txt")
	result := runCopilotFakeWithConfig(t, copilotUsageFileScript(argvPath),
		Config{BuiltinRuntime: true, CLIVersion: "1.0.77"}, ExecOptions{})

	if result.Status != "completed" {
		t.Fatalf("status = %q (%s), want completed", result.Status, result.Error)
	}
	for _, a := range readArgv(t, argvPath) {
		if a == "--usage-output-file" {
			t.Fatalf("expected no --usage-output-file for CLI 1.0.77")
		}
	}
	if len(result.Usage) != 0 {
		t.Fatalf("expected no usage, got %#v", result.Usage)
	}
}
