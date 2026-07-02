//go:build windows

package agent

import (
	"os/exec"
	"syscall"
	"testing"
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
