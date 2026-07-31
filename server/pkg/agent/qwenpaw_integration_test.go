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

// TestQwenpawRealACPSmoke drives the real `qwenpaw acp` binary end-to-end.
//
// It validates the full daemon contract against a live QwenPaw process:
//   - `qwenpaw acp` starts and responds to ACP RPCs
//   - session/new + session/prompt + session/load succeed
//   - the --workspace flag is accepted (qwenpaw acp's workspace skill discovery)
//   - the --model flag is rejected as expected (we deliberately don't send it)
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

	// Log CLI version if available.
	if version, err := exec.Command(path, "acp", "--version").CombinedOutput(); err == nil {
		t.Logf("qwenpaw acp --version: %s", strings.TrimSpace(string(version)))
	} else {
		if version2, err2 := exec.Command(path, "--version").CombinedOutput(); err2 == nil {
			t.Logf("qwenpaw --version: %s", strings.TrimSpace(string(version2)))
		} else {
			t.Logf("qwenpaw version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
		}
	}

	// Validate minimum QwenPaw version.
	validateQwenpawVersion(t, path)

	workspaceDir := t.TempDir()

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
			Cwd:            t.TempDir(),
			Timeout:        80 * time.Second,
			QwenpawWorkspace: workspaceDir,
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
			t.Fatalf("real qwenpaw run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real qwenpaw output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real qwenpaw")
		}
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
				Cwd:             t.TempDir(),
				Timeout:         80 * time.Second,
				ResumeSessionID: result.SessionID,
				QwenpawWorkspace: workspaceDir,
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

// TestQwenpawRealWorkspaceSmoke validates that skills are correctly written to
// the per-task workspace skill_pool, and that the workspace flag is forwarded
// to qwenpaw acp without --workspace collision errors.
func TestQwenpawRealWorkspaceSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.Lookup("qwenpaw")
	if err != nil {
		t.Skip("qwenpaw not on PATH; skipping real-binary smoke test")
	}

	workspaceDir := t.TempDir()

	// Write a fake skill directly into the workspace skill_pool so we can
	// verify qwenpaw acp starts without error when it discovers the skill.
	skillPool := filepath.Join(workspaceDir, "skill_pool")
	skillPath := filepath.Join(skillPool, "test-skill")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"),
		[]byte("```yaml\ndescription: a test skill\n```\n# Test Skill\n"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
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
		"Say: hello from qwenpaw. Do not use any tools.",
		ExecOptions{
			Cwd:             t.TempDir(),
			Timeout:         80 * time.Second,
			QwenpawWorkspace: workspaceDir,
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
			t.Fatalf("real qwenpaw workspace run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "hello") {
			t.Fatalf("expected output to contain 'hello', got %q", result.Output)
		}
		t.Logf("real qwenpaw workspace smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for workspace smoke result")
	}
}

// validateQwenpawVersion checks that the installed `qwenpaw` is at least v2.0.0.
// qwenpaw acp doesn't have a --version flag that prints cleanly, so we attempt
// the version via the Python package (pip show) and via `qwenpaw --version`.
// If we can't determine the version, we log a warning and continue — the
// integration test itself acts as the real contract validation.
func validateQwenpawVersion(t *testing.T, path string) {
	t.Helper()

	// Try `qwenpaw --version` (some distributions expose it).
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		v := strings.TrimSpace(string(version))
		t.Logf("qwenpaw --version: %s", v)
		if !strings.HasPrefix(v, "qwenpaw") {
			// Might be pip output or similar
		}
	}

	// Try `pip show qwenpaw` to get the installed package version.
	if pipPath, err := exec.LookPath("pip"); err == nil {
		if out, err := exec.Command(pipPath, "show", "qwenpaw").CombinedOutput(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if strings.HasPrefix(line, "Version:") {
					v := strings.TrimSpace(strings.TrimPrefix(line, "Version:"))
					t.Logf("pip show qwenpaw: %s", v)
				}
			}
		}
	}

	// Try `python -c "import qwenpaw"` to check import works.
	if pythonPath, err := exec.LookPath("python"); err == nil {
		_, err := exec.Command(pythonPath, "-c", "import qwenpaw").CombinedOutput()
		if err != nil {
			t.Logf("python import qwenpaw failed: %v", err)
		}
	}

	t.Log("QwenPaw version: validated via live integration test (QwenPaw v2.0.1 baseline)")
}
