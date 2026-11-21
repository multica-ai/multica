//go:build linux

package localenv

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// inspectProcesses reads each process's arguments. Linux has no notion of an
// app with a window of its own to ask about, so none is reported as one.
func inspectProcesses(_ context.Context, pids []int) map[int]Process {
	procs := make(map[int]Process, len(pids))
	for _, pid := range pids {
		cmdline, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
		if err != nil {
			continue
		}
		procs[pid] = Process{Args: strings.TrimSpace(strings.ReplaceAll(string(cmdline), "\x00", " "))}
	}
	return procs
}

func focusApp(context.Context, int, string) error { return ErrFocusUnsupported }
