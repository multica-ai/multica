package agent

// FIR-4013 — a failed `result` message with no result text must still name its
// cause. Empty here is what the daemon turns into "claude execution failed",
// which taskfailure.Classify cannot match and failrouter therefore never
// retries.

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestClaudeResultErrorFromSubtype(t *testing.T) {
	tests := []struct {
		subtype string
		want    string
	}{
		{"error_max_turns", "claude stopped: the run reached its maximum number of turns (error_max_turns)"},
		{"  error_max_turns  ", "claude stopped: the run reached its maximum number of turns (error_max_turns)"},
		{"error_during_execution", "claude stopped: the run aborted during execution (error_during_execution)"},
		{"error_something_new", "claude stopped: error_something_new"},
		{"", ""},
		{"   ", ""},
		{"success", ""},
	}
	for _, tc := range tests {
		if got := claudeResultErrorFromSubtype(tc.subtype); got != tc.want {
			t.Errorf("subtype %q = %q, want %q", tc.subtype, got, tc.want)
		}
	}
}

// The rendered text must not accidentally trip an unrelated classifier rule —
// mislabelling a turn-cap stop as a quota or network failure would route it to
// the wrong retry/pause action. Landing in unknown is the honest outcome until
// the taxonomy gains a rule of its own; what matters is that the text now says
// what happened.
func TestClaudeResultSubtypeTextDoesNotMisclassify(t *testing.T) {
	for _, subtype := range []string{"error_max_turns", "error_during_execution"} {
		msg := claudeResultErrorFromSubtype(subtype)
		if strings.TrimSpace(msg) == "" {
			t.Fatalf("subtype %q rendered empty", subtype)
		}
		if got := taskfailure.Classify(msg); got != taskfailure.ReasonAgentUnknown {
			t.Errorf("Classify(%q) = %q, want %q — the wording must not match a rule it does not belong to",
				msg, got, taskfailure.ReasonAgentUnknown)
		}
	}
}

// End-to-end at the real call site: the CLI reports a run it could not finish
// with is_error and NO result text. Before FIR-4013 this produced
// Result.Error == "", which the daemon rendered as "claude execution failed".
func TestClaudeExecuteReportsMaxTurnsSubtypeAsError(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "claude")
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"session_id\":\"sess-max-turns\"}'\n" +
		"printf '%s\\n' '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"partial work\"}]}}'\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"error_max_turns\",\"is_error\":true,\"session_id\":\"sess-max-turns\",\"num_turns\":80}'\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("status = %q, want failed", result.Status)
		}
		if strings.TrimSpace(result.Error) == "" {
			t.Fatal("Error is empty — the daemon would render this as \"claude execution failed\"")
		}
		if !strings.Contains(result.Error, "error_max_turns") {
			t.Errorf("Error = %q, want it to name the terminal subtype", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
