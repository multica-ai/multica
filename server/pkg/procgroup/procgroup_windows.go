//go:build windows

package procgroup

import (
	"fmt"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// platJob holds the Job Object kernel handle.
type platJob struct {
	handle windows.Handle
}

// newPlatJob creates a Job Object with KILL_ON_JOB_CLOSE so the
// kernel reaps every process in the job when the last handle to
// the Job closes (graceful or not).
func newPlatJob() (platJob, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return platJob{}, fmt.Errorf("create job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	_, err = windows.SetInformationJobObject(
		h,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		windows.CloseHandle(h)
		return platJob{}, fmt.Errorf("set kill-on-close: %w", err)
	}

	return platJob{handle: h}, nil
}

// prepareCmd is a no-op on Windows; the Job Object is attached
// after cmd.Start by attach().
func (p *platJob) prepareCmd(_ *exec.Cmd) {}

// attach binds the started process to the Job Object. Once
// attached, every subsequent process the child spawns
// auto-inherits the Job (Windows 8+).
func (p *platJob) attach(proc *os.Process) error {
	if p.handle == 0 {
		return nil
	}
	processHandle, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.PROCESS_SET_QUOTA,
		false,
		uint32(proc.Pid),
	)
	if err != nil {
		return fmt.Errorf("open process %d: %w", proc.Pid, err)
	}
	defer windows.CloseHandle(processHandle)

	if err := windows.AssignProcessToJobObject(p.handle, processHandle); err != nil {
		return fmt.Errorf("assign pid %d to job: %w", proc.Pid, err)
	}
	return nil
}

// close releases the Job Object handle. The kernel sees the last
// handle close and triggers KILL_ON_JOB_CLOSE, terminating every
// process in the job.
func (p *platJob) close() error {
	if p.handle == 0 {
		return nil
	}
	err := windows.CloseHandle(p.handle)
	p.handle = 0
	return err
}
