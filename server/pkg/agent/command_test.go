package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseCommandRuntimeConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid full config", func(t *testing.T) {
		t.Parallel()
		raw := []byte(`{"command":"python3","args":["scripts/check.py","--strict"],"working_dir":"subdir","timeout_seconds":120}`)
		cfg, err := ParseCommandRuntimeConfig(raw)
		if err != nil {
			t.Fatalf("ParseCommandRuntimeConfig: %v", err)
		}
		if cfg.Command != "python3" {
			t.Errorf("Command = %q, want python3", cfg.Command)
		}
		if len(cfg.Args) != 2 || cfg.Args[0] != "scripts/check.py" || cfg.Args[1] != "--strict" {
			t.Errorf("Args = %v, want [scripts/check.py --strict]", cfg.Args)
		}
		if cfg.WorkingDir != "subdir" {
			t.Errorf("WorkingDir = %q, want subdir", cfg.WorkingDir)
		}
		if cfg.TimeoutSeconds != 120 {
			t.Errorf("TimeoutSeconds = %d, want 120", cfg.TimeoutSeconds)
		}
	})

	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"empty body", "", "runtime_config.command is required"},
		{"empty object", "{}", "runtime_config.command is required"},
		{"blank command", `{"command":"   "}`, "runtime_config.command is required"},
		{"whitespace in command", `{"command":"python3 script.py"}`, "single executable token"},
		{"nul in command", "{\"command\":\"python3\\u0000\"}", "NUL bytes"},
		{"nul in args", "{\"command\":\"python3\",\"args\":[\"a\\u0000b\"]}", "NUL bytes"},
		{"nul in working_dir", "{\"command\":\"python3\",\"working_dir\":\"dir\\u0000\"}", "NUL bytes"},
		{"negative timeout", `{"command":"python3","timeout_seconds":-5}`, "non-negative"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCommandRuntimeConfig([]byte(tc.raw))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateCommandRuntimeConfig(%s) = %v, want error containing %q", tc.raw, err, tc.want)
			}
		})
	}
}

func TestStripANSIAndControl(t *testing.T) {
	t.Parallel()

	raw := "Loading...\r\x1b[1;32m## Code Review\x1b[0m"
	if got := stripANSIAndControl(raw); got != "## Code Review" {
		t.Fatalf("stripANSIAndControl(%q) = %q, want %q", raw, got, "## Code Review")
	}
}

func TestCommandBackend_Execute(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell script fixture")
	}

	t.Run("streams stdout with newlines strips ANSI reads JSON stdin and records zero usage", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := filepath.Join(dir, "runner.sh")
		body := "#!/bin/sh\n" +
			"input=$(cat)\n" +
			"printf '\\033[1mreceived:%s\\033[0m\\n' \"$input\"\n" +
			"printf 'arg1:%s arg2:%s\\n' \"$1\" \"$2\"\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		backend, err := New("command", Config{
			ExecutablePath: "./runner.sh",
			LaunchPrefix:   []string{"--fixed"},
		})
		if err != nil {
			t.Fatalf("New(command): %v", err)
		}

		session, err := backend.Execute(context.Background(), `{"task_id":"t-123"}`, ExecOptions{
			Cwd:        dir,
			CustomArgs: []string{"--custom"},
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var coalesced strings.Builder
		for msg := range session.Messages {
			if msg.Type == MessageText {
				coalesced.WriteString(msg.Content)
			}
		}
		res := <-session.Result
		if res.Status != "completed" {
			t.Fatalf("Status = %q (error %q), want completed", res.Status, res.Error)
		}
		if len(res.Usage) != 0 {
			t.Fatalf("Usage = %v, want empty (zero tokens/cost)", res.Usage)
		}
		wantOutput := "received:{\"task_id\":\"t-123\"}\narg1:--fixed arg2:--custom"
		if res.Output != wantOutput {
			t.Fatalf("Output = %q, want %q", res.Output, wantOutput)
		}
		if strings.TrimSpace(coalesced.String()) != wantOutput {
			t.Fatalf("coalesced stream = %q, want %q with preserved newlines", coalesced.String(), wantOutput)
		}
	})

	t.Run("parses structured JSON result envelope", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := filepath.Join(dir, "structured.sh")
		body := "#!/bin/sh\n" +
			"echo 'running deterministic checks...'\n" +
			"echo '{\"multica_result\":true,\"status\":\"completed\",\"output\":\"All 42 guardrail checks passed.\"}'\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		backend, err := New("command", Config{ExecutablePath: script})
		if err != nil {
			t.Fatalf("New(command): %v", err)
		}
		session, err := backend.Execute(context.Background(), `{}`, ExecOptions{Cwd: dir})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range session.Messages {
		}
		res := <-session.Result
		if res.Status != "completed" {
			t.Fatalf("Status = %q, want completed", res.Status)
		}
		if res.Output != "All 42 guardrail checks passed." {
			t.Fatalf("Output = %q, want structured output", res.Output)
		}
	})

	t.Run("non-zero exit with stdout report classifies as check_failed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := filepath.Join(dir, "review.sh")
		body := "#!/bin/sh\n" +
			"echo '## Review Findings'\n" +
			"echo '- Missing validation in handler.go'\n" +
			"exit 1\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		backend, err := New("command", Config{ExecutablePath: script})
		if err != nil {
			t.Fatalf("New(command): %v", err)
		}
		session, err := backend.Execute(context.Background(), `{}`, ExecOptions{Cwd: dir})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range session.Messages {
		}
		res := <-session.Result
		if res.Status != "check_failed" {
			t.Fatalf("Status = %q, want check_failed", res.Status)
		}
		if !strings.Contains(res.Output, "## Review Findings") {
			t.Fatalf("Output = %q, want review markdown preserved", res.Output)
		}
	})

	t.Run("non-zero exit with stderr only classifies as tool failed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := filepath.Join(dir, "fail.sh")
		body := "#!/bin/sh\n" +
			"echo 'fatal: unknown revision or path not in the working tree' >&2\n" +
			"exit 2\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		backend, err := New("command", Config{ExecutablePath: script})
		if err != nil {
			t.Fatalf("New(command): %v", err)
		}
		session, err := backend.Execute(context.Background(), `{}`, ExecOptions{Cwd: dir})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range session.Messages {
		}
		res := <-session.Result
		if res.Status != "failed" {
			t.Fatalf("Status = %q, want failed", res.Status)
		}
		if !strings.Contains(res.Error, "fatal: unknown revision or path not in the working tree") {
			t.Fatalf("Error = %q, want stderr diagnostic included", res.Error)
		}
	})

	t.Run("enforces per-run timeout", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		script := filepath.Join(dir, "slow.sh")
		body := "#!/bin/sh\nsleep 30\n"
		if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
			t.Fatalf("write script: %v", err)
		}

		backend, err := New("command", Config{ExecutablePath: script})
		if err != nil {
			t.Fatalf("New(command): %v", err)
		}
		session, err := backend.Execute(context.Background(), `{}`, ExecOptions{
			Cwd:     dir,
			Timeout: 100 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		for range session.Messages {
		}
		res := <-session.Result
		if res.Status != "timeout" {
			t.Fatalf("Status = %q (error %q), want timeout", res.Status, res.Error)
		}
	})
}

func TestCommandAgentsDisabled(t *testing.T) {
	cases := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"no", false},
		{"off", false},
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{" yes ", true},
		{"on", true},
	}
	for _, tc := range cases {
		t.Run("val="+tc.val, func(t *testing.T) {
			t.Setenv(CommandDisabledEnv, tc.val)
			if got := CommandAgentsDisabled(); got != tc.want {
				t.Fatalf("CommandAgentsDisabled() with %q = %v, want %v", tc.val, got, tc.want)
			}
		})
	}
}
