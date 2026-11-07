package daemon

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatWorkingDirectoryValidatedWithoutProject(t *testing.T) {
	var failures atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failures.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Daemon{cfg: Config{DaemonID: "machine"}, client: NewClient(srv.URL), logger: slog.Default(), localPathLocks: NewLocalPathLocker()}
	for _, path := range []string{"relative", filepath.Join(t.TempDir(), "missing")} {
		task := Task{ID: "chat-task", ChatSessionID: "chat", WorkingDirectory: &protocol.ChatWorkingDirectory{DaemonID: "machine", Path: path}}
		if release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), task, slog.Default()); !abort || release != nil {
			t.Fatalf("selected directory %q bypassed validation: abort=%v", path, abort)
		}
	}
	if failures.Load() != 2 {
		t.Fatalf("reported %d failures, want 2", failures.Load())
	}
	valid := Task{ID: "valid-chat", ChatSessionID: "chat", WorkingDirectory: &protocol.ChatWorkingDirectory{DaemonID: "machine", Path: t.TempDir()}}
	if release, abort := d.acquireLocalDirectoryLockIfNeeded(context.Background(), valid, slog.Default()); abort || release != nil {
		t.Fatalf("valid chat directory must run without a path lock: abort=%v", abort)
	}
}
