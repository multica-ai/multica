//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	// detachedProcess severs the inherited console so closing the parent
	// cmd/PowerShell window no longer propagates CTRL_CLOSE_EVENT to the daemon.
	detachedProcess = 0x00000008
	// createBreakawayFromJob lets the daemon escape its parent shell's Job
	// Object. Modern Windows Terminal / cmd.exe / PowerShell host the
	// processes they spawn inside a Job Object that has KILL_ON_JOB_CLOSE
	// set, so when the parent shell exits the kernel kills every process
	// inside that job — including a child we tried to "detach" with
	// detachedProcess alone. detachedProcess only severs the console, not
	// the Job Object inheritance. Adding createBreakawayFromJob makes
	// CreateProcess place the new process outside the parent's Job, so
	// the daemon survives parent-shell exit.
	//
	// If the parent's Job has not granted BREAKAWAY_OK, CreateProcess
	// returns ERROR_ACCESS_DENIED. In that case the caller falls back to
	// detachedProcess alone — the daemon is then at the mercy of the
	// parent's Job lifecycle, which is the pre-fix behaviour.
	createBreakawayFromJob = 0x01000000
	sigBreak               = syscall.Signal(0x15)
)

// daemonSysProcAttr returns the attributes used when spawning the background
// daemon. The default is detachedProcess + createBreakawayFromJob so the
// daemon survives both the parent's console close and the parent's Job
// Object close. The daemon's stdout/stderr are already redirected to the
// log file before Start() is called, so losing the console is safe; and
// `daemon stop` talks to it via HTTP /shutdown rather than
// GenerateConsoleCtrlEvent, so losing the process group is also safe.
//
// The withBreakaway argument exists so the caller can retry with
// withBreakaway=false when CreateProcess fails with ERROR_ACCESS_DENIED
// (the parent Job does not allow breakaway).
func daemonSysProcAttr(withBreakaway bool) *syscall.SysProcAttr {
	flags := uint32(detachedProcess)
	if withBreakaway {
		flags |= createBreakawayFromJob
	}
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: flags,
	}
}

// isAccessDeniedSpawnErr reports whether the error returned from
// (*exec.Cmd).Start() is the Windows ERROR_ACCESS_DENIED, which is what
// CreateProcess returns when CREATE_BREAKAWAY_FROM_JOB is requested but
// the parent's Job Object has not set JOB_OBJECT_LIMIT_BREAKAWAY_OK.
func isAccessDeniedSpawnErr(err error) bool {
	return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

func notifyShutdownContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, sigBreak)
}

func tailLogFile(logPath string, lines int, follow bool) error {
	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()

	// Find start position for the last N lines by reverse-scanning from EOF.
	var tailStart int64
	if size > 0 {
		scanBuf := make([]byte, 8192)
		nlCount := 0
		pos := size
	scan:
		for pos > 0 {
			chunk := int64(len(scanBuf))
			if chunk > pos {
				chunk = pos
			}
			pos -= chunk
			f.ReadAt(scanBuf[:chunk], pos)
			for i := chunk - 1; i >= 0; i-- {
				if scanBuf[i] == '\n' {
					nlCount++
					if nlCount > lines {
						tailStart = pos + i + 1
						break scan
					}
				}
			}
		}
	}

	if _, err := f.Seek(tailStart, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(os.Stdout, f); err != nil {
		return err
	}

	if !follow {
		return nil
	}

	buf := make([]byte, 4096)
	for {
		time.Sleep(500 * time.Millisecond)
		n, readErr := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
	}
}
