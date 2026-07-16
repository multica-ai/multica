//go:build linux

package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxIsolationExecutesDescriptorBoundScriptAndCwd(t *testing.T) {
	const helper = "/usr/bin/bwrap"
	if _, err := os.Stat(helper); err != nil {
		t.Skip("bubblewrap is unavailable")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "tool.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf '%s\\n' \"$PWD\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := newCommandLauncher(newLinuxIsolation(helper))
	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Cwd:        work,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation: &TaskIsolationPolicy{
			WritableRoots: []string{root},
			SystemRoots:   existingSystemRootsForTest(t),
		},
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("Output: %v: %s", err, exitErr.Stderr)
		}
		t.Fatalf("Output: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != work {
		t.Fatalf("PWD = %q, want %q", got, work)
	}
}

func TestLinuxIsolationPreservesProtectedFileAfterAllMounts(t *testing.T) {
	const helper = "/usr/bin/bwrap"
	if _, err := os.Stat(helper); err != nil {
		t.Skip("bubblewrap is unavailable")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		t.Fatal(err)
	}
	authority := filepath.Join(t.TempDir(), "task-authority.json")
	const authorityContent = `{"managed_by":"multica-daemon-task-authority","task_id":"expected"}`
	if err := os.WriteFile(authority, []byte(authorityContent), 0o600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "tool.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ncat /run/multica/task-authority.json\nif printf tampered > /run/multica/task-authority.json 2>/dev/null; then exit 91; fi\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	launcher := newCommandLauncher(newLinuxIsolation(helper))
	cmd, err := launcher.Command(context.Background(), CommandRequest{
		Executable: executable,
		Cwd:        work,
		Env:        map[string]string{"PATH": "/usr/bin:/bin"},
		Isolation: &TaskIsolationPolicy{
			WritableRoots: []string{root},
			SystemRoots:   existingSystemRootsForTest(t),
			ReadOnlyFiles: []ReadOnlyFileMount{{Source: authority, Target: "/run/multica/task-authority.json"}},
		},
	})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("Output: %v: %s", err, exitErr.Stderr)
		}
		t.Fatalf("Output: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != authorityContent {
		t.Fatalf("protected authority content = %q, want %q", got, authorityContent)
	}
	hostContent, err := os.ReadFile(authority)
	if err != nil {
		t.Fatal(err)
	}
	if string(hostContent) != authorityContent {
		t.Fatalf("host authority content changed to %q", hostContent)
	}
}

func TestOpenPathNoSymlinksFallbackRejectsAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(real, "tool")
	if err := os.WriteFile(file, []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_RDONLY
	fd, err := openPathNoSymlinksFallback(filepath.Join(alias, "tool"), flags)
	if err == nil {
		_ = unix.Close(fd)
		t.Fatal("ancestor symlink unexpectedly accepted")
	}
}

func TestOpenPathNoSymlinksFallbackOpensCanonicalPath(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "tool")
	if err := os.WriteFile(file, []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	fd, err := openPathNoSymlinksFallback(file, unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_RDONLY)
	if err != nil {
		t.Fatalf("open fallback: %v", err)
	}
	defer unix.Close(fd)
	opened := os.NewFile(uintptr(fd), file)
	if opened == nil {
		t.Fatal("adopt fallback descriptor")
	}
	defer opened.Close()
	info, err := opened.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Fatal("opened file reported as directory")
	}
}
