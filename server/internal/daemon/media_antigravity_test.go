package daemon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/media"
	"github.com/multica-ai/multica/server/internal/testutil/agyimage"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func antigravityNativeFixture(t *testing.T, mode string) (*media.Antigravity, string) {
	t.Helper()
	t.Setenv(agyimage.ModeEnv, mode)
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	auth := filepath.Join(home, agyimage.TokenPath)
	if err := os.MkdirAll(filepath.Dir(auth), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(auth, []byte(agyimage.Token), 0600); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "calls")
	t.Setenv("AGY_IMAGE_TEST_COUNTER", counter)
	return &media.Antigravity{Command: agent.NewCommand(command, nil), Home: home}, counter
}

func TestMediaAntigravityDiscoveryReadiness(t *testing.T) {
	for _, failure := range []string{"missing", "directory", "invalid", "enterprise", "catalog-fail", "catalog-empty"} {
		t.Run(failure, func(t *testing.T) {
			executor, counter := antigravityNativeFixture(t, "ready")
			redirectTestHome(t, executor.Home)
			d := &Daemon{cfg: Config{DiscoverNativeMedia: true, Agents: map[string]AgentEntry{"antigravity": {Path: executor.Command.Path}}}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), agentVersions: map[string]string{}}
			check := func(ready bool) {
				t.Helper()
				entries, _, _ := d.detectBuiltinRuntimes(t.Context())
				if len(entries) != 1 {
					t.Fatalf("missing runtime: %v", entries)
				}
				caps := d.withMediaCapabilities(entries)[0]["media_capabilities"].([]string)
				native := d.mediaExecutor("antigravity", "")
				if ready {
					e, ok := native.(*media.Antigravity)
					if !ok || e.Home != executor.Home || e.Command.Path != executor.Command.Path || !slices.Equal(caps, []string{"media-image-agy-v1"}) {
						t.Fatalf("caps=%v executor=%#v", caps, native)
					}
				} else if len(caps) != 0 || native != nil {
					t.Fatalf("retained unready executor: %v %#v", caps, native)
				}
				if d.mediaExecutor("antigravity", "custom") != nil || d.mediaExecutor("claude", "") != nil {
					t.Fatal("executor crossed runtime identity")
				}
			}
			check(true)
			auth := filepath.Join(executor.Home, agyimage.TokenPath)
			want := "probe\n"
			switch failure {
			case "missing", "directory":
				os.Remove(auth)
				if failure == "directory" {
					os.Mkdir(auth, 0700)
				}
			case "invalid":
				os.WriteFile(auth, []byte("bad json"), 0600)
			case "enterprise":
				os.WriteFile(auth, []byte(`{"auth_method":"enterprise","token":{"access_token":"fake"}}`), 0600)
			default:
				t.Setenv(agyimage.ModeEnv, failure)
				want += "probe\n"
			}
			check(false)
			calls, _ := os.ReadFile(counter)
			if string(calls) != want {
				t.Fatalf("calls=%q want=%q", calls, want)
			}
		})
	}
}

func TestMediaAntigravityDiscoveryGates(t *testing.T) {
	executor, counter := antigravityNativeFixture(t, "ready")
	redirectTestHome(t, executor.Home)
	d := &Daemon{cfg: Config{DiscoverNativeMedia: true}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if got := d.probeNativeMedia(t.Context(), "claude", AgentEntry{Path: executor.Command.Path}); got != "" {
		t.Fatal(got)
	}
	d.cfg.DiscoverNativeMedia = false
	if got := d.probeNativeMedia(t.Context(), "antigravity", AgentEntry{Path: executor.Command.Path}); got != "" {
		t.Fatal(got)
	}
	d.cfg.DiscoverNativeMedia = true
	d.cfg.MediaExecutor = func(string, string) media.Executor { return nil }
	if got := d.probeNativeMedia(t.Context(), "antigravity", AgentEntry{Path: executor.Command.Path}); got != "" {
		t.Fatal(got)
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatal("disabled or foreign discovery launched CLI")
	}
}

func TestMediaAntigravityCapabilityRefresh(t *testing.T) {
	executor, _ := antigravityNativeFixture(t, "ready")
	redirectTestHome(t, executor.Home)
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.DiscoverNativeMedia = true
	d.cfg.Agents = map[string]AgentEntry{"antigravity": {Path: executor.Command.Path}}
	stubAgentProbe(t, d.cfg.Agents)
	fx.setWorkspaces(WorkspaceInfo{ID: "ws-1", Name: "one"}, WorkspaceInfo{ID: "ws-2", Name: "two"})
	if err := d.syncWorkspacesFromAPI(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	check := func(ws string, ready bool) {
		t.Helper()
		d.mu.Lock()
		defer d.mu.Unlock()
		if (d.workspaces[ws].builtinMediaCapabilities["antigravity"] == "media-image-agy-v1") != ready {
			t.Fatalf("%s capability snapshot=%v", ws, d.workspaces[ws].builtinMediaCapabilities)
		}
	}
	check("ws-1", true)
	check("ws-2", true)
	before := fx.registerCallCount()
	t.Setenv(agyimage.ModeEnv, "catalog-fail")
	fx.failRegisterFor("ws-2", true)
	d.refreshAgentVersions(t.Context())
	check("ws-1", false)
	check("ws-2", true)
	fx.failRegisterFor("ws-2", false)
	d.refreshAgentVersions(t.Context())
	check("ws-2", false)
	if got := fx.registerCallCount() - before; got != 2 {
		t.Fatalf("registration retries=%d", got)
	}
	t.Setenv(agyimage.ModeEnv, "ready")
	d.refreshAgentVersions(t.Context())
	check("ws-1", true)
	check("ws-2", true)
	d.refreshAgentVersions(t.Context())
	if got := fx.registerCallCount() - before; got != 4 {
		t.Fatalf("capability recovery/unchanged registrations=%d", got)
	}
}

func TestMediaAntigravityRequestValidation(t *testing.T) {
	executor, counter := antigravityNativeFixture(t, "ready")
	for _, request := range []protocol.MediaRequest{
		{Capability: "video-generate"}, {Capability: "image-edit"},
		{Capability: "image-generate", Model: "nano-banana"}, {Capability: "image-generate", Duration: 2},
	} {
		_, err := executor.Submit(t.Context(), request, nil, t.TempDir())
		var invalid *media.RequestError
		if !errors.As(err, &invalid) {
			t.Fatalf("request=%+v error=%v", request, err)
		}
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatal("invalid request spent native quota")
	}
}

func TestMediaAntigravityQuotaAndUploadRetry(t *testing.T) {
	for _, mode := range []string{"ready", "edit", "quota-result", "quota-transcript", "quota-log", "escape"} {
		t.Run(mode, func(t *testing.T) {
			executor, counter := antigravityNativeFixture(t, mode)
			var uploads atomic.Int32
			var outputPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer job-token" {
					t.Error("transfer did not use job-scoped token")
				}
				if strings.Contains(r.URL.Path, "/inputs/") {
					w.Write([]byte("reference image"))
					return
				}
				if r.Method == http.MethodGet {
					w.Write([]byte(`{"state":"running"}`))
					return
				}
				b, _ := io.ReadAll(r.Body)
				if http.DetectContentType(b) != "image/png" {
					t.Error("upload was not native image")
				}
				if uploads.Add(1) < 3 {
					w.WriteHeader(503)
					return
				}
				w.Write([]byte(`{"id":"native-image-attachment"}`))
			}))
			defer server.Close()
			d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor {
				return &recordingNativeExecutor{Executor: executor, path: &outputPath}
			}}, client: NewClient(server.URL)}
			request := protocol.MediaRequest{Capability: "image-generate", Provider: "agy", Prompt: "fixture"}
			if mode == "edit" {
				request.Capability = "image-edit"
				request.Inputs = []protocol.MediaInput{{AttachmentID: "input", Role: "subject"}}
			}
			result := d.executeMedia(t.Context(), Runtime{ID: "agy-host", Provider: "antigravity"}, protocol.MediaAssignment{Token: "job-token", Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute), Request: request}})
			if strings.HasPrefix(mode, "quota-") {
				if !result.RateLimited || result.Error != "" || uploads.Load() != 0 {
					t.Fatalf("quota receipt=%+v uploads=%d", result, uploads.Load())
				}
				_, err := executor.Submit(context.Background(), request, nil, t.TempDir())
				var limited *media.RateLimitError
				if !errors.As(err, &limited) {
					t.Fatalf("not RateLimitError: %v", err)
				}
			} else if mode == "escape" {
				if result.Error == "" || result.RateLimited || uploads.Load() != 0 {
					t.Fatalf("escape uploaded: %+v", result)
				}
			} else {
				if result.Error != "" || result.RateLimited || uploads.Load() != 3 {
					t.Fatalf("receipt=%+v uploads=%d", result, uploads.Load())
				}
				if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
					t.Fatal("job output not cleaned")
				}
			}
			calls, _ := os.ReadFile(counter)
			want := "generate\n"
			if strings.HasPrefix(mode, "quota-") {
				want += "generate\n"
			}
			if string(calls) != want {
				t.Fatalf("generation replayed: %q", calls)
			}
			content, err := os.ReadFile(filepath.Join(executor.Home, agyimage.TokenPath))
			if err != nil || string(content) != agyimage.Token {
				t.Fatal("host authentication changed")
			}
		})
	}
}
