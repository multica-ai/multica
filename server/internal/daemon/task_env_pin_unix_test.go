//go:build !windows

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskIdentityPinSurvivesSecretsWrapperOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	wrapper := filepath.Join(dir, "doppler")
	agent := filepath.Join(dir, "agent")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nwhile [ $# -gt 0 ]; do\n  arg=$1\n  shift\n  if [ \"$arg\" = \"--\" ]; then\n    break\n  fi\ndone\nexport MULTICA_TOKEN=mul_from_doppler\nexec \"$@\"\n"), 0o700); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' \"$MULTICA_TOKEN\"\n"), 0o700); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	pin, err := writeTaskIdentityPin(dir, map[string]string{"MULTICA_TOKEN": "mat_task"})
	if err != nil {
		t.Fatalf("writeTaskIdentityPin: %v", err)
	}

	withoutPin := exec.Command(wrapper, "run", "--", agent)
	withoutPin.Env = []string{"MULTICA_TOKEN=mat_task", "PATH=/usr/bin:/bin"}
	out, err := withoutPin.Output()
	if err != nil {
		t.Fatalf("wrapper without pin: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "mul_from_doppler" {
		t.Fatalf("without pin token = %q, want mul_from_doppler (precondition)", got)
	}

	prefix := insertTaskIdentityPinAfterWrapper([]string{"run", "--", agent}, pin)
	args := append([]string{}, prefix...)
	withPin := exec.Command(wrapper, args...)
	withPin.Env = []string{"MULTICA_TOKEN=mat_task", "PATH=/usr/bin:/bin"}
	out, err = withPin.Output()
	if err != nil {
		t.Fatalf("wrapper with pin: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "mat_task" {
		t.Fatalf("with pin token = %q, want mat_task", got)
	}
}

func TestTaskIdentityPinQuotesUnsafeValuesAndSkipsUnsafeKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	agent := filepath.Join(dir, "agent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s\\n' \"$MULTICA_TOKEN\"\nprintf '%s\\n' \"${UNSAFE-unset}\"\n"), 0o700); err != nil {
		t.Fatalf("write agent: %v", err)
	}

	pin, err := writeTaskIdentityPinScript(dir, map[string]string{
		"MULTICA_TOKEN": "mat_it's_ok",
		"BAD-KEY":       "nope",
	})
	if err != nil {
		t.Fatalf("writeTaskIdentityPin: %v", err)
	}
	if pin == "" {
		t.Fatal("expected pin path")
	}

	cmd := exec.Command(pin, agent)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("pin exec: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("output lines = %#v", lines)
	}
	if lines[0] != "mat_it's_ok" {
		t.Fatalf("quoted token = %q", lines[0])
	}
	if lines[1] != "unset" {
		t.Fatalf("unsafe key leaked: %q", lines[1])
	}
}
