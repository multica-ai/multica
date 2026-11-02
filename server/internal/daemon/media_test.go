package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/media"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type cancellingMediaExecutor struct {
	calls int
	dir   string
}

func (e *cancellingMediaExecutor) Capabilities() []string { return []string{"media-image-codex-v1"} }
func (e *cancellingMediaExecutor) Submit(ctx context.Context, _ protocol.MediaRequest, _ []media.Input, dir string) (media.Artifact, error) {
	e.calls++
	e.dir = dir
	<-ctx.Done()
	return media.Artifact{}, ctx.Err()
}

func TestMediaCancellationStopsSubmitAndCleansFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/media/jobs/job/status" {
			t.Errorf("unexpected transfer: %s", r.URL.Path)
		}
		w.WriteHeader(409)
	}))
	defer server.Close()
	executor := &cancellingMediaExecutor{}
	d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor { return executor }}, client: NewClient(server.URL)}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := d.executeMedia(ctx, Runtime{ID: "runtime", Provider: "codex"}, protocol.MediaAssignment{Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute)}})
	if result.Error == "" || executor.calls != 1 || ctx.Err() != nil {
		t.Fatal("remote cancellation did not stop one Submit promptly")
	}
	if _, err := os.Stat(executor.dir); !os.IsNotExist(err) {
		t.Fatal("cancelled job left temporary files")
	}
}

type artifactMediaExecutor struct {
	path func(string) (string, error)
}

func (e artifactMediaExecutor) Capabilities() []string {
	return []string{"media-image-agy-v1"}
}
func (e artifactMediaExecutor) Submit(_ context.Context, _ protocol.MediaRequest, _ []media.Input, dir string) (media.Artifact, error) {
	path, err := e.path(dir)
	return media.Artifact{Path: path}, err
}

func TestMediaOutputCannotEscapeJobDirectory(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "private-fixture.txt")
	if err := os.WriteFile(outside, []byte("fixture only; must never be transferred"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, symlink := range []bool{false, true} {
		name := "absolute path"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			var uploads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { uploads.Add(1); w.Write([]byte(`{"id":"unexpected"}`)) }))
			defer server.Close()
			executor := artifactMediaExecutor{path: func(dir string) (string, error) {
				if symlink {
					p := filepath.Join(dir, "output.png")
					return p, os.Symlink(outside, p)
				}
				return outside, nil
			}}
			d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor { return executor }}, client: NewClient(server.URL)}
			result := d.executeMedia(t.Context(), Runtime{ID: "runtime", Provider: "antigravity"}, protocol.MediaAssignment{Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute)}})
			if result.Error != "media output must be inside job directory" || uploads.Load() != 0 {
				t.Fatalf("host file escaped: error=%s uploads=%d", result.Error, uploads.Load())
			}
		})
	}
}

func TestMediaEmbeddedCapabilitiesUseExistingRuntime(t *testing.T) {
	executor := artifactMediaExecutor{}
	d := &Daemon{cfg: Config{MediaExecutor: func(provider, profile string) media.Executor {
		if provider == "antigravity" && profile == "verified" {
			return executor
		}
		return nil
	}}}
	entries := d.withMediaCapabilities([]map[string]string{{"type": "antigravity", "profile_id": "verified"}, {"type": "antigravity", "profile_id": "unavailable"}, {"type": "codex"}})
	for i, entry := range entries {
		got := entry["media_capabilities"].([]string)
		if i == 0 && !slices.Equal(got, executor.Capabilities()) || i != 0 && len(got) != 0 {
			t.Fatalf("verified embedded capabilities: %v", entries)
		}
	}
}

func TestMediaNativeSnapshotDoesNotBroadcastToProfiles(t *testing.T) {
	d := &Daemon{}
	entries := d.withMediaCapabilities([]map[string]string{
		{"type": "codex", "native_media_capability": "media-image-codex-v1"},
		{"type": "antigravity", "native_media_capability": "media-image-codex-v1"},
		{"type": "codex", "profile_id": "custom", "native_media_capability": "media-image-codex-v1"},
	})
	for i, entry := range entries {
		caps := entry["media_capabilities"].([]string)
		if (len(caps) == 1) != (i == 0) {
			t.Fatalf("capability crossed runtime boundary: %v", entries)
		}
		if _, ok := entry["native_media_capability"]; ok {
			t.Fatal("internal snapshot leaked into registration")
		}
	}
}

func TestMediaCapabilityRefreshWithoutCLIVersionChange(t *testing.T) {
	fx := newVersionRefreshFixture(t)
	d := fx.daemon
	d.mu.Lock()
	for _, ws := range d.workspaces {
		ws.builtinMediaCapabilities = map[string]string{"codex": "media-image-codex-v1"}
	}
	d.mu.Unlock()
	before := fx.registerCallCount()
	fx.failRegisterFor("ws-2", true)
	d.refreshAgentVersions(t.Context())
	d.mu.Lock()
	if d.workspaces["ws-1"].builtinMediaCapabilities["codex"] != "" || d.workspaces["ws-2"].builtinMediaCapabilities["codex"] == "" {
		t.Error("capability acknowledgement did not follow successful registration")
	}
	d.mu.Unlock()
	fx.failRegisterFor("ws-2", false)
	d.refreshAgentVersions(t.Context())
	if got := fx.registerCallCount() - before; got != 2 {
		t.Fatalf("successful registrations=%d, want one per workspace", got)
	}
	d.refreshAgentVersions(t.Context())
	if got := fx.registerCallCount() - before; got != 2 {
		t.Fatalf("unchanged capabilities re-registered: %d", got)
	}
}

func TestMediaWorkerReportsDuringShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reported := make(chan protocol.MediaResult, 1)
	var claims atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/media/claim":
			claims.Add(1)
			json.NewEncoder(w).Encode(protocol.MediaClaimResponse{Assignment: &protocol.MediaAssignment{Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute)}, Attempt: "attempt"}})
		case "/api/daemon/media/result":
			var result protocol.MediaResult
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Error(err)
			}
			reported <- result
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	executor := artifactMediaExecutor{path: func(string) (string, error) { cancel(); return "", context.Canceled }}
	d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor { return executor }}, client: NewClient(server.URL), wsRPC: newWSRPCClient(wsRPCResponseGrace)}
	done := make(chan struct{})
	go func() { defer close(done); d.mediaWorker(ctx, Runtime{ID: "runtime", Provider: "antigravity"}) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("worker did not finish shutdown")
	}
	select {
	case result := <-reported:
		if result.JobID != "job" || result.Attempt != "attempt" || result.Error == "" {
			t.Fatal("invalid shutdown receipt", result)
		}
	default:
		t.Fatal("cancelled worker did not report independently")
	}
	if claims.Load() != 1 {
		t.Fatal("shutdown claimed another job")
	}
}
