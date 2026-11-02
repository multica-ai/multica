package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/media"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestMediaNativeProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_MEDIA_COUNTER") == "" {
		return
	}
	runMediaNativeProcess()
}

func runMediaNativeProcess() {
	counter := os.Getenv("MULTICA_TEST_MEDIA_COUNTER")
	mode := os.Getenv("MULTICA_TEST_MEDIA_PROBE")
	for _, arg := range os.Args[1:] {
		if arg == "--version" {
			_, _ = os.Stdout.WriteString("codex-cli 0.160.1\n")
			return
		}
	}
	write := func(v any) { b, _ := json.Marshal(v); _, _ = os.Stdout.Write(append(b, '\n')) }
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Input []struct {
					Type string `json:"type"`
					Path string `json:"path"`
				} `json:"input"`
			} `json:"params"`
		}
		_ = json.Unmarshal(scanner.Bytes(), &req)
		result := map[string]any{}
		if req.Method == "initialized" {
			continue
		}
		switch req.Method {
		case "initialize":
			if mode != "" {
				f, _ := os.OpenFile(counter, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				_, _ = f.WriteString("probe\n")
				_ = f.Close()
			}
			if mode == "eof" {
				return
			}
		case "account/read":
			kind := "chatgpt"
			if mode == "apikey" {
				kind = "apiKey"
			}
			result["account"] = map[string]string{"type": kind}
		case "modelProvider/capabilities/read":
			result["imageGeneration"] = mode != "unsupported"
		case "experimentalFeature/list":
			result["data"] = []map[string]any{{"name": "image_generation", "enabled": true}}
		}
		if req.Method == "thread/start" {
			result["thread"] = map[string]string{"id": "native"}
		}
		if req.Method == "turn/start" {
			if mode == "edit" {
				references := 0
				for _, input := range req.Params.Input {
					if input.Type == "localImage" {
						data, err := os.ReadFile(input.Path)
						if err != nil || http.DetectContentType(data) != "image/png" {
							os.Exit(2)
						}
						references++
					}
				}
				if references != 1 {
					os.Exit(3)
				}
			}
			f, _ := os.OpenFile(counter, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			_, _ = f.WriteString("submit\n")
			_ = f.Close()
			if mode == "turn-quota" || mode == "error-quota" {
				failure := map[string]string{"codexErrorInfo": "usageLimitExceeded"}
				if mode == "turn-quota" {
					write(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "native", "turn": map[string]any{"status": "failed", "error": failure}}})
				} else {
					write(map[string]any{"method": "error", "params": map[string]any{"threadId": "native", "willRetry": false, "error": failure}})
				}
				write(map[string]any{"id": req.ID, "result": result})
				continue
			}
			path := filepath.Join(os.Getenv("CODEX_HOME"), "output.png")
			f, _ = os.Create(path)
			_ = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 3, 3)))
			_ = f.Close()
			write(map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "native", "item": map[string]string{"id": "one", "type": "imageGeneration", "status": "completed", "savedPath": path}}})
			write(map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "native", "turn": map[string]string{"status": "completed"}}})
		}
		write(map[string]any{"id": req.ID, "result": result})
	}
	os.Exit(0)
}

func TestMediaNativeDiscoveryReadiness(t *testing.T) {
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"missing-auth", "directory-auth", "apikey", "unsupported", "eof"} {
		t.Run(failure, func(t *testing.T) {
			home := t.TempDir()
			auth := filepath.Join(home, "auth.json")
			if err := os.WriteFile(auth, []byte(`{"fixture":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			counter := filepath.Join(t.TempDir(), "calls")
			t.Setenv("CODEX_HOME", home)
			t.Setenv("MULTICA_TEST_MEDIA_COUNTER", counter)
			t.Setenv("MULTICA_TEST_MEDIA_PROBE", "ready")
			d := &Daemon{cfg: Config{DiscoverNativeMedia: true, Agents: map[string]AgentEntry{"codex": {Path: command}}}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), agentVersions: map[string]string{}}
			check := func(ready bool) {
				t.Helper()
				entries, _, _ := d.detectBuiltinRuntimes(t.Context())
				if len(entries) != 1 {
					t.Fatalf("missing builtin runtime: %v", entries)
				}
				payload := d.withMediaCapabilities(entries)
				caps := payload[0]["media_capabilities"].([]string)
				executor := d.mediaExecutor("codex", "")
				if ready {
					if !slices.Equal(caps, []string{"media-image-codex-v1"}) || executor == nil {
						t.Fatalf("ready runtime: caps=%v executor=%v", caps, executor)
					}
					native, ok := executor.(*media.Codex)
					if !ok || native.Home != home || native.Command.Path != command {
						t.Fatalf("wrong native executor: %#v", executor)
					}
				} else if len(caps) != 0 || executor != nil {
					t.Fatalf("unready runtime retained capability/executor: %v %v", caps, executor)
				}
				if d.mediaExecutor("codex", "custom") != nil {
					t.Fatal("builtin executor leaked into custom profile")
				}
			}
			check(true)
			t.Setenv("MULTICA_TEST_MEDIA_PROBE", failure)
			wantCalls := "probe\nprobe\n"
			if failure == "missing-auth" || failure == "directory-auth" {
				if err := os.Remove(auth); err != nil {
					t.Fatal(err)
				}
				if failure == "directory-auth" {
					if err := os.Mkdir(auth, 0700); err != nil {
						t.Fatal(err)
					}
				}
				wantCalls = "probe\n"
			}
			check(false)
			calls, err := os.ReadFile(counter)
			if err != nil || string(calls) != wantCalls {
				t.Fatalf("probe calls=%q want=%q err=%v", calls, wantCalls, err)
			}
		})
	}
}

func TestMediaNativeDiscoverySkipsOtherProviders(t *testing.T) {
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"fixture":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "calls")
	t.Setenv("CODEX_HOME", home)
	t.Setenv("MULTICA_TEST_MEDIA_COUNTER", counter)
	t.Setenv("MULTICA_TEST_MEDIA_PROBE", "ready")
	d := &Daemon{cfg: Config{DiscoverNativeMedia: true}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, provider := range []string{"antigravity", "claude"} {
		if cap := d.probeNativeMedia(t.Context(), provider, AgentEntry{Path: command}); cap != "" || d.mediaExecutor(provider, "") != nil {
			t.Fatalf("native Codex support inferred for %s: %s", provider, cap)
		}
	}
	if _, err := os.Stat(counter); !os.IsNotExist(err) {
		t.Fatalf("non-Codex provider invoked probe: %v", err)
	}
}

func TestMediaNativeQuotaReachesScheduler(t *testing.T) {
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"turn-quota", "error-quota"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"fixture":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			counter := filepath.Join(t.TempDir(), "calls")
			t.Setenv("MULTICA_TEST_MEDIA_COUNTER", counter)
			t.Setenv("MULTICA_TEST_MEDIA_PROBE", mode)
			executor := &media.Codex{Command: agent.NewCommand(command, nil), Home: home}
			request := protocol.MediaRequest{Capability: "image-generate", Provider: "codex", Prompt: "fixture"}
			_, err := executor.Submit(t.Context(), request, nil, t.TempDir())
			var limited *media.RateLimitError
			if !errors.As(err, &limited) {
				t.Fatalf("quota did not map to RateLimitError: %v", err)
			}
			var transfers atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { transfers.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor { return executor }}, client: NewClient(server.URL)}
			result := d.executeMedia(t.Context(), Runtime{ID: "host", Provider: "codex"}, protocol.MediaAssignment{Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute), Request: request}})
			if !result.RateLimited || result.Error != "" || transfers.Load() != 0 {
				t.Fatalf("quota did not reach scheduler: %+v transfers=%d", result, transfers.Load())
			}
			calls, err := os.ReadFile(counter)
			if err != nil || string(calls) != "probe\nsubmit\nprobe\nsubmit\n" {
				t.Fatalf("unexpected replay: %q %v", calls, err)
			}
		})
	}
}

func TestMediaNativeUploadRetryDoesNotRegenerate(t *testing.T) {
	command, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"fixture":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(t.TempDir(), "calls")
	t.Setenv("MULTICA_TEST_MEDIA_COUNTER", counter)
	executor := &media.Codex{Command: agent.NewCommand(command, []string{"-test.run=^TestMediaNativeProcess$", "--"}), Home: home}
	var uploads atomic.Int32
	var outputPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(200)
			return
		}
		if r.Header.Get("Authorization") != "Bearer job-token" {
			t.Error("transfer did not use the scoped job token")
		}
		b, _ := io.ReadAll(r.Body)
		if http.DetectContentType(b) != "image/png" {
			t.Error("upload was not the native PNG")
		}
		if uploads.Add(1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"native-output-attachment"}`))
	}))
	defer server.Close()
	d := &Daemon{cfg: Config{MediaExecutor: func(string, string) media.Executor {
		return &recordingNativeExecutor{Executor: executor, path: &outputPath}
	}}, client: NewClient(server.URL)}
	result := d.executeMedia(t.Context(), Runtime{ID: "host", Provider: "codex"}, protocol.MediaAssignment{Token: "job-token", Job: protocol.MediaJob{ID: "job", ExpiresAt: time.Now().Add(time.Minute), Request: protocol.MediaRequest{Capability: "image-generate", Provider: "codex", Prompt: "fixture"}}})
	if result.Error != "" || result.RateLimited || uploads.Load() != 3 {
		t.Fatalf("result=%+v uploads=%d", result, uploads.Load())
	}
	calls, err := os.ReadFile(counter)
	if err != nil || string(calls) != "submit\n" {
		t.Fatalf("model replayed: %q %v", calls, err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("native output retained after upload: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err != nil {
		t.Fatal("host auth removed during job cleanup")
	}
}

type recordingNativeExecutor struct {
	media.Executor
	path *string
}

func (e *recordingNativeExecutor) Submit(ctx context.Context, r protocol.MediaRequest, in []media.Input, dir string) (media.Artifact, error) {
	a, err := e.Executor.Submit(ctx, r, in, dir)
	*e.path = a.Path
	return a, err
}
