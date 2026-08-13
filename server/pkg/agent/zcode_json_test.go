package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFakeZcodeJSON writes a POSIX-sh script at dir/zcode that dispatches on
// its argv to emulate the public zcode-app-cli's --json behavior. It advertises
// --json (not --stream-json) so the capability gate routes runs through
// zcodeBackend. handlers maps a substring of the joined argv ("$*") to the
// shell snippet that runs when it matches; the "default" key is the fallback.
//
// The script always answers `--help` by advertising --json only.
func writeFakeZcodeJSON(t *testing.T, dir string, handlers map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("if [ \"$1\" = \"--help\" ] || [ \"$1\" = \"-h\" ]; then\n")
	b.WriteString("  printf 'Usage: zcode --prompt <text> [--json] [--resume <id>] [--cwd <dir>]\\n'\n")
	b.WriteString("  exit 0\n")
	b.WriteString("fi\n")
	b.WriteString("case \"$*\" in\n")
	// Non-default handlers first (deterministic order); default is the tail.
	keys := make([]string, 0, len(handlers))
	for k := range handlers {
		if k != "default" {
			keys = append(keys, k)
		}
	}
	for _, substr := range keys {
		b.WriteString("  *" + substr + "*)\n")
		b.WriteString("    " + handlers[substr] + "\n")
		b.WriteString("    ;;\n")
	}
	if def, ok := handlers["default"]; ok {
		b.WriteString("  *)\n")
		b.WriteString("    " + def + "\n")
		b.WriteString("    ;;\n")
	}
	b.WriteString("esac\n")
	path := filepath.Join(dir, "zcode")
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatalf("write fake zcode: %v", err)
	}
	return path
}

// drainResult reads all messages then the single Result from a session.
func drainResult(t *testing.T, session *Session) Result {
	t.Helper()
	for range session.Messages {
	}
	select {
	case r := <-session.Result:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("Result never delivered")
		return Result{}
	}
}

// TestZcodeJSONBackendAuthFailureSurfacesStderr verifies that a non-zero exit
// with an auth/config/network error on stderr surfaces to the user as a failed
// Result whose Error carries the bounded stderr tail — not an opaque exit
// status. It must not be flagged as a resume rejection (no --resume).
func TestZcodeJSONBackendAuthFailureSurfacesStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	dir := t.TempDir()
	fake := writeFakeZcodeJSON(t, dir, map[string]string{
		"default": "printf '%s\\n' 'authentication failed: invalid token' >&2; exit 1",
	})
	backend := &zcodeBackend{cfg: Config{ExecutablePath: fake, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "do work", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res := drainResult(t, session)
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "authentication failed") {
		t.Fatalf("Error = %q, want it to surface the stderr tail", res.Error)
	}
	if res.ResumeRejected {
		t.Fatalf("ResumeRejected = true on an auth failure (no --resume); want false")
	}
}

// TestZcodeJSONBackendInvalidResumeSetsResumeRejected verifies that a failed
// --resume against a missing session sets Result.ResumeRejected so the daemon
// falls back to a fresh session instead of looping on the dead id. zcode is NOT
// in resumeRejectionUndetectable, so the daemon trusts this signal.
func TestZcodeJSONBackendInvalidResumeSetsResumeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	dir := t.TempDir()
	fake := writeFakeZcodeJSON(t, dir, map[string]string{
		// Match a --resume sess_ arg anywhere in the joined argv.
		"sess_dead": "printf '%s\\n' 'no conversation found for session sess_dead' >&2; exit 1",
		"default":   "printf '%s\\n' '{\"sessionId\":\"sess_ok\",\"response\":\"ok\"}'; exit 0",
	})
	backend := &zcodeBackend{cfg: Config{ExecutablePath: fake, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "continue", ExecOptions{
		Timeout: 10 * time.Second, ResumeSessionID: "sess_dead",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res := drainResult(t, session)
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
	if !res.ResumeRejected {
		t.Fatalf("ResumeRejected = false; want true for a rejected --resume (error=%q)", res.Error)
	}
	if !strings.Contains(res.Error, "no conversation found") {
		t.Fatalf("Error = %q, want it to surface the stderr tail", res.Error)
	}
}

// TestZcodeJSONBackendMalformedJSONFailsClosed verifies that a successful exit
// with non-JSON stdout is reported as a failed parse, carrying a bounded stdout
// sample and the stderr tail rather than crashing or silently completing.
func TestZcodeJSONBackendMalformedJSONFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	dir := t.TempDir()
	fake := writeFakeZcodeJSON(t, dir, map[string]string{
		"default": "printf '%s\\n' 'this is not json at all'; exit 0",
	})
	backend := &zcodeBackend{cfg: Config{ExecutablePath: fake, Logger: slog.Default()}}
	session, err := backend.Execute(context.Background(), "do work", ExecOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	res := drainResult(t, session)
	if res.Status != "failed" {
		t.Fatalf("Status = %q, want failed", res.Status)
	}
	if !strings.Contains(res.Error, "parse JSON summary") {
		t.Fatalf("Error = %q, want a parse error", res.Error)
	}
	// The offending stdout sample is attached to Output for diagnosis.
	if !strings.Contains(res.Output, "this is not json") {
		t.Fatalf("Output = %q, want the bounded stdout sample", res.Output)
	}
}

// TestZcodeJSONBackendGateRejectsIncompatibleBuild verifies the capability gate
// (point 3): a binary that advertises neither --stream-json nor --json is not a
// compatible ZCode build, so Execute fails closed with a clear message instead
// of spawning a CLI that errors on --json.
func TestZcodeJSONBackendGateRejectsIncompatibleBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based fake not supported on Windows")
	}
	dir := t.TempDir()
	// A binary whose --help advertises neither flag.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--help\" ] || [ \"$1\" = \"-h\" ]; then printf 'Usage: zcode (interactive only)\\n'; exit 0; fi\n" +
		"echo nope; exit 1\n"
	path := filepath.Join(dir, "zcode")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake: %v", err)
	}
	backend := &zcodeBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default()}}
	_, err := backend.Execute(context.Background(), "do work", ExecOptions{Timeout: 10 * time.Second})
	if err == nil {
		t.Fatal("Execute succeeded for an incompatible build; want a capability-gate error")
	}
	if !strings.Contains(err.Error(), "does not advertise the --json summary protocol") {
		t.Fatalf("err = %q, want a capability-gate message", err)
	}
}
