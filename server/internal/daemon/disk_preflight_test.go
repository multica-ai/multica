package daemon

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiskPreflightThresholdsAndHysteresis(t *testing.T) {
	var logs bytes.Buffer
	values := []uint64{19, 14, 16, 20}
	p := &diskPreflight{
		path:        "/workspaces",
		warningGiB:  20,
		criticalGiB: 15,
		recoveryGiB: 20,
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		freeGiB: func(string) (uint64, error) {
			value := values[0]
			values = values[1:]
			return value, nil
		},
	}

	for i, want := range []bool{true, false, false, true} {
		if got := p.allowTaskClaim(); got != want {
			t.Fatalf("step %d allow = %v, want %v", i, got, want)
		}
	}
	if got := logs.String(); strings.Count(got, "disk preflight") != 3 {
		t.Fatalf("transition logs = %d, want 3; logs:\n%s", strings.Count(got, "disk preflight"), got)
	}
}

func TestRunBatchPollerCriticalDiskDoesNotClaimOrCreateWorkdir(t *testing.T) {
	var claimCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/daemon/tasks/claim") {
			claimCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tasks":[]}`))
	}))
	defer srv.Close()

	root := t.TempDir()
	d := New(Config{
		ServerBaseURL:      srv.URL,
		WorkspacesRoot:     root,
		PollInterval:       5 * time.Millisecond,
		MaxConcurrentTasks: 1,
	}, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	d.workspaces["ws-1"] = &workspaceState{workspaceID: "ws-1", runtimeIDs: []string{"rt-1"}}
	d.diskPreflight = &diskPreflight{
		path:        root,
		warningGiB:  20,
		criticalGiB: 15,
		recoveryGiB: 20,
		logger:      d.logger,
		freeGiB:     func(string) (uint64, error) { return 14, nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	var taskWG sync.WaitGroup
	d.runBatchPoller(ctx, ctx, newTaskSlotSemaphore(1), make(chan struct{}, 1), &taskWG)

	if got := claimCalls.Load(); got != 0 {
		t.Fatalf("claim calls = %d, want 0", got)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read workspaces root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("critical preflight created workspaces root entries: %v", entries)
	}
}

func TestDiskPreflightErrorFailsClosedWithoutSpam(t *testing.T) {
	var logs bytes.Buffer
	p := &diskPreflight{
		path:        "/workspaces",
		warningGiB:  20,
		criticalGiB: 15,
		recoveryGiB: 20,
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		freeGiB: func(string) (uint64, error) {
			return 0, errors.New("statfs failed")
		},
	}

	if p.allowTaskClaim() || p.allowTaskClaim() {
		t.Fatal("preflight errors must fail closed")
	}
	if got := strings.Count(logs.String(), "disk preflight failed closed"); got != 1 {
		t.Fatalf("error logs = %d, want 1; logs:\n%s", got, logs.String())
	}
}
