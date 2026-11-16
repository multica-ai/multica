//go:build linux || darwin

package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Exercise normal completion through the run transport. A descendant holding
// stdout must not keep the scanner blocked after the runtime leader exits.
func TestNormalExitReapsDescendantProcessGroup(t *testing.T) {
	stream := `printf '%s\n' '{"type":"step_start","sessionID":"ses_fake","part":{"type":"step-start"}}'
printf '%s\n' '{"type":"text","sessionID":"ses_fake","part":{"type":"text","text":"done"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses_fake","part":{"type":"step-finish","reason":"stop"}}'
`
	for _, runtime := range []struct{ name, input, events string }{
		{"opencode", "cat >/dev/null\n", stream},
		{"codearts", "cat >/dev/null\n", stream},
		{"deveco", "", stream},
		{"claude", "IFS= read -r prompt\n", `printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"session_id":"ses_fake","result":"done"}'` + "\n"},
		{"dsh", "IFS= read -r prompt\n", `printf '%s\n' '{"v":1,"type":"ready","runtime":"dsh"}'
printf '%s\n' '{"v":1,"type":"result","request_id":"task-1","status":"completed","session_id":"ses_fake","output":"done"}'
`},
	} {
		for _, tc := range []struct{ name, redirect string }{
			{"redirected pipes", ">/dev/null 2>&1 </dev/null"},
			{"inherited pipes", ""},
		} {
			t.Run(runtime.name+"/"+tc.name, func(t *testing.T) {
				tempDir := t.TempDir()
				pidFile := filepath.Join(tempDir, "pids")
				stopFile := filepath.Join(tempDir, "stop")
				childPID := 0
				t.Cleanup(func() {
					_ = os.WriteFile(stopFile, nil, 0600)
					if childPID != 0 {
						waitRuntimeDescendantGone(t, childPID)
					}
				})
				fakePath := filepath.Join(tempDir, runtime.name)
				// Finite even on a regression: no cleanup signals a reaped PID.
				script := "#!/bin/sh\n" + runtime.input +
					"( i=0; while [ ! -f \"$STOP_FILE\" ] && [ \"$i\" -lt 300 ]; do sleep 0.1; i=$((i+1)); done ) " + tc.redirect + " &\n" +
					`printf '%s %s\n' "$$" "$!" > "$OPENCODE_PID_FILE"` + "\n" +
					runtime.events + "exit 0\n"
				writeTestExecutable(t, fakePath, []byte(script))
				backend, err := New(runtime.name, Config{
					TaskID:         "task-1",
					ExecutablePath: fakePath,
					Logger:         slog.Default(),
					Env:            map[string]string{"OPENCODE_PID_FILE": pidFile, "STOP_FILE": stopFile, opencodeTransportEnv: "run"},
				})
				if err != nil {
					t.Fatal(err)
				}
				session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Cwd: tempDir})
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					for range session.Messages {
					}
				}()
				pids := waitForPids(t, pidFile)
				childPID = pids[1]
				select {
				case result := <-session.Result:
					if result.Status != "completed" || result.Error != "" || result.Output != "done" {
						t.Fatalf("normal completion: %+v", result)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("runtime exited, but descendant kept task pipes open")
				}
				waitRuntimeDescendantGone(t, pids[1])
			})
		}
	}
}

// The complete assertion window must be shorter than the fixture's lifetime;
// natural expiry must never satisfy the descendant-cleanup regression.
func waitRuntimeDescendantGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("runtime descendant %d survived cleanup deadline", pid)
}
