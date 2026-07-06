//go:build windows

package agent

import (
	"errors"
	"fmt"
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

const windowsProcessGroupHostJobHelperEnv = "MULTICA_WINDOWS_PROCESS_GROUP_HOST_JOB_HELPER"

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

func TestPrepareProcessGroupCreatesKillOnCloseSuspendedJob(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "hi")
	hideAgentWindow(cmd)

	job, err := prepareProcessGroup(cmd)
	if err != nil {
		t.Fatalf("prepareProcessGroup failed: %v", err)
	}
	defer windows.CloseHandle(job)

	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr should be initialized")
	}
	if cmd.SysProcAttr.CreationFlags&createSuspended == 0 {
		t.Errorf("CreationFlags should include CREATE_SUSPENDED (0x%x), got 0x%x",
			createSuspended, cmd.SysProcAttr.CreationFlags)
	}

	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	err = windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
	if err != nil {
		t.Fatalf("QueryInformationJobObject failed: %v", err)
	}
	if info.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
		t.Errorf("job LimitFlags should include KILL_ON_JOB_CLOSE (0x%x), got 0x%x",
			windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, info.BasicLimitInformation.LimitFlags)
	}
}

func TestStartProcessGroupKillsGrandchildWhenLeaderExits(t *testing.T) {
	runProcessGroupGrandchildCleanup(t)
}

func TestStartProcessGroupWorksWhenHostAlreadyInJob(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable failed: %v", err)
	}

	cmd := exec.Command(exe, "-test.run=^TestStartProcessGroupHostJobHelper$", "-test.v")
	cmd.Env = append(os.Environ(), windowsProcessGroupHostJobHelperEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("host-job helper failed: %v\n%s", err, output)
	}
}

func TestStartProcessGroupHostJobHelper(t *testing.T) {
	if os.Getenv(windowsProcessGroupHostJobHelperEnv) != "1" {
		t.Skip("helper process only")
	}

	parentJob, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		t.Fatalf("CreateJobObject parent failed: %v", err)
	}
	defer windows.CloseHandle(parentJob)

	if err := windows.AssignProcessToJobObject(parentJob, windows.CurrentProcess()); err != nil {
		t.Fatalf("assign helper process to parent job: %v", err)
	}

	runProcessGroupGrandchildCleanup(t)
}

func runProcessGroupGrandchildCleanup(t *testing.T) {
	t.Helper()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	leaderScript := strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$child = Start-Process -FilePath powershell.exe -WindowStyle Hidden -PassThru -ArgumentList @(",
		"  '-NoProfile',",
		"  '-ExecutionPolicy', 'Bypass',",
		"  '-Command', 'Start-Sleep -Seconds 60'",
		")",
		"$child.Id | Set-Content -Path $env:MULTICA_TEST_GRANDCHILD_PID_FILE -NoNewline -Encoding ascii",
	}, "\n")

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", leaderScript)
	cmd.Env = append(os.Environ(), "MULTICA_TEST_GRANDCHILD_PID_FILE="+pidFile)
	hideAgentWindow(cmd)
	configureProcessGroup(cmd)

	var waited bool
	cleaned := false
	t.Cleanup(func() {
		if cleaned || cmd.Process == nil {
			return
		}
		signalProcessGroup(cmd.Process, syscall.SIGKILL)
		releaseProcessGroup(cmd.Process)
		if !waited {
			_ = cmd.Wait()
		}
	})

	if err := startProcessGroup(cmd); err != nil {
		t.Fatalf("startProcessGroup failed: %v", err)
	}

	grandchildPID := waitForPIDFile(t, pidFile)
	if running, err := windowsProcessRunning(grandchildPID); err != nil {
		t.Fatalf("checking grandchild before release: %v", err)
	} else if !running {
		t.Fatalf("grandchild process %d exited before job release", grandchildPID)
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("leader process failed: %v", err)
	}
	waited = true

	releaseProcessGroup(cmd.Process)
	cleaned = true
	waitForProcessExit(t, grandchildPID)
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse pid file %s: %v", path, err)
			}
			if pid <= 0 {
				t.Fatalf("pid file %s contained invalid pid %d", path, pid)
			}
			return pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pid file %s", path)
	return 0
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		running, err := windowsProcessRunning(pid)
		if err != nil {
			t.Fatalf("checking process %d: %v", pid, err)
		}
		if !running {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %d still running after job release", pid)
}

func windowsProcessRunning(pid int) (bool, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(handle)

	const stillActive = 259
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, fmt.Errorf("get exit code: %w", err)
	}
	return exitCode == stillActive, nil
}
