//go:build linux

package taskresource

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// TestSystemdCgroupOOMIsolation is opt-in because it deliberately trips a
// 48 MiB task budget. It proves the test process (standing in for the daemon)
// survives, a sibling task still runs, the exact OOM counters are captured,
// and the first task's descendant is not orphaned.
func TestSystemdCgroupOOMIsolation(t *testing.T) {
	if os.Getenv("MULTICA_RUN_CGROUP_OOM_STRESS") != "1" {
		t.Skip("set MULTICA_RUN_CGROUP_OOM_STRESS=1 to run the cgroup OOM stress test")
	}
	if os.Getenv("MULTICA_CGROUP_OOM_HELPER") == "1" {
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("MULTICA_CGROUP_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(3)
		}
		blocks := make([][]byte, 0, 16)
		for {
			blocks = append(blocks, make([]byte, 8<<20))
			for i := range blocks[len(blocks)-1] {
				blocks[len(blocks)-1][i] = 1
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	testKey := strconv.FormatInt(time.Now().UnixNano(), 10)
	s := Start(ctx, Options{TaskID: "integration-oom-" + testKey, MemoryMaxBytes: 48 << 20, SwapMaxBytes: 0})
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	if s.IsolationMode() != IsolationSystemd {
		t.Fatalf("systemd isolation unavailable: %s", s.FallbackReason())
	}
	prefix := s.CommandPrefix()
	pidFile := t.TempDir() + "/child.pid"
	args := append(append([]string(nil), prefix[1:]...), os.Args[0], "-test.run=TestSystemdCgroupOOMIsolation")
	cmd := exec.CommandContext(ctx, prefix[0], args...)
	cmd.Env = append(os.Environ(), "MULTICA_CGROUP_OOM_HELPER=1", "MULTICA_CGROUP_CHILD_PID_FILE="+pidFile)
	if err := cmd.Run(); err == nil {
		t.Fatal("memory-over-budget helper unexpectedly succeeded")
	}
	usage := s.Close()
	s = nil
	if !usage.MemoryOOM || usage.OOMKills == 0 {
		t.Fatalf("usage = %#v, want positive memory OOM evidence", usage)
	}

	sibling := Start(ctx, Options{TaskID: "integration-sibling-" + testKey, MemoryMaxBytes: 64 << 20, SwapMaxBytes: 0})
	defer func() {
		if sibling != nil {
			sibling.Close()
		}
	}()
	if sibling.IsolationMode() != IsolationSystemd {
		t.Fatalf("sibling systemd isolation unavailable: %s", sibling.FallbackReason())
	}
	siblingPrefix := sibling.CommandPrefix()
	siblingArgs := append(append([]string(nil), siblingPrefix[1:]...), "/bin/true")
	if err := exec.CommandContext(ctx, siblingPrefix[0], siblingArgs...).Run(); err != nil {
		t.Fatalf("sibling task after OOM: %v", err)
	}
	sibling.Close()
	sibling = nil

	rawPID, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}
	pid, _ := strconv.Atoi(string(rawPID))
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("descendant pid %d still exists after task OOM and slice cleanup", pid)
}

// TestSystemdCgroupCancellationCleanup proves that killing the systemd-run
// client cannot detach the actual task process from daemon cleanup: stopping
// the task slice still removes its descendants.
func TestSystemdCgroupCancellationCleanup(t *testing.T) {
	if os.Getenv("MULTICA_RUN_CGROUP_OOM_STRESS") != "1" {
		t.Skip("set MULTICA_RUN_CGROUP_OOM_STRESS=1 to run the cgroup cleanup test")
	}
	if os.Getenv("MULTICA_CGROUP_CANCEL_HELPER") == "1" {
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("MULTICA_CGROUP_CHILD_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(3)
		}
		_ = child.Wait()
		os.Exit(0)
	}

	supervisorCtx, supervisorCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer supervisorCancel()
	testKey := strconv.FormatInt(time.Now().UnixNano(), 10)
	s := Start(supervisorCtx, Options{TaskID: "integration-cancel-" + testKey, MemoryMaxBytes: 64 << 20, SwapMaxBytes: 0})
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	if s.IsolationMode() != IsolationSystemd {
		t.Fatalf("systemd isolation unavailable: %s", s.FallbackReason())
	}

	runCtx, cancelRun := context.WithCancel(supervisorCtx)
	prefix := s.CommandPrefix()
	pidFile := t.TempDir() + "/child.pid"
	args := append(append([]string(nil), prefix[1:]...), os.Args[0], "-test.run=TestSystemdCgroupCancellationCleanup")
	cmd := exec.CommandContext(runCtx, prefix[0], args...)
	cmd.Env = append(os.Environ(), "MULTICA_CGROUP_CANCEL_HELPER=1", "MULTICA_CGROUP_CHILD_PID_FILE="+pidFile)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start cancellable task: %v", err)
	}

	var rawPID []byte
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rawPID, err = os.ReadFile(pidFile)
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read descendant pid: %v", err)
	}

	cancelRun()
	_ = cmd.Wait()
	s.Close()
	s = nil
	pid, _ := strconv.Atoi(string(rawPID))
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("descendant pid %d still exists after task cancellation and slice cleanup", pid)
}
