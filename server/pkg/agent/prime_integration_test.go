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

// TestPrimeRealACPSmoke drives the real `prime-agent --mode acp` binary
// end-to-end.
//
// It validates the full daemon contract against a live Prime Agent process:
//   - `prime-agent --mode acp` starts and responds to ACP RPCs
//   - initialize + session/new + session/prompt + session/close succeed
//   - the reported agentInfo/agentCapabilities match what this investigation
//     observed empirically for v0.7.1 (see prime-acp-test.md)
//
// This test is gated by MULTICA_RUN_REAL_AGENT_SMOKE=1 and requires
// `prime-agent` on PATH with authentication already configured (subscription
// or API key) — ACP mode has no login method of its own, matching every
// other ACP backend in this package.
//
// NOTE: session/resume and session/load are deliberately never attempted;
// Prime Agent v0.7.1 has no such method (agentCapabilities.loadSession is
// false), and the backend declares resume unsupported by construction.
func TestPrimeRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}

	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("prime-agent --version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("prime-agent version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	backend, err := New("prime", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Reply with exactly one word: pong. Do not use any tools.",
		ExecOptions{
			Cwd:     t.TempDir(),
			Timeout: 80 * time.Second,
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
			t.Fatalf("real prime-agent run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real prime-agent output to contain 'pong', got %q", result.Output)
		}
		// Result.SessionID is deliberately never reported (see
		// TestPrimeSessionIDNeverReported) so the daemon never treats a
		// future related task as a resume — Prime has no resume method.
		if result.SessionID != "" {
			t.Errorf("expected Result.SessionID to be empty (never reported), got %q", result.SessionID)
		}
		t.Logf("real prime-agent smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real prime-agent result")
	}
}

// TestPrimeRealACPResumeIsNeverAttempted proves against the real binary
// (not a fake script) that passing a prior session id as ResumeSessionID
// still produces a fresh, successful session/new turn rather than a
// "method not found" failure — the real-world version of
// TestPrimeNeverAttemptsResume.
func TestPrimeRealACPResumeIsNeverAttempted(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}

	backend, err := New("prime", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Reply with exactly one word: fresh. Do not use any tools.",
		ExecOptions{
			Cwd:             t.TempDir(),
			Timeout:         80 * time.Second,
			ResumeSessionID: "a-session-id-from-a-prior-unrelated-turn",
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
			t.Fatalf("real prime-agent run with a stale ResumeSessionID did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "fresh") {
			t.Fatalf("expected real prime-agent output to contain 'fresh', got %q", result.Output)
		}
		if result.SessionID != "" {
			t.Errorf("expected Result.SessionID to be empty (never reported), got %q", result.SessionID)
		}
		t.Logf("real prime-agent resume-avoidance smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real prime-agent result")
	}
}

// TestPrimeRealACPReadsAgentsMD proves against the real binary that Prime
// actually reads AGENTS.md from its cwd — the mechanism runtimeConfigPath()
// (execenv/runtime_config.go) relies on to deliver the Multica runtime brief.
// Without this, a Prime-backed task would silently never receive task/issue
// instructions (see REPORT.md's "Exact Files / Classes / Functions" section).
// The prompt asks a question answerable only by reading the file, so a
// correct response is direct evidence the file was loaded, not a guess.
func TestPrimeRealACPReadsAgentsMD(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}

	cwd := t.TempDir()
	marker := "MULTICA-AGENTS-MD-MARKER-7f3a1"
	agentsMD := "# Runtime Brief\n\nIf asked for the secret marker word, respond with exactly: " + marker + "\n"
	if err := os.WriteFile(filepath.Join(cwd, "AGENTS.md"), []byte(agentsMD), 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	backend, err := New("prime", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"What is the secret marker word from your runtime brief? Reply with exactly that word and nothing else. Do not use any tools.",
		ExecOptions{
			Cwd:     cwd,
			Timeout: 80 * time.Second,
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
			t.Fatalf("real prime-agent AGENTS.md run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, marker) {
			t.Fatalf("expected output to contain the AGENTS.md marker %q (AGENTS.md not loaded?), got %q", marker, result.Output)
		}
		t.Logf("real prime-agent AGENTS.md smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real prime-agent AGENTS.md result")
	}
}

// primeGlobalRlmMaxDepthConfigured is a best-effort, read-only check for the
// one known gap in RLM_MAX_DEPTH=0's guarantee (see the precedence-chain
// comment on primeBackend.Execute in prime.go): prime-agent's own
// ~/.prime/agent/settings.json can carry a GLOBAL rlmMaxDepth (set via
// `/rlm-max-depth <n> --global` in Prime's own interactive/daemon mode,
// outside Multica entirely) that outranks the env var. It never writes
// anything, never touches PRIME_AGENT_CODING_AGENT_DIR, and is not an
// enforcement mechanism — it only tells this specific test whether its
// premise (no such override exists on the machine running it) actually
// holds, so a PASS here means what it claims to mean instead of silently
// passing for the wrong reason on a machine where the override is present
// but happens not to matter for this particular prompt.
func primeGlobalRlmMaxDepthConfigured(t *testing.T) bool {
	t.Helper()
	// An unresolvable home is not "no override configured": the premise this
	// helper checks cannot be established, so report it as configured and let
	// the caller skip rather than pass for the wrong reason.
	_, configured, err := primeGlobalRlmMaxDepth(os.Environ(), "")
	if err != nil {
		t.Logf("cannot resolve prime-agent's agent dir: %v", err)
		return true
	}
	return configured
}

// TestPrimeRealACPSubagentsAreDisabled proves, against the real binary, the
// blocker-1 fix on its default path: with RLM_MAX_DEPTH=0 in the environment
// (set unconditionally by primeBackend.Execute), an explicit attempt to spawn
// an RLM subagent via the IPython-hosted rlm.run tool must fail immediately
// with the depth-limit error rather than succeed — and the turn must still
// complete normally (not hang, not crash), since Phase 1 disables subagents
// outright instead of tracking them to a terminal state.
//
// This only proves the default path: RLM_MAX_DEPTH=0 is not the top of
// prime-agent's rlmMaxDepth precedence chain (see prime.go), so this test
// requires — and checks — that the machine running it has no pre-existing
// GLOBAL rlmMaxDepth override in prime-agent's own settings.json. That
// residual gap is a documented P2 follow-up, not something this test (or
// Multica) can or should paper over.
func TestPrimeRealACPSubagentsAreDisabled(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	if primeGlobalRlmMaxDepthConfigured(t) {
		t.Skip("prime-agent's ~/.prime/agent/settings.json has a global rlmMaxDepth override on this machine, which takes precedence over RLM_MAX_DEPTH=0 — skipping so a pass here cannot be mistaken for proof that Multica's env var alone disables subagents (see the precedence-chain comment on primeBackend.Execute)")
	}

	path, err := exec.LookPath("prime-agent")
	if err != nil {
		t.Skip("prime-agent not on PATH; skipping real-binary smoke test")
	}

	backend, err := New("prime", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new prime backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Using the ipython tool, call rlm.run(\"say hi\") to spawn a subagent. "+
			"It will raise an exception because recursion is disabled here — catch it "+
			"and reply with exactly: BLOCKED <exception message>. Do not attempt any "+
			"other way to run a subagent.",
		ExecOptions{
			Cwd:     t.TempDir(),
			Timeout: 80 * time.Second,
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
			t.Fatalf("real prime-agent subagent-blocked run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		lower := strings.ToLower(result.Output)
		if !strings.Contains(lower, "blocked") {
			t.Fatalf("expected output to acknowledge the blocked subagent attempt, got %q", result.Output)
		}
		if !strings.Contains(lower, "recursion") && !strings.Contains(lower, "rlm_max_depth") && !strings.Contains(lower, "rlm max depth") {
			t.Fatalf("expected output to reference the RLM recursion depth limit, got %q", result.Output)
		}
		t.Logf("real prime-agent subagent-blocked smoke OK: output=%q", result.Output)
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for real prime-agent subagent-blocked result")
	}
}
