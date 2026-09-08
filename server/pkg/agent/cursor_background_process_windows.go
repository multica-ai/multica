//go:build windows

package agent

import (
	"errors"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type cursorWindowsBackgroundProcess struct{ job windows.Handle }

var cursorIsProcessInJob = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func cursorProcessInJob(process, job windows.Handle) (bool, error) {
	var member int32
	ok, _, err := cursorIsProcessInJob.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&member)))
	if ok == 0 {
		return false, err
	}
	return member != 0, nil
}

func cursorProcessCreated(process windows.Handle) (uint64, error) {
	var created, exited, kernel, user windows.Filetime
	err := windows.GetProcessTimes(process, &created, &exited, &kernel, &user)
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), err
}

func captureCursorBackgroundProcess(cmd *exec.Cmd, pid int) (*cursorBackgroundProcess, error) {
	if cmd == nil || cmd.Process == nil || pid <= 0 || uint64(pid) > uint64(^uint32(0)) || pid == cmd.Process.Pid {
		return nil, errCursorBackgroundProcessInvalid
	}
	root, ok := lookupProcessTree(cmd)
	if !ok {
		return nil, errCursorBackgroundProcessInvalid
	}
	parents, err := cursorWindowsProcessParents()
	if err != nil {
		return nil, err
	}
	type heldProcess struct {
		pid     uint32
		handle  windows.Handle
		created uint64
	}
	var held []heldProcess
	defer func() {
		for _, p := range held {
			_ = windows.CloseHandle(p.handle)
		}
	}()
	open := func(pid uint32) (heldProcess, error) {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			return heldProcess{}, err
		}
		created, err := cursorProcessCreated(h)
		member, memberErr := cursorProcessInJob(h, root.job)
		if err != nil || memberErr != nil || !member {
			_ = windows.CloseHandle(h)
			return heldProcess{}, errCursorBackgroundProcessInvalid
		}
		p := heldProcess{pid, h, created}
		held = append(held, p)
		return p, nil
	}
	target, err := open(uint32(pid))
	if err != nil {
		return nil, err
	}
	// Held handles and creation ordering reject reused parent PIDs. Membership
	// of this launch's Job also prevents a payload from claiming another task.
	child := target
	seen := map[uint32]bool{}
	for child.pid != uint32(cmd.Process.Pid) {
		parentPID := parents[child.pid]
		if parentPID == 0 || seen[parentPID] {
			return nil, errCursorBackgroundProcessInvalid
		}
		seen[parentPID] = true
		parent, err := open(parentPID)
		if err != nil || parent.created > child.created {
			return nil, errCursorBackgroundProcessInvalid
		}
		child = parent
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = windows.CloseHandle(job)
		}
	}()
	if err := windows.AssignProcessToJobObject(job, target.handle); err != nil {
		return nil, err
	}
	// Assign each parent BEFORE enumerating its immediate children. New children
	// now inherit the Job, and pre-existing children are attached breadth first.
	queue := []heldProcess{target}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		parents, err := cursorWindowsProcessParents()
		if err != nil {
			return nil, err
		}
		for childPID, parentPID := range parents {
			if parentPID != parent.pid {
				continue
			}
			child, err := open(childPID)
			if err != nil {
				return nil, err
			}
			if child.created < parent.created {
				return nil, errCursorBackgroundProcessIdentity
			}
			member, err := cursorProcessInJob(child.handle, job)
			if err != nil {
				return nil, err
			}
			if member {
				continue
			}
			if err := windows.AssignProcessToJobObject(job, child.handle); err != nil {
				return nil, err
			}
			queue = append(queue, child)
		}
	}
	// A failed capture must not kill a healthy shell. Enable kill-on-close only
	// after the whole observed subtree has been claimed successfully.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return nil, err
	}
	success = true
	return &cursorBackgroundProcess{platform: &cursorWindowsBackgroundProcess{job: job}}, nil
}

func (p *cursorWindowsBackgroundProcess) alive() (bool, error) {
	active, err := (ownedProcessTree{job: p.job}).activeProcesses()
	return active > 0, err
}
func (p *cursorWindowsBackgroundProcess) terminate() error {
	if err := windows.TerminateJobObject(p.job, 1); err != nil {
		return err
	}
	deadline := time.Now().Add(time.Second)
	for {
		alive, err := p.alive()
		if err != nil || !alive {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Cursor background job did not exit after one second")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func (p *cursorWindowsBackgroundProcess) close() { _ = windows.CloseHandle(p.job) }

func cursorWindowsProcessParents() (map[uint32]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	parents := make(map[uint32]uint32)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	for {
		parents[entry.ProcessID] = entry.ParentProcessID
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return parents, nil
			}
			return nil, err
		}
	}
}
