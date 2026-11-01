//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type shutdownProviderLog struct {
	lockedBuffer
	stopped chan struct{}
	once    sync.Once
}

func (l *shutdownProviderLog) Write(p []byte) (int, error) {
	n, err := l.lockedBuffer.Write(p)
	if strings.Contains(string(p), "opencode finished") {
		l.once.Do(func() { close(l.stopped) })
	}
	return n, err
}

// Run a test-owned provider through runTask, then verify handleTask delivers
// shutdown as retryable recovery. The provider blocks on a FIFO, not a timer.
func TestHandleTask_ShutdownThroughRunTask(t *testing.T) {
	redirectTestHome(t, t.TempDir())
	t.Setenv("MULTICA_OPENCODE_TRANSPORT", "run")
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")
	if err := syscall.Mkfifo(gate, 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
case "$1" in
--version) echo 1.18.31; exit 0;;
models) exit 0;;
run)
 cat >/dev/null
 printf '%s\n' '{"type":"step_start","sessionID":"ses-shutdown","part":{"type":"step-start"}}'
 printf '%s\n' '{"type":"text","sessionID":"ses-shutdown","part":{"type":"text","text":"shutdown-ready"}}'
 exec cat "$SHUTDOWN_GATE";;
esac
exit 1
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	reports := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var body struct {
				Messages []TaskMessageData `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			for _, msg := range body.Messages {
				if strings.Contains(msg.Content, "shutdown-ready") {
					once.Do(func() { close(ready); cancel() })
				}
			}
		case strings.HasSuffix(r.URL.Path, "/fail"), strings.HasSuffix(r.URL.Path, "/complete"), strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			body["endpoint"] = r.URL.Path
			reports <- body
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
	}))
	defer srv.Close()
	logs := &shutdownProviderLog{stopped: make(chan struct{})}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	d := New(Config{ServerBaseURL: srv.URL, WorkspacesRoot: t.TempDir(), AgentTimeout: 20 * time.Second, Agents: map[string]AgentEntry{"opencode": {Path: bin}}}, logger)
	d.executionEnvironmentCommand = nil
	d.cancelPollInterval = time.Hour
	d.runtimeIndex["rt-shutdown"] = Runtime{ID: "rt-shutdown", Provider: "opencode"}
	d.setAgentVersion("opencode", "1.18.31")
	task := Task{ID: "task-shutdown", WorkspaceID: "ws-shutdown", RuntimeID: "rt-shutdown", IssueID: "issue-shutdown", AgentID: "agent-shutdown", AuthToken: "mat_test_shutdown", Agent: &AgentData{ID: "agent-shutdown", Name: "shutdown fixture", CustomEnv: map[string]string{"SHUTDOWN_GATE": gate}}}
	done := make(chan struct{})
	go func() { defer close(done); d.handleTask(ctx, task, 0) }()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		cancel()
		<-done
		t.Fatalf("fake provider never reported readiness; logs: %s", logs.String())
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish handleTask")
	}
	select {
	case report := <-reports:
		if !strings.HasSuffix(report["endpoint"].(string), "/fail") || report["failure_reason"] != "runtime_recovery" {
			t.Fatalf("shutdown callback: %v", report)
		}
		if report["error"] != "daemon shut down while task was in flight" {
			t.Errorf("shutdown diagnostic: %v", report)
		}
		workDir, ok := report["work_dir"].(string)
		if !ok || workDir == "" {
			t.Fatalf("workdir lost: %v", report)
		}
		if _, err := os.Stat(workDir); err != nil {
			t.Errorf("delivered workdir: %v", err)
		}
	default:
		t.Fatal("missing shutdown terminal callback")
	}
	select {
	case report := <-reports:
		t.Errorf("duplicate terminal callback: %v", report)
	default:
	}
	select {
	case <-logs.stopped:
	case <-time.After(5 * time.Second):
		t.Fatalf("provider process cleanup did not finish: %s", logs.String())
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	if len(d.activeEnvRoots) != 0 {
		t.Errorf("active environment guard leaked: %v", d.activeEnvRoots)
	}
	if stats, err := d.terminalReports.stats(); err != nil || stats.PendingCount != 0 {
		t.Errorf("acknowledged callback still queued: %+v", stats)
	}
}
