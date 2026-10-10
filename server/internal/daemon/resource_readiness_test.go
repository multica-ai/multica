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
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestResourceReadinessMeasuresBeforeAnyTaskAndRecovers(t *testing.T) {
	repo := t.TempDir()
	if out, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %s %v", out, err)
	}
	file, err := os.Create(filepath.Join(repo, "debug"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((200 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	ref, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: "d", ExecutionMode: "worktree"})
	unavailable, _ := json.Marshal(localDirectoryRef{LocalPath: filepath.Join(repo, "missing"), DaemonID: "d", ExecutionMode: "worktree"})
	foreign, _ := json.Marshal(localDirectoryRef{LocalPath: repo, DaemonID: "elsewhere", ExecutionMode: "worktree"})
	var mu sync.Mutex
	reports := map[string]protocol.WorktreeReadinessReport{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]any{"resources": []protocol.WorktreeReadinessResource{{ID: "source", ResourceRef: ref}, {ID: "missing", ResourceRef: unavailable}, {ID: "foreign", ResourceRef: foreign}}})
			return
		}
		var report protocol.WorktreeReadinessReport
		if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
			t.Error(err)
		}
		mu.Lock()
		reports[report.ID] = report
		mu.Unlock()
	}))
	defer srv.Close()
	d := &Daemon{cfg: Config{DaemonID: "d"}, client: NewClient(srv.URL), logger: slog.Default()}
	d.refreshWorktreeReadiness(context.Background(), "runtime")
	if reports["source"].Measurement.Status != "blocked" || reports["source"].Measurement.TotalBytes != (200<<20)+1 {
		t.Fatalf("bad measurement: %+v", reports)
	}
	if reports["missing"].Measurement.Status != "unavailable" {
		t.Fatal("missing source not reported unavailable")
	}
	if _, ok := reports["foreign"]; ok {
		t.Fatal("inspected another daemon's source")
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("debug\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.refreshWorktreeReadiness(context.Background(), "runtime")
	if reports["source"].Measurement.Status != "ready" {
		t.Fatalf("did not recover: %+v", reports["source"])
	}
}
