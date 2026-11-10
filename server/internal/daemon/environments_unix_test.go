//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/localenv"
)

func TestEnvironmentHandlersStartListAndStop(t *testing.T) {
	s, _ := newTestEnvironmentService(t)
	d := &Daemon{logger: s.logger, environments: s}
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, "repo", ".multica"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[environments.worker]\nlabel = \"Queue worker\"\nstart = \"exec sleep 60\"\nurl = \"\"\n"
	if err := os.WriteFile(filepath.Join(workDir, "repo", ".multica", "environments.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	d.registerActiveRepoCheckoutTask("mat_env_a", activeRepoCheckoutTask{WorkspaceID: "ws", TaskID: "task-a", IssueID: "issue-a", WorkDir: workDir})
	d.registerActiveRepoCheckoutTask("mat_env_b", activeRepoCheckoutTask{WorkspaceID: "ws", TaskID: "task-b", IssueID: "issue-b", WorkDir: workDir})

	rec := doEnvironmentRequest(d.environmentStartHandler(), "/environments/start", "mat_env_a",
		`{"name":"worker","dir":"`+filepath.Join(workDir, "repo")+`","wait":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body.String())
	}
	var started environmentResponse
	json.NewDecoder(rec.Body).Decode(&started)
	if started.Environment == nil || started.Environment.State != localenv.StateRunning || started.Environment.Label != "Queue worker" {
		t.Fatalf("started = %+v", started.Environment)
	}
	t.Cleanup(func() {
		s.manager.Stop(localenv.Task{TaskID: "task-a", IssueID: "issue-a", WorkspaceID: "ws"}, "worker")
	})

	rec = doEnvironmentRequest(d.environmentListHandler(), "/environments/list", "mat_env_b", `{}`)
	var other environmentResponse
	json.NewDecoder(rec.Body).Decode(&other)
	if len(other.Environments) != 0 {
		t.Fatalf("another issue lists %+v", other.Environments)
	}
	rec = doEnvironmentRequest(d.environmentStopHandler(), "/environments/stop", "mat_env_b", `{"name":"worker"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("another issue stopping the environment = %d", rec.Code)
	}

	rec = doEnvironmentRequest(d.environmentStartHandler(), "/environments/start", "mat_env_a",
		`{"name":"web","dir":"`+filepath.Join(workDir, "repo")+`"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "declared: worker") {
		t.Fatalf("start of an undeclared environment = %d %s", rec.Code, rec.Body.String())
	}
	rec = doEnvironmentRequest(d.environmentStartHandler(), "/environments/start", "mat_env_a", `{"name":"worker"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "run from inside the repository") {
		t.Fatalf("start above the repository = %d %s", rec.Code, rec.Body.String())
	}

	rec = doEnvironmentRequest(d.environmentStopHandler(), "/environments/stop", "mat_env_a", `{"name":"worker"}`)
	var stopped environmentResponse
	json.NewDecoder(rec.Body).Decode(&stopped)
	if rec.Code != http.StatusOK || stopped.Environment == nil || stopped.Environment.State != localenv.StateStopped {
		t.Fatalf("stop = %d %+v", rec.Code, stopped.Environment)
	}
}

func TestEnvironmentReportKeepsACrashedEnvironmentsOutputLocal(t *testing.T) {
	const secret = "fake-password-for-tests"
	s, reporter := newTestEnvironmentService(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".multica"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[environments.api]\nstart = \"echo 'cannot reach postgres://demo:" + secret + "@db.invalid/app'; exit 3\"\nurl = \"\"\n"
	if err := os.WriteFile(filepath.Join(root, ".multica", "environments.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	task := localenv.Task{TaskID: "task-a", IssueID: "issue-a", WorkspaceID: "ws", RootDir: root}
	env, err := s.manager.Start(task, root, "api", os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deadline := time.Now().Add(10 * time.Second)
	for env.State != localenv.StateExited && time.Now().Before(deadline) {
		env = s.manager.WaitReady(ctx, env.Key)
		time.Sleep(20 * time.Millisecond)
	}

	s.report(context.Background(), time.Now())
	calls := reporter.take()
	if len(calls) != 1 || len(calls[0].envs) != 1 {
		t.Fatalf("reports = %+v, want one snapshot with the crashed environment", calls)
	}
	if got := calls[0].envs[0]; got.State != localenv.StateExited || got.Detail != "exit status 3" {
		t.Fatalf("reported %+v, want exited with only its exit status", got)
	}
	if log, _ := os.ReadFile(env.LogPath); !strings.Contains(string(log), secret) {
		t.Fatalf("local log = %q, want the command's output kept there", log)
	}
}
