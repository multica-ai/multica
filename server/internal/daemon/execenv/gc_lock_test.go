package execenv

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGCLockCoversRemovalAndRecreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace", "task")
	lock, _, err := claimEnvRoot(root, "workspace", "task")
	if err != nil {
		t.Fatal(err)
	}
	releaseLockFile(lock)
	ran, err := WithEnvRootGCLock(root, func(release func()) error {
		release()
		if err := os.RemoveAll(root); err != nil {
			return err
		}
		// The .task_lock inode is gone. The persistent gate must still exclude
		// a startup that would otherwise create and lock a new inode.
		next, _, err := claimEnvRoot(root, "workspace", "task")
		if next != nil {
			releaseLockFile(next)
			t.Error("startup entered during GC removal")
		}
		if err == nil {
			t.Error("startup must fail while GC holds the mutation gate")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("GC lock: ran=%v err=%v", ran, err)
	}
	next, _, err := claimEnvRoot(root, "workspace", "task")
	if err != nil {
		t.Fatalf("startup after GC: %v", err)
	}
	releaseLockFile(next)
}

func TestGCLockAcrossProcesses(t *testing.T) {
	if root := os.Getenv("MULTICA_TEST_GC_LOCK_CHILD"); root != "" {
		lock, _, err := claimEnvRoot(root, "workspace", "task")
		if err != nil {
			t.Fatal(err)
		}
		defer releaseLockFile(lock)
		fmt.Println("locked")
		// Block until the parent kills this process: no deferred unlock can run.
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		return
	}
	root := filepath.Join(t.TempDir(), "workspace", "task")
	cmd := exec.Command(os.Args[0], "-test.run=^TestGCLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "MULTICA_TEST_GC_LOCK_CHILD="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("child readiness: %q %v", line, err)
	}
	ran, err := WithEnvRootGCLock(root, func(func()) error { t.Error("GC entered live child root"); return nil })
	if err != nil || ran {
		t.Fatalf("live child lock: ran=%v err=%v", ran, err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ran, err = WithEnvRootGCLock(root, func(release func()) error { release(); return os.RemoveAll(root) })
	if err != nil || !ran {
		t.Fatalf("crashed child root not reclaimable: ran=%v err=%v", ran, err)
	}
}
