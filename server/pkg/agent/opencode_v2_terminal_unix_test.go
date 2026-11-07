//go:build !windows

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestOpencodeV2ExecuteExitIsTerminal drives the real Execute path against a
// fake 2.x CLI: the stream half (processEvents) waives the missing final
// step_finish, and the process half must still decide the outcome.
func TestOpencodeV2ExecuteExitIsTerminal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		lines      []string
		exitCode   int
		wantStatus string
		wantErr    string
	}{
		{"open text step, exit 0", []string{v2TermStepStart, v2TermText}, 0, "completed", ""},
		{"tool then bare text, exit 0",
			[]string{v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermText}, 0, "completed", ""},
		{"error event, exit 1", []string{v2TermStepStart, v2TermText, v2TermErrorInvalidOut}, 1, "failed",
			"provider.invalid-output"},
		{"open step, no error, exit 1", []string{v2TermStepStart, v2TermText}, 1, "failed", "exited with error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var script strings.Builder
			script.WriteString("#!/bin/sh\ncat > /dev/null\n")
			for _, l := range tc.lines {
				script.WriteString("printf '%s\\n' '" + l + "'\n")
			}
			script.WriteString("exit " + string(rune('0'+tc.exitCode)) + "\n")

			fakePath := filepath.Join(t.TempDir(), "opencode")
			writeTestExecutable(t, fakePath, []byte(script.String()))

			backend, err := New("opencode", Config{
				ExecutablePath: fakePath,
				CLIVersion:     "opencode v2.1.3",
				BuiltinRuntime: true,
				Env:            map[string]string{opencodeTransportEnv: "run"},
				Logger:         slog.Default(),
			})
			if err != nil {
				t.Fatalf("new opencode backend: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "prompt", ExecOptions{Cwd: t.TempDir(), Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			res := <-session.Result

			if res.Status != tc.wantStatus {
				t.Fatalf("status = %q (err %q), want %q", res.Status, res.Error, tc.wantStatus)
			}
			if !strings.Contains(res.Error, tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", res.Error, tc.wantErr)
			}
			if tc.wantStatus == "completed" && res.Output != "DONE" {
				t.Fatalf("output = %q, want DONE", res.Output)
			}
		})
	}
}

// TestOpencodeV2BrokenPromptWriteFails: a 2.x CLI that closes stdin before the
// prompt is delivered, then streams a tool step plus final text and exits 0,
// must fail on the prompt write (review on #9463). The prompt is far larger
// than the pipe buffer so the write cannot complete.
func TestOpencodeV2BrokenPromptWriteFails(t *testing.T) {
	t.Parallel()

	var script strings.Builder
	script.WriteString("#!/bin/sh\nexec 0<&-\n")
	for _, l := range []string{v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermText} {
		script.WriteString("printf '%s\\n' '" + l + "'\n")
	}
	script.WriteString("exit 0\n")
	fakePath := filepath.Join(t.TempDir(), "opencode")
	writeTestExecutable(t, fakePath, []byte(script.String()))

	cfg := v2TermConfig()
	cfg.ExecutablePath = fakePath
	backend, err := New("opencode", cfg)
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, strings.Repeat("undelivered prompt\n", 100000), ExecOptions{Cwd: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	res := <-session.Result
	if res.Status != "failed" || !strings.Contains(res.Error, "prompt write failed") {
		t.Fatalf("status = %q error = %q, want failed prompt delivery", res.Status, res.Error)
	}
}
