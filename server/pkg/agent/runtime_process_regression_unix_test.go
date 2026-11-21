//go:build linux || darwin

package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRuntimeExitFixture is an owned subprocess fixture, never a provider.
func TestRuntimeExitFixture(t *testing.T) {
	mode := os.Getenv("MULTICA_EXIT_FIXTURE")
	if mode == "" {
		return
	}
	if strings.HasPrefix(mode, "child-") {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("MULTICA_EXIT_STOP")); err == nil {
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
		os.Exit(0)
	}
	if mode != "held-stdin" {
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(10)
	}
	child := exec.Command(exe, "-test.run=^TestRuntimeExitFixture$")
	child.Env = append(os.Environ(), "MULTICA_EXIT_FIXTURE=child-"+mode, "GORACE=atexit_sleep_ms=0")
	if mode == "escaped-stdout" {
		child.Stdout = os.Stdout
		child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	} else {
		child.Stdin = os.Stdin
	}
	if err := child.Start(); err != nil {
		os.Exit(11)
	}
	if err := os.WriteFile(os.Getenv("MULTICA_EXIT_PIDS"), []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0600); err != nil {
		os.Exit(12)
	}
	if mode == "escaped-stdout" {
		fmt.Println(`{"v":1,"type":"ready","runtime":"dsh"}`)
		fmt.Println(`{"v":1,"type":"session","request_id":"task-1","session_id":"session-1"}`)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(os.Getenv("MULTICA_EXIT_RELEASE")); err == nil {
			os.Exit(0)
		}
		time.Sleep(10 * time.Millisecond)
	}
	os.Exit(13)
}

func TestDshCancellationWithHeldDescriptorsReturns(t *testing.T) {
	for _, mode := range []string{"escaped-stdout", "held-stdin", "held-cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			pidFile, release, stop := filepath.Join(dir, "pids"), filepath.Join(dir, "release"), filepath.Join(dir, "stop")
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			fake := filepath.Join(dir, "dsh")
			writeTestExecutable(t, fake, []byte("#!/bin/sh\nexec \"$MULTICA_EXIT_BINARY\" -test.run=^TestRuntimeExitFixture$\n"))
			taskID := "task-1"
			if mode == "held-cancel" {
				taskID = strings.Repeat("task", 1<<18)
			}
			b, err := New("dsh", Config{ExecutablePath: fake, TaskID: taskID, Logger: slog.Default(), Env: map[string]string{
				"MULTICA_EXIT_BINARY": exe, "MULTICA_EXIT_FIXTURE": mode, "MULTICA_EXIT_PIDS": pidFile,
				"MULTICA_EXIT_RELEASE": release, "MULTICA_EXIT_STOP": stop, "GORACE": "atexit_sleep_ms=0",
			}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			var childPID int
			t.Cleanup(func() {
				cancel()
				_ = os.WriteFile(release, nil, 0600)
				_ = os.WriteFile(stop, nil, 0600)
				if childPID != 0 {
					waitRuntimeDescendantGone(t, childPID)
				}
			})
			done := make(chan error, 1)
			go func() {
				prompt := "prompt"
				if mode == "held-stdin" {
					prompt = strings.Repeat("x", 1<<20)
				}
				session, err := b.Execute(ctx, prompt, ExecOptions{Cwd: dir})
				if err != nil {
					done <- err
					return
				}
				go func() {
					for range session.Messages {
					}
				}()
				<-session.Result
				done <- nil
			}()
			pids := waitForPids(t, pidFile)
			childPID = pids[1]
			cancel()
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("DSH cancellation stranded an inherited descriptor")
			}
		})
	}
}

func TestRuntimeCancellationDrainsBufferedOutput(t *testing.T) {
	cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", "printf 'final usage event'")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	if err := observeRuntimeExit(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer close(done)
	closeRuntimeOutputAfterDrain(done, func() { _ = stdout.Close() })
	data, err := io.ReadAll(stdout)
	if err != nil || string(data) != "final usage event" {
		t.Fatalf("buffered final event lost: %q, %v", data, err)
	}
}

func TestOwnedRuntimeCleanupRetriesMissedGroupSignal(t *testing.T) {
	cmd := NewCommand("/bin/sh", nil).exec(context.Background(), "-c", "sleep 5 &\nexit 0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		t.Fatal(err)
	}
	defer func() { signalProcessGroup(cmd, syscall.SIGKILL); _ = stdout.Close(); _ = cmd.Wait() }()
	if err := observeRuntimeExit(cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	passes := 0
	reapOwnedRuntimeGroup(cmd, func(cmd *exec.Cmd, sig syscall.Signal) {
		passes++
		if passes > 1 {
			signalProcessGroup(cmd, sig)
		}
	})
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, stdout); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first signal miss left the descendant holding stdout")
	}
}

func TestDshCompletedFramePreservesContextOutcome(t *testing.T) {
	for _, status := range []string{"cancelled", "timeout"} {
		t.Run(status, func(t *testing.T) {
			fake := writeDshFixture(t, `
IFS= read -r command
printf '%s\n' '{"v":1,"type":"ready","runtime":"dsh"}'
printf '%s\n' '{"v":1,"type":"result","request_id":"task-1","status":"completed","output":"done"}'
printf '%s\n' '{"v":1,"type":"session","request_id":"task-1","session_id":"session-1"}'
IFS= read -r cancel
exit 0
`)
			b, err := New("dsh", Config{ExecutablePath: fake, TaskID: "task-1", Logger: slog.Default()})
			if err != nil {
				t.Fatal(err)
			}
			var ctx context.Context
			var cancel context.CancelFunc
			if status == "timeout" {
				ctx, cancel = context.WithTimeout(context.Background(), 300*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			session, err := b.Execute(ctx, "prompt", ExecOptions{Cwd: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if message := <-session.Messages; message.SessionID != "session-1" {
				t.Fatalf("fixture did not emit completed frame before cancellation: %+v", message)
			}
			if status == "cancelled" {
				cancel()
			}
			go func() {
				for range session.Messages {
				}
			}()
			select {
			case result := <-session.Result:
				if result.Status != status {
					t.Fatalf("context became a process failure: %+v", result)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("context result did not return")
			}
		})
	}
}

// This fixture runs explicitly inside the denied-waitid container verification.
func TestRuntimeObservationFailureReachesBackendResult(t *testing.T) {
	if os.Getenv("MULTICA_TEST_WAITID_DENIED") != "1" {
		t.Skip("requires process-scoped waitid denial")
	}
	for _, name := range []string{"claude", "codearts", "deveco", "opencode", "dsh"} {
		t.Run(name, func(t *testing.T) {
			fake := filepath.Join(t.TempDir(), name)
			writeTestExecutable(t, fake, []byte("#!/bin/sh\nsleep 0.05\nexit 0\n"))
			b, err := New(name, Config{ExecutablePath: fake, Logger: slog.Default(), Env: map[string]string{opencodeTransportEnv: "run"}})
			if err != nil {
				t.Fatal(err)
			}
			session, err := b.Execute(context.Background(), "prompt", ExecOptions{Cwd: t.TempDir()})
			if err == nil {
				go func() {
					for range session.Messages {
					}
				}()
				select {
				case result := <-session.Result:
					if result.Status == "completed" {
						t.Fatalf("unobserved execution succeeded: %+v", result)
					}
					err = fmt.Errorf("%s", result.Error)
				case <-time.After(2 * time.Second):
					t.Fatal("denied observation stranded backend")
				}
			}
			if !strings.Contains(err.Error(), "observe runtime exit: operation not permitted") {
				t.Fatalf("observation error hidden: %v", err)
			}
		})
	}
}
