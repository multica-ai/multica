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

	"golang.org/x/sys/windows"
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

// repointStdioToErrLog releases the daemon.log handle that an older self-update
// launcher may have inherited onto this process's stdout/stderr, then makes the
// bounded crash sink the process's standard output/error. Go opens files
// without FILE_SHARE_DELETE, so while that inherited handle stays open the
// rotating writer's rename-on-rotate fails — permanently, until the next clean
// restart. Closing os.Stdout/os.Stderr drops the inherited handle; SetStdHandle
// then rebinds the standard handles so panics and any raw writes still land in
// daemon.err.log. Best-effort: on any failure we leave stdio as inherited.
func repointStdioToErrLog(errLogPath string) {
	f, err := openBoundedErrLog(errLogPath)
	if err != nil {
		return
	}
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()
	h := windows.Handle(f.Fd())
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, h)
	os.Stdout = f
	os.Stderr = f
}

// openLogForTail opens the log for reading with FILE_SHARE_DELETE (which Go's
// os.Open omits). Without delete-sharing, a long-lived reader — `daemon logs
// -f` — would block the rotator's rename and silently freeze rotation on
// Windows.
func openLogForTail(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

func tailLogFile(logPath string, lines int, follow bool) error {
	f, err := openLogForTail(logPath)
	if err != nil {
		return err
	}
	defer func() { f.Close() }()

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

	offset, _ := f.Seek(0, io.SeekCurrent)
	buf := make([]byte, 4096)
	for {
		time.Sleep(500 * time.Millisecond)
		// Detect rotation: the rotator renamed our file away and created a
		// fresh, smaller one at the same path. Our handle still points at the
		// rotated-away file (EOF forever), so reopen by path and start over —
		// mirrors the Unix `tail -F` behaviour and the Desktop tail's
		// size-shrink reset.
		if fi, statErr := os.Stat(logPath); statErr == nil && fi.Size() < offset {
			if nf, reopenErr := openLogForTail(logPath); reopenErr == nil {
				f.Close()
				f = nf
				offset = 0
			}
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			os.Stdout.Write(buf[:n])
			offset += int64(n)
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
	}
}
