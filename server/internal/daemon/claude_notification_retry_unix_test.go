//go:build !windows

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the real runTask retry and the retirement reported to the server.
// A failed fresh attempt must never start a third process. All transcripts and
// executables are synthetic fixtures, never an installed Claude or user data.
func TestRunTaskClaudeNotificationRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, retryFails := range []bool{false, true} {
		name := "fresh-succeeds"
		if retryFails {
			name = "fresh-fails"
		}
		t.Run(name, func(t *testing.T) {
			d, callsFile, cleanup := newLeaderReuseTestDaemon(t)
			defer cleanup()
			configDir := t.TempDir()
			projectDir := filepath.Join(configDir, "projects", "fixture")
			if err := os.MkdirAll(projectDir, 0o700); err != nil {
				t.Fatal(err)
			}
			transcript := filepath.Join(projectDir, "notification-session.jsonl")
			const original = "{\"type\":\"user\",\"message\":\"original history\"}\n"
			if err := os.WriteFile(transcript, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			const record = `{"type":"queue-operation","operation":"enqueue","sessionId":"notification-session","content":"<task-notification><task-id>background-task</task-id></task-notification>"}`
			script := `#!/bin/sh
resume=false
for arg in "$@"; do
  if [ "$arg" = '--resume' ]; then resume=true; fi
done
printf '%s\n' "$resume" >> "$CLAUDE_TEST_CALLS"
IFS= read -r input
if [ "$resume" = true ]; then
  printf '%s\n' '` + record + `' >> "$CLAUDE_TEST_TRANSCRIPT"
  printf '%s\n' '{"type":"system","subtype":"init","session_id":"notification-session"}'
  exit 1
fi
if [ "$CLAUDE_RETRY_FAIL" = 1 ]; then exit 2; fi
case "$input" in
  *multica-supplement-initialize*)
    printf '%s\n' '{"type":"control_response","response":{"subtype":"success","request_id":"multica-supplement-initialize"}}'
    IFS= read -r _ ;;
esac
session=notification-session
if [ "$CLAUDE_RETRY_PHASE" = 1 ]; then session=replacement-session; fi
printf '{"type":"result","session_id":"%s","result":"done"}\n' "$session"
`
			writeTestExecutable(t, d.cfg.Agents["claude"].Path, []byte(script))
			task := leaderReuseTestTask("notification-initial")
			task.Agent.CustomEnv = map[string]string{
				"IS_SANDBOX": "1", "CLAUDE_CONFIG_DIR": configDir,
				"CLAUDE_TEST_TRANSCRIPT": transcript, "CLAUDE_TEST_CALLS": callsFile,
			}
			first, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
			if err != nil || first.Status != "completed" {
				t.Fatalf("initial run: %+v, %v", first, err)
			}
			task.ID = "notification-resume"
			task.PriorWorkDir = first.WorkDir
			task.PriorSessionID = first.SessionID
			task.Agent.CustomEnv["CLAUDE_RETRY_PHASE"] = "1"
			if retryFails {
				task.Agent.CustomEnv["CLAUDE_RETRY_FAIL"] = "1"
			}
			result, err := d.runTask(context.Background(), task, "claude", 0, d.logger)
			if err != nil {
				t.Fatal(err)
			}
			if result.RetiredSessionID != "notification-session" {
				t.Fatalf("wrong retirement: %+v", result)
			}
			if retryFails {
				if result.Status != "blocked" || result.SessionID != "" || !strings.HasPrefix(result.Comment, "claude exited with error: exit status 1") {
					t.Fatalf("failed retry must preserve original failure without resurrecting old session: %+v", result)
				}
			} else if result.Status != "completed" || result.SessionID != "replacement-session" || result.Comment != "done" {
				t.Fatalf("fresh retry did not recover: %+v", result)
			}
			calls, err := os.ReadFile(callsFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(calls) != "false\ntrue\nfalse\n" {
				t.Fatalf("expected initial, resume, and exactly one fresh retry; calls = %q", calls)
			}
			data, err := os.ReadFile(transcript)
			if err != nil || string(data) != original+record+"\n" {
				t.Fatalf("old transcript was modified: %q, %v", data, err)
			}
		})
	}
}
