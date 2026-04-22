//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	detachedProcess = 0x00000008
	sigBreak        = syscall.Signal(0x15)
)

// daemonSysProcAttr returns the attributes used when spawning the background
// daemon. DETACHED_PROCESS severs the inherited console so closing the parent
// cmd/PowerShell window no longer propagates CTRL_CLOSE_EVENT to the daemon.
// Because the detached daemon shares no console with the stop caller,
// `daemon stop` talks to it via the HTTP /shutdown endpoint rather than
// GenerateConsoleCtrlEvent. The daemon's stdout/stderr are already
// redirected to the log file before Start() is called, so losing the
// console is safe.
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess,
	}
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
