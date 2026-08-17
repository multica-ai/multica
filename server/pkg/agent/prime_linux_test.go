//go:build linux

package agent

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestPrimeSupervisorGoneRecognizesUnreapedZombie(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()
	pgid, err := primeSupervisorIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	startToken, err := primeProcessStartToken(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if primeSupervisorGone(cmd.Process.Pid, pgid, startToken) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("unreaped killed supervisor was not recognized as terminated")
}
