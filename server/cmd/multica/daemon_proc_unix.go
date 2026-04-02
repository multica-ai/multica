//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// setSysProcAttrDetach configures the command to run in a new session,
// detached from the parent terminal.
func setSysProcAttrDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// sendTermSignal sends SIGTERM to the process for graceful shutdown.
func sendTermSignal(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

// tailLogFile uses the Unix `tail` command to display log file contents.
func tailLogFile(path string, lines int, follow bool) error {
	args := []string{"-n", strconv.Itoa(lines)}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, path)

	cmd := exec.Command("tail", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// applyPendingUpdate cleans up stale update artifacts.
// On Unix, UpdateViaDownload does atomic in-place replacement, so no staged
// .update file mechanism is needed. This just cleans up any leftover files.
func applyPendingUpdate() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	os.Remove(exePath + ".update")
	os.Remove(exePath + ".old")
}
