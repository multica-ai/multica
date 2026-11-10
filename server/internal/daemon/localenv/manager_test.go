//go:build !windows

package localenv

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestHelperProcess is the environment under test: the test binary re-run as
// a server on MULTICA_PORT, or as a command that fails on start.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("LOCALENV_HELPER") {
	case "serve":
		ln, err := net.Listen("tcp", "127.0.0.1:"+os.Getenv("MULTICA_PORT"))
		if err != nil {
			fmt.Println("listen:", err)
			os.Exit(2)
		}
		fmt.Println("serving", os.Getenv("MULTICA_ENVIRONMENT"))
		http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }))
	case "detach":
		// What `make up` does: start the server in a process group of its
		// own, then stay around for as long as the environment should live.
		server := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		server.Env = append(os.Environ(), "LOCALENV_HELPER=serve")
		server.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := server.Start(); err != nil {
			fmt.Println("detach:", err)
			os.Exit(2)
		}
		os.WriteFile(os.Getenv("LOCALENV_PID_FILE"), []byte(strconv.Itoa(server.Process.Pid)), 0o600)
		server.Wait()
	case "crash":
		fmt.Println("boom: cannot reach postgres://demo:" + crashSecret + "@db.invalid/app")
		os.Exit(3)
	}
}

const crashSecret = "fake-password-for-tests"

func helperCommand(mode string) string {
	return fmt.Sprintf("LOCALENV_HELPER=%s exec '%s' -test.run='^TestHelperProcess$'", mode, os.Args[0])
}

func newTestManager(t *testing.T) (*Manager, Task) {
	t.Helper()
	root := t.TempDir()
	writeConfig(t, root, fmt.Sprintf(`
[environments.web]
label = "Web app"
start = "%s"

[environments.broken]
start = "%s"
`, helperCommand("serve"), helperCommand("crash")))
	m := NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	task := Task{TaskID: "task-1", IssueID: "issue-1", WorkspaceID: "ws-1", RootDir: root}
	t.Cleanup(func() {
		for _, env := range m.Snapshot() {
			if env.State == StateStarting || env.State == StateRunning {
				m.Stop(task, env.Name)
			}
		}
	})
	return m, task
}

func waitReady(t *testing.T, m *Manager, key string) Environment {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return m.WaitReady(ctx, key)
}

func TestManagerStartsSupervisesAndStops(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	env = waitReady(t, m, env.Key)
	if env.State != StateRunning || env.Label != "Web app" || env.URL != fmt.Sprintf("http://localhost:%d", env.Port) {
		t.Fatalf("started environment = %+v", env)
	}
	if env.ExpiresAt == nil || env.ExpiresAt.Sub(env.StartedAt) != DefaultTTL {
		t.Fatalf("ExpiresAt = %v, want start + %s", env.ExpiresAt, DefaultTTL)
	}
	resp, err := http.Get(env.URL)
	if err != nil {
		t.Fatalf("environment is not serving: %v", err)
	}
	resp.Body.Close()

	again, err := m.Start(task, task.RootDir, "web", "")
	if err != nil || again.Key != env.Key {
		t.Fatalf("second Start = %+v, %v; want the running environment back", again, err)
	}

	stopped, err := m.Stop(task, "web")
	if err != nil || stopped.State != StateStopped {
		t.Fatalf("Stop = %+v, %v", stopped, err)
	}
	if groupAlive(env.pgid) {
		t.Fatal("process group still alive after Stop")
	}
	if _, err := m.Stop(task, "web"); err != ErrNotFound {
		t.Fatalf("second Stop = %v, want ErrNotFound", err)
	}
}

func TestManagerReportsACrash(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "broken", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	env = waitReady(t, m, env.Key)
	if env.State != StateExited || env.Detail != "exit status 3" {
		t.Fatalf("crashed environment = %+v, want exited with only its exit status", env)
	}
	if env.ExpiresAt != nil {
		t.Fatalf("an ended environment has no expiry, got %v", env.ExpiresAt)
	}
	log, err := os.ReadFile(env.LogPath)
	if err != nil || !strings.Contains(string(log), crashSecret) {
		t.Fatalf("local log = %q, %v; want the command's output kept there", log, err)
	}
}

func TestManagerWaitsForAHealthyReadyCheck(t *testing.T) {
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	root := t.TempDir()
	writeConfig(t, root, fmt.Sprintf("[environments.web]\nstart = \"exec sleep 60\"\nurl = %q\n", srv.URL))
	m := NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	task := Task{TaskID: "task-1", IssueID: "issue-1", WorkspaceID: "ws-1", RootDir: root}
	env, err := m.Start(task, root, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { m.Stop(task, "web") })

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	got := m.WaitReady(ctx, env.Key)
	cancel()
	if got.State != StateStarting {
		t.Fatalf("state while the ready check answers 503 = %q, want starting", got.State)
	}
	healthy.Store(true)
	if got = waitReady(t, m, env.Key); got.State != StateRunning {
		t.Fatalf("state once the ready check answers 200 = %q, want running", got.State)
	}
}

func TestManagerStopsAnExpiredEnvironment(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitReady(t, m, env.Key)
	m.now = func() time.Time { return time.Now().Add(DefaultTTL + time.Minute) }
	m.Tick()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if got := m.List(task)[0]; got.State == StateStopped {
			if got.Detail != "expired" {
				t.Fatalf("expired environment detail = %q", got.Detail)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("environment past its TTL is still %+v", m.List(task)[0])
}

func TestManagerAdoptsEnvironmentsAfterRestart(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitReady(t, m, env.Key)

	restarted := NewManager(m.dir, m.logger)
	if err := restarted.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	adopted := restarted.List(task)
	if len(adopted) != 1 || adopted[0].Key != env.Key || adopted[0].State != StateRunning {
		t.Fatalf("adopted = %+v, want the running environment", adopted)
	}
	if got := restarted.Owner(env.pgid); got != env.Key {
		t.Fatalf("restarted manager names %q as the owner of the adopted process, want %q", got, env.Key)
	}
	if _, err := restarted.Stop(task, "web"); err != nil {
		t.Fatalf("Stop after restart: %v", err)
	}
	if groupAlive(env.pgid) {
		t.Fatal("adopted environment still alive after Stop")
	}
}

func TestManagerScopesEnvironmentsToTheIssue(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitReady(t, m, env.Key)

	laterTask := task
	laterTask.TaskID = "task-2"
	if got := m.List(laterTask); len(got) != 1 {
		t.Fatalf("a later task on the same issue sees %d environments, want 1", len(got))
	}
	otherIssue := Task{TaskID: "task-3", IssueID: "issue-2", WorkspaceID: "ws-1", RootDir: task.RootDir}
	if got := m.List(otherIssue); len(got) != 0 {
		t.Fatalf("another issue sees %d environments", len(got))
	}
	if _, err := m.Stop(otherIssue, "web"); err != ErrNotFound {
		t.Fatalf("another issue stopped the environment: %v", err)
	}
}

func TestManagerRefusesDirectoriesOutsideTheTask(t *testing.T) {
	m, task := newTestManager(t)
	outside := t.TempDir()
	writeConfig(t, outside, fmt.Sprintf("[environments.web]\nstart = \"%s\"", helperCommand("serve")))
	if _, err := m.Start(task, outside, "web", ""); err == nil {
		t.Fatal("Start accepted a directory outside the task")
	}
	if _, err := m.Start(task, task.RootDir, "missing", ""); err == nil || !strings.Contains(err.Error(), "declared: broken, web") {
		t.Fatalf("Start of an undeclared environment = %v", err)
	}
	if len(m.Snapshot()) != 0 {
		t.Fatal("refused starts left environments behind")
	}
}

func TestManagerAllocatesDistinctPortBlocks(t *testing.T) {
	m, task := newTestManager(t)
	first, err := m.Start(task, task.RootDir, "web", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	other := Task{TaskID: "task-9", IssueID: "issue-9", WorkspaceID: "ws-1", RootDir: task.RootDir}
	second, err := m.Start(other, task.RootDir, "web", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { m.Stop(other, "web") })
	if first.Port == second.Port || first.Port%portBlock != 0 || second.Port%portBlock != 0 {
		t.Fatalf("ports = %d and %d, want distinct block starts", first.Port, second.Port)
	}
	if _, err := os.Stat(filepath.Join(m.dir, strings.TrimPrefix(first.Key, "config:"), stateFile)); err != nil {
		t.Fatalf("state was not persisted: %v", err)
	}
}

func TestManagerOwnsAServerDetachedIntoItsOwnProcessGroup(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(t.TempDir(), "server.pid")
	writeConfig(t, root, fmt.Sprintf(`
[environments.web]
start = "LOCALENV_PID_FILE='%s' %s"
`, pidFile, helperCommand("detach")))
	m := NewManager(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	task := Task{TaskID: "task-1", IssueID: "issue-1", WorkspaceID: "ws-1", RootDir: root}
	env, err := m.Start(task, root, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if ready := waitReady(t, m, env.Key); ready.State != StateRunning {
		t.Fatalf("environment = %+v, want running", ready)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read the detached server's pid: %v", err)
	}
	server, _ := strconv.Atoi(string(data))
	t.Cleanup(func() {
		m.Stop(task, "web")
		syscall.Kill(server, syscall.SIGKILL)
	})

	if processGroup(server) == env.pgid {
		t.Fatal("the server shares the start command's process group; the test proves nothing")
	}
	if got := m.Owner(server); got != env.Key {
		t.Fatalf("Owner(detached server) = %q, want %q", got, env.Key)
	}
	if got := m.Owner(os.Getpid()); got != "" {
		t.Fatalf("Owner(this test) = %q, want none", got)
	}
	if _, err := m.Stop(task, "web"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := m.Owner(server); got != "" {
		t.Fatalf("Owner after stop = %q, want none: a stopped environment owns nothing", got)
	}
}
