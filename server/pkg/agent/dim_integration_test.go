//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDimRealACPSmoke drives the real `dim acp` binary end-to-end.
//
// It validates the full daemon contract against a live Dim (dimcode) process:
//   - `dim acp` starts and responds to ACP RPCs (initialize, session/new)
//   - session/set_config_option (permission=full-access, mode=agent) succeeds
//   - session/prompt returns a completed turn
//   - the agent can actually write a file under the injected full-access
//     permission (the whole point of raising permission from the read-only
//     default) — a sentinel file is written and read back
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires `dim`
// on PATH with an active Dim OAuth login. The RPCs it exercises are the ones
// the execution path needs, verified against dimcode 0.3.10.
func TestDimRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	// Discover `dim` binary on PATH.
	path, err := exec.LookPath("dim")
	if err != nil {
		t.Skip("dim not on PATH; skipping real-binary smoke test")
	}

	// Log CLI version.
	if version, err := exec.Command(path, "version").CombinedOutput(); err == nil {
		t.Logf("dim version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("dim version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	backend, err := New("dim", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new dim backend: %v", err)
	}

	cwd := t.TempDir()
	sentinel := filepath.Join(cwd, "sentinel.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Prompt the agent to write a sentinel file under full-access. This is
	// the real permission proof: the read-only default would deny both the
	// command execution and the file write, so success confirms
	// set_config_option landed for permission=full-access AND mode=agent.
	session, err := backend.Execute(ctx,
		"Use your shell/exec tool to run: echo dim-exec-ok > sentinel.txt — then reply with exactly: done",
		ExecOptions{
			Cwd:     cwd,
			Timeout: 100 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Drain messages in background.
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real dim run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real dim")
		}
		// The hard proof: the sentinel file must exist with the expected
		// content. A permission failure would leave it absent.
		data, readErr := os.ReadFile(sentinel)
		if readErr != nil {
			t.Fatalf("sentinel file was not written (permission=full-access not effective): %v", readErr)
		}
		if !strings.Contains(string(data), "dim-exec-ok") {
			t.Fatalf("sentinel file content unexpected: %q", string(data))
		}
		t.Logf("real dim smoke OK: session=%s output=%q sentinel=%q", result.SessionID, result.Output, strings.TrimSpace(string(data)))

	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for real dim result")
	}
}

// TestDimRealCrossRunResume verifies cross-run session continuity against a
// real `dim acp`: run A establishes conversation context (a secret token),
// then a fresh run B in a new process resumes that session via session/load
// and must be able to recall the token. This is the regression review #2
// requires: run B uses context established only in run A.
//
// dim 0.3.10+ releases its per-process session lock within ~5s of the owning
// process exiting, so the resume succeeds once run A's process has torn down.
// Gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires `dim` on PATH with an
// active Dim OAuth login.
func TestDimRealCrossRunResume(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("dim")
	if err != nil {
		t.Skip("dim not on PATH; skipping real-binary smoke test")
	}

	backend, err := New("dim", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new dim backend: %v", err)
	}

	cwd := t.TempDir()
	secret := "ZEBRA-91337"

	// Run A: establish context by telling the agent a secret token.
	runCtxA, cancelA := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelA()
	sessionA, err := backend.Execute(runCtxA,
		"Remember this secret token: "+secret+". Reply with exactly: ok",
		ExecOptions{
			Cwd:     cwd,
			Timeout: 100 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("run A execute: %v", err)
	}
	go func() {
		for range sessionA.Messages {
		}
	}()
	var sessionID string
	select {
	case result := <-sessionA.Result:
		if result.Status != "completed" {
			t.Fatalf("run A did not complete: status=%q error=%q", result.Status, result.Error)
		}
		sessionID = result.SessionID
		if sessionID == "" {
			t.Fatal("run A returned no session id; cannot resume")
		}
		t.Logf("run A OK: session=%s", sessionID)
	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for run A result")
	}

	// Run B starts immediately: dim 0.3.10+ releases the per-process session
	// lock instantly when the prior run sent session/close (graceful exit), so
	// no sleep is needed. The bounded retry in the backend covers the rare
	// case where the lock has not been released yet.
	t.Logf("run B starting immediately after run A...")

	// Run B: resume run A's session and ask for the secret. The token was
	// only ever mentioned in run A, so a correct resume recalls it.
	runCtxB, cancelB := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancelB()
	sessionB, err := backend.Execute(runCtxB,
		"What was the secret token I told you? Reply with exactly the token and nothing else.",
		ExecOptions{
			Cwd:             cwd,
			Timeout:         100 * time.Second,
			ResumeSessionID: sessionID,
		},
	)
	if err != nil {
		t.Fatalf("run B execute: %v", err)
	}
	go func() {
		for range sessionB.Messages {
		}
	}()
	select {
	case result := <-sessionB.Result:
		if result.Status != "completed" {
			t.Fatalf("run B did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if result.ResumeRejected {
			t.Fatal("run B reported ResumeRejected=true; session/load should have succeeded after the release window")
		}
		if !strings.Contains(result.Output, secret) {
			t.Fatalf("run B did not recall the secret from run A: output=%q (expected to contain %q)", result.Output, secret)
		}
		t.Logf("run B OK: cross-run continuity confirmed, session=%s output=%q", result.SessionID, result.Output)
	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for run B result")
	}
}
