package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsDshBackend(t *testing.T) {
	t.Parallel()
	backend, err := New("dsh", Config{ExecutablePath: "/nonexistent/dsh"})
	if err != nil {
		t.Fatalf("New(dsh): %v", err)
	}
	if _, ok := backend.(*dshBackend); !ok {
		t.Fatalf("New(dsh) = %T, want *dshBackend", backend)
	}
}

func TestBuildDshArgsModelPatchAndIgnoresCustomArgs(t *testing.T) {
	t.Parallel()
	args, patchPath, err := buildDshArgs("run the tests", ExecOptions{
		Model:      "deepseek-chat",
		ExtraArgs:  []string{"--dump-config", "--some-extra"},
		CustomArgs: []string{"--patch", "user-override.yml", "--dump-default-config", "--help", "--some-flag"},
	}, slog.Default())
	if err != nil {
		t.Fatalf("buildDshArgs: %v", err)
	}
	defer cleanupDshModelPatch(patchPath)

	if patchPath == "" {
		t.Fatal("expected a model patch path for a selected model")
	}
	// Custom/extra args must never reach the command line: any token after
	// the task positional is joined into the task text, and any option token
	// makes the headless app exit with an error.
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"--dump-config", "--dump-default-config", "--help", "--some-extra", "--some-flag", "user-override.yml", "web"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("managed argument %q leaked into %v", forbidden, args)
		}
	}
	wantPrefix := []string{"--profile", "headless", "--patch", patchPath, "run the tests"}
	if len(args) != len(wantPrefix) {
		t.Fatalf("args = %v, want exactly %v", args, wantPrefix)
	}
	for i, want := range wantPrefix {
		if args[i] != want {
			t.Fatalf("args[%d] = %q, want %q; all=%v", i, args[i], want, args)
		}
	}

	patchBytes, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read model patch: %v", err)
	}
	patch := string(patchBytes)
	for _, want := range []string{"- id: agent-default-model", "provider: deepseek-official", "model: deepseek-chat"} {
		if !strings.Contains(patch, want) {
			t.Fatalf("model patch missing %q:\n%s", want, patch)
		}
	}
	// Windows has no Unix permission bits, so the 0600 assertion is
	// POSIX-only; the Chmod call is still exercised on both platforms.
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(patchPath); err != nil {
			t.Fatalf("stat model patch: %v", err)
		} else if st.Mode().Perm() != 0o600 {
			t.Fatalf("model patch mode = %v, want 0600", st.Mode().Perm())
		}
	}
}

func TestBuildDshArgsNoModelNoPatch(t *testing.T) {
	t.Parallel()
	args, patchPath, err := buildDshArgs("task", ExecOptions{}, slog.Default())
	if err != nil {
		t.Fatalf("buildDshArgs: %v", err)
	}
	if patchPath != "" {
		t.Fatalf("unexpected patch path %q for empty model", patchPath)
	}
	if len(args) != 3 || args[0] != "--profile" || args[1] != "headless" || args[2] != "task" {
		t.Fatalf("args = %v, want [--profile headless task]", args)
	}
}

func TestDshHeartbeatIntervalTracksWatchdogWindow(t *testing.T) {
	t.Parallel()
	if got := dshHeartbeatIntervalFor(ExecOptions{}); got != dshHeartbeatInterval {
		t.Fatalf("no watchdog window: interval = %v, want %v", got, dshHeartbeatInterval)
	}
	if got := dshHeartbeatIntervalFor(ExecOptions{IdleWatchdogTimeout: 2 * time.Minute}); got != time.Minute {
		t.Fatalf("2m watchdog: interval = %v, want 1m", got)
	}
	// A window wider than twice the default keeps the default interval.
	if got := dshHeartbeatIntervalFor(ExecOptions{IdleWatchdogTimeout: 30 * time.Minute}); got != dshHeartbeatInterval {
		t.Fatalf("30m watchdog: interval = %v, want default %v", got, dshHeartbeatInterval)
	}
}

func TestWriteDshModelPatchRejectsBadModel(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"", "deepseek chat", "foo\nbar", "a;rm -rf /", "--model", "-x", ".hidden"} {
		if _, err := writeDshModelPatch(model); err == nil {
			t.Fatalf("writeDshModelPatch(%q) succeeded, want error", model)
		}
	}
}

func fakeDshScript() string {
	return `#!/bin/sh
if [ -n "$DSH_ARGS_FILE" ]; then printf '%s\n' "$@" > "$DSH_ARGS_FILE"; fi
if [ -n "$DSH_PATCH_CAPTURE" ]; then
  for arg in "$@"; do
    if [ "$arg" = "--patch" ]; then
      shift
      cp "$1" "$DSH_PATCH_CAPTURE"
      break
    fi
    shift
  done
fi
case "$DSH_MODE" in
  error)
    printf '%s\n' 'dsh: authentication_error: synthetic dsh auth failure' >&2
    exit 1
    ;;
  exit)
    echo 'synthetic dsh stderr' >&2
    exit 7
    ;;
  spin)
    while :; do :; done
    ;;
  *)
    printf 'PONG\n'
    ;;
esac
`
}

func newFakeDshBackend(t *testing.T, env map[string]string) *dshBackend {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		// Windows npm installs dsh as a dsh.cmd/dsh.ps1 launcher pair; the
		// backend routes through PowerShell -File dsh.ps1, so the fake must
		// provide both files with the .cmd path handed to the backend.
		writeTestExecutable(t, filepath.Join(dir, "dsh.cmd"), []byte("@echo off\r\n"))
		writeTestExecutable(t, filepath.Join(dir, "dsh.ps1"), []byte(fakeDshPS1Script()))
		return &dshBackend{cfg: Config{ExecutablePath: filepath.Join(dir, "dsh.cmd"), Logger: slog.Default(), Env: env}}
	}
	path := filepath.Join(dir, "dsh")
	writeTestExecutable(t, path, []byte(fakeDshScript()))
	return &dshBackend{cfg: Config{ExecutablePath: path, Logger: slog.Default(), Env: env}}
}

// fakeDshPS1Script impersonates the npm dsh.ps1 launcher body: it forwards
// argv to the "CLI" (here: emit the final answer / failure behavior selected
// by DSH_MODE) and honours the same capture env vars as the POSIX fixture.
func fakeDshPS1Script() string {
	return `$ErrorActionPreference = 'Stop'
if ($env:DSH_ARGS_FILE) { $args | Out-File -FilePath $env:DSH_ARGS_FILE -Encoding utf8 }
if ($env:DSH_PATCH_CAPTURE) {
  for ($i = 0; $i -lt $args.Count - 1; $i++) {
    if ($args[$i] -eq '--patch') { Copy-Item $args[$i + 1] $env:DSH_PATCH_CAPTURE; break }
  }
}
switch ($env:DSH_MODE) {
  'error' { [Console]::Error.WriteLine('dsh: authentication_error: synthetic dsh auth failure'); exit 1 }
  'exit'  { [Console]::Error.WriteLine('synthetic dsh stderr'); exit 7 }
  'spin'  { while ($true) { Start-Sleep -Milliseconds 100 } }
  default { Write-Output 'PONG' }
}
`
}

func awaitDshResult(t *testing.T, session *Session) ([]Message, Result) {
	t.Helper()
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a result")
		}
		return messages, result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for dsh result")
		return nil, Result{}
	}
}

func TestDshBackendCompletesWithFinalAnswer(t *testing.T) {
	t.Parallel()
	backend := newFakeDshBackend(t, nil)
	session, err := backend.Execute(context.Background(), "reply PONG", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	messages, result := awaitDshResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error=%q)", result.Status, result.Error)
	}
	if result.Output != "PONG" {
		t.Fatalf("output = %q, want PONG", result.Output)
	}
	if result.Error != "" {
		t.Fatalf("unexpected error %q on completed run", result.Error)
	}
	var sawStatus, sawText bool
	for _, message := range messages {
		switch message.Type {
		case MessageStatus:
			sawStatus = message.Status == "running"
		case MessageText:
			sawText = message.Content == "PONG"
		}
	}
	if !sawStatus || !sawText {
		t.Fatalf("missing status/text messages (status=%v text=%v); messages=%+v", sawStatus, sawText, messages)
	}
}

func TestDshBackendFailureKeepsOutputEmpty(t *testing.T) {
	t.Parallel()
	backend := newFakeDshBackend(t, map[string]string{"DSH_MODE": "exit"})
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitDshResult(t, session)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Output != "" {
		t.Fatalf("output = %q on failed run, want empty (fail-closed contract)", result.Output)
	}
	if result.Error == "" {
		t.Fatal("expected an error message on failed run")
	}
}

func TestDshBackendTimeout(t *testing.T) {
	t.Parallel()
	backend := newFakeDshBackend(t, map[string]string{"DSH_MODE": "spin"})
	session, err := backend.Execute(context.Background(), "task", ExecOptions{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitDshResult(t, session)
	if result.Status != "timeout" {
		t.Fatalf("status = %q, want timeout (error=%q)", result.Status, result.Error)
	}
}

func TestDshBackendCancellation(t *testing.T) {
	t.Parallel()
	backend := newFakeDshBackend(t, map[string]string{"DSH_MODE": "spin"})
	ctx, cancel := context.WithCancel(context.Background())
	session, err := backend.Execute(ctx, "task", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, result := awaitDshResult(t, session)
	if result.Status != "aborted" {
		t.Fatalf("status = %q, want aborted (error=%q)", result.Status, result.Error)
	}
}

func TestDshBackendModelPatchReachesCLI(t *testing.T) {
	t.Parallel()
	patchCapture := filepath.Join(t.TempDir(), "captured-patch.yml")
	backend := newFakeDshBackend(t, map[string]string{"DSH_PATCH_CAPTURE": patchCapture})
	session, err := backend.Execute(context.Background(), "task", ExecOptions{
		Model:   "deepseek-chat",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_, result := awaitDshResult(t, session)
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error=%q)", result.Status, result.Error)
	}
	patched, err := os.ReadFile(patchCapture)
	if err != nil {
		t.Fatalf("dsh did not receive --patch file: %v", err)
	}
	if !strings.Contains(string(patched), "model: deepseek-chat") {
		t.Fatalf("captured patch missing model override:\n%s", string(patched))
	}
}
