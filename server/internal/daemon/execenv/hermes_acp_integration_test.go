//go:build agentintegration

package execenv

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Run against each installed Hermes release. Inference stays on a local fixture;
// the test never loads the user's config, provider credentials or messaging accounts.
func TestHermesManagedHomeRealACP(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to run a real Hermes binary")
	}
	cli, err := exec.LookPath("hermes")
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(cli, "--version").CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", version)
	const liveTurnPrompt = "Hold this cancelled turn until interrupted. Return alpha without tools."
	var holdRequests atomic.Bool
	activeRequest := make(chan struct{}, 1)
	releaseRequests := make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"object":"list","data":[{"id":"fixture-model","object":"model"}]}`)
			return
		}
		var request struct {
			Stream   bool
			Messages json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if holdRequests.Load() && strings.Contains(string(request.Messages), liveTurnPrompt) {
			select {
			case activeRequest <- struct{}{}:
			default:
			}
			select {
			case <-releaseRequests:
			case <-r.Context().Done():
			}
			return
		}
		if request.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"alpha\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","created":1,"model":"fixture-model","choices":[{"index":0,"message":{"role":"assistant","content":"alpha"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`)
	}))
	defer model.Close()
	defer close(releaseRequests)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	source, store := t.TempDir(), filepath.Join(t.TempDir(), "conversation")
	// Mirror only PM's installed dependencies; no real user data or secrets.
	install := ""
	for _, line := range strings.Split(string(version), "\n") {
		if value, ok := strings.CutPrefix(line, "Install directory: "); ok {
			install = value
		}
	}
	if install == "" {
		t.Fatal("Hermes version did not identify its install directory")
	}
	// Old Hermes can republish its source launchers using a borrowed home's
	// tool path. Restore them before deleting that temporary home.
	launchers := filepath.Join(install, ".hermes", "bin")
	if entries, err := os.ReadDir(launchers); err == nil {
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				continue
			}
			path := filepath.Join(launchers, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, data, info.Mode()); err != nil {
					t.Error(err)
				}
			})
		}
	}
	for _, name := range []string{"installs", "tools"} {
		if err := os.Symlink(filepath.Join(filepath.Dir(install), name), filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(source, "config.yaml"), fmt.Sprintf("model:\n  provider: custom\n  default: fixture-model\n  base_url: %s/v1\n  api_key: local-fixture-key\n  api_mode: chat_completions\nmcp_servers: {}\nagent: {max_turns: 1, disabled_toolsets: [all]}\nmemory: {memory_enabled: false, user_profile_enabled: false}\ngateway:\n  api_server: {enabled: true}\n", model.URL))
	mustWrite(t, filepath.Join(source, ".env"), fmt.Sprintf("API_SERVER_KEY=local-fixture-key-at-least-32-characters\nAPI_SERVER_ENABLED=false\nAPI_SERVER_HOST=127.0.0.1\nAPI_SERVER_PORT=%d\n", occupied.Addr().(*net.TCPAddr).Port))
	// A host PID must never be followed by gateway cleanup.
	mustWrite(t, filepath.Join(source, "gateway.pid"), "host fixture must stay untouched")
	var sessionID string
	for turn := range 2 {
		scratch := filepath.Join(t.TempDir(), "hermes-home")
		home, err := prepareHermesHome(scratch, source, false, []SkillContextForEnv{{Name: "fixture", Content: "Local test"}}, nil, "", store, testLogger())
		if err != nil {
			t.Fatal(err)
		}
		if home.HistoryPresent != (turn > 0) {
			t.Fatalf("turn %d history=%v", turn, home.HistoryPresent)
		}
		user := t.TempDir()
		cfg := agent.Config{ExecutablePath: cli, Logger: slog.Default(), BuiltinRuntime: true,
			Env: map[string]string{"HOME": user, "HERMES_HOME": home.Home,
				"HERMES_DISABLE_LAZY_INSTALLS": "1", "HERMES_ACP_SKIP_CONFIGURED_MCP": "1",
				"OPENAI_API_KEY": "local-fixture-key", "OPENAI_BASE_URL": model.URL + "/v1"}}
		func() {
			defer func() {
				if err := agent.StopHermesTaskGateway(cfg, home.Home); err != nil {
					t.Error(err)
				}
				if data, err := os.ReadFile(filepath.Join(home.Home, "gateway_state.json")); err == nil {
					var state struct {
						State string `json:"gateway_state"`
					}
					if err := json.Unmarshal(data, &state); err != nil || state.State != "stopped" {
						t.Errorf("task gateway did not stop: state=%s err=%v", state.State, err)
					}
				}
			}()
			backend, err := agent.New("hermes", cfg)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "Reply with alpha without tools.", agent.ExecOptions{Cwd: user, ResumeSessionID: sessionID})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			t.Logf("turn=%d status=%s output=%q session=%s error=%s", turn, result.Status, result.Output, result.SessionID, result.Error)
			if result.Status != "completed" || !strings.Contains(result.Output, "alpha") || result.SessionID == "" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if sessionID != "" && result.SessionID != sessionID {
				t.Fatal("follow-up lost the original session")
			}
			sessionID = result.SessionID
		}()
	}
	t.Run("legacy_gateway_record", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(store, "gateway_state.json")); err == nil {
			t.Skip("gateway-backed ACP supports the interrupt flag")
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		// A conversation used by a newer release may retain a stale lock after
		// downgrading to in-process ACP, whose CLI rejects gateway stop --yes.
		mustWrite(t, filepath.Join(store, "gateway.lock"), "")
		cfg := agent.Config{ExecutablePath: cli, Logger: slog.Default(),
			Env: map[string]string{"HOME": t.TempDir(), "HERMES_HOME": store}}
		if err := agent.StopHermesTaskGateway(cfg, store); err != nil {
			t.Fatalf("clean up a retained gateway record with an older CLI: %v", err)
		}
	})
	t.Run("interrupt_live_turn", func(t *testing.T) {
		if _, err := os.Stat(filepath.Join(store, "gateway_state.json")); os.IsNotExist(err) {
			t.Skip("in-process ACP has no detached gateway")
		} else if err != nil {
			t.Fatal(err)
		}
		holdRequests.Store(true)
		user := t.TempDir()
		cfg := agent.Config{ExecutablePath: cli, Logger: slog.Default(), BuiltinRuntime: true,
			Env: map[string]string{"HOME": user, "HERMES_HOME": store,
				"HERMES_DISABLE_LAZY_INSTALLS": "1", "HERMES_ACP_SKIP_CONFIGURED_MCP": "1",
				"OPENAI_API_KEY": "local-fixture-key", "OPENAI_BASE_URL": model.URL + "/v1"}}
		gatewayStop := func(interrupt bool) ([]byte, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args := []string{"--profile", "default", "gateway", "stop"}
			if interrupt {
				args = append(args, "--yes")
			}
			cmd := exec.CommandContext(ctx, cli, args...)
			cmd.Env = os.Environ()
			for key, value := range cfg.Env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			return cmd.CombinedOutput()
		}
		// Cleanup must also work when the regression assertion fails before the
		// production stop helper is fixed; no detached gateway may outlive this test.
		defer func() {
			if output, err := gatewayStop(true); err != nil {
				t.Errorf("forced fixture cleanup failed: %v: %s", err, output)
			}
		}()
		backend, err := agent.New("hermes", cfg)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		session, err := backend.Execute(ctx, liveTurnPrompt, agent.ExecOptions{Cwd: user, ResumeSessionID: sessionID})
		if err != nil {
			t.Fatal(err)
		}
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for range session.Messages {
			}
		}()
		select {
		case <-activeRequest:
		case <-time.After(45 * time.Second):
			t.Fatal("Hermes never started the blocked model request")
		}
		// Killing the ACP client does not stop the detached owner or its worker.
		cancel()
		select {
		case result := <-session.Result:
			if result.Status != "aborted" {
				t.Fatalf("cancelled ACP result = %+v", result)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("cancelled ACP client did not exit")
		}
		<-drained
		// Prove this is still a live turn, not an idle-gateway cleanup test.
		if output, err := gatewayStop(false); err == nil || !strings.Contains(string(output), "Refusing to stop the gateway") {
			t.Fatalf("expected refusal for live work without --yes: err=%v output=%s", err, output)
		}
		if err := agent.StopHermesTaskGateway(cfg, store); err != nil {
			t.Fatalf("stop detached gateway with cancelled live turn: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(store, "gateway_state.json"))
		if err != nil {
			t.Fatal(err)
		}
		var state struct {
			State        string `json:"gateway_state"`
			ActiveAgents int    `json:"active_agents"`
		}
		if err := json.Unmarshal(data, &state); err != nil || state.State != "stopped" || state.ActiveAgents != 0 {
			t.Fatalf("gateway still has live work: state=%+v err=%v", state, err)
		}
	})
}
