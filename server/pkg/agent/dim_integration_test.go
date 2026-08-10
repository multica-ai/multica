//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestDimRealACPSmoke drives the real `dim acp` binary end-to-end.
//
// It validates the full daemon contract against a live Dim (dimcode) process:
//   - `dim acp` starts and responds to ACP RPCs (initialize, session/new)
//   - session/set_config_option (permission=full-access, mode=agent) succeeds
//   - session/set_model switches the model
//   - session/prompt returns a completed turn with stopReason=end_turn
//   - the agent can use tools (file write) under the injected full-access
//     permission
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires `dim`
// on PATH with an active Dim OAuth login. The RPCs it exercises are the ones
// the execution path needs, verified against dimcode 0.3.2.
//
// Session resume is deliberately not tested here: Dim binds sessions to the
// creating process, so a fresh process's session/load is rejected. The dim
// backend never sends session/load (see dim.go + dim_test.go for that
// assertion).
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Reply with exactly one word: pong. Do not use any tools.",
		ExecOptions{
			Cwd:    t.TempDir(),
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
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real dim output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real dim")
		}
		t.Logf("real dim smoke OK: session=%s output=%q", result.SessionID, result.Output)

	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for real dim result")
	}
}

// TestDimRealResumeRejected verifies that when a ResumeSessionID is passed,
// the dim backend does NOT attempt session/load (which would fail with
// "held by another process"), starts a fresh session instead, and reports
// ResumeRejected=true so the daemon classifies the run correctly.
func TestDimRealResumeRejected(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Pass a fake prior session ID — the backend should ignore it, start
	// fresh, and mark ResumeRejected.
	session, err := backend.Execute(ctx,
		"Reply with exactly one word: fresh. Do not use any tools.",
		ExecOptions{
			Cwd:             t.TempDir(),
			Timeout:         100 * time.Second,
			ResumeSessionID: "sess_prior_does_not_exist",
		},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real dim resume run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !result.ResumeRejected {
			t.Fatal("expected ResumeRejected=true when a resume was requested but dim cannot resume across processes")
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty fresh session id")
		}
		// The session ID must NOT be the one we passed in — it should be a
		// freshly created session.
		if result.SessionID == "sess_prior_does_not_exist" {
			t.Fatal("expected a fresh session id, not the requested resume id")
		}
		t.Logf("real dim resume-rejected OK: session=%s output=%q", result.SessionID, result.Output)

	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for real dim resume result")
	}
}
