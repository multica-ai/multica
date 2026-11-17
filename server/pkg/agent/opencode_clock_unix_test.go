//go:build !windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckOpenCodeResumeClock(t *testing.T) {
	for _, tc := range []struct {
		name, body               string
		wantSkew, wantUnverified bool
	}{
		{"normal", `printf '%s' '{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"info":{"time":{"created":1}}}]}'`, false, false},
		{"future", `printf '%s' '{"info":{"id":"ses-test","time":{"updated":9999999999999}},"messages":[]}'`, true, false},
		{"failure with private stderr", `echo private-transcript >&2; exit 1`, false, true},
		{"malformed", `echo private-transcript`, false, true},
		{"oversized", `printf '{"ignored":"'; head -c 33554433 /dev/zero | LC_ALL=C tr '\000' x; printf '"}'`, false, true},
		{"timeout", `sleep 30`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			bin := filepath.Join(t.TempDir(), "opencode")
			// Assert storage/project selection without injecting task MCP servers
			// or resolving an installed agent and its account.
			body := `#!/bin/sh
[ "$1" = export ] && [ "$2" = ses-test ] && [ "$3" = --sanitize ] || exit 2
[ "$PWD" = "$CLOCK_EXPECTED_CWD" ] && [ "$(pwd)" = "$CLOCK_EXPECTED_CWD" ] || exit 3
[ "$XDG_DATA_HOME" = clock-test-storage ] || exit 4
[ -z "$OPENCODE_CONFIG_CONTENT" ] || exit 5
` + tc.body + "\n"
			if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := Config{ExecutablePath: bin, BuiltinRuntime: true, CLIVersion: "1.18.30", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{"CLOCK_EXPECTED_CWD": cwd, "XDG_DATA_HOME": "clock-test-storage"}}
			opts := ExecOptions{Cwd: cwd, ResumeSessionID: "ses-test", McpConfig: []byte(`{"mcpServers":{"clock-mcp":{"command":"unused"}}}`)}
			ctx := context.Background()
			if tc.name == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			err := CheckOpenCodeResumeClock(ctx, cfg, opts)
			if errors.Is(err, ErrOpenCodeClockSkew) != tc.wantSkew || (err != nil && !errors.Is(err, ErrOpenCodeClockSkew)) != tc.wantUnverified {
				t.Fatalf("check = %v, want skew=%v unverified=%v", err, tc.wantSkew, tc.wantUnverified)
			}
			if err != nil && strings.Contains(err.Error(), "private-transcript") {
				t.Fatal("export contents leaked")
			}
		})
	}
}

func TestCheckOpenCodeResumeClockUnsupportedContractsDoNotExecute(t *testing.T) {
	for _, tc := range []struct {
		version string
		builtin bool
		args    []string
	}{
		{"1.18.30", false, nil},
		{"1.18.30", true, []string{"--server=https://unused.invalid"}},
		{"1.18.30", true, []string{"--attach", "https://unused.invalid"}},
		{"1.18.30", true, []string{"--session", "other-session"}},
		{"1.18.30", true, []string{"-sother-session"}},
		{"1.18.30", true, []string{"-csother-session"}},
	} {
		cfg := Config{ExecutablePath: filepath.Join(t.TempDir(), "does-not-exist"), BuiltinRuntime: tc.builtin, CLIVersion: tc.version}
		err := CheckOpenCodeResumeClock(context.Background(), cfg, ExecOptions{ResumeSessionID: "ses-test", CustomArgs: tc.args})
		if err == nil || errors.Is(err, ErrOpenCodeClockSkew) || strings.Contains(err.Error(), "executable not found") {
			t.Fatalf("unsupported contract must be rejected before lookup: %v", err)
		}
	}
}

func TestCheckOpenCodeResumeClockAcrossVersions(t *testing.T) {
	for _, tc := range []struct {
		version, command string
		standalone       bool
	}{
		{"1.1.54", "export", false}, {"1.18.30", "export", false}, {"1.18.31", "export", false}, {"1.99.0", "export", false},
		{"opencode v2.0.10", "session", false}, {"opencode v2.1.0", "session", true}, {"", "export", false},
	} {
		t.Run(tc.version, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "opencode")
			body := `#!/bin/sh
[ "$1" = "$CLOCK_EXPECTED_COMMAND" ] || exit 4
if [ "$1" = session ]; then
  [ "$2" = export ] && [ "$3" = ses-test ] || exit 2
  if [ "$CLOCK_STANDALONE" = true ]; then
    [ "$4" = --standalone ] && [ "$5" = --sanitize ] || exit 5
  else
    [ "$4" = --sanitize ] || exit 6
  fi
  printf '%s' '{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"id":"msg-test","type":"user","time":{"created":9999999999999}}]}'
else
  [ "$1" = export ] && [ "$2" = ses-test ] && [ "$3" = --sanitize ] || exit 3
  printf '%s' '{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"info":{"time":{"created":9999999999999}}}]}'
fi
`
			if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := Config{ExecutablePath: bin, BuiltinRuntime: true, CLIVersion: tc.version, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{"CLOCK_EXPECTED_COMMAND": tc.command, "CLOCK_STANDALONE": fmt.Sprint(tc.standalone)}}
			opts := ExecOptions{ResumeSessionID: "ses-test"}
			if tc.standalone {
				opts.CustomArgs = []string{"--standalone"}
			}
			if err := CheckOpenCodeResumeClock(context.Background(), cfg, opts); !errors.Is(err, ErrOpenCodeClockSkew) {
				t.Fatalf("version %q bypassed a future-dated session: %v", tc.version, err)
			}
		})
	}
}

func TestCheckOpenCodeResumeClockWithoutSanitizeFlag(t *testing.T) {
	for _, unsupported := range []bool{true, false} {
		t.Run(fmt.Sprint(unsupported), func(t *testing.T) {
			dir := t.TempDir()
			bin, calls := filepath.Join(dir, "opencode"), filepath.Join(dir, "calls")
			body := `#!/bin/sh
printf 'called\n' >> "$CLOCK_CALLS"
if [ -n "$3" ]; then
  if [ "$CLOCK_UNSUPPORTED" = true ]; then
    echo 'Unknown argument: sanitize' >&2
  else
    echo 'private export error' >&2
  fi
  exit 1
fi
printf '%s' '{"info":{"id":"ses-test","time":{"updated":1}},"messages":[{"info":{"time":{"created":9999999999999}},"parts":[{"text":"private transcript"}]}]}'
`
			if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := Config{ExecutablePath: bin, BuiltinRuntime: true, CLIVersion: "1.1.54", Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Env: map[string]string{"CLOCK_CALLS": calls, "CLOCK_UNSUPPORTED": fmt.Sprint(unsupported)}}
			err := CheckOpenCodeResumeClock(context.Background(), cfg, ExecOptions{ResumeSessionID: "ses-test"})
			if errors.Is(err, ErrOpenCodeClockSkew) != unsupported || err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("old CLI export check = %v", err)
			}
			raw, readErr := os.ReadFile(calls)
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := 1
			if unsupported {
				want = 2
			}
			if strings.Count(string(raw), "called") != want {
				t.Fatalf("export calls = %s, want %d", raw, want)
			}
		})
	}
}
