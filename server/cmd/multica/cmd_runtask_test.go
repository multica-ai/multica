package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRunTaskCmd_ValidatesPayload builds the multica binary and confirms the
// run-task subcommand parses --task-file, fails on incomplete payloads, and
// surfaces the validation error to stderr. End-to-end coverage for the agent
// happy path lives in TestRunOneTask_HappyPath (daemon package); this test
// only proves the CLI wiring.
func TestRunTaskCmd_ValidatesPayload(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("not supported on %s", runtime.GOOS)
	}

	bin := filepath.Join(t.TempDir(), "multica")
	build := exec.Command("go", "build", "-o", bin, "./")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	payload, _ := json.Marshal(map[string]any{"id": "t1"})
	pf := filepath.Join(t.TempDir(), "task.json")
	if err := os.WriteFile(pf, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "run-task", "--task-file", pf)
	cmd.Env = append(os.Environ(),
		"MULTICA_TOKEN=tk",
		"MULTICA_SERVER_URL=http://example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error from incomplete payload, got nil; output: %s", out)
	}
	if want := "missing runtime_id"; !strings.Contains(string(out), want) {
		t.Fatalf("expected error mentioning %q, got: %s", want, out)
	}
}

func TestRunTaskCmd_MissingTaskFileFlag(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("not supported on %s", runtime.GOOS)
	}

	bin := filepath.Join(t.TempDir(), "multica")
	build := exec.Command("go", "build", "-o", bin, "./")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin, "run-task")
	cmd.Env = append(os.Environ(), "MULTICA_TOKEN=tk", "MULTICA_SERVER_URL=http://example.invalid")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected error when --task-file omitted, got nil; output: %s", out)
	}
	if want := "--task-file is required"; !strings.Contains(string(out), want) {
		t.Fatalf("expected error mentioning %q, got: %s", want, out)
	}
}
