package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorktreePreflightParksSiblingsAndResumesSameTasks(t *testing.T) {
	for _, cancelOnServer := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "cancel"}[cancelOnServer], func(t *testing.T) {
			repo := t.TempDir()
			if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %s: %v", out, err)
			}
			if err := os.Mkdir(filepath.Join(repo, "debug"), 0700); err != nil {
				t.Fatal(err)
			}
			big, err := os.Create(filepath.Join(repo, "debug", "big"))
			if err != nil {
				t.Fatal(err)
			}
			if err := big.Truncate((200 << 20) + 1); err != nil {
				t.Fatal(err)
			}
			big.Close()
			var cancelled atomic.Bool
			parked := make(chan string, 2)
			leased := make(chan string, 2)
			leasePaths := map[string]bool{}
			var mu sync.Mutex
			counts := map[string]int{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/wait-local-directory"):
					var req struct {
						Reason string `json:"reason"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
					}
					if !strings.Contains(req.Reason, "debug") || !strings.Contains(req.Reason, "209715201") {
						t.Errorf("missing diagnostic: %s", req.Reason)
					}
					mu.Lock()
					counts[r.URL.Path]++
					mu.Unlock()
					parked <- r.URL.Path
				case strings.HasSuffix(r.URL.Path, "/prepare-lease"):
					mu.Lock()
					first := !leasePaths[r.URL.Path]
					leasePaths[r.URL.Path] = true
					mu.Unlock()
					if first {
						leased <- r.URL.Path
					}
				case strings.HasSuffix(r.URL.Path, "/status"):
					if cancelled.Load() {
						_, _ = w.Write([]byte(`{"status":"cancelled"}`))
					} else {
						_, _ = w.Write([]byte(`{"status":"waiting_local_directory"}`))
					}
				default:
					t.Errorf("unexpected request (must not fail or enqueue): %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			d := &Daemon{cfg: Config{DaemonID: "d"}, client: NewClient(srv.URL), logger: slog.Default(), cancelPollInterval: 10 * time.Millisecond, prepareLeaseRefresh: 5 * time.Millisecond}
			raw, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: "d", ExecutionMode: "worktree"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			finished := make(chan bool, 2)
			for _, id := range []string{"first", "second"} {
				task := Task{ID: id, RuntimeID: "runtime", ProjectResources: []ProjectResourceData{{ID: "r", ResourceType: "local_directory", ResourceRef: raw}}}
				go func() {
					_, aborted := d.acquireLocalDirectoryLockIfNeeded(ctx, task, slog.Default())
					finished <- aborted
				}()
			}
			for range 2 {
				select {
				case <-parked:
				case <-ctx.Done():
					t.Fatal("did not park both tasks")
				}
			}
			for range 2 {
				select {
				case <-leased:
				case <-ctx.Done():
					t.Fatal("wait did not renew both task leases")
				}
			}
			if cancelOnServer {
				cancelled.Store(true)
			} else {
				if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("debug/\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				select {
				case aborted := <-finished:
					if aborted != cancelOnServer {
						t.Errorf("aborted=%v", aborted)
					}
				case <-ctx.Done():
					t.Fatal("did not release tasks")
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(counts) != 2 {
				t.Errorf("wrong task identities: %v", counts)
			}
			for id, count := range counts {
				if count != 1 {
					t.Errorf("duplicate park for %s: %d", id, count)
				}
			}
			if d.resourceWaitTasks.Load() != 0 {
				t.Fatal("wait accounting leaked")
			}
		})
	}
}
