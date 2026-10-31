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

// TestZcodeRealStreamJSONSmoke drives the real `zcode` CLI end-to-end.
//
// It validates the full daemon contract against a live ZCode process:
//   - `zcode -p --output-format stream-json` emits the ZCode Protocol event
//     stream the backend parses (turn lifecycle, session ids, usage)
//   - the terminal session.updated event finalizes a successful run
//   - `--resume <sess_id>` continues the same session under the same id
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires `zcode`
// on PATH (the community zcode-app-cli distribution) with a working provider
// configuration — either `zcode login` account plans or a personal provider
// with a saved default model in ~/.zcode/v2/provider_config.json.
func TestZcodeRealStreamJSONSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("zcode")
	if err != nil {
		t.Skip("zcode not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("zcode --version: %s", strings.TrimSpace(string(version)))
	}

	backend, err := New("zcode", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}

	workDir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Reply with exactly one word: pong. Do not use any tools.",
		ExecOptions{Cwd: workDir, Timeout: 80 * time.Second},
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	var sessionID string
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real zcode run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real zcode output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Fatal("expected a non-empty session id from real zcode")
		}
		sessionID = result.SessionID
		t.Logf("real zcode smoke OK: session=%s output=%q", result.SessionID, result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real zcode result")
	}

	t.Run("stream resume", func(t *testing.T) {
		backend2, err := New("zcode", Config{ExecutablePath: path, Logger: slog.Default()})
		if err != nil {
			t.Fatalf("new zcode backend (resume): %v", err)
		}
		ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel2()
		session2, err := backend2.Execute(ctx2,
			"Say: resume-ok. Do not use any tools.",
			ExecOptions{Cwd: workDir, Timeout: 80 * time.Second, ResumeSessionID: sessionID},
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
			if r.SessionID != sessionID {
				t.Fatalf("resumed session id = %q, want the requested %q", r.SessionID, sessionID)
			}
			t.Logf("real zcode resume OK: session=%s output=%q", r.SessionID, r.Output)
		case <-time.After(90 * time.Second):
			t.Fatal("timeout waiting for resumed result")
		}
	})
}
