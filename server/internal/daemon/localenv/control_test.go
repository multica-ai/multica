//go:build !windows

package localenv

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIDFromKeyRejectsAnythingButARecordID(t *testing.T) {
	if id, ok := IDFromKey("config:0123456789abcdef"); !ok || id != "0123456789abcdef" {
		t.Fatalf("IDFromKey(valid) = %q, %v", id, ok)
	}
	for _, key := range []string{"detected:0123456789abcdef", "config:../state", "config:", "config:0123456789ABCDEF"} {
		if _, ok := IDFromKey(key); ok {
			t.Fatalf("IDFromKey(%q) accepted", key)
		}
	}
}

func TestManagerKeepsACrashExcerpt(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "broken", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	env = waitReady(t, m, env.Key)
	if env.State != StateExited || !strings.HasPrefix(env.Excerpt, "boom: cannot reach") {
		t.Fatalf("crashed environment = %+v, want its last output line as the excerpt", env)
	}
}

func TestManagerControlsAnEnvironmentByID(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	env = waitReady(t, m, env.Key)
	id, _ := IDFromKey(env.Key)

	if issueID, workspaceID, err := m.RecordOwner(id); err != nil || issueID != task.IssueID || workspaceID != task.WorkspaceID {
		t.Fatalf("Owner = %q, %q, %v", issueID, workspaceID, err)
	}
	out, err := m.LogTail(id, 10, 4096)
	if err != nil || !strings.Contains(out, "serving web") {
		t.Fatalf("LogTail = %q, %v", out, err)
	}

	restarted, err := m.Restart(id, func(string) (Task, bool) { return task, true })
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if restarted.Key == env.Key {
		t.Fatal("a restart starts a new environment")
	}
	restarted = waitReady(t, m, restarted.Key)
	if restarted.State != StateRunning || restarted.Name != "web" {
		t.Fatalf("restarted environment = %+v", restarted)
	}
	if groupAlive(env.pgid) {
		t.Fatal("the restarted environment's old process group is still alive")
	}

	newID, _ := IDFromKey(restarted.Key)
	stopped, err := m.StopID(newID)
	if err != nil || stopped.State != StateStopped {
		t.Fatalf("StopID = %+v, %v", stopped, err)
	}
	if _, err := m.StopID(newID); err != nil {
		t.Fatalf("stopping an ended environment again = %v, want no error", err)
	}
}

func TestManagerRestartsAndReadsAForgottenEnvironment(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "web", os.Getenv("PATH"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	env = waitReady(t, m, env.Key)
	id, _ := IDFromKey(env.Key)
	if _, err := m.StopID(id); err != nil {
		t.Fatalf("StopID: %v", err)
	}
	later := time.Now().Add(endedRetention + time.Minute)
	m.now = func() time.Time { return later }
	m.Tick()
	for _, e := range m.Snapshot() {
		if e.Key == env.Key {
			t.Fatal("an ended environment past retention is still in the snapshot")
		}
	}

	if out, err := m.LogTail(id, 10, 4096); err != nil || !strings.Contains(out, "serving web") {
		t.Fatalf("LogTail after the record left memory = %q, %v", out, err)
	}
	m.now = time.Now
	restarted, err := m.Restart(id, func(string) (Task, bool) { return task, true })
	if err != nil {
		t.Fatalf("Restart after the record left memory: %v", err)
	}
	if restarted = waitReady(t, m, restarted.Key); restarted.State != StateRunning {
		t.Fatalf("restarted environment = %+v", restarted)
	}
}

func TestManagerRestartRefusesAnotherIssuesDirectory(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "broken", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitReady(t, m, env.Key)
	id, _ := IDFromKey(env.Key)
	other := task
	other.IssueID = "issue-2"
	if _, err := m.Restart(id, func(string) (Task, bool) { return other, true }); err == nil {
		t.Fatal("Restart started an environment under another issue")
	}
}

func TestManagerRestartReportsCleanedUpCode(t *testing.T) {
	m, task := newTestManager(t)
	env, err := m.Start(task, task.RootDir, "broken", "")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitReady(t, m, env.Key)
	id, _ := IDFromKey(env.Key)
	if err := os.RemoveAll(filepath.Join(task.RootDir, ".multica")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restart(id, func(string) (Task, bool) { return task, true }); err != ErrGone {
		t.Fatalf("Restart = %v, want ErrGone", err)
	}
}

func TestManagerUnknownIDs(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.LogTail("0123456789abcdef", 10, 100); err != ErrUnknown {
		t.Fatalf("LogTail(unknown) = %v, want ErrUnknown", err)
	}
	if _, err := m.LogTail("../../etc", 10, 100); err != ErrUnknown {
		t.Fatalf("LogTail(path) = %v, want ErrUnknown", err)
	}
}

func TestLastLinesAndTailBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.log")
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %03d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readTail(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(lastLines(data, 500)); got != "line 098\nline 099\nline 100" {
		t.Fatalf("byte-capped tail = %q, want whole lines only", got)
	}
	data, _ = readTail(path, 1<<20)
	if got := string(lastLines(data, 2)); got != "line 099\nline 100" {
		t.Fatalf("line-capped tail = %q", got)
	}
}

func TestDiscoverDeclaredFindsCheckoutsInTheWorkdir(t *testing.T) {
	workdir := t.TempDir()
	writeConfig(t, filepath.Join(workdir, "app"), `
[environments.web]
label = "Web app"
start = "make web"

[environments.api]
start = "make server"
`)
	writeConfig(t, filepath.Join(workdir, "node_modules", "pkg"), `
[environments.ignored]
start = "true"
`)
	got := DiscoverDeclared(workdir)
	if len(got) != 2 || got[0].Name != "api" || got[1].Name != "web" || got[0].Repo != "app" ||
		got[1].Label != "Web app" || got[0].Start != "make server" || got[0].RepoRoot != filepath.Join(workdir, "app") {
		t.Fatalf("DiscoverDeclared = %+v", got)
	}
	if DiscoverDeclared(filepath.Join(workdir, "missing")) != nil {
		t.Fatal("a missing workdir declares nothing")
	}
}

func TestDetectorStopsOnlyTheListeningProcess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "LOCALENV_HELPER=serve", fmt.Sprintf("MULTICA_PORT=%d", port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	t.Cleanup(func() { cmd.Process.Kill(); <-done })

	listening := true
	d := NewDetector(nil)
	d.list = func(context.Context) ([]Listener, error) {
		if !listening {
			return nil, nil
		}
		select {
		case <-done:
			listening = false
			return nil, nil
		default:
			return []Listener{{PID: cmd.Process.Pid, Port: port}}, nil
		}
	}
	env := Environment{Source: SourceDetected, Name: "helper", Port: port, pid: cmd.Process.Pid}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := d.Stop(ctx, env); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the listening process is still running")
	}

	if err := d.Stop(ctx, Environment{Source: SourceDetected, Port: port, pid: os.Getpid()}); err != ErrUnknown {
		t.Fatalf("Stop(self) = %v, want ErrUnknown", err)
	}
}
