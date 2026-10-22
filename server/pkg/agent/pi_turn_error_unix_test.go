//go:build unix

package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	piEscapedStdoutHelperEnv = "MULTICA_TEST_PI_ESCAPED_STDOUT_HELPER"
	piEscapedStdoutPIDEnv    = "MULTICA_TEST_PI_ESCAPED_STDOUT_PID_FILE"
)

// TestPiEscapedStdoutHolderProcess runs only in the subprocess launched by the
// real regression below. The detached child inherits stdout, moves into its own
// session, and therefore survives process-group cancellation of this helper.
func TestPiEscapedStdoutHolderProcess(t *testing.T) {
	if os.Getenv(piEscapedStdoutHelperEnv) != "1" {
		return
	}

	holder := exec.Command("sleep", "30")
	holder.Stdout = os.Stdout
	holder.Stderr = os.Stderr
	holder.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := holder.Start(); err != nil {
		t.Fatalf("start detached stdout holder: %v", err)
	}
	pidFile := os.Getenv(piEscapedStdoutPIDEnv)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(holder.Process.Pid)), 0o600); err != nil {
		t.Fatalf("write detached stdout holder pid: %v", err)
	}

	for _, event := range []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","stopReason":"error","errorMessage":"OpenAI API error (413): request body too large"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
	} {
		fmt.Println(event)
	}
	time.Sleep(5 * time.Minute)
}

func TestPiExecuteTurnErrorGraceClosesStdoutHeldByEscapedDescendant(t *testing.T) {
	const providerError = "OpenAI API error (413): request body too large"
	const grace = 50 * time.Millisecond

	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test executable: %v", err)
	}
	pidFile := filepath.Join(t.TempDir(), "escaped.pid")
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return
		}
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	})

	// The wrapper consumes Pi's stdin contract and then execs this test binary
	// as the fake runtime. The helper's detached child retains the exact stdout
	// pipe returned by cmd.StdoutPipe.
	fakePath := filepath.Join(t.TempDir(), "pi")
	script := fmt.Sprintf("#!/bin/sh\ncat > /dev/null\nexec %q -test.v -test.run '^TestPiEscapedStdoutHolderProcess$'\n", testBinary)
	writeTestExecutable(t, fakePath, []byte(script))
	backend, err := New("pi", Config{
		ExecutablePath: fakePath,
		Env: map[string]string{
			piEscapedStdoutHelperEnv: "1",
			piEscapedStdoutPIDEnv:    pidFile,
		},
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}
	pi := backend.(*piBackend)
	pi.turnErrorGrace = grace

	started := time.Now()
	session, err := pi.Execute(context.Background(), "prompt-ignored", ExecOptions{
		ResumeSessionID: filepath.Join(t.TempDir(), "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	statusSeen := make(chan bool, 1)
	go func() {
		for msg := range session.Messages {
			if msg.Type == MessageStatus {
				statusSeen <- true
				return
			}
		}
		statusSeen <- false
	}()
	select {
	case seen := <-statusSeen:
		if !seen {
			t.Fatal("fake Pi closed messages before agent_start")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fake Pi never emitted agent_start")
	}
	result := waitPiResult(t, session, 3*time.Second)
	if result.Status != "failed" || result.Error != providerError {
		t.Fatalf("result = %+v, want the provider failure", result)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("result took %s after %s grace; escaped stdout holder blocked finalization", elapsed, grace)
	}
	if _, ok := <-session.Result; ok {
		t.Fatal("result channel produced more than one terminal result")
	}
}
