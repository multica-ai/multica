//go:build !windows

package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/localenv"
)

type fakeEnvironmentController struct {
	recordingEnvironmentReporter
	mu       sync.Mutex
	queue    []EnvironmentCommand
	results  map[string]EnvironmentCommandResult
	declared map[string][]localenv.Declared
}

func (c *fakeEnvironmentController) ClaimEnvironmentCommands(context.Context, string) ([]EnvironmentCommand, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cmds := c.queue
	c.queue = nil
	return cmds, nil
}

func (c *fakeEnvironmentController) ReportEnvironmentCommandResult(_ context.Context, _, commandID string, result EnvironmentCommandResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results[commandID] = result
	return nil
}

func (c *fakeEnvironmentController) ReportTaskEnvironmentConfigs(_ context.Context, taskID string, envs []localenv.Declared) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.declared[taskID] = envs
	return nil
}

func (c *fakeEnvironmentController) run(t *testing.T, s *environmentService, cmd EnvironmentCommand) EnvironmentCommandResult {
	t.Helper()
	c.mu.Lock()
	c.queue = append(c.queue, cmd)
	c.mu.Unlock()
	s.claimCommands(context.Background(), "runtime-1")
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		result, ok := c.results[cmd.ID]
		c.mu.Unlock()
		if ok {
			return result
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("command %s was never answered", cmd.ID)
	return EnvironmentCommandResult{}
}

func TestEnvironmentCommandsStartReadStopAndRestart(t *testing.T) {
	ctl := &fakeEnvironmentController{results: map[string]EnvironmentCommandResult{}, declared: map[string][]localenv.Declared{}}
	s := newEnvironmentService(Config{WorkspacesRoot: t.TempDir()}, ctl, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	envRoot := filepath.Join(s.workspacesRoot, "acme-ws", "acme-1-task")
	repo := filepath.Join(envRoot, "workdir", "app")
	if err := os.MkdirAll(filepath.Join(repo, ".multica"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "[environments.worker]\nlabel = \"Queue worker\"\nstart = \"echo worker up; exec sleep 60\"\nurl = \"\"\n"
	if err := os.WriteFile(filepath.Join(repo, ".multica", "environments.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := execenv.WriteGCMeta(envRoot, execenv.GCMeta{Kind: execenv.GCKindIssue, IssueID: "issue-a", TaskID: "task-a", WorkspaceID: "ws"}, s.logger); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, env := range s.manager.Snapshot() {
			if id, ok := localenv.IDFromKey(env.Key); ok {
				s.manager.StopID(id)
			}
		}
	})

	s.reportDeclared(context.Background(), "task-a", envRoot)
	if got := ctl.declared["task-a"]; len(got) != 1 || got[0].Repo != "app" || got[0].Name != "worker" || got[0].Label != "Queue worker" {
		t.Fatalf("declared = %+v", got)
	}

	started := ctl.run(t, s, EnvironmentCommand{ID: "c1", Action: "start", WorkspaceID: "ws", IssueID: "issue-a", TaskID: "task-a", Repo: "app", Name: "worker"})
	if !started.OK || !strings.HasPrefix(started.EnvironmentKey, "config:") {
		t.Fatalf("start = %+v", started)
	}
	key := started.EnvironmentKey

	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if env := s.manager.WaitReady(waitCtx, key); env.State != localenv.StateRunning {
		t.Fatalf("started environment = %+v", env)
	}

	if other := ctl.run(t, s, EnvironmentCommand{ID: "c2", Action: "logs", WorkspaceID: "ws", IssueID: "issue-b", LocalKey: key}); other.OK {
		t.Fatal("another issue read the environment's log")
	}
	var logs EnvironmentCommandResult
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		delete(ctl.results, "c3")
		logs = ctl.run(t, s, EnvironmentCommand{ID: "c3", Action: "logs", WorkspaceID: "ws", IssueID: "issue-a", LocalKey: key})
		if logs.OK && logs.Output != nil && strings.Contains(*logs.Output, "worker up") {
			break
		}
	}
	if !logs.OK || logs.Output == nil || !strings.Contains(*logs.Output, "worker up") {
		t.Fatalf("logs = %+v", logs)
	}

	restarted := ctl.run(t, s, EnvironmentCommand{ID: "c4", Action: "restart", WorkspaceID: "ws", IssueID: "issue-a", LocalKey: key})
	if !restarted.OK || restarted.EnvironmentKey == key {
		t.Fatalf("restart = %+v", restarted)
	}
	var old localenv.Environment
	for _, env := range s.manager.Snapshot() {
		if env.Key == key {
			old = env
		}
	}
	if old.State != localenv.StateStopped {
		t.Fatalf("the restarted environment's old run = %+v, want stopped", old)
	}

	newID, _ := localenv.IDFromKey(restarted.EnvironmentKey)
	if stopped := ctl.run(t, s, EnvironmentCommand{ID: "c5", Action: "stop", WorkspaceID: "ws", IssueID: "issue-a", LocalKey: restarted.EnvironmentKey}); !stopped.OK {
		t.Fatalf("stop = %+v", stopped)
	}
	for _, env := range s.manager.Snapshot() {
		if strings.HasSuffix(env.Key, newID) && env.State != localenv.StateStopped {
			t.Fatalf("stopped environment = %+v", env)
		}
	}

	if gone := ctl.run(t, s, EnvironmentCommand{ID: "c6", Action: "start", WorkspaceID: "ws", IssueID: "issue-z", TaskID: "task-z", Repo: "app", Name: "worker"}); gone.OK {
		t.Fatal("started an environment for an issue with no directory on this machine")
	}
	if unknown := ctl.run(t, s, EnvironmentCommand{ID: "c7", Action: "reboot", WorkspaceID: "ws", IssueID: "issue-a"}); unknown.OK {
		t.Fatal("an unknown command succeeded")
	}
}
