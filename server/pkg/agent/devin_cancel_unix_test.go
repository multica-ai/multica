//go:build unix

package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDevinTerminatesOwnedProcessTree(t *testing.T) {
	for _, stage := range []string{"cancel-prompt", "unsupported-resume", "cancel-model-discovery"} {
		t.Run(stage, func(t *testing.T) {
			failEarly := stage == "unsupported-resume"
			dir := t.TempDir()
			pidFile := filepath.Join(dir, "pids")
			fakePath := filepath.Join(dir, "devin")
			writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
spawn_child() {
  ( sleep 300 ) >/dev/null 2>&1 &
  printf '%s %s\n' "$$" "$!" > "$DEVIN_PID_FILE"
}
if [ "$1" = models ]; then
  spawn_child
  while true; do sleep 1; done
fi

while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      if [ "$DEVIN_EARLY_FAILURE" = true ]; then spawn_child; fi
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      if [ "$DEVIN_EARLY_FAILURE" = true ]; then while true; do sleep 1; done; fi
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"devin-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      spawn_child
      while true; do sleep 1; done
      ;;
  esac
done
while true; do sleep 1; done
`))
			backend, err := New("devin", Config{ExecutablePath: fakePath, Logger: slog.Default(), Env: map[string]string{
				"DEVIN_PID_FILE": pidFile, "DEVIN_EARLY_FAILURE": map[bool]string{false: "false", true: "true"}[failEarly],
			}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			opts := ExecOptions{Cwd: dir}
			if failEarly {
				opts.ResumeSessionID = "old-session"
			}
			if stage == "cancel-model-discovery" {
				opts.Model = "opus"
			}
			session, err := backend.Execute(ctx, "keep working", opts)
			if err != nil {
				t.Fatal(err)
			}
			messagesClosed := make(chan struct{})
			go func() {
				defer close(messagesClosed)
				for range session.Messages {
				}
			}()
			pids := waitForPids(t, pidFile)
			t.Cleanup(func() {
				for _, pid := range pids {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			})
			if !failEarly {
				cancel()
			}
			select {
			case result := <-session.Result:
				if failEarly {
					if result.Status != "failed" || !result.ResumeRejected {
						t.Fatalf("unsupported resume = %+v", result)
					}
				} else if result.Status != "aborted" {
					t.Fatalf("cancelled prompt = %+v", result)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("process cleanup waited for the task timeout")
			}
			select {
			case <-messagesClosed:
			case <-time.After(3 * time.Second):
				t.Fatal("readers were not joined")
			}
			for _, pid := range pids {
				waitProcessGone(t, pid)
			}
		})
	}
}

func TestDevinBoundsEscapedPipeHolders(t *testing.T) {
	for _, tc := range []struct{ name, cases, status string }{
		{"completed", `
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"protocolVersion":1}}\n' "$id" ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"sessionId":"devin-session"}}\n' "$id" ;;
    *'"method":"session/prompt"'*)
      %s
      printf '{"jsonrpc":"2.0","id":%%s,"result":{"stopReason":"end_turn"}}\n' "$id"
      exit 0 ;;`, "completed"},
		{"early-failure", `
    *'"method":"initialize"'*)
      %s
      printf '{"jsonrpc":"2.0","id":%%s,"error":{"code":-32603,"message":"initialize refused"}}\n' "$id"
      exit 0 ;;`, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session, closed := startACPWithEscapedHolder(t, "devin", tc.cases)
			result := awaitHermesResult(t, session)
			if result.Status != tc.status {
				t.Fatalf("result=%+v, want %s", result, tc.status)
			}
			awaitHermesMessagesClosed(t, closed)
		})
	}
}
