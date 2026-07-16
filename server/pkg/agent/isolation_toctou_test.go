package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCommandLauncherDetectsRootReplacementBeforeStart exercises the
// validate-to-start TOCTOU window: a root is opened and bound, then replaced
// at the same pathname before Start. Launch must fail closed.
func TestCommandLauncherDetectsRootReplacementBeforeStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path replacement")
	}

	root := t.TempDir()
	writable := filepath.Join(root, "task")
	if err := os.Mkdir(writable, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "tool.sh")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nprintf ok\n"))

	// Keep system roots minimal and present.
	systemRoots := existingSystemRootsForTest(t)
	policy := &TaskIsolationPolicy{
		WritableRoots: []string{writable, root},
		SystemRoots:   systemRoots,
		Network:       NetworkAccessNone,
	}

	// Darwin intentionally fails closed because sandbox-exec cannot preserve
	// cwd and executable identity through final exec. Exercise the launch-time
	// replacement check with the deterministic recording isolation instead.
	var launcher *CommandLauncher
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat("/usr/bin/bwrap"); err == nil {
			launcher = newCommandLauncher(newLinuxIsolation("/usr/bin/bwrap"))
		}
	}
	if launcher == nil {
		launcher = newCommandLauncher(&recordingIsolation{})
	}

	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Args:       nil,
		Cwd:        writable,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation:  policy,
		WaitDelay:  time.Second,
	})
	if err != nil {
		t.Fatalf("Command prepare: %v", err)
	}
	defer cmd.Close()

	// Replace the writable root directory with a new inode at the same path.
	replacementParent := filepath.Join(root, "replacement-staging")
	if err := os.Mkdir(replacementParent, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(replacementParent, "task")
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	// Move original aside and put replacement at the validated path.
	originalMoved := filepath.Join(root, "task-original")
	if err := os.Rename(writable, originalMoved); err != nil {
		t.Fatalf("rename original root: %v", err)
	}
	if err := os.Rename(replacement, writable); err != nil {
		t.Fatalf("install replacement root: %v", err)
	}

	err = cmd.Start()
	if err == nil {
		_ = cmd.Wait()
		t.Fatal("Start unexpectedly succeeded after root replacement")
	}
	if !strings.Contains(err.Error(), "replaced") && !strings.Contains(err.Error(), "changed") && !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Start failed for unexpected reason: %v", err)
	}
}

func TestDarwinCommandLauncherFailsClosedWithoutIdentityBoundExec(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin isolation contract")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "tool.sh")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nexit 0\n"))
	launcher := newCommandLauncher(newDarwinIsolation("/usr/bin/sandbox-exec"))
	_, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Cwd:        root,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation: &TaskIsolationPolicy{
			WritableRoots: []string{root},
			SystemRoots:   existingSystemRootsForTest(t),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot bind cwd and executable identity") {
		t.Fatalf("Command error = %v, want identity-bound exec failure", err)
	}
}

// TestCommandLauncherDetectsExecutableReplacementBeforeStart ensures a swapped
// executable path fails closed on Start after prepare-time validation.
func TestCommandLauncherDetectsExecutableReplacementBeforeStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path replacement")
	}

	root := t.TempDir()
	executable := filepath.Join(root, "tool.sh")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nprintf original\n"))
	policy := &TaskIsolationPolicy{
		WritableRoots: []string{root},
		SystemRoots:   existingSystemRootsForTest(t),
	}
	launcher := newCommandLauncher(&recordingIsolation{})
	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Cwd:        root,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation:  policy,
	})
	if err != nil {
		t.Fatalf("Command prepare: %v", err)
	}
	defer cmd.Close()

	// Replace executable inode at the same path.
	tmp := filepath.Join(root, "tool-new.sh")
	writeTestExecutable(t, tmp, []byte("#!/bin/sh\nprintf replaced\n"))
	if err := os.Rename(executable, filepath.Join(root, "tool-old.sh")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, executable); err != nil {
		t.Fatal(err)
	}

	if err := cmd.Start(); err == nil {
		_ = cmd.Wait()
		t.Fatal("Start unexpectedly succeeded after executable replacement")
	} else if !strings.Contains(err.Error(), "replaced") && !strings.Contains(err.Error(), "changed") && !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Start failed for unexpected reason: %v", err)
	}
}

// TestLinuxBoundArgsReserveLeadingExtraFiles ensures isolation mount FDs start
// after caller-reserved leading descriptors (Pi session FD 3).
func TestLinuxBoundArgsReserveLeadingExtraFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux bubblewrap FD layout")
	}

	root := t.TempDir()
	policy, err := bindTaskIsolationPolicy(TaskIsolationPolicy{
		WritableRoots: []string{root},
		SystemRoots:   existingSystemRootsForTest(t),
	})
	if err != nil {
		t.Fatalf("bind policy: %v", err)
	}
	defer policy.Close()

	executablePath := filepath.Join(root, "tool")
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable, err := executableIdentity(executablePath)
	if err != nil {
		t.Fatalf("executable identity: %v", err)
	}
	defer executable.Close()
	cwd, err := currentWorkingDirectoryIdentity(root)
	if err != nil {
		t.Fatalf("cwd identity: %v", err)
	}
	defer cwd.Close()

	args, extraFiles, err := renderLinuxBubblewrapArgsBound(policy, executable, cwd, []string{"arg"}, 1)
	if err != nil {
		t.Fatalf("render bound args: %v", err)
	}
	if len(extraFiles) == 0 {
		t.Fatal("expected isolation extra files")
	}
	// With one leading ExtraFile, first isolation mount is child FD 4.
	if !isolationContainsAdjacent(args, "--bind-fd", "4") && !isolationContainsAdjacent(args, "--ro-bind-fd", "4") {
		t.Fatalf("expected isolation mounts to start at FD 4 after leading ExtraFile, args=%#v", args)
	}
	if isolationContainsAdjacent(args, "--bind-fd", "3") || isolationContainsAdjacent(args, "--ro-bind-fd", "3") {
		t.Fatalf("isolation mounts collided with leading FD 3: %#v", args)
	}
	if len(extraFiles) < 2 || extraFiles[len(extraFiles)-2] != cwd.File || extraFiles[len(extraFiles)-1] != executable.File {
		t.Fatalf("cwd/executable descriptors are not final inherited files: %#v", extraFiles)
	}
	wantCwdFD := fmt.Sprintf("%d", 3+1+len(extraFiles)-2)
	wantExecutableFD := fmt.Sprintf("%d", 3+1+len(extraFiles)-1)
	if !isolationContainsSequence(args, "--bind-fd", wantCwdFD, cwd.Path) || !isolationContainsSequence(args, "--ro-bind-fd", wantExecutableFD, executable.Path) {
		t.Fatalf("cwd/executable FD offsets are incorrect: %#v", args)
	}
	if !isolationContainsAdjacent(args, "--chdir", cwd.Path) || !containsString(args, executable.Path) {
		t.Fatalf("cwd/executable namespace paths are not preserved: %#v", args)
	}
}

func TestBoundIsolationRejectsExactReadOnlyFileReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path replacement")
	}

	root := t.TempDir()
	authority := filepath.Join(t.TempDir(), "task-authority.json")
	if err := os.WriteFile(authority, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := bindTaskIsolationPolicy(TaskIsolationPolicy{
		WritableRoots: []string{root},
		ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: "/run/multica/task-authority.json"}},
	})
	if err != nil {
		t.Fatalf("bind policy: %v", err)
	}
	defer policy.Close()

	old := filepath.Join(filepath.Dir(authority), "old-authority")
	if err := os.Rename(authority, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authority, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recheckBoundIsolation(policy); err == nil {
		t.Fatal("exact read-only file replacement unexpectedly passed recheck")
	}
}

func TestLinuxBoundArgsMountExactReadOnlyFileAfterWritableRoots(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux bubblewrap FD layout")
	}

	root := t.TempDir()
	authority := filepath.Join(t.TempDir(), "task-authority.json")
	executablePath := filepath.Join(root, "tool")
	if err := os.WriteFile(authority, []byte("authority"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executablePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	policy, err := bindTaskIsolationPolicy(TaskIsolationPolicy{
		WritableRoots: []string{root},
		ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: "/run/multica/task-authority.json"}},
	})
	if err != nil {
		t.Fatalf("bind policy: %v", err)
	}
	defer policy.Close()
	executable, err := executableIdentity(executablePath)
	if err != nil {
		t.Fatal(err)
	}
	defer executable.Close()
	cwd, err := currentWorkingDirectoryIdentity(root)
	if err != nil {
		t.Fatal(err)
	}
	defer cwd.Close()

	args, _, err := renderLinuxBubblewrapArgsBound(policy, executable, cwd, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	bindIndex := isolationSequenceIndex(args, "--bind-fd", root)
	readOnlyIndex := isolationSequenceIndex(args, "--ro-bind-fd", "/run/multica/task-authority.json")
	cwdIndex := isolationSequenceIndex(args, "--bind-fd", cwd.Path)
	executableIndex := isolationSequenceIndex(args, "--ro-bind-fd", executable.Path)
	if bindIndex < 0 || cwdIndex < 0 || executableIndex < 0 || readOnlyIndex < 0 || readOnlyIndex <= bindIndex || readOnlyIndex <= cwdIndex || readOnlyIndex <= executableIndex {
		t.Fatalf("exact file was not mounted read-only after every namespace destination: %#v", args)
	}
}

func TestCommandLauncherRejectsReadOnlyFileTargetShadowedByCWDOrExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX task namespace paths")
	}

	root := t.TempDir()
	readOnlyRoot := filepath.Join(root, "read-only")
	writableRoot := filepath.Join(root, "writable")
	privateRoot := filepath.Join(root, "daemon-private")
	for _, dir := range []string{readOnlyRoot, writableRoot, privateRoot} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(readOnlyRoot, "tool.sh")
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nexit 0\n"))
	authority := filepath.Join(privateRoot, "task-authority.json")
	if err := os.WriteFile(authority, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		target string
	}{
		{name: "cwd shadows descendant target", target: filepath.Join(readOnlyRoot, "task-authority.json")},
		{name: "target equals cwd", target: readOnlyRoot},
		{name: "target equals executable", target: executable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := newCommandLauncher(&recordingIsolation{})
			cmd, err := launcher.Command(context.Background(), CommandRequest{
				Executable: executable,
				Cwd:        readOnlyRoot,
				Env:        map[string]string{"PATH": "/usr/bin:/bin"},
				Isolation: &TaskIsolationPolicy{
					WritableRoots: []string{writableRoot},
					ReadOnlyRoots: []string{readOnlyRoot},
					ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: tt.target}},
				},
			})
			if cmd != nil {
				_ = cmd.Close()
			}
			if err == nil {
				t.Fatalf("read-only target %q shadowed by cwd or executable unexpectedly accepted", tt.target)
			}
		})
	}
}

func isolationSequenceIndex(values []string, operation, target string) int {
	for i := 0; i+2 < len(values); i++ {
		if values[i] == operation && values[i+2] == target {
			return i
		}
	}
	return -1
}

func isolationContainsSequence(values []string, sequence ...string) bool {
	for i := 0; i+len(sequence) <= len(values); i++ {
		match := true
		for j := range sequence {
			if values[i+j] != sequence[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
