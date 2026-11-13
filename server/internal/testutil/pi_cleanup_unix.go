//go:build linux || darwin

package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const piCleanupFixtureMode = "MULTICA_TEST_PI_CLEANUP_MODE"

// RunPiCleanupFixture dispatches before testing parses the runtime's argv.
// Both agent and daemon tests use the same fake, never a user-installed CLI.
func RunPiCleanupFixture() bool {
	mode := os.Getenv(piCleanupFixtureMode)
	if mode == "" {
		return false
	}
	dir := os.Getenv("MULTICA_TEST_PI_CLEANUP_DIR")
	if mode == "writer" {
		signal.Ignore(syscall.SIGTERM)
		f, err := os.OpenFile(filepath.Join(dir, "receipt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			panic(err)
		}
		defer f.Close()
		end := time.Now().Add(15 * time.Second)
		for time.Now().Before(end) {
			_, _ = f.WriteString("x")
			time.Sleep(20 * time.Millisecond)
		}
		return true
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	term := make(chan os.Signal, 1)
	if mode == "graceful" {
		signal.Notify(term, syscall.SIGTERM)
	} else {
		signal.Ignore(syscall.SIGTERM)
	}
	writer := exec.Command(os.Args[0])
	writer.Env = append(os.Environ(), piCleanupFixtureMode+"=writer")
	writer.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	writer.Stdout, writer.Stderr = os.Stdout, os.Stderr
	if err := writer.Start(); err != nil {
		panic(err)
	}
	data, _ := json.Marshal([]int{os.Getpid(), writer.Process.Pid})
	_ = os.WriteFile(filepath.Join(dir, "pids.json"), data, 0o600)
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		if info, err := os.Stat(filepath.Join(dir, "receipt")); err == nil && info.Size() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Println(`{"type":"agent_start"}`)
	fmt.Println(`{"type":"tool_execution_start","toolName":"bash","toolCallId":"fixture"}`)
	if mode == "success" {
		fmt.Println(`{"type":"tool_execution_end","toolCallId":"fixture","result":"server started"}`)
		fmt.Println(`{"type":"agent_end"}`)
		// A successful run may intentionally leave a detached service. Keep
		// inherited output open too: finalization must not wait for the service.
		return true
	}
	if mode == "exit" {
		time.Sleep(200 * time.Millisecond)
		return true
	}
	select {
	case <-term:
		_ = os.WriteFile(filepath.Join(dir, "term"), []byte("received"), 0o600)
		_ = writer.Process.Kill()
		_ = writer.Wait()
	case <-time.After(15 * time.Second):
		_ = writer.Process.Kill()
		_ = writer.Wait()
	}
	return true
}

// PiCleanupExecutable returns an isolated executable plus a scratch directory
// containing pids.json, receipt, and (for graceful shutdown) the TERM receipt.
func PiCleanupExecutable(t *testing.T, mode string) (string, string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "pi-fixture")
	// Match the agent package's executable fixture writer: sibling forks must
	// not inherit an open write descriptor and cause Linux ETXTBSY on exec.
	syscall.ForkLock.RLock()
	err = os.WriteFile(fake, []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", bin)), 0o700)
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		data, _ := os.ReadFile(filepath.Join(dir, "pids.json"))
		var pids []int
		if json.Unmarshal(data, &pids) == nil {
			for _, pid := range pids {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return fake, dir, map[string]string{piCleanupFixtureMode: mode, "MULTICA_TEST_PI_CLEANUP_DIR": dir}
}
