//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const (
	// Windows process creation flags.
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// setSysProcAttrDetach configures the command to run detached from the parent
// terminal on Windows, using CREATE_NEW_PROCESS_GROUP and DETACHED_PROCESS flags.
func setSysProcAttrDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
		HideWindow:    true,
	}
}

// sendTermSignal forcefully terminates the process on Windows.
// Windows does not support SIGTERM; this is a last-resort fallback after
// the HTTP /shutdown endpoint has been tried.
func sendTermSignal(process *os.Process) error {
	return process.Kill()
}

// tailLogFile implements log tailing natively in Go for Windows,
// since the `tail` command is not available.
func tailLogFile(path string, lines int, follow bool) error {
	lastLines, endOffset, err := readLastNLines(path, lines)
	if err != nil {
		return err
	}
	for _, line := range lastLines {
		fmt.Println(line)
	}

	if !follow {
		return nil
	}

	// Follow mode: poll for new content.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(endOffset, io.SeekStart); err != nil {
		return err
	}

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			// No more data; wait and retry.
			time.Sleep(200 * time.Millisecond)
			reader.Reset(f)
			continue
		}
	}
}

// readLastNLines reads the last N lines from a file and returns them along with
// the byte offset at the end of the file.
func readLastNLines(path string, n int) ([]string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := info.Size()

	if size == 0 {
		return nil, 0, nil
	}

	// Scan backwards from end of file to find N newlines.
	buf := make([]byte, 1)
	newlines := 0
	offset := size - 1

	for offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, 0, err
		}
		if _, err := f.Read(buf); err != nil {
			return nil, 0, err
		}
		if buf[0] == '\n' {
			newlines++
			if newlines > n {
				offset++ // Move past the newline.
				break
			}
		}
		offset--
	}

	// Read from offset to end.
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, 0, err
	}
	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, size, nil
}

// applyPendingUpdate checks for a staged .update binary and applies it.
// On Windows, UpdateViaDownload stages the new binary as <exe>.update instead
// of replacing in-place (running executables cannot be overwritten).
// This function is called at daemon startup to complete the update.
func applyPendingUpdate() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	updatePath := exePath + ".update"

	if _, err := os.Stat(updatePath); os.IsNotExist(err) {
		return // No pending update.
	}

	// The old daemon process has exited, so no file lock.
	oldPath := exePath + ".old"
	os.Remove(oldPath) // Clean up previous .old file.

	if err := os.Rename(exePath, oldPath); err != nil {
		return // Cannot rename current binary; skip update.
	}
	if err := os.Rename(updatePath, exePath); err != nil {
		// Rollback: restore the old binary.
		os.Rename(oldPath, exePath)
		return
	}
	os.Remove(oldPath) // Clean up old version.
}
