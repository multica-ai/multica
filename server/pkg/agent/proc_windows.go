//go:build windows

package agent

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
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
const (
	createNewConsole = 0x00000010
	createSuspended  = 0x00000004
)

var processGroupJobs sync.Map // map[int]windows.Handle

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

// configureProcessGroup is a no-op on Windows: there is no Setpgid/process-group
// signalling. Callers that need descendant cleanup must start with
// startProcessGroup, which assigns the child to a kill-on-close Job Object.
func configureProcessGroup(cmd *exec.Cmd) {}

func prepareProcessGroup(cmd *exec.Cmd) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("set job object kill-on-close: %w", err)
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createSuspended
	return job, nil
}

func startProcessGroup(cmd *exec.Cmd) error {
	job, err := prepareProcessGroup(cmd)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}

	var assignErr error
	if err := cmd.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
	}); err != nil {
		assignErr = err
	}
	if assignErr != nil {
		_ = windows.CloseHandle(job)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("assign process to job object: %w", assignErr)
	}

	processGroupJobs.Store(cmd.Process.Pid, job)
	if err := resumeProcess(cmd.Process.Pid); err != nil {
		releaseProcessGroup(cmd.Process)
		_ = cmd.Wait()
		return fmt.Errorf("resume suspended process: %w", err)
	}
	return nil
}

func resumeProcess(pid int) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("create thread snapshot: %w", err)
	}
	defer windows.CloseHandle(snapshot)

	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	if err := windows.Thread32First(snapshot, &entry); err != nil {
		return fmt.Errorf("read first thread: %w", err)
	}

	found := false
	for {
		if entry.OwnerProcessID == uint32(pid) {
			found = true
			if err := resumeThread(entry.ThreadID); err != nil {
				return err
			}
		}

		err := windows.Thread32Next(snapshot, &entry)
		if err == nil {
			continue
		}
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		return fmt.Errorf("read next thread: %w", err)
	}
	if !found {
		return fmt.Errorf("no threads found for pid %d", pid)
	}
	return nil
}

func resumeThread(threadID uint32) error {
	thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, threadID)
	if err != nil {
		return fmt.Errorf("open thread %d: %w", threadID, err)
	}
	defer windows.CloseHandle(thread)
	if _, err := windows.ResumeThread(thread); err != nil {
		return fmt.Errorf("resume thread %d: %w", threadID, err)
	}
	return nil
}

func releaseProcessGroup(p *os.Process) {
	if p == nil {
		return
	}
	if job, ok := processGroupJobs.LoadAndDelete(p.Pid); ok {
		_ = windows.CloseHandle(job.(windows.Handle))
	}
}

// signalProcessGroup terminates the process tree on Windows by closing the
// kill-on-close Job Object created by startProcessGroup. Windows has no
// SIGTERM/SIGKILL distinction or process-group signalling, so the signal is
// ignored. If the process was not started through startProcessGroup, fall back
// to killing the direct child.
func signalProcessGroup(p *os.Process, _ syscall.Signal) {
	if p == nil {
		return
	}
	if _, ok := processGroupJobs.Load(p.Pid); ok {
		releaseProcessGroup(p)
		return
	}
	_ = p.Kill()
}
