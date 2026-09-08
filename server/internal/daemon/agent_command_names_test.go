package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAgentCLIGuardDetectsSwallowedFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "claude --version --token super-secret >/dev/null 2>&1 || true")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after a swallowed agent CLI failure: %s", out)
	}
	if !strings.Contains(string(out), "unexpected agent CLI invocation: claude [arguments redacted]") {
		t.Fatalf("guard diagnostic missing invocation: %s", out)
	}
	if strings.Contains(string(out), "super-secret") {
		t.Fatalf("guard diagnostic exposed command arguments: %s", out)
	}
}

func TestAgentCLIGuardFailsClosedWhenSetupFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the full guarded backend suite runs on Linux/macOS")
	}
	invalidTempDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidTempDir, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write invalid temp directory fixture: %v", err)
	}
	executedMarker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join("..", "..", "..", "scripts", "go-test-with-agent-cli-guard.sh")
	cmd := exec.Command(script, "--", "/bin/sh", "-c", "printf ran >\"$1\"", "sh", executedMarker)
	cmd.Env = append(os.Environ(), "TMPDIR="+invalidTempDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("guard succeeded after setup failure: %s", out)
	}
	if _, statErr := os.Stat(executedMarker); !os.IsNotExist(statErr) {
		t.Fatalf("wrapped command ran after guard setup failure: %v", statErr)
	}
}
