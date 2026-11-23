package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestListRuntimeDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"project with spaces", "中文"} {
		if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, "file.txt"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "中文"), filepath.Join(home, "linked")); err != nil {
		t.Fatal(err)
	}
	listing, err := listRuntimeDirectories("")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != home || listing.Selectable || len(listing.Entries) != 3 {
		t.Fatalf("home listing = %+v", listing)
	}
	for _, entry := range listing.Entries {
		if entry.Path != filepath.Join(home, entry.Name) {
			t.Fatalf("entry = %+v", entry)
		}
	}
	listing, err = listRuntimeDirectories(filepath.Join(home, "project with spaces"))
	if err != nil || !listing.Selectable || listing.ParentPath == nil || *listing.ParentPath != home {
		t.Fatalf("project = %+v, %v", listing, err)
	}
	before, _ := os.ReadDir(home)
	if _, err := listRuntimeDirectories(home); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadDir(home)
	if len(before) != len(after) {
		t.Fatal("browsing modified directory")
	}
	readOnly := filepath.Join(home, "read-only")
	if err := os.Mkdir(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	if listing, err := listRuntimeDirectories(readOnly); err != nil || !listing.Selectable {
		t.Fatalf("read-only browsing = %+v, %v", listing, err)
	}
	root, err := listRuntimeDirectories(string(filepath.Separator))
	if err != nil || root.Selectable || root.ParentPath != nil {
		t.Fatalf("root = %+v, %v", root, err)
	}
}

func TestListRuntimeDirectoriesRejectsInvalidPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", filepath.Join(home, "missing"), file} {
		t.Run(path, func(t *testing.T) {
			if _, err := listRuntimeDirectories(path); err == nil {
				t.Fatalf("accepted %q", path)
			}
		})
	}
	link := filepath.Join(home, "home-link")
	if err := os.Symlink(home, link); err != nil {
		t.Fatal(err)
	}
	listing, err := listRuntimeDirectories(link)
	if err != nil || listing.Selectable {
		t.Fatalf("home symlink = %+v, %v", listing, err)
	}
	unreadable := filepath.Join(home, "unreadable")
	if err := os.Mkdir(unreadable, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0700) })
	if os.Geteuid() != 0 {
		if _, err := listRuntimeDirectories(unreadable); err == nil {
			t.Fatal("accepted unreadable directory")
		}
	}
}

func TestDirectoryHeartbeatActionReportsWithoutAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	reports := make(chan struct {
		Status string                     `json:"status"`
		Result *protocol.DirectoryListing `json:"result"`
		Error  string                     `json:"error"`
	}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/runtimes/runtime/directories/request/result" {
			t.Errorf("result path = %s", r.URL.Path)
		}
		var body struct {
			Status string                     `json:"status"`
			Result *protocol.DirectoryListing `json:"result"`
			Error  string                     `json:"error"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		reports <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Daemon{cfg: Config{DaemonID: "daemon"}, client: NewClient(srv.URL), logger: slog.Default(), runtimeIndex: map[string]Runtime{"runtime": {ID: "runtime", Provider: "not-an-agent-provider"}}}
	for _, daemon := range []string{"daemon", "different-machine"} {
		d.handleHeartbeatActions(context.Background(), "runtime", &HeartbeatResponse{PendingDirectories: &protocol.DaemonHeartbeatPendingDirectories{ID: "request", DaemonID: daemon}})
		select {
		case report := <-reports:
			if daemon == "daemon" {
				if report.Status != "completed" || report.Result == nil || report.Result.Path != home {
					t.Fatalf("report = %+v", report)
				}
			} else if report.Status != "failed" || report.Error == "" {
				t.Fatalf("wrong machine report = %+v", report)
			}
		case <-time.After(time.Second):
			t.Fatal("directory action did not report")
		}
	}
}
