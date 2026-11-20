//go:build linux

package localenv

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func listListeners(ctx context.Context) ([]Listener, error) {
	inodes := make(map[string]int)
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if data, err := os.ReadFile(f); err == nil {
			parseProcNetTCP(string(data), inodes)
		}
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var listeners []Listener
	for _, p := range procs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(p.Name())
		if err != nil {
			continue
		}
		base := filepath.Join("/proc", p.Name())
		fds, err := os.ReadDir(filepath.Join(base, "fd"))
		if err != nil {
			continue
		}
		ports := make(map[int]bool)
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			if port, ok := inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")]; ok {
				ports[port] = true
			}
		}
		if len(ports) == 0 {
			continue
		}
		cwd, _ := os.Readlink(filepath.Join(base, "cwd"))
		comm, _ := os.ReadFile(filepath.Join(base, "comm"))
		for port := range ports {
			listeners = append(listeners, Listener{PID: pid, Port: port, Command: strings.TrimSpace(string(comm)), Cwd: cwd})
		}
	}
	return listeners, nil
}
