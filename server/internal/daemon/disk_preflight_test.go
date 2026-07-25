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
	values := []uint64{14, 10, 9, 11, 15}
	p := &diskPreflight{
		path:        "/workspaces",
		warningGiB:  15,
		criticalGiB: 10,
		recoveryGiB: 15,
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
		freeGiB: func(string) (uint64, error) {
			value := values[0]
			values = values[1:]
			return value, nil
		},
	}

	for i, want := range []bool{false, false, false, false, true} {
		if got := p.allowTaskClaim(); got != want {
			t.Fatalf("step %d allow = %v, want %v", i, got, want)
		}
	}
	if got := logs.String(); strings.Count(got, "disk preflight") != 3 {
		t.Fatalf("transition logs = %d, want 3; logs:\n%s", strings.Count(got, "disk preflight"), got)
	}
}

func TestRunBatchPollerDiskAdmissionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		freeGiB    uint64
		wantClaims bool
	}{
		{name: "critical boundary", freeGiB: 10, wantClaims: false},
		{name: "below admission floor", freeGiB: 14, wantClaims: false},
		{name: "admission floor boundary", freeGiB: 15, wantClaims: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
				warningGiB:  15,
				criticalGiB: 10,
				recoveryGiB: 15,
				logger:      d.logger,
				freeGiB:     func(string) (uint64, error) { return tc.freeGiB, nil },
			}

			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			var taskWG sync.WaitGroup
			d.runBatchPoller(ctx, ctx, newTaskSlotSemaphore(1), make(chan struct{}, 1), &taskWG)

			gotClaims := claimCalls.Load()
			if tc.wantClaims && gotClaims == 0 {
				t.Fatal("claim calls = 0, want at least 1")
			}
			if !tc.wantClaims && gotClaims != 0 {
				t.Fatalf("claim calls = %d, want 0", gotClaims)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatalf("read workspaces root: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("disk preflight created workspaces root entries: %v", entries)
			}
		})
	}
}

func TestDiskPreflightErrorFailsClosedWithoutSpam(t *testing.T) {
	var logs bytes.Buffer
	p := &diskPreflight{
		path:        "/workspaces",
		warningGiB:  15,
		criticalGiB: 10,
		recoveryGiB: 15,
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
