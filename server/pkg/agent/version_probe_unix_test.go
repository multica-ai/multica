//go:build !windows

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeHermesForVersionProbe writes a fake hermes that appends each argv it is
// called with to a log, one line per invocation, and answers with body. It
// returns the script path and the log path.
func fakeHermesForVersionProbe(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "hermes")
	record := filepath.Join(dir, "argv.log")
	content := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n%s\n", record, body)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}
	return script, record
}

func readProbeInvocations(t *testing.T, record string) []string {
	t.Helper()
	data, err := os.ReadFile(record)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read argv log: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// MUL-9132: `hermes --version` runs an update check against GitHub, so the
// Hermes probe must go through `acp --version`, and a Hermes that answers it
// must never see the root `--version`.
func TestDetectVersionHermesUsesACPVersion(t *testing.T) {
	// The root --version "hangs" here, standing in for the slow network fetch:
	// reaching it would fail the test on the timeout as well as the argv check.
	script, record := fakeHermesForVersionProbe(t, `
if [ "$1" = "acp" ] && [ "$2" = "--version" ]; then echo 0.22.5; exit 0; fi
if [ "$1" = "--version" ]; then exec sleep 30; fi
exit 2`)
	orig := detectVersionTimeout
	detectVersionTimeout = 5 * time.Second
	t.Cleanup(func() { detectVersionTimeout = orig })

	version, err := DetectVersion(context.Background(), Command{Path: script}.ForVersionProbe("hermes"))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != "0.22.5" {
		t.Fatalf("version = %q, want 0.22.5", version)
	}
	if got, want := readProbeInvocations(t, record), []string{"acp --version"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("invocations = %q, want %q", got, want)
	}
}

// A custom runtime's fixed_args are Hermes global flags (`-p <profile>`), which
// argparse accepts only before the subcommand — the same position the backend
// launches them in.
func TestDetectVersionHermesKeepsLaunchPrefixBeforeACP(t *testing.T) {
	script, record := fakeHermesForVersionProbe(t, `echo 0.22.5`)

	cmd := NewCommand(script, []string{"-p", "work"}).ForVersionProbe("hermes")
	if _, err := DetectVersion(context.Background(), cmd); err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got, want := readProbeInvocations(t, record), []string{"-p work acp --version"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("invocations = %q, want %q", got, want)
	}
}

// A Hermes older than v0.14.0 has no `acp --version`; argparse rejects it with
// exit 2 and the probe falls back to the root `--version`.
func TestDetectVersionHermesFallsBackWhenACPVersionIsRejected(t *testing.T) {
	script, record := fakeHermesForVersionProbe(t, `
if [ "$1" = "--version" ]; then echo "Hermes Agent v0.13.0 (2026.5.7)"; exit 0; fi
echo "hermes: error: unrecognized arguments: --version" >&2
exit 2`)

	version, err := DetectVersion(context.Background(), Command{Path: script}.ForVersionProbe("hermes"))
	if err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if version != "Hermes Agent v0.13.0 (2026.5.7)" {
		t.Fatalf("version = %q, want the root --version line", version)
	}
	if got, want := readProbeInvocations(t, record), []string{"acp --version", "--version"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("invocations = %q, want %q", got, want)
	}
}

// A hang is not a rejection: falling back would spend the rest of the budget on
// the root `--version` and its network check, the thing this probe avoids.
func TestDetectVersionHermesDoesNotFallBackOnTimeout(t *testing.T) {
	script, record := fakeHermesForVersionProbe(t, `exec sleep 30`)
	orig := detectVersionTimeout
	detectVersionTimeout = time.Second
	t.Cleanup(func() { detectVersionTimeout = orig })

	if _, err := DetectVersion(context.Background(), Command{Path: script}.ForVersionProbe("hermes")); err == nil {
		t.Fatal("expected an error from a hanging probe, got nil")
	}
	// The kill can land before a cold shell writes its log line, so assert only
	// that the root --version never ran.
	for _, got := range readProbeInvocations(t, record) {
		if got != "acp --version" {
			t.Fatalf("hermes was invoked as %q after the probe timed out; want no fallback", got)
		}
	}
}

// Every other provider keeps the plain `--version` probe.
func TestDetectVersionOtherProvidersKeepRootVersion(t *testing.T) {
	script, record := fakeHermesForVersionProbe(t, `echo 1.2.3`)

	if _, err := DetectVersion(context.Background(), Command{Path: script}.ForVersionProbe("claude")); err != nil {
		t.Fatalf("DetectVersion: %v", err)
	}
	if got, want := readProbeInvocations(t, record), []string{"--version"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("invocations = %q, want %q", got, want)
	}
}
