package daemon

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackendServiceIdentity(t *testing.T) {
	for _, pair := range [][2]string{{"https://EXAMPLE.test:443/", "https://example.test"}, {"wss://example.test/ws", "https://example.test"}, {"http://example.test:80/", "http://example.test"}, {"HTTPS://example.test/a/../b", "https://example.test/b"}, {"https://example.test/%61", "https://example.test/a"}, {"https://example.test/任务", "https://example.test/%E4%BB%BB%E5%8A%A1"}, {"https://example.test/a!", "https://example.test/a%21"}} {
		a, err := BackendServiceKey(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		b, err := BackendServiceKey(pair[1])
		if err != nil || a != b {
			t.Fatalf("not same backend: %v %v", pair, err)
		}
	}
	a, _ := BackendServiceKey("https://example.test/a")
	b, _ := BackendServiceKey("https://example.test/b")
	if a == b {
		t.Fatal("deployment paths collapsed")
	}
	for _, raw := range []string{"file:///tmp/backend", "https://user:secret@example.test", "https:///missing"} {
		if _, err := BackendServiceKey(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestServiceDrainKeepsExistingTasksAndBlocksNewClaims(t *testing.T) {
	d := &Daemon{}
	if !d.tryEnterClaim() {
		t.Fatal("initial claim refused")
	}
	d.activeTasks.Store(1)
	w := httptest.NewRecorder()
	d.drainHandler()(w, httptest.NewRequest(http.MethodPost, "/drain", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"idle":false`) {
		t.Fatal(w.Body.String())
	}
	if d.tryEnterClaim() {
		t.Fatal("claimed after drain")
	}
	if d.activeTasks.Load() != 1 {
		t.Fatal("drain cancelled running task")
	}
	d.releaseClaimBarrier()
	if d.tryEnterClaim() {
		t.Fatal("update barrier released service drain")
	}
	d.exitClaim()
	d.activeTasks.Store(0)
	w = httptest.NewRecorder()
	d.drainHandler()(w, httptest.NewRequest(http.MethodDelete, "/drain", nil))
	if !d.tryEnterClaim() {
		t.Fatal("cancelled drain did not resume")
	}
	d.exitClaim()
}

func TestExecutionStateProfileKeepsExplicitDefault(t *testing.T) {
	empty := ""
	if got := (Config{Profile: "desktop", StateProfile: &empty}).ExecutionStateProfile(); got != "" {
		t.Fatal(got)
	}
	if got := (Config{Profile: "legacy"}).ExecutionStateProfile(); got != "legacy" {
		t.Fatal(got)
	}
}

func TestBoundStateRejectsOtherProfileInsteadOfFreshSession(t *testing.T) {
	root := t.TempDir()
	if err := validateBoundPriorState(root, filepath.Join(root, "workspace-a", "task-a", "workdir")); err != nil {
		t.Fatal(err)
	}
	if err := validateBoundPriorState(root, filepath.Join(t.TempDir(), "workspace-a", "task-a", "workdir")); err == nil {
		t.Fatal("silently accepted another profile's prior workdir")
	}
}
