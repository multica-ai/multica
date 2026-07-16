package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingIsolation struct {
	executable string
	args       []string
	err        error
}

func TestPreparedCommandCloseBeforeStartIsTerminal(t *testing.T) {
	cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
	if err := cmd.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := cmd.Start(); err == nil {
		t.Fatal("Start succeeded after Close")
	}
}

func TestPreparedCommandDuplicateStartAndWaitFail(t *testing.T) {
	cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Start(); err == nil {
		t.Fatal("duplicate Start succeeded")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("duplicate Wait succeeded")
	}
}

func TestPreparedCommandConcurrentStartClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			if err := cmd.Start(); err == nil {
				_ = cmd.Wait()
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = cmd.Close()
		}()
		close(start)
		wg.Wait()
	}
}

func TestPreparedCommandConcurrentWaitAllowsOneCaller(t *testing.T) {
	cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			errs <- cmd.Wait()
		}()
	}
	close(start)
	var successes int
	for i := 0; i < 2; i++ {
		if err := <-errs; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful Wait calls = %d, want 1", successes)
	}
}

func TestPreparedCommandRejectsConfigurationAfterStartOrClose(t *testing.T) {
	t.Run("started", func(t *testing.T) {
		cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
		if err := cmd.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer cmd.Wait()
		if err := cmd.SetStderr(os.Stderr); err == nil {
			t.Fatal("SetStderr succeeded after Start")
		}
		if _, err := cmd.StdoutPipe(); err == nil {
			t.Fatal("StdoutPipe succeeded after Start")
		}
	})
	t.Run("closed", func(t *testing.T) {
		cmd := newPreparedCommand(exec.Command("/usr/bin/true"))
		if err := cmd.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if err := cmd.SetCancel(func() error { return nil }); err == nil {
			t.Fatal("SetCancel succeeded after Close")
		}
		if _, err := cmd.CombinedOutput(); err == nil {
			t.Fatal("CombinedOutput succeeded after Close")
		}
	})
}

func (r *recordingIsolation) WrapBound(_ *boundIsolationPolicy, executable, cwd pathIdentity, args []string, leadingExtraFiles int) (string, []string, []*os.File, error) {
	_ = cwd
	_ = leadingExtraFiles
	r.executable = executable.Path
	r.args = append([]string(nil), args...)
	if r.err != nil {
		return "", nil, nil, r.err
	}
	return executable.Path, args, nil, nil
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
	prepared := cmd.(*PreparedCommand)
	if prepared.cmd.Dir != workDir {
		t.Fatalf("cmd.Dir = %q, want %q", prepared.cmd.Dir, workDir)
	}
	if prepared.cmd.WaitDelay != 2*time.Second {
		t.Fatalf("cmd.WaitDelay = %v, want 2s", prepared.cmd.WaitDelay)
	}
	for _, entry := range prepared.cmd.Env {
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
	for _, root := range []string{"/bin", "/usr", "/lib", "/lib64", "/System", "/Library"} {
		if _, err := os.Stat(root); err == nil {
			roots = append(roots, root)
		}
	}
	return roots
}

func TestCommandLauncherCanonicalizesStableSystemExecutableAlias(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux usr-merge aliases")
	}
	resolved, err := filepath.EvalSymlinks("/bin/sh")
	canonical, stableAlias := stableSystemAliasPathForOS("linux", "/bin/sh")
	if err != nil || !stableAlias || !pathWithin(resolved, canonicalRoot(canonical)) {
		t.Skip("host does not use /bin as a symlink alias")
	}
	root := t.TempDir()
	launcher := newCommandLauncher(&recordingIsolation{})
	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: "/bin/sh",
		Args:       []string{"-c", "exit 0"},
		Cwd:        root,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation: &TaskIsolationPolicy{
			WritableRoots: []string{root},
			SystemRoots:   []string{"/bin"},
		},
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	defer cmd.Close()
	prepared := cmd.(*PreparedCommand)
	if prepared.cmd.Args[len(prepared.cmd.Args)-3] != resolved {
		t.Fatalf("wrapped executable = %q, want %q", prepared.cmd.Args[len(prepared.cmd.Args)-3], resolved)
	}
}

func TestResolvedStableSystemExecutableAliasStaysInCanonicalRoot(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		resolved string
		want     bool
	}{
		{name: "direct usr merge target", path: "/bin/sh", resolved: "/usr/bin/sh", want: true},
		{name: "command alias in usr bin", path: "/bin/sh", resolved: "/usr/bin/dash", want: true},
		{name: "usr local escape", path: "/bin/tool", resolved: "/usr/local/bin/tool"},
		{name: "opt escape", path: "/bin/tool", resolved: "/opt/tool"},
		{name: "unlisted alias", path: "/sbin/tool", resolved: "/usr/sbin/tool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isResolvedStableSystemAliasForOS("linux", tt.path, tt.resolved); got != tt.want {
				t.Fatalf("isResolvedStableSystemAliasForOS(%q, %q) = %v, want %v", tt.path, tt.resolved, got, tt.want)
			}
		})
	}
}
