//go:build linux

package processtree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPID1OrphanReaping is an opt-in disposable-container probe. See README.md
// for the supported --init run and the intentionally failing PID 1 control.
func TestPID1OrphanReaping(t *testing.T) {
	if os.Getenv("MULTICA_PID1_PROBE") != "1" {
		t.Skip("run the compiled test binary in an isolated container")
	}
	t.Logf("runner PID=%d PPID=%d", os.Getpid(), os.Getppid())
	for _, code := range []int{0, 7} {
		cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf("exit %d", code))
		err := Run(context.Background(), cmd, time.Second)
		if code == 0 && err != nil {
			t.Fatalf("direct child exit 0: %v", err)
		}
		var exitErr *exec.ExitError
		if code != 0 && (!errors.As(err, &exitErr) || exitErr.ExitCode() != code) {
			t.Fatalf("direct child exit %d: %v", code, err)
		}
	}
	t.Log("direct-child controls: exit 0 and exit 7 preserved")
	var descendants []int
	for cycle := 1; cycle <= 3; cycle++ {
		cmd := exec.Command("/bin/sh", "-c", `sleep 60 </dev/null >/dev/null 2>&1 & printf '%s\n' "$!"`)
		started := time.Now()
		out, err := CombinedOutput(context.Background(), cmd, time.Second)
		elapsed := time.Since(started)
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(out)))
		if parseErr != nil {
			t.Fatalf("descendant PID: output=%q run error=%v parse error=%v", out, err, parseErr)
		}
		descendants = append(descendants, pid)
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
			t.Fatalf("leader did not exit successfully: %v", cmd.ProcessState)
		}
		t.Logf("cycle=%d leader=%d exit=0 descendant=%d elapsed=%s error=%v", cycle, cmd.Process.Pid, pid, elapsed.Round(time.Millisecond), err)
		if err != nil {
			t.Errorf("successful leader with terminated descendant should complete cleanup: %v", err)
		}
		zombies := 0
		for _, descendant := range descendants {
			data, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", descendant))
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			if statErr != nil {
				t.Fatal(statErr)
			}
			fields := strings.Fields(string(data)[strings.LastIndexByte(string(data), ')')+1:])
			if len(fields) < 3 {
				t.Fatalf("invalid proc stat: %q", data)
			}
			t.Logf("remaining PID=%d state=%s PPID=%s PGID=%s", descendant, fields[0], fields[1], fields[2])
			if fields[0] == "Z" && fields[1] == strconv.Itoa(os.Getpid()) {
				zombies++
			}
			t.Errorf("descendant %d still present after cleanup", descendant)
		}
		t.Logf("cycle=%d unreaped adopted zombies from this test=%d", cycle, zombies)
	}
}
