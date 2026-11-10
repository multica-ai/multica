//go:build darwin || linux

package localenv

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestListListenersFindsAServerAndItsDirectory(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LOCALENV_HELPER=serve", fmt.Sprintf("MULTICA_PORT=%d", port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		listeners, err := listListeners(context.Background())
		if err != nil {
			t.Fatalf("listListeners: %v", err)
		}
		for _, l := range listeners {
			if l.PID == cmd.Process.Pid && l.Port == port {
				if l.Cwd != dir || l.Command == "" {
					t.Fatalf("listener = %+v, want cwd %s and a command name", l, dir)
				}
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server pid %d on port %d was never listed", cmd.Process.Pid, port)
}
