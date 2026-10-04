//go:build windows && gitbashintegration

package daemon

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// This is an opt-in test of real Git for Windows, not an agent CLI or account.
// Run with -tags=gitbashintegration and MULTICA_TEST_GIT_BASH=<bash.exe>.
func TestWindowsClaudeGitBashTaskTempLifecycle(t *testing.T) {
	bash := os.Getenv("MULTICA_TEST_GIT_BASH")
	if bash == "" {
		t.Skip("set MULTICA_TEST_GIT_BASH to explicitly select Git for Windows")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	base := t.TempDir()
	hostTemp := filepath.Join(base, "user-temp")
	if err := os.Mkdir(hostTemp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMP", hostTemp)
	t.Setenv("TEMP", hostTemp)
	t.Setenv("CLAUDE_CODE_TMPDIR", "")

	newTask := func() (string, *os.File) {
		t.Helper()
		dir, err := os.MkdirTemp(base, execenv.TaskTempDirPrefix)
		if err != nil {
			t.Fatal(err)
		}
		lock, err := execenv.LockTaskTempDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { execenv.ReleaseTaskTempLock(lock) })
		return dir, lock
	}

	start := func(dir string, orphan bool) (map[string]string, func()) {
		t.Helper()
		script := `printf 'PID='; /usr/bin/cat /proc/$$/winpid; printf '\n'
printf 'TMPDIR=%s\n' "$(/usr/bin/cygpath -w "$TMPDIR")"
printf 'TARGET=%s\n' "$(/usr/bin/cygpath -w /tmp)"
if file=$(/usr/bin/mktemp /tmp/multica-regression-XXXXXXXX); then
  /usr/bin/rm -- "$file"
  printf 'TMP_OK=yes\n'
else
  printf 'TMP_OK=no\n'
fi
printf 'READY\n'
IFS= read -r release
`
		if orphan {
			// The launcher's inherited stdin stays open in the Bash child.
			script = "exec 3<&0\n/usr/bin/bash --noprofile --norc -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' <&3 &\n"
		}
		input, feed, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		output, sink, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr, err := os.CreateTemp(base, "bash-stderr-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { feed.Close(); output.Close(); stderr.Close() })
		cmd := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c", script)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = input, sink, stderr
		cmd.Env = os.Environ()
		for key, value := range taskMulticaEnvironment(Task{}, "claude", "", "", "", "", "", 0, 0, dir) {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		input.Close()
		sink.Close()
		go func() { <-ctx.Done(); feed.Close(); output.Close() }()
		snapshot := map[string]string{}
		scanner := bufio.NewScanner(output)
		ready := false
		for scanner.Scan() {
			line := scanner.Text()
			if line == "READY" {
				ready = true
				break
			}
			if key, value, ok := strings.Cut(line, "="); ok {
				snapshot[key] = value
			}
		}
		if !ready {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatalf("Git Bash did not become ready: %v, snapshot=%v", scanner.Err(), snapshot)
		}
		pid, err := strconv.Atoi(snapshot["PID"])
		if err != nil {
			t.Fatal(err)
		}
		child, err := os.FindProcess(pid)
		if err != nil {
			t.Fatal(err)
		}
		if orphan {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("Bash launcher did not exit successfully: %v", err)
			}
		}
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			io.WriteString(feed, "release\n")
			feed.Close()
			if orphan {
				child.Wait()
			} else {
				cmd.Wait()
				child.Release()
			}
		}
		t.Cleanup(stop)
		if !strings.EqualFold(snapshot["TMPDIR"], dir) {
			t.Fatalf("Bash TMPDIR=%q, want this task's %q", snapshot["TMPDIR"], dir)
		}
		return snapshot, stop
	}

	first, firstLock := newTask()
	initial, stopFirst := start(first, false)
	if !strings.EqualFold(initial["TARGET"], hostTemp) && !strings.EqualFold(initial["TARGET"], first) {
		t.Skipf("another MSYS process already owns /tmp at %q", initial["TARGET"])
	}
	second, secondLock := newTask()
	overlap, stopOrphan := start(second, true)
	stopFirst()
	execenv.ReleaseTaskTempLock(firstLock)
	if err := execenv.RemoveTaskTempDir(first); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatalf("completed task temp was not removed: %v", err)
	}
	if removed, _ := execenv.PruneTaskTempDirs(base, 0, time.Now(), nil); removed != 0 {
		t.Fatalf("GC removed %d dirs while the second task held its lock", removed)
	}
	third, thirdLock := newTask()
	afterDelete, stopThird := start(third, false)
	for name, snapshot := range map[string]map[string]string{"overlap": overlap, "after deletion with orphan alive": afterDelete} {
		if snapshot["TMP_OK"] != "yes" {
			t.Errorf("%s: /tmp unusable: %v", name, snapshot)
		}
		if !strings.EqualFold(snapshot["TARGET"], hostTemp) {
			t.Errorf("%s: /tmp target %q, want stable host temp %q", name, snapshot["TARGET"], hostTemp)
		}
	}
	stopThird()
	stopOrphan()
	for dir, lock := range map[string]*os.File{second: secondLock, third: thirdLock} {
		execenv.ReleaseTaskTempLock(lock)
		if err := execenv.RemoveTaskTempDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	fourth, _ := newTask()
	sequential, stopFourth := start(fourth, false)
	if sequential["TMP_OK"] != "yes" || !strings.EqualFold(sequential["TARGET"], hostTemp) {
		t.Fatalf("sequential task after cleanup: %v", sequential)
	}
	stopFourth()
}
