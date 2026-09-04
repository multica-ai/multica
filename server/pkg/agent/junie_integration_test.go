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

// TestJunieRealACPSmoke exercises the real Junie ACP transport with a model the
// operator has explicitly confirmed is local. Requiring a separate test-only
// model variable prevents an ambient JetBrains login or API key from silently
// turning this opt-in smoke test into a billable cloud request.
func TestJunieRealACPSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("junie")
	if err != nil {
		t.Skip("junie not on PATH")
	}
	model := strings.TrimSpace(os.Getenv("MULTICA_JUNIE_LOCAL_SMOKE_MODEL"))
	if model == "" {
		t.Skip("set MULTICA_JUNIE_LOCAL_SMOKE_MODEL to an explicitly verified local Junie model id")
	}
	backend, err := New("junie", Config{
		ExecutablePath: path,
		Logger:         slog.Default(),
		Env:            map[string]string{"JUNIE_API_KEY": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	session, err := backend.Execute(ctx,
		"Use a file-writing tool to write exactly junie-acp-ok to junie-smoke.txt, then reply with exactly done.",
		ExecOptions{Cwd: cwd, Model: model, ThinkingLevel: "high", Timeout: 150 * time.Second},
	)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if result.Status != "completed" || result.SessionID == "" {
		t.Fatalf("real Junie run failed: %#v", result)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "junie-smoke.txt"))
	if err != nil || strings.TrimSpace(string(data)) != "junie-acp-ok" {
		t.Fatalf("Junie did not complete the file tool call: data=%q err=%v", data, err)
	}
}
