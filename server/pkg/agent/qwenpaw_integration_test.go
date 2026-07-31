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

// TestQwenpawRealACPSmoke drives the real `qwenpaw acp` binary end-to-end.
//
// It validates the full daemon contract against a live QwenPaw process:
//   - `qwenpaw acp` starts and responds to ACP RPCs
//   - session/new + session/prompt + session/load succeed
//   - the --workspace flag is accepted (qwenpaw acp's workspace skill discovery)
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires
// `qwenpaw` on PATH. It is validated against QwenPaw v2.0.1.
//
// NOTE: model override via session/set_model is deliberately not attempted;
// QwenPaw does not support it, and the backend declares model override
// unsupported.
func TestQwenpawRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	// Discover `qwenpaw` binary on PATH.
	path, err := exec.LookPath("qwenpaw")
	if err != nil {
		t.Skip("qwenpaw not on PATH; skipping real-binary smoke test")
	}

	// Log CLI version.
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("qwenpaw --version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("qwenpaw version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	backend, err := New("qwenpaw", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new qwenpaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Reply with exactly one word: pong. Do not use any tools.",
		ExecOptions{
			Cwd:              t.TempDir(),
			Timeout:          80 * time.Second,
			QwenpawWorkspace: t.TempDir(),
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

	var sessionID string
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real qwenpaw run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real qwenpaw output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real qwenpaw")
		}
		sessionID = result.SessionID
		t.Logf("real qwenpaw smoke OK: session=%s output=%q", result.SessionID, result.Output)

	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real qwenpaw result")
	}

	// Verify session/load: pass the same session ID back as ResumeSessionID
	// and confirm a fresh backend can resume it.
	t.Run("session load resume", func(t *testing.T) {
		backend2, err := New("qwenpaw", Config{
			ExecutablePath: path,
			Logger:         slog.Default(),
		})
		if err != nil {
			t.Fatalf("new qwenpaw backend (resume): %v", err)
		}

		ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel2()

		session2, err := backend2.Execute(ctx2,
			"Say: resume-ok. Do not use any tools.",
			ExecOptions{
				Cwd:              t.TempDir(),
				Timeout:          80 * time.Second,
				ResumeSessionID:  sessionID,
				QwenpawWorkspace: t.TempDir(),
			},
		)
		if err != nil {
			t.Fatalf("resume execute: %v", err)
		}

		go func() {
			for range session2.Messages {
			}
		}()

		select {
		case r := <-session2.Result:
			if r.Status != "completed" {
				t.Fatalf("resumed run did not complete: status=%q error=%q", r.Status, r.Error)
			}
			if !strings.Contains(strings.ToLower(r.Output), "resume-ok") {
				t.Fatalf("expected resumed output to contain 'resume-ok', got %q", r.Output)
			}
			t.Logf("real qwenpaw resume OK: session=%s output=%q", r.SessionID, r.Output)
		case <-time.After(90 * time.Second):
			t.Fatal("timeout waiting for resumed result")
		}
	})
}
