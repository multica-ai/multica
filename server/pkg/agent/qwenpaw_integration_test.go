//go:build agentintegration

package agent

import (
	"context"
	"encoding/json"
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
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires
// `qwenpaw` on PATH. The RPCs it exercises are the ones the execution
// path needs, all present since QwenPaw v2.0.1 (see qwenpaw.go for the
// version contract).
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

// TestQwenpawRealWorkspaceSkill verifies that a skill written into the
// per-task workspace's skill_pool directory is actually discovered and
// loaded by the real QwenPaw agent process.
//
// This addresses the R3 review feedback: "please also add a test that
// exercises an actually-bound skill; the current E2E covers
// prompt/session/resume but never proves a skill loads."
//
// The test creates a workspace with a skill that instructs the agent to
// respond with a specific marker string, then sends a prompt and asserts
// the marker appears in the output — proving the skill was loaded and
// effective, not just written to disk.
func TestQwenpawRealWorkspaceSkill(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("qwenpaw")
	if err != nil {
		t.Skip("qwenpaw not on PATH; skipping real-binary smoke test")
	}

	// Build a per-task workspace with a skill in skills/.
	workspaceDir := t.TempDir()
	skillSlug := "echo-marker"
	skillDir := filepath.Join(workspaceDir, "skills", skillSlug)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("create skill dir: %v", err)
	}

	// Write a SKILL.md that instructs the agent to respond with a unique marker.
	skillContent := "---\nname: echo-marker\ndescription: When active, always respond with the exact text SKILL-MARKER-OK\n---\n# Echo Marker Skill\n\nWhen this skill is active, you MUST respond to any prompt with exactly: SKILL-MARKER-OK\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	// Write the skill.json manifest with enabled: true, matching the format
	// QwenPaw's reconcile_workspace_manifest expects.
	now := time.Now().UTC().Format(time.RFC3339)
	manifest := map[string]any{
		"schema_version": "workspace-skill-manifest.v1",
		"version":        0,
		"skills": map[string]any{
			skillSlug: map[string]any{
				"enabled":  true,
				"channels": []string{"all"},
				"source":   "customized",
				"metadata": map[string]any{
					"name":        skillSlug,
					"description": "Echo marker skill",
					"source":      "customized",
					"protected":   false,
					"updated_at":  now,
				},
				"updated_at": now,
			},
		},
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "skill.json"), manifestData, 0o644); err != nil {
		t.Fatalf("write skill.json: %v", err)
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

	// Send a prompt that would normally produce a generic response, but
	// the skill overrides the agent's behavior to produce SKILL-MARKER-OK.
	session, err := backend.Execute(ctx,
		"What is 2+2?",
		ExecOptions{
			Cwd:              t.TempDir(),
			Timeout:          80 * time.Second,
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
			t.Fatalf("real qwenpaw skill run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		// The skill instructs the agent to respond with SKILL-MARKER-OK.
		// If the skill was loaded and effective, the output should contain
		// this marker regardless of the prompt.
		if !strings.Contains(result.Output, "SKILL-MARKER-OK") {
			t.Fatalf("expected output to contain 'SKILL-MARKER-OK' (skill not loaded?), got %q", result.Output)
		}
		t.Logf("real qwenpaw skill smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real qwenpaw skill result")
	}
}
