//go:build linux

package execenv

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const (
	runtimeConfigFaultHelperModeEnv = "MULTICA_RUNTIME_CONFIG_FAULT_HELPER_MODE"
	runtimeConfigFaultHelperPathEnv = "MULTICA_RUNTIME_CONFIG_FAULT_HELPER_PATH"
)

// TestWriteRuntimeConfigFilePartialWriteFailurePreservesOriginal is the
// end-to-end regression for a torn os.WriteFile. RLIMIT_FSIZE makes the staging
// write fail after 64 bytes: the production call must return that error without
// truncating the existing user-owned file. The old direct os.WriteFile
// implementation leaves path as a 64-byte prefix and fails this test.
func TestWriteRuntimeConfigFilePartialWriteFailurePreservesOriginal(t *testing.T) {
	if os.Getenv(runtimeConfigFaultHelperModeEnv) == "write" {
		runRuntimeConfigFaultHelper(t, "write")
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := []byte("# User-owned AGENTS.md\n\nThese bytes must survive a failed Multica runtime-brief write.\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed original: %v", err)
	}

	runRuntimeConfigFaultSubprocess(t, "write", path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target after failed write: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed runtime-config write changed user file\n got: %q\nwant: %q", got, original)
	}
}

func TestCleanupRuntimeConfigPartialWriteFailurePreservesInjectedFile(t *testing.T) {
	if os.Getenv(runtimeConfigFaultHelperModeEnv) == "cleanup" {
		runRuntimeConfigFaultHelper(t, "cleanup")
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	original := []byte("# User-owned AGENTS.md\n\nThese bytes must survive a failed Multica runtime-brief cleanup.\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed original: %v", err)
	}
	if err := writeRuntimeConfigFile(path, "runtime brief"); err != nil {
		t.Fatalf("seed injected file: %v", err)
	}
	injected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read injected file: %v", err)
	}

	runRuntimeConfigFaultSubprocess(t, "cleanup", path)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target after failed cleanup: %v", err)
	}
	if !bytes.Equal(got, injected) {
		t.Fatalf("failed runtime-config cleanup changed injected file\n got: %q\nwant: %q", got, injected)
	}
}

// runRuntimeConfigFaultSubprocess keeps the process-wide RLIMIT_FSIZE away
// from the parent go test process. In particular, cmd intentionally omits
// -test.testlogfile: otherwise Go's test-cache recorder can try to write while
// the 64-byte limit is active and fail the package after every test has passed.
func runRuntimeConfigFaultSubprocess(t *testing.T, mode, path string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(),
		runtimeConfigFaultHelperModeEnv+"="+mode,
		runtimeConfigFaultHelperPathEnv+"="+path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runtime-config fault helper failed: %v\n%s", err, output)
	}
}

func runRuntimeConfigFaultHelper(t *testing.T, mode string) {
	t.Helper()
	path := os.Getenv(runtimeConfigFaultHelperPathEnv)
	if path == "" {
		t.Fatal("runtime-config fault helper path is empty")
	}

	err := withRuntimeConfigFileSizeLimit(t, 64, func() error {
		switch mode {
		case "write":
			return writeRuntimeConfigFile(path, strings.Repeat("load-bearing runtime instruction\n", 1024))
		case "cleanup":
			return CleanupRuntimeConfig(filepath.Dir(path), "codex")
		default:
			return nil
		}
	})
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded under a 64-byte file-size limit", mode)
	}
}

func withRuntimeConfigFileSizeLimit(t *testing.T, limit uint64, fn func() error) error {
	t.Helper()
	var oldLimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &oldLimit); err != nil {
		t.Fatalf("get RLIMIT_FSIZE: %v", err)
	}
	if oldLimit.Max < limit {
		t.Skipf("RLIMIT_FSIZE hard limit %d is below requested limit %d", oldLimit.Max, limit)
	}
	limited := oldLimit
	limited.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatalf("set RLIMIT_FSIZE: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	restored := false
	defer func() {
		if !restored {
			_ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &oldLimit)
			signal.Reset(syscall.SIGXFSZ)
		}
	}()

	err := fn()
	if restoreErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &oldLimit); restoreErr != nil {
		t.Fatalf("restore RLIMIT_FSIZE: %v", restoreErr)
	}
	signal.Reset(syscall.SIGXFSZ)
	restored = true
	return err
}
