//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
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

func primeRealTestRoot(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
		base = "/tmp"
	}
	root, err := os.MkdirTemp(base, "mp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestPrimeRealTransportLifecycle verifies the pinned official RPC and detached
// daemon lifecycle without selecting a model, sending a prompt, authenticating
// a provider, or consuming quota. It intentionally does not use the consuming
// real-smoke opt-in gate.
func TestPrimeRealTransportLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-binary lifecycle test in -short mode")
	}
	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary lifecycle test")
	}
	if err := CheckPrimeAdmission(); err != nil {
		t.Skipf("Prime Agent isolation admission is required: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	detected, err := DetectPrimeVersion(ctx, path)
	if err != nil {
		t.Fatalf("detect Prime version: %v", err)
	}
	if err := CheckPrimeVersion(detected); err != nil {
		t.Fatal(err)
	}

	root := primeRealTestRoot(t)
	tmpDir := filepath.Join(root, "tmp")
	stateDir := filepath.Join(root, "state")
	for _, dir := range []string{tmpDir, stateDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	socketPath := filepath.Join(tmpDir, "multica-prime-daemon.sock")
	cmd := exec.CommandContext(ctx, path, "--mode", "rpc", "--no-extensions", "--daemon-socket", socketPath)
	configureProcessGroup(cmd)
	cmd.Cancel = func() error { return nil }
	cmd.WaitDelay = 5 * time.Second
	cmd.Dir = t.TempDir()
	cmd.Env = buildPrimeEnv(map[string]string{
		"TMPDIR": tmpDir, "TMP": tmpDir, "TEMP": tmpDir,
		"PRIME_AGENT_CODING_AGENT_DIR": stateDir,
	})
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := newStderrTail(io.Discard, agentStderrTailBytes)
	cmd.Stderr = stderr
	if err := startOwnedProcessTree(cmd, slog.Default()); err != nil {
		t.Fatalf("start Prime RPC client: %v", err)
	}
	defer releaseProcessGroup(cmd)
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	var identity *primeDaemonIdentity
	clientReaped, daemonCleaned := false, false
	defer func() {
		if identity == nil {
			identity, _ = observePrimeTaskDaemon(tmpDir, socketPath, kernelPrimePeerIdentity)
		}
		if !clientReaped {
			signalProcessGroup(cmd, syscall.SIGKILL)
			select {
			case <-waitDone:
			case <-time.After(primeTerminateGrace):
			}
		}
		if identity != nil && !daemonCleaned {
			_ = shutdownPrimeTaskDaemon(tmpDir, socketPath, identity, kernelPrimePeerIdentity)
		}
	}()
	frames, frameErrs := pumpPrimeFrames(ctx, stdout)
	rpc := &primeRPC{in: stdin, frames: frames, errs: frameErrs, events: func(primeFrame) {}}

	stateResp, err := rpc.request(ctx, map[string]any{"type": "get_state"})
	if err != nil {
		waitSummary := "client still running"
		select {
		case waitErr := <-waitDone:
			clientReaped = true
			waitSummary = fmt.Sprintf("client wait: %v", waitErr)
		case <-time.After(500 * time.Millisecond):
		}
		diagnostic := withAgentStderr(fmt.Sprintf("Prime get_state: %v (%s)", err, waitSummary), "Prime Agent", sanitizeAgentDiagnostic(stderr.Tail()))
		t.Fatal(diagnostic)
	}
	var state primeState
	if err := json.Unmarshal(stateResp.Data, &state); err != nil || state.SessionID == "" || state.MessageCount == nil || state.IsStreaming {
		t.Fatalf("invalid Prime get_state: state=%+v err=%v", state, err)
	}
	identity, err = observePrimeTaskDaemon(tmpDir, socketPath, kernelPrimePeerIdentity)
	if err != nil {
		t.Fatalf("observe Prime daemon identity: %v", err)
	}

	_, _ = rpc.write(map[string]any{"type": "abort"})
	_ = stdin.Close()
	var clientErr error
	select {
	case clientErr = <-waitDone:
		clientReaped = true
	case <-time.After(primeTerminateGrace):
		signalProcessGroup(cmd, syscall.SIGTERM)
		if !waitProcessGroupGone(cmd, primeTerminateGrace) {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		clientErr = <-waitDone
		clientReaped = true
	}
	_ = stdout.Close()
	if err := shutdownPrimeTaskDaemon(tmpDir, socketPath, identity, kernelPrimePeerIdentity); err != nil {
		t.Fatalf("targeted Prime daemon shutdown: %v", err)
	}
	daemonCleaned = true
	if !primeSupervisorGone(identity.PID, identity.PGID) {
		t.Fatal("Prime daemon supervisor survived targeted shutdown")
	}
	if clientErr != nil {
		t.Fatalf("Prime RPC client exited unsuccessfully: %v", clientErr)
	}
}

// TestPrimeRealRPCSmoke drives the official Prime Agent RPC transport end to
// end. It is opt-in because it may access an authenticated provider and consume
// quota; the default test suite must never execute an installed agent CLI.
func TestPrimeRealRPCSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}
	if err := CheckPrimeAdmission(); err != nil {
		t.Skipf("Prime Agent isolation admission is required: %v", err)
	}
	stateDir := strings.TrimSpace(os.Getenv("MULTICA_PRIME_REAL_STATE_DIR"))
	if stateDir == "" {
		t.Skip("set MULTICA_PRIME_REAL_STATE_DIR to an existing pre-authenticated, private 0700 Prime state directory")
	}
	if !filepath.IsAbs(stateDir) {
		t.Fatalf("MULTICA_PRIME_REAL_STATE_DIR must be absolute, got %q", stateDir)
	}
	stateInfo, err := os.Lstat(stateDir)
	if err != nil {
		t.Fatalf("inspect MULTICA_PRIME_REAL_STATE_DIR: %v", err)
	}
	if stateInfo.Mode()&os.ModeSymlink != 0 || !stateInfo.IsDir() || stateInfo.Mode().Perm() != 0o700 {
		t.Fatalf("MULTICA_PRIME_REAL_STATE_DIR must be a real private 0700 directory")
	}
	model := strings.TrimSpace(os.Getenv("MULTICA_PRIME_REAL_MODEL"))
	if model != "" {
		if _, _, err := parsePrimeModel(model); err != nil {
			t.Fatalf("MULTICA_PRIME_REAL_MODEL: %v", err)
		}
	}

	smokeRoot := primeRealTestRoot(t)
	privateTmp := filepath.Join(smokeRoot, "tmp")
	if err := os.Mkdir(privateTmp, 0o700); err != nil {
		t.Fatalf("create private Prime TMPDIR: %v", err)
	}
	backend, err := New("prime", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
		Env: map[string]string{
			"TMPDIR":                       privateTmp,
			"TMP":                          privateTmp,
			"TEMP":                         privateTmp,
			"PRIME_AGENT_CODING_AGENT_DIR": stateDir,
		},
	})
	if err != nil {
		t.Fatalf("new Prime backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx, "Reply with exactly one lowercase word: alpha. Do not use any tools.", ExecOptions{
		Cwd:     t.TempDir(),
		Model:   model,
		Timeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute Prime Agent: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("Prime smoke did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "alpha") {
			t.Fatalf("expected Prime output to contain alpha, got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Fatal("Prime smoke returned no session ID")
		}
		var tokens int64
		for _, usage := range result.Usage {
			tokens += usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
		}
		if tokens == 0 {
			// Prime documents that session stats may be zero for providers that do
			// not expose usage. Keep the smoke informative without making those
			// authenticated provider configurations fail spuriously.
			t.Log("Prime session stats reported zero tokens for this provider")
		} else {
			t.Logf("Prime smoke usage: %d tokens", tokens)
		}
	case <-ctx.Done():
		t.Fatalf("timeout waiting for Prime result: %v", ctx.Err())
	}
}
