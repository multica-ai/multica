//go:build linux || darwin

package execenv

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	runtimeConfigUmaskHelperEnv     = "MULTICA_RUNTIME_CONFIG_UMASK_HELPER"
	runtimeConfigUmaskHelperPathEnv = "MULTICA_RUNTIME_CONFIG_UMASK_HELPER_PATH"
)

func TestWriteRuntimeConfigFileRejectsReadOnlyTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := []byte("user-owned read-only instructions\n")
	if err := os.WriteFile(path, original, 0o444); err != nil {
		t.Fatalf("seed read-only target: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = probe.Close()
		t.Skip("current user can write a 0444 file; permission gate cannot be asserted")
	}
	if err := writeRuntimeConfigFile(path, "runtime brief"); err == nil {
		t.Fatal("write unexpectedly replaced a read-only target")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target after rejection: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("read-only target changed\n got: %q\nwant: %q", got, original)
	}
}

func TestCleanupRuntimeConfigRejectsReadOnlyTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("user instructions\n"), 0o644); err != nil {
		t.Fatalf("seed cleanup target: %v", err)
	}
	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("inject runtime config: %v", err)
	}
	injected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read injected target: %v", err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("make injected target read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	probe, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = probe.Close()
		t.Skip("current user can write a 0444 file; permission gate cannot be asserted")
	}
	if err := CleanupRuntimeConfig(dir, "codex"); err == nil {
		t.Fatal("cleanup unexpectedly replaced a read-only target")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target after rejected cleanup: %v", err)
	}
	if !bytes.Equal(got, injected) {
		t.Fatalf("read-only target changed during rejected cleanup\n got: %q\nwant: %q", got, injected)
	}
}

func TestWriteRuntimeConfigFileRejectsMultipleHardLinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	alias := filepath.Join(dir, "shared-instructions.md")
	original := []byte("hard-linked user instructions\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed hard-linked target: %v", err)
	}
	if err := os.Link(path, alias); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}

	if err := writeRuntimeConfigFile(path, "runtime brief"); err == nil {
		t.Fatal("write unexpectedly replaced one entry of a multiple-hard-link file")
	}
	for _, candidate := range []string{path, alias} {
		got, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatalf("read %s after rejection: %v", candidate, err)
		}
		if !bytes.Equal(got, original) {
			t.Fatalf("hard-linked target %s changed\n got: %q\nwant: %q", candidate, got, original)
		}
	}
}

func TestWriteRuntimeConfigFilePreservesUnixMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := []byte("user instructions\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatalf("seed metadata target: %v", err)
	}

	attrName := "user.multica.runtime-config-test"
	if runtime.GOOS == "darwin" {
		attrName = "com.multica.runtime-config-test"
	}
	attrValue := []byte("preserve-me")
	if err := unix.Setxattr(path, attrName, attrValue, 0); err != nil {
		t.Skipf("extended attributes unavailable: %v", err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat metadata target: %v", err)
	}
	beforeStat := beforeInfo.Sys().(*syscall.Stat_t)

	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("inject runtime config: %v", err)
	}
	if err := CleanupRuntimeConfig(dir, "codex"); err != nil {
		t.Fatalf("cleanup runtime config: %v", err)
	}

	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after round trip: %v", err)
	}
	afterStat := afterInfo.Sys().(*syscall.Stat_t)
	if beforeStat.Uid != afterStat.Uid || beforeStat.Gid != afterStat.Gid {
		t.Fatalf("owner/group changed from %d:%d to %d:%d", beforeStat.Uid, beforeStat.Gid, afterStat.Uid, afterStat.Gid)
	}
	if got, want := afterInfo.Mode().Perm(), beforeInfo.Mode().Perm(); got != want {
		t.Fatalf("mode after round trip = %04o, want %04o", got, want)
	}
	gotAttr := make([]byte, len(attrValue))
	n, err := unix.Getxattr(path, attrName, gotAttr)
	if err != nil {
		t.Fatalf("read preserved extended attribute: %v", err)
	}
	if !bytes.Equal(gotAttr[:n], attrValue) {
		t.Fatalf("extended attribute after round trip = %q, want %q", gotAttr[:n], attrValue)
	}
}

func TestWriteMissingRuntimeConfigUsesPrivatePermissions(t *testing.T) {
	if os.Getenv(runtimeConfigUmaskHelperEnv) == "1" {
		oldUmask := syscall.Umask(0o077)
		defer syscall.Umask(oldUmask)
		if err := writeRuntimeConfigFile(os.Getenv(runtimeConfigUmaskHelperPathEnv), "runtime brief"); err != nil {
			t.Fatalf("write missing runtime config: %v", err)
		}
		return
	}

	path := filepath.Join(t.TempDir(), "AGENTS.md")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWriteMissingRuntimeConfigUsesPrivatePermissions$")
	cmd.Env = append(os.Environ(),
		runtimeConfigUmaskHelperEnv+"=1",
		runtimeConfigUmaskHelperPathEnv+"="+path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restrictive-umask helper failed: %v\n%s", err, output)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat newly created runtime config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("new runtime config mode = %04o, want private 0600", got)
	}
}
