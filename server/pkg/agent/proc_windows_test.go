//go:build windows

package agent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TestHideAgentWindowSetsCreateNewConsole guards against a regression where
// hideAgentWindow reverts to CREATE_NO_WINDOW. CREATE_NO_WINDOW strips the
// console entirely, which forces Windows to allocate a new visible console
// per grandchild that doesn't itself pass CREATE_NO_WINDOW — the popup
// storm reported in #1521.
func TestHideAgentWindowSetsCreateNewConsole(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "hi")
	hideAgentWindow(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should be initialized")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow should be true")
	}
	if cmd.SysProcAttr.CreationFlags&createNewConsole == 0 {
		t.Errorf("CreationFlags should include CREATE_NEW_CONSOLE (0x%x), got 0x%x",
			createNewConsole, cmd.SysProcAttr.CreationFlags)
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow != 0 {
		t.Errorf("CreationFlags must NOT include CREATE_NO_WINDOW (0x%x), got 0x%x — "+
			"see #1521 for why this causes grandchild popups",
			createNoWindow, cmd.SysProcAttr.CreationFlags)
	}
}

// TestHideAgentWindowPreservesExistingSysProcAttr ensures hideAgentWindow
// does not overwrite fields set by callers — a regression caught in PR #1474
// where the whole SysProcAttr struct was replaced. We verify both a
// non-CreationFlags field and a pre-existing CreationFlags bit survive.
//
// CREATE_UNICODE_ENVIRONMENT (0x00000400) is chosen because it is documented
// as compatible with CREATE_NEW_CONSOLE (unlike CREATE_NEW_PROCESS_GROUP,
// which Windows silently ignores when combined with CREATE_NEW_CONSOLE), so
// a surviving bit here is semantically meaningful, not just bitwise intact.
func TestHideAgentWindowPreservesExistingSysProcAttr(t *testing.T) {
	const createUnicodeEnvironment = 0x00000400
	cmd := exec.Command("cmd.exe", "/c", "echo", "hi")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags:    createUnicodeEnvironment,
		NoInheritHandles: true,
	}
	hideAgentWindow(cmd)

	if !cmd.SysProcAttr.NoInheritHandles {
		t.Error("NoInheritHandles set by caller should be preserved")
	}
	if cmd.SysProcAttr.CreationFlags&createUnicodeEnvironment == 0 {
		t.Error("existing CreationFlags bits (CREATE_UNICODE_ENVIRONMENT) should be preserved")
	}
	if cmd.SysProcAttr.CreationFlags&createNewConsole == 0 {
		t.Error("CREATE_NEW_CONSOLE should be OR'd into existing flags")
	}
}

// TestCodexInitializeRetrySupportedWithOwnedProcessTree pins the inverse of the
// invariant this file used to hold. Now that a launched Codex is assigned to a
// Job Object, descendant termination is observable, so the retry is no longer
// categorically suppressed on Windows. The cleanup_confirmed gate at the call
// site still suppresses it whenever ownership could not be taken, so a host
// where the assignment fails behaves exactly as it did before.
func TestCodexInitializeRetrySupportedWithOwnedProcessTree(t *testing.T) {
	if !codexInitializeRetrySupported() {
		t.Fatal("Codex initialize retry should be available once the process tree is owned by a Job Object")
	}
}

// TestStartOwnedProcessTreeCapturesImmediateDescendants is the pre-attach
// window: Windows grants job membership only to processes created after the
// assignment and never retroactively, so a child that spawns its real work
// immediately could escape a job it joined after Start. The helper here spawns
// its grandchild as the very first thing it does, and the assertion is direct —
// the job's own accounting must have seen both processes, not just the wrapper.
//
// That distinction matters more than it looks: a job holding only a wrapper that
// exits reports the tree empty while the escaped process is still running, which
// would make cleanup look confirmed when it is not.
func TestStartOwnedProcessTreeCapturesImmediateDescendants(t *testing.T) {
	exePath, pidPath := buildDescendantSpawner(t)

	cmd := exec.Command(exePath, "spawn")
	cmd.Env = append(os.Environ(), "DESCENDANT_PID_FILE="+pidPath)
	hideAgentWindow(cmd)
	if err := startOwnedProcessTree(cmd, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("startOwnedProcessTree: %v", err)
	}
	t.Cleanup(func() { releaseProcessGroup(cmd) })

	descendantPid := waitForDescendantPid(t, pidPath)
	if !processStillRunning(descendantPid) {
		t.Fatalf("descendant %d was not running; the test cannot prove anything", descendantPid)
	}
	if total := jobTotalProcesses(t, cmd); total < 2 {
		t.Fatalf("job accounted for %d processes, want the child and its descendant; the descendant escaped the job", total)
	}

	signalProcessGroup(cmd, syscall.SIGKILL)
	if !waitProcessGroupGone(cmd, 10*time.Second) {
		t.Fatal("waitProcessGroupGone reported the tree still active after terminating the job")
	}
	_ = cmd.Wait()
	if processStillRunning(descendantPid) {
		t.Fatalf("descendant %d survived termination of its owning process tree", descendantPid)
	}
}

// TestWaitProcessGroupGoneWithoutOwnershipReportsUnconfirmed covers the
// degraded path: a command started with a plain Start owns no job, so there is
// nothing to observe, and callers must read that as "cleanup unconfirmed"
// rather than as success.
func TestWaitProcessGroupGoneWithoutOwnershipReportsUnconfirmed(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	hideAgentWindow(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if waitProcessGroupGone(cmd, 50*time.Millisecond) {
		t.Fatal("waitProcessGroupGone must not confirm cleanup for an unowned process")
	}
}

// TestStartOwnedProcessTreeLeavesNoSuspendedChild guards the CREATE_SUSPENDED
// path: a child that is never resumed would hang the launch forever, so a
// successful return has to mean the process is actually running.
func TestStartOwnedProcessTreeLeavesNoSuspendedChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran.txt")
	cmd := exec.Command("cmd.exe", "/c", "echo ran > "+marker)
	hideAgentWindow(cmd)
	if err := startOwnedProcessTree(cmd, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("startOwnedProcessTree: %v", err)
	}
	t.Cleanup(func() { releaseProcessGroup(cmd) })

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child exited with error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("child never exited; it was most likely left suspended")
	}
	if _, err := os.ReadFile(marker); err != nil {
		t.Fatalf("child produced no output, so it never ran: %v", err)
	}
}

// jobTotalProcesses reports how many processes the command's job has ever
// accounted for, which is what proves membership regardless of whether a
// descendant has already exited.
func jobTotalProcesses(t *testing.T, cmd *exec.Cmd) uint32 {
	t.Helper()
	tree, ok := lookupProcessTree(cmd)
	if !ok {
		t.Fatal("command owns no process tree")
	}
	var info jobObjectBasicAccountingInformation
	if err := windows.QueryInformationJobObject(
		tree.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		t.Fatalf("query job accounting: %v", err)
	}
	return info.TotalProcesses
}

// buildDescendantSpawner compiles a helper that starts a long-lived grandchild
// and records its pid, then blocks. It is the smallest shape that distinguishes
// "killed the child" from "killed the tree".
func buildDescendantSpawner(t *testing.T) (exePath, pidPath string) {
	t.Helper()
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "spawner.go")
	exePath = filepath.Join(tempDir, "spawner.exe")
	pidPath = filepath.Join(tempDir, "descendant.pid")
	const source = `package main
import (
	"fmt"
	"os"
	"os/exec"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "descendant" { time.Sleep(10*time.Minute); return }
	child := exec.Command(os.Args[0], "descendant")
	if err := child.Start(); err != nil { panic(err) }
	if err := os.WriteFile(os.Getenv("DESCENDANT_PID_FILE"), []byte(fmt.Sprint(child.Process.Pid)), 0600); err != nil { panic(err) }
	time.Sleep(10*time.Minute)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", exePath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build descendant spawner: %v: %s", err, output)
	}
	return exePath, pidPath
}

func waitForDescendantPid(t *testing.T, pidPath string) int {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidPath)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && pid > 0 {
				t.Cleanup(func() {
					if process, err := os.FindProcess(pid); err == nil {
						_ = process.Kill()
					}
				})
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("descendant pid never appeared at %s", pidPath)
	return 0
}

// processStillRunning reports whether pid names a live process. An exited
// process can still be openable while a handle to it is held, so the exit code
// is what decides.
func processStillRunning(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}

func TestCodexWindowsDescendantsDieWithTheOwnedProcessTree(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "fake_codex.go")
	exePath := filepath.Join(tempDir, "fake_codex.exe")
	pidPath := filepath.Join(tempDir, "descendant.pid")
	const source = `package main
import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"time"
)
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" { fmt.Println("codex-cli windows-test"); return }
	if len(os.Args) > 1 && os.Args[1] == "descendant" { time.Sleep(30*time.Second); return }
	s := bufio.NewScanner(os.Stdin)
	if !s.Scan() { return }
	fmt.Println("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}")
	if !s.Scan() { return }
	if !s.Scan() { return }
	child := exec.Command(os.Args[0], "descendant")
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil { panic(err) }
	_ = os.WriteFile(os.Getenv("DESCENDANT_PID_FILE"), []byte(fmt.Sprint(child.Process.Pid)), 0600)
	time.Sleep(time.Hour)
}`
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", exePath, sourcePath)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Windows fake app-server: %v: %s", err, output)
	}

	codexGracefulShutdownTimeoutNanos.Store(int64(100 * time.Millisecond))
	codexProcessWaitDelayNanos.Store(int64(200 * time.Millisecond))
	t.Cleanup(func() {
		codexGracefulShutdownTimeoutNanos.Store(0)
		codexProcessWaitDelayNanos.Store(0)
		if raw, err := os.ReadFile(pidPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
				}
			}
		}
	})

	var logs bytes.Buffer
	backend, err := New("codex", Config{
		ExecutablePath: exePath,
		Logger:         slog.New(slog.NewJSONHandler(&logs, nil)),
		Env:            map[string]string{"DESCENDANT_PID_FILE": pidPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Timeout:          5 * time.Second,
		HandshakeTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	result := <-session.Result
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cleanup exceeded bound: %s", elapsed)
	}
	if result.Status != "failed" || !strings.Contains(result.Error, "thread/start") {
		t.Fatalf("expected thread/start failure, got %+v", result)
	}
	entries := parseJSONLogEntries(t, logs.String())
	failure := findCodexLifecyclePhase(t, entries, "thread_start_failure")
	if failure["cleanup_confirmed"] != true || failure["reaped"] != true {
		t.Fatalf("Windows tree cleanup should be confirmed once the tree is owned: %v", failure)
	}
	if phaseCount(entries, "thread_start_response") != 0 {
		t.Fatalf("unexpected thread_start_response: %v", entries)
	}

	// The descendant inherited its stdout from the app-server and outlives a
	// plain child kill; it must not outlive the tree.
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("descendant pid was never recorded: %v", err)
	}
	descendantPid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse descendant pid %q: %v", raw, err)
	}
	if processStillRunning(descendantPid) {
		t.Fatalf("descendant %d survived codex cleanup", descendantPid)
	}
}

func phaseCount(entries []map[string]any, phase string) int {
	count := 0
	for _, entry := range entries {
		if entry["phase"] == phase {
			count++
		}
	}
	return count
}
