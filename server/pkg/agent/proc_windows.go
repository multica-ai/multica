//go:build windows

package agent

import (
	"fmt"
	"log/slog"
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

// createSuspended starts the child with its initial thread suspended, so it can
// be placed in a Job Object before it executes a single instruction.
const createSuspended = 0x00000004

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

// configureProcessGroup is a no-op on Windows: there is no Setpgid equivalent,
// and job membership cannot be requested before the process exists. Ownership
// is taken by startOwnedProcessTree instead, which a backend opts into by
// calling it in place of cmd.Start.
func configureProcessGroup(cmd *exec.Cmd) {}

// ownedProcessTree is the Windows equivalent of a Unix process group: a Job
// Object the agent belongs to, which every process it goes on to create
// inherits membership of.
type ownedProcessTree struct {
	job windows.Handle
}

// ownedProcessTrees is keyed by the *exec.Cmd of one launch, never by pid. A pid
// is only unique while its process is alive, so a launch that outlived its
// process could otherwise look up — and terminate — an unrelated task that
// happened to inherit the number.
var (
	ownedProcessTreesMu sync.Mutex
	ownedProcessTrees   = map[*exec.Cmd]ownedProcessTree{}
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

// startOwnedProcessTree starts cmd and takes ownership of every process it goes
// on to create. Backends that need whole-tree cleanup call this instead of
// cmd.Start.
//
// The child is created suspended and assigned to a Job Object before it runs.
// That ordering is the whole point: Windows grants membership only to processes
// created *after* the assignment, and never retroactively. Assigning after a
// plain Start leaves a window in which the direct child — usually a cmd.exe
// wrapping a .cmd shim — has already spawned the real agent outside the job.
// That is worse than owning nothing, because the job would then hold only a
// wrapper that exits immediately: accounting would report the tree empty while
// the escaped app-server is still running, and cleanup would be reported as
// confirmed when it is not.
//
// The child is never left suspended. If ownership cannot be taken it is resumed
// anyway and runs unowned, exactly as it did before Job Objects, with the reason
// logged. Only a failure to resume is unrecoverable: that child is killed and
// the error returned, so the caller fails the launch instead of waiting forever
// on a process that will never run.
func startOwnedProcessTree(cmd *exec.Cmd, logger *slog.Logger) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createSuspended

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := ownProcessTree(cmd); err != nil && logger != nil {
		logger.Warn("could not take ownership of the agent process tree; descendant cleanup will be best-effort",
			"error", err, "pid", cmd.Process.Pid)
	}

	if err := resumeProcess(cmd.Process.Pid); err != nil {
		// The child cannot run, so nothing downstream can succeed. Drop
		// ownership first: closing the job terminates the suspended child, and
		// Kill covers the case where ownership was never taken.
		releaseProcessGroup(cmd)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("resume suspended child: %w", err)
	}
	return nil
}

// ownProcessTree creates the Job Object and assigns the (still suspended) child
// to it. The process handle is only needed for the assignment and is closed
// straight after: termination and accounting both go through the job.
func ownProcessTree(cmd *exec.Cmd) error {
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
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("open process: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("assign process to job object: %w", err)
	}

	ownedProcessTreesMu.Lock()
	defer ownedProcessTreesMu.Unlock()
	ownedProcessTrees[cmd] = ownedProcessTree{job: job}
	return nil
}

// resumeProcess releases the initial thread of a CREATE_SUSPENDED child. A
// freshly created process has exactly one thread; the snapshot is filtered by
// owning pid so a concurrent enumeration cannot resume somebody else's.
func resumeProcess(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ThreadEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	resumed := 0
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(pid) {
			continue
		}
		thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if openErr != nil {
			return fmt.Errorf("open thread %d: %w", entry.ThreadID, openErr)
		}
		_, resumeErr := windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		if resumeErr != nil {
			return fmt.Errorf("resume thread %d: %w", entry.ThreadID, resumeErr)
		}
		resumed++
	}
	if resumed == 0 {
		return fmt.Errorf("no threads found for pid %d", pid)
	}
	return nil
}

// releaseProcessGroup drops ownership of a finished tree. Closing the job handle
// is what kills anything still inside it, so this must run only after the caller
// has reaped the process — never on a path that could still be serving a live
// agent.
func releaseProcessGroup(cmd *exec.Cmd) {
	ownedProcessTreesMu.Lock()
	tree, ok := ownedProcessTrees[cmd]
	delete(ownedProcessTrees, cmd)
	ownedProcessTreesMu.Unlock()
	if ok {
		_ = windows.CloseHandle(tree.job)
	}
}

func lookupProcessTree(cmd *exec.Cmd) (ownedProcessTree, bool) {
	ownedProcessTreesMu.Lock()
	defer ownedProcessTreesMu.Unlock()
	tree, ok := ownedProcessTrees[cmd]
	return tree, ok
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
// possible; when ownership was not taken, waitProcessGroupGone returns false and
// the caller's cleanup_confirmed gate suppresses the retry anyway.
func codexInitializeRetrySupported() bool { return true }

// signalProcessGroup terminates the whole owned process tree. Windows has no
// SIGTERM/SIGKILL distinction and no process-group signalling, so the signal is
// ignored and the Job Object is terminated; the caller's grace window still
// applies before this is invoked with SIGKILL. Without an owned tree this falls
// back to killing the direct child alone.
func signalProcessGroup(cmd *exec.Cmd, _ syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if tree, ok := lookupProcessTree(cmd); ok {
		if err := windows.TerminateJobObject(tree.job, 1); err == nil {
			return
		}
	}
	_ = cmd.Process.Kill()
}

// waitProcessGroupGone reports whether every process in the owned tree is gone,
// polling the job's accounting information until the timeout. Without an owned
// tree there is nothing to observe, so it reports false — callers treat that as
// "cleanup could not be confirmed" rather than as success.
func waitProcessGroupGone(cmd *exec.Cmd, timeout time.Duration) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	tree, ok := lookupProcessTree(cmd)
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
