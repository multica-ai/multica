//go:build agentintegration

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resolveZcodeSmokeExecutable locates a runnable zcode for the real-binary
// smoke test. It prefers MULTICA_ZCODE_PATH, then `zcode` on PATH, then a
// wrapper around the local zcode-cli source checkout's vendored runtime
// (~/work/zcode-cli/vendor/zcode.cjs, run through a resolved node).
func resolveZcodeSmokeExecutable(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("MULTICA_ZCODE_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		t.Fatalf("MULTICA_ZCODE_PATH=%s does not exist", p)
	}
	if p, err := exec.LookPath("zcode"); err == nil {
		return p
	}
	vendor := filepath.Join(os.Getenv("HOME"), "work", "zcode-cli", "vendor", "zcode.cjs")
	if _, err := os.Stat(vendor); err != nil {
		t.Skip("no zcode executable and no vendored runtime checkout; skipping real-binary smoke test")
	}
	node := resolveNodeExecutable()
	if node == "" {
		t.Skip("node not found to run the vendored zcode runtime; skipping real-binary smoke test")
	}
	wrapper := filepath.Join(t.TempDir(), "zcode")
	script := "#!/bin/sh\nexec " + shellQuote(node) + " " + shellQuote(vendor) + " \"$@\"\n"
	writeTestExecutable(t, wrapper, []byte(script))
	return wrapper
}

// resolveNodeExecutable finds a node binary; PATH does not always carry one.
func resolveNodeExecutable() string {
	if p := os.Getenv("NODE"); p != "" {
		return p
	}
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	glob, _ := filepath.Glob(filepath.Join(os.Getenv("HOME"), ".nvm", "versions", "node", "*", "bin", "node"))
	if len(glob) > 0 {
		return glob[len(glob)-1]
	}
	return ""
}

// TestZcodeRealAppServerSmoke drives the real `zcode app-server` runtime
// end-to-end: session/create → subscribe → send → consume events → close.
func TestZcodeRealAppServerSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path := resolveZcodeSmokeExecutable(t)
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("zcode CLI version: %s", strings.TrimSpace(string(version)))
	}

	runtimeID := fmt.Sprintf("rt-zcode-smoke-%d", time.Now().UnixNano())
	cleanupZcodeProc(t, runtimeID)
	backend, err := New("zcode", Config{ExecutablePath: path, Logger: slog.Default(), RuntimeID: runtimeID})
	if err != nil {
		t.Fatalf("new zcode backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	workdir := t.TempDir()
	session, err := backend.Execute(ctx, "Reply with exactly one word: pong. Do not use any tools.", ExecOptions{
		Cwd:                       workdir,
		Timeout:                   100 * time.Second,
		HandshakeTimeout:          30 * time.Second,
		SemanticInactivityTimeout: 90 * time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for msg := range session.Messages {
			t.Logf("zcode msg: type=%s tool=%s content=%q", msg.Type, msg.Tool, truncateForLogf(msg.Content))
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("real zcode run did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if !strings.Contains(strings.ToLower(result.Output), "pong") {
			t.Fatalf("expected real zcode output to contain 'pong', got %q", result.Output)
		}
		if result.SessionID == "" {
			t.Error("expected a non-empty session id from real zcode")
		}
		t.Logf("real zcode smoke OK: session=%s output=%q usage=%v", result.SessionID, result.Output, result.Usage)

		// Second turn on the SAME runtime reuses the live app-server process
		// and resumes the in-memory session — the reason the backend keeps a
		// long-lived process instead of spawning per turn.
		if resumed := runZcodeResumeTurn(t, backend, result.SessionID, workdir); resumed != "" {
			t.Logf("real zcode resume OK: resumed_session=%s", resumed)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("timeout waiting for real zcode result")
	}
}

// runZcodeResumeTurn executes a follow-up turn that resumes sessionID on the
// same backend, returning the session id it ran on (or "" if the resume
// fell back to a fresh session).
func runZcodeResumeTurn(t *testing.T, backend Backend, sessionID, workdir string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "What did I ask you for? Reply with one word.", ExecOptions{
		Cwd:                    workdir,
		Timeout:                50 * time.Second,
		HandshakeTimeout:       20 * time.Second,
		ResumeSessionID:        sessionID,
		ResumeExpected:         true,
		ResumeContinuityNotice: "[continuity lost] ",
	})
	if err != nil {
		t.Fatalf("resume execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("resume turn did not complete: status=%q error=%q", result.Status, result.Error)
		}
		if result.SessionID != sessionID {
			t.Fatalf("resume turn ran on %q, want resumed session %q", result.SessionID, sessionID)
		}
		t.Logf("resume output=%q", result.Output)
		return result.SessionID
	case <-time.After(60 * time.Second):
		t.Fatal("timeout waiting for resume result")
		return ""
	}
}

func truncateForLogf(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
