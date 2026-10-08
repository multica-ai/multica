//go:build unix

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

func TestMcodeUnsupportedResumeTerminatesProcessTreeBeforeWait(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "mcode")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      ( sleep 300 ) >/dev/null 2>&1 &
      child=$!
      printf '%s %s\n' "$$" "$child" > "$MCODE_PID_FILE"
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      while true; do sleep 1; done
      ;;
  esac
done
while true; do sleep 1; done
`))

	backend, err := New("mcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"MCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new mcode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	session, err := backend.Execute(ctx, "continue", ExecOptions{
		Cwd:             tempDir,
		ResumeSessionID: "mcode-old-session",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	pids := waitForPids(t, pidFile)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	select {
	case result := <-session.Result:
		if result.Status != "failed" || !result.ResumeRejected {
			t.Fatalf("result = %+v, want failed ResumeRejected", result)
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("result did not arrive before process cleanup")
	}

	select {
	case _, ok := <-session.Messages:
		if ok {
			t.Fatal("message stream emitted unexpected message")
		}
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("message stream did not close before process cleanup")
	}

	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}

// TestMcodeCancellationTerminatesProcessTree verifies the public Backend.Execute
// cancellation contract: an aborted MCode run must not leave the ACP process or
// any tool subprocess it spawned running after the result is returned.
func TestMcodeCancellationTerminatesProcessTree(t *testing.T) {
	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "pids")
	fakePath := filepath.Join(tempDir, "mcode")
	writeTestExecutable(t, fakePath, []byte(`#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}\n' "$id"
      ;;
    *'"method":"session/new"'*)
      printf '{"jsonrpc":"2.0","id":%s,"result":{"sessionId":"mcode-cancel-session"}}\n' "$id"
      ;;
    *'"method":"session/prompt"'*)
      ( sleep 300 ) >/dev/null 2>&1 &
      child=$!
      printf '%s %s\n' "$$" "$child" > "$MCODE_PID_FILE"
      while true; do sleep 1; done
      ;;
  esac
done
`))

	backend, err := New("mcode", Config{
		ExecutablePath: fakePath,
		Logger:         slog.Default(),
		Env:            map[string]string{"MCODE_PID_FILE": pidFile},
	})
	if err != nil {
		t.Fatalf("new mcode backend: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := backend.Execute(ctx, "keep working", ExecOptions{Cwd: tempDir})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	pids := waitForPids(t, pidFile)
	t.Cleanup(func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		_ = os.Remove(pidFile)
	})
	cancel()

	select {
	case result := <-session.Result:
		if result.Status != "aborted" {
			t.Errorf("status = %q, want aborted", result.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Execute did not return after cancellation")
	}

	for _, pid := range pids {
		waitProcessGone(t, pid)
	}
}
