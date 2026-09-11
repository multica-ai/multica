//go:build !windows

package processtree

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const processTreeFinishTimeout = 5 * time.Second

// startTime is the /proc/<pid>/stat "starttime" observed right after
// Start() returned, tagging which process actually owns this controller's
// pid number. Zero means it could not be read (non-Linux, restricted /proc)
// — stillOurs then falls back to the pre-patch behavior of trusting the pid
// unconditionally, rather than reporting a false negative.
type controller struct {
	startTime uint64
}

func newController(cmd *exec.Cmd) (*controller, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &controller{}, nil
}

// attach tags the just-started process the instant it is available, so the
// later interrupt/stop/finish calls — which may run long after this pid has
// exited and potentially been reused — can tell whether they are still
// signalling the process this controller started.
func (c *controller) attach(cmd *exec.Cmd) error {
	if cmd.Process != nil {
		c.startTime, _ = procStartTime(cmd.Process.Pid)
	}
	return nil
}

// stillOurs reports whether pid still refers to the process tagged with
// startTime in attach. See the equivalent (and the reasoning behind it) in
// server/pkg/agent/pidtag_unix.go — this package cannot import that one
// without an import cycle, so the same small check is duplicated here rather
// than shared.
func (c *controller) stillOurs(pid int) bool {
	if c.startTime == 0 {
		return true
	}
	current, err := procStartTime(pid)
	if err != nil {
		return false
	}
	return current == c.startTime
}

func (c *controller) interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if !c.stillOurs(cmd.Process.Pid) {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

func (c *controller) stop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if !c.stillOurs(cmd.Process.Pid) {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return cmd.Process.Kill()
	}
	return nil
}

func (c *controller) finish(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
		return nil
	}
	if !c.stillOurs(pid) {
		// The pid we started has already exited and either no longer exists
		// or now belongs to something this controller never launched.
		// Nothing left here is ours to reap.
		return nil
	}
	// A normally-exited leader can still leave a descendant holding inherited
	// pipes or repository locks. Kill the remaining group before returning
	// ownership of the repository to another operation.
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.Now().Add(processTreeFinishTimeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return fmt.Errorf("process group %d still active after %s", pid, processTreeFinishTimeout)
}

func (*controller) close() {}

// procStartTime parses field 22 (starttime, in clock ticks since boot) from
// /proc/<pid>/stat. The process name in field 2 is parenthesized and may
// itself contain spaces or parentheses, so field boundaries are found from
// the *last* ')' rather than by a fixed-width split.
func procStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, err
	}
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 || idx+2 > len(data) {
		return 0, os.ErrInvalid
	}
	// Fields from here on: state(3) ppid(4) pgrp(5) session(6) tty_nr(7)
	// tpgid(8) flags(9) minflt(10) cminflt(11) majflt(12) cmajflt(13)
	// utime(14) stime(15) cutime(16) cstime(17) priority(18) nice(19)
	// num_threads(20) itrealvalue(21) starttime(22) ...
	// so starttime is at 0-based index 22-3 = 19 in this slice.
	const starttimeIndex = 19
	fields := strings.Fields(string(data[idx+1:]))
	if starttimeIndex >= len(fields) {
		return 0, os.ErrInvalid
	}
	return strconv.ParseUint(fields[starttimeIndex], 10, 64)
}
