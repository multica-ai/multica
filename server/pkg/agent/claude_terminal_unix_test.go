//go:build unix

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeTerminalResultStopsLingeringProcess(t *testing.T) {
	claudeResultExitGraceNanos.Store(int64(150 * time.Millisecond))
	claudeTerminateGraceNanos.Store(int64(100 * time.Millisecond))
	t.Cleanup(func() {
		claudeResultExitGraceNanos.Store(0)
		claudeTerminateGraceNanos.Store(0)
	})
	for _, tc := range []struct {
		name, text, terminalReason, wantStatus string
		isError, lateResult, leaderExits       bool
	}{
		{name: "session limit", text: "Session usage limit reached", isError: true, wantStatus: "failed"},
		{name: "successful hand-in", text: "Opened the pull request", wantStatus: "completed"},
		{name: "late background result", text: "First terminal answer", lateResult: true, wantStatus: "completed"},
		{name: "leader exits but descendant owns stdout", text: "Completed before child exits", leaderExits: true, wantStatus: "completed"},
		{name: "structured failure", text: "Context window exhausted", terminalReason: "prompt_too_long", wantStatus: "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pids")
			fake := filepath.Join(dir, "claude")
			event, err := json.Marshal(map[string]any{
				"type": "result", "subtype": "success", "is_error": tc.isError,
				"terminal_reason": tc.terminalReason, "session_id": "terminal-session", "result": tc.text,
				"modelUsage": map[string]any{"test-model": map[string]int{"inputTokens": 11, "outputTokens": 5}},
			})
			if err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\ntrap '' TERM\nIFS= read -r prompt\n" +
				"( trap '' TERM; exec sleep 300 ) &\nchild=$!\n" +
				"printf '%s %s\\n' \"$$\" \"$child\" > \"$CLAUDE_PID_FILE\"\n" +
				fmt.Sprintf("printf '%%s\\n' '%s'\n", strings.ReplaceAll(string(event), "'", "'\"'\"'"))
			if tc.lateResult {
				script += "printf '%s\\n' '{\"type\":\"result\",\"is_error\":true,\"session_id\":\"late-session\",\"result\":\"Unrelated background failure\",\"modelUsage\":{\"test-model\":{\"inputTokens\":999}}}'\n"
			}
			if tc.leaderExits {
				script += "exit 0\n"
			} else {
				script += "wait \"$child\"\n"
			}
			writeTestExecutable(t, fake, []byte(script))
			backend, err := New("claude", Config{ExecutablePath: fake, Logger: slog.Default(), Env: map[string]string{"CLAUDE_PID_FILE": pidFile}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session, err := backend.Execute(ctx, "fixture prompt", ExecOptions{Cwd: dir, Model: "test-model", Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				for range session.Messages {
				}
			}()
			pids := waitForPids(t, pidFile)
			select {
			case res := <-session.Result:
				if res.Status != tc.wantStatus {
					t.Errorf("status = %q, want %q: %s", res.Status, tc.wantStatus, res.Error)
				}
				if tc.wantStatus == "completed" && (res.Output != tc.text || res.Error != "") {
					t.Errorf("terminal answer changed: output=%q error=%q", res.Output, res.Error)
				}
				if tc.wantStatus == "failed" && !strings.Contains(res.Error, tc.text) {
					t.Errorf("lost original terminal failure: %q", res.Error)
				}
				if res.SessionID != "terminal-session" || res.Usage["test-model"].InputTokens != 11 {
					t.Errorf("terminal session/usage changed: session=%q usage=%+v", res.SessionID, res.Usage)
				}
				if res.DurationMs >= 900 {
					t.Errorf("waited for the task deadline after a terminal result: %dms", res.DurationMs)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("terminal result did not finish the run")
			}
			for _, pid := range pids {
				waitProcessGone(t, pid)
			}
		})
	}
}
