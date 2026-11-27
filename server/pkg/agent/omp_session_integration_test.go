//go:build agentintegration

package agent

import (
	"bytes"
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

// TestOmpSessionStartupWithoutModelCall checks the real CLI's session contract
// without sending a prompt. Use an explicitly selected OMP binary and an empty
// home/config directory; no user account or provider credential is consulted.
func TestOmpSessionStartupWithoutModelCall(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run real-agent smoke tests")
	}
	binary := os.Getenv("MULTICA_OMP_SMOKE_EXECUTABLE")
	if binary == "" {
		t.Skip("set MULTICA_OMP_SMOKE_EXECUTABLE to the OMP binary to verify")
	}
	home := t.TempDir()
	cwd := t.TempDir()
	env := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home,
		"USERPROFILE=" + home, "TMPDIR=" + t.TempDir(),
		"PI_CODING_AGENT_DIR=" + filepath.Join(home, "agent"),
		// Select a catalog model without requiring a stored account. With no
		// prompt, print mode never enters session.prompt or calls the provider.
		"ANTHROPIC_API_KEY=unused-no-model-call",
	}
	opts := ExecOptions{Model: "anthropic/claude-sonnet-4-20250514", CustomArgs: []string{"--no-extensions", "--no-skills", "--no-title"}}
	run := func(args []string) (string, string, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir, cmd.Env = cwd, env
		cmd.Stdin = strings.NewReader("")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		return stdout.String(), stderr.String(), err
	}
	empty := filepath.Join(cwd, "empty.jsonl")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := run(buildOmpArgs(empty, "", opts, slog.Default()))
	if err == nil || !strings.Contains(stderr, "session file holds no entries") {
		t.Fatalf("expected OMP's empty-resume refusal: %v, %s", err, stderr)
	}
	stdout, stderr, err := run(buildOmpArgs("", t.TempDir(), opts, slog.Default()))
	if err != nil {
		t.Fatalf("fresh startup: %v, %s", err, stderr)
	}
	var header string
	for _, line := range strings.Split(stdout, "\n") {
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "session" {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("fresh startup did not emit a session header: %s", stdout)
	}
	// A prompt-free invocation may discard its draft. Persist its native header
	// explicitly to verify path-based resume without generating a model turn.
	valid := filepath.Join(cwd, "valid.jsonl")
	if err := os.WriteFile(valid, []byte(header+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := run(buildOmpArgs(valid, "", opts, slog.Default())); err != nil {
		t.Fatalf("valid resume startup: %v, %s", err, stderr)
	}
}
