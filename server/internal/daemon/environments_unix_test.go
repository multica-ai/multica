//go:build !windows

package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
