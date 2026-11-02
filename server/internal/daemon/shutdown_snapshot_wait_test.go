package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleTask_ShutdownSnapshotWait(t *testing.T) {
	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.cfg.DaemonID = "snapshot-daemon"
	d.localPathLocks = NewLocalPathLocker()
	d.cancelPollInterval = time.Hour
	dir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	release, err := d.localPathLocks.Acquire(context.Background(), realDir, "holder", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	parked := make(chan struct{}, 1)
	failures := make(chan map[string]string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/wait-local-directory"):
			parked <- struct{}{}
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"status":"running"}`))
		case strings.HasSuffix(r.URL.Path, "/fail"):
			var payload map[string]string
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			failures <- payload
		}
	}))
	defer srv.Close()
	d.client = NewClient(srv.URL)
	d.runner = taskRunnerFunc(d.runTask)
	task := leaderReuseTestTask("snapshot-waiter")
	task.IsLeaderTask = false
	ref, err := json.Marshal(localDirectoryRef{LocalPath: dir, DaemonID: d.cfg.DaemonID, ExecutionMode: "worktree"})
	if err != nil {
		t.Fatal(err)
	}
	task.ProjectResources = []ProjectResourceData{{ID: "resource", ResourceType: localDirectoryResourceType, ResourceRef: ref}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); d.handleTask(ctx, task, 0) }()
	select {
	case <-parked:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("runTask never entered the snapshot mutex wait")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not interrupt the snapshot mutex wait")
	}
	select {
	case payload := <-failures:
		if payload["failure_reason"] != "runtime_recovery" || !strings.Contains(payload["error"], "wait for a consistent snapshot") {
			t.Fatalf("snapshot shutdown callback = %v", payload)
		}
	default:
		t.Fatal("missing snapshot shutdown callback")
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatalf("agent launched during snapshot wait: %v", err)
	}
	if count := d.resourceWaitTasks.Load(); count != 0 {
		t.Fatalf("resource wait count = %d, want 0", count)
	}
}
