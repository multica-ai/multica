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

// TestZcodeRealProtocolSmoke drives the real `zcode app-server` end-to-end.
//
// It validates the daemon contract against a live ZCode CLI process:
//   - `zcode app-server` starts and answers session/create
//   - a prompt turn completes with a non-empty session id
//   - the agent can execute a shell command and write a sentinel file
//     (session/create pins mode=yolo, so no interactive permission prompt
//     blocks the turn)
//
// Gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 per AGENTS.md. The CLI is
// discovered from MULTICA_ZCODE_PATH (a `node <zcode.cjs> "$@"` wrapper for
// machines that only have the desktop app bundle, not a global install) or
// `zcode` on PATH, and requires an active Z.AI login.
func TestZcodeRealProtocolSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path := os.Getenv("MULTICA_ZCODE_PATH")
	if path == "" {
		discovered, err := exec.LookPath("zcode")
		if err != nil {
			t.Skip("zcode not on PATH and MULTICA_ZCODE_PATH unset; skipping real-binary smoke test")
		}
		path = discovered
	}

	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("zcode version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("zcode version unavailable: %v", err)
	}

	backend, err := New("zcode", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}

	cwd := t.TempDir()
	sentinel := filepath.Join(cwd, "sentinel.txt")

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// A headless app-server session has no runtime default model — a
	// modelless create succeeded but the turn failed with CONFIGURATION_ERROR
	// "Select a model before continuing" — so pick one from the real catalog.
	// The registry's provider ids are account-shaped and cannot be guessed:
	// three hardcoded attempts (account:zai-start-plan,
	// account:zai-individual-coding-plan) all answered "Provider Registry 中
	// 不存在 Model", which is exactly what discovery exists to answer.
	// Override with MULTICA_ZCODE_SMOKE_MODEL for a specific pick.
	model := os.Getenv("MULTICA_ZCODE_SMOKE_MODEL")
	thinkingLevel := ""
	if model == "" {
		catalog, err := ListModels(ctx, "zcode", NewCommand(path, nil))
		if err != nil {
			t.Fatalf("discover zcode models: %v", err)
		}
		if len(catalog.Models) == 0 {
			t.Fatal("zcode model discovery returned no models; is the CLI logged in?")
		}
		for _, m := range catalog.Models {
			// Prefer a reasoning-capable model: zcode rejects its selection
			// with "Reasoning level is required" unless options.reasoningLevel
			// carries one of the model's advertised levels, and a reasoning
			// smoke exercises that contract end-to-end.
			if m.Thinking == nil || len(m.Thinking.SupportedLevels) == 0 {
				continue
			}
			model = m.ID
			thinkingLevel = m.Thinking.DefaultLevel
			if thinkingLevel == "" {
				thinkingLevel = m.Thinking.SupportedLevels[0].Value
			}
			break
		}
		if model == "" {
			model = catalog.Models[0].ID
		}
		t.Logf("smoke model from discovery: %s (thinking level %q)", model, thinkingLevel)
	} else {
		thinkingLevel = "low"
	}

	// Prompt the agent to write a sentinel file. This is the real execution
	// proof: under mode=yolo the shell tool auto-approves, so success confirms
	// session/create, the prompt turn and the event stream all work against
	// the live protocol server. Reasoning-capable models reject their
	// selection without options.reasoningLevel, so the smoke pins the
	// discovered model's own default thinking level.
	session, err := backend.Execute(ctx,
		"Use your shell/exec tool to run: echo zcode-exec-ok > sentinel.txt — then reply with exactly: done",
		ExecOptions{
			Cwd:           cwd,
			Model:         model,
			ThinkingLevel: thinkingLevel,
			Timeout:       170 * time.Second,
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
			t.Fatalf("real zcode run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real zcode")
		} else {
			t.Logf("zcode session id: %s", result.SessionID)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the real zcode run to finish")
	}

	content, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel file not written by real zcode: %v", err)
	}
	if strings.TrimSpace(string(content)) != "zcode-exec-ok" {
		t.Fatalf("sentinel content mismatch: %q", strings.TrimSpace(string(content)))
	}
}
