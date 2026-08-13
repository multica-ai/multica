//go:build windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// createNewConsole allocates a fresh console for the child process. Combined
// with HideWindow=true (STARTF_USESHOWWINDOW + SW_HIDE) the console window
// stays off-screen, and — critically — any grandchildren the agent spawns
// (tool subprocesses like bash, cmd, netstat, findstr) inherit this hidden
// console instead of each allocating their own visible one.
//
// Using CREATE_NO_WINDOW here instead would strip the console entirely,
// which forces Windows to allocate a new visible console per grandchild
// when the grandchild is a console-subsystem program that doesn't itself
// pass CREATE_NO_WINDOW — the exact popup storm reported in #1521.
const createNewConsole = 0x00000010

// hideAgentWindow configures cmd to suppress the console window on Windows
// while still giving descendant processes a hidden console to inherit.
// Stdio pipes set via cmd.StdoutPipe/StdinPipe keep working because
// STARTF_USESTDHANDLES takes precedence over the new console's stdio.
func hideAgentWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNewConsole
}

// configureProcessGroup is a no-op on Windows: a process cannot join a Job
// Object before it exists, so ownership is taken in attachProcessGroup once
// Start has returned.
func configureProcessGroup(cmd *exec.Cmd) {}

// ownedProcessTree is the Windows equivalent of a Unix process group: a Job
// Object the agent was assigned to, which every process it goes on to create
// inherits membership of.
//
// The process handle is held open for as long as the entry lives. That is not
// bookkeeping — an open handle is what stops Windows recycling the pid, so the
// pid stays a safe key and a later terminate can never reach an unrelated
// process that inherited the number.
type ownedProcessTree struct {
	job     windows.Handle
	process windows.Handle
}

var (
	ownedProcessTreesMu sync.Mutex
	ownedProcessTrees   = map[int]ownedProcessTree{}
)

// jobObjectBasicAccountingInformation mirrors JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
// x/sys/windows exports the info class but not the struct.
type jobObjectBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// attachProcessGroup takes ownership of a started agent's process tree so that
// a later signalProcessGroup reaches the descendants it spawned, not just the
// direct child. On Windows the direct child is frequently a `cmd.exe` wrapping
// a `.cmd` shim, so killing only the child left the whole real tree — the Node
// wrapper, the native app-server, and any tool subprocess below them — running
// as orphans. See #6883, where Codex command runners from earlier task dates
// were still alive.
//
// This runs after Start because a process can only join a job once it exists.
// The window between the two is the same one processtree accepts, and it is
// small relative to when agents spawn subprocesses of their own.
//
// A failure here is not fatal: cleanup falls back to terminating the direct
// child, which is exactly the previous behaviour. Callers log the error rather
// than failing the launch.
func attachProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create job object: %w", err)
	}
	// KILL_ON_JOB_CLOSE makes the tree die with the last handle, so a daemon
	// crash cannot strand agent descendants. It also means releaseProcessGroup
	// must only run once the tree is finished with.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("set KILL_ON_JOB_CLOSE: %w", err)
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("open process: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(process)
		_ = windows.CloseHandle(job)
		return fmt.Errorf("assign process to job object: %w", err)
	}

	ownedProcessTreesMu.Lock()
	defer ownedProcessTreesMu.Unlock()
	// A live entry for this pid cannot exist: its own open process handle would
	// have prevented the pid being reused. Close any stale one defensively so a
	// missed release cannot leak a handle forever.
	if stale, ok := ownedProcessTrees[cmd.Process.Pid]; ok {
		stale.close()
	}
	ownedProcessTrees[cmd.Process.Pid] = ownedProcessTree{job: job, process: process}
	return nil
}

// releaseProcessGroup drops ownership of a finished tree. Closing the job
// handle is what kills anything still inside it, so this must run only after
// the caller has reaped the process — never on a path that could still be
// serving a live agent.
func releaseProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	ownedProcessTreesMu.Lock()
	tree, ok := ownedProcessTrees[cmd.Process.Pid]
	delete(ownedProcessTrees, cmd.Process.Pid)
	ownedProcessTreesMu.Unlock()
	if ok {
		tree.close()
	}
}

func lookupProcessTree(pid int) (ownedProcessTree, bool) {
	ownedProcessTreesMu.Lock()
	defer ownedProcessTreesMu.Unlock()
	tree, ok := ownedProcessTrees[pid]
	return tree, ok
}

func (t ownedProcessTree) close() {
	// Job first: closing it terminates whatever is left inside. The process
	// handle is released afterwards so the pid stays reserved until then.
	_ = windows.CloseHandle(t.job)
	_ = windows.CloseHandle(t.process)
}

func (t ownedProcessTree) activeProcesses() (uint32, error) {
	var info jobObjectBasicAccountingInformation
	if err := windows.QueryInformationJobObject(
		t.job,
		windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	); err != nil {
		return 0, fmt.Errorf("query job object members: %w", err)
	}
	return info.ActiveProcesses, nil
}

// codexInitializeRetrySupported reports whether descendant termination can be
// positively confirmed. Owning the tree in a Job Object is what makes that
// possible; when attachProcessGroup failed, waitProcessGroupGone returns false
// and the caller's cleanup_confirmed gate suppresses the retry anyway.
func codexInitializeRetrySupported() bool { return true }

// signalProcessGroup terminates the whole owned process tree. Windows has no
// SIGTERM/SIGKILL distinction and no process-group signalling, so the signal is
// ignored and the Job Object is terminated; the caller's grace window still
// applies before this is invoked with SIGKILL. Without an owned tree this falls
// back to killing the direct child alone.
func signalProcessGroup(p *os.Process, _ syscall.Signal) {
	if p == nil {
		return
	}
	if tree, ok := lookupProcessTree(p.Pid); ok {
		if err := windows.TerminateJobObject(tree.job, 1); err == nil {
			return
		}
	}
	_ = p.Kill()
}

// waitProcessGroupGone reports whether every process in the owned tree is gone,
// polling the job's accounting information until the timeout. Without an owned
// tree there is nothing to observe, so it reports false — callers treat that as
// "cleanup could not be confirmed" rather than as a failure.
func waitProcessGroupGone(p *os.Process, timeout time.Duration) bool {
	if p == nil {
		return false
	}
	tree, ok := lookupProcessTree(p.Pid)
	if !ok {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		active, err := tree.activeProcesses()
		if err != nil {
			return false
		}
		if active == 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
