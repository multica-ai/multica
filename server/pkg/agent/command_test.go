package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordingIsolation struct {
	executable string
	args       []string
	err        error
}

func (r *recordingIsolation) Wrap(_ TaskIsolationPolicy, executable string, args []string) (string, []string, error) {
	r.executable = executable
	r.args = append([]string(nil), args...)
	return executable, args, r.err
}

func TestCommandLauncherRejectsMissingPolicyBeforeExecution(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "marker")
	launcher := newCommandLauncher(&recordingIsolation{})
	_, err := launcher.Command(context.Background(), CommandRequest{
		Executable: "/usr/bin/touch",
		Args:       []string{marker},
		Cwd:        t.TempDir(),
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
	})
	if err == nil || !strings.Contains(err.Error(), "isolation policy") {
		t.Fatalf("expected missing isolation policy error, got %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("command ran without an isolation policy: stat error = %v", statErr)
	}
}

func TestCommandLauncherOwnsExecutableArgsCwdAndExplicitEnv(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.Mkdir(workDir, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "print-env.sh")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$PWD\" \"$TASK_VALUE\" \"${DAEMON_ONLY_SECRET-unset}\"\n"))

	t.Setenv("DAEMON_ONLY_SECRET", "must-not-leak")
	isolation := &recordingIsolation{}
	launcher := newCommandLauncher(isolation)
	policy := &TaskIsolationPolicy{
		WritableRoots: []string{root},
		SystemRoots:   existingSystemRootsForTest(t),
		Network:       NetworkAccessPublicAndLoopback,
	}
	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Args:       []string{"arg-one"},
		Cwd:        workDir,
		Env: map[string]string{
			"PATH":       "/usr/bin:/bin",
			"TASK_VALUE": "task-only",
		},
		Isolation: policy,
		WaitDelay: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Output: %v", err)
	}
	gotLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	wantWorkDir, err := filepath.EvalSymlinks(workDir)
	if err != nil {
		t.Fatalf("resolve workdir: %v", err)
	}
	wantLines := []string{wantWorkDir, "task-only", "unset"}
	if !reflect.DeepEqual(gotLines, wantLines) {
		t.Fatalf("child output = %#v, want %#v", gotLines, wantLines)
	}
	if isolation.executable != executable || !reflect.DeepEqual(isolation.args, []string{"arg-one"}) {
		t.Fatalf("isolation received (%q, %#v), want (%q, %#v)", isolation.executable, isolation.args, executable, []string{"arg-one"})
	}
	if cmd.Dir != workDir {
		t.Fatalf("cmd.Dir = %q, want %q", cmd.Dir, workDir)
	}
	if cmd.WaitDelay != 2*time.Second {
		t.Fatalf("cmd.WaitDelay = %v, want 2s", cmd.WaitDelay)
	}
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, "DAEMON_ONLY_SECRET=") {
			t.Fatalf("daemon-only environment leaked: %q", entry)
		}
	}
}

func TestCommandLauncherRejectsRelativeAndParentTraversalPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	policy := &TaskIsolationPolicy{WritableRoots: []string{root}, SystemRoots: existingSystemRootsForTest(t)}
	launcher := newCommandLauncher(&recordingIsolation{})

	tests := []struct {
		name       string
		executable string
		cwd        string
	}{
		{name: "relative executable", executable: "sh", cwd: root},
		{name: "relative cwd", executable: "/bin/sh", cwd: "relative"},
		{name: "executable traversal", executable: root + "/child/../tool", cwd: root},
		{name: "cwd traversal", executable: "/bin/sh", cwd: root + "/child/.."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := launcher.Command(context.Background(), CommandRequest{
				Executable: tt.executable,
				Cwd:        tt.cwd,
				Env:        map[string]string{"PATH": "/usr/bin:/bin"},
				Isolation:  policy,
			})
			if err == nil {
				t.Fatal("expected path validation error")
			}
		})
	}
}

func existingSystemRootsForTest(t *testing.T) []string {
	t.Helper()
	var roots []string
	for _, root := range []string{"/bin", "/usr", "/System", "/Library"} {
		if _, err := os.Stat(root); err == nil {
			roots = append(roots, root)
		}
	}
	return roots
}
