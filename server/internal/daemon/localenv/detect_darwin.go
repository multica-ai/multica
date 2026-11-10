//go:build darwin

package localenv

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func listListeners(ctx context.Context) ([]Listener, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// +c 0 keeps full command names; lsof truncates them to nine characters
	// otherwise.
	out, err := lsof(ctx, "-nP", "+c", "0", "-iTCP", "-sTCP:LISTEN", "-Fpcn")
	if err != nil {
		return nil, err
	}
	listeners := parseLsofListeners(out)
	if len(listeners) == 0 {
		return nil, nil
	}
	pids := make([]string, 0, len(listeners))
	seen := make(map[int]bool)
	for _, l := range listeners {
		if !seen[l.PID] {
			seen[l.PID] = true
			pids = append(pids, strconv.Itoa(l.PID))
		}
	}
	out, err = lsof(ctx, "-a", "-d", "cwd", "-Fpn", "-p", strings.Join(pids, ","))
	if err != nil {
		return nil, err
	}
	cwds := parseLsofCwd(out)
	for i := range listeners {
		listeners[i].Cwd = cwds[listeners[i].PID]
	}
	return listeners, nil
}

// lsof exits 1 when it matches nothing, which is not an error here.
func lsof(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "lsof", args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return string(out), nil
	}
	return string(out), err
}
