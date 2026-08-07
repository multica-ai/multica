package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPlatformAgentProviderContract(t *testing.T) {
	backend, err := New("platform-agent-cli", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.(*platformAgentBackend); !ok {
		t.Fatalf("New(platform-agent-cli) returned %T", backend)
	}
	if IsSupportedType("platform-agent-cli") {
		t.Fatal("built-in platform runtime must not enter the custom profile whitelist")
	}
	if got := LaunchHeader("platform-agent-cli"); got != "platform-agent-cli app-server" {
		t.Fatalf("LaunchHeader = %q", got)
	}
	if ModelSelectionSupported("platform-agent-cli") {
		t.Fatal("platform runtime owns model selection")
	}
}

func TestPlatformAgentMinimumVersion(t *testing.T) {
	if err := CheckMinVersion("platform-agent-cli", "platform-agent-cli 0.1.0"); err != nil {
		t.Fatal(err)
	}
	if err := CheckMinVersion("platform-agent-cli", "platform-agent-cli 0.0.9"); err == nil {
		t.Fatal("expected below-minimum error")
	}
}

func TestPlatformAgentListModelsReturnsEmptyCatalog(t *testing.T) {
	catalog, err := ListModels(context.Background(), "platform-agent-cli", "/path/that/must/not/be-resolved")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Models == nil {
		t.Fatal("Models must be an empty, non-nil slice")
	}
	if len(catalog.Models) != 0 {
		t.Fatalf("Models = %+v, want empty", catalog.Models)
	}
	if catalog.Fallback {
		t.Fatal("platform-owned empty catalog must not be marked as fallback")
	}
}

func TestPlatformAgentExecOptionsDropsCodexOwnedOverrides(t *testing.T) {
	want := ExecOptions{
		Cwd:                       "/workspace",
		SystemPrompt:              "runtime brief",
		ThreadName:                "thread name",
		MaxTurns:                  7,
		Timeout:                   11 * time.Second,
		SemanticInactivityTimeout: 12 * time.Second,
		IdleWatchdogTimeout:       13 * time.Second,
		HandshakeTimeout:          14 * time.Second,
		ResumeSessionID:           "thread-1",
		ResumeExpected:            true,
		ResumeContinuityNotice:    "continuity",
	}
	got := platformAgentExecOptions(ExecOptions{
		Cwd:                       want.Cwd,
		Model:                     "codex-model",
		SystemPrompt:              want.SystemPrompt,
		ThreadName:                want.ThreadName,
		MaxTurns:                  want.MaxTurns,
		Timeout:                   want.Timeout,
		SemanticInactivityTimeout: want.SemanticInactivityTimeout,
		IdleWatchdogTimeout:       want.IdleWatchdogTimeout,
		HandshakeTimeout:          want.HandshakeTimeout,
		ResumeSessionID:           want.ResumeSessionID,
		ResumeExpected:            want.ResumeExpected,
		ResumeContinuityNotice:    want.ResumeContinuityNotice,
		ExtraArgs:                 []string{"--extra"},
		CustomArgs:                []string{"--custom"},
		McpConfig:                 json.RawMessage(`{"mcpServers":{"server":{}}}`),
		ThinkingLevel:             "high",
		ServiceTier:               "priority",
	})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("platformAgentExecOptions() = %+v, want %+v", got, want)
	}
}

func TestNewPlatformAgentBackendOwnsEnvironmentCopy(t *testing.T) {
	env := map[string]string{"CODEX_HOME": "/codex", "KEEP": "value"}
	backend := newPlatformAgentBackend(Config{
		Env:          env,
		CodexVersion: "codex 9.9.9",
	})
	if got := env["CODEX_HOME"]; got != "/codex" {
		t.Fatalf("caller-owned environment mutated: CODEX_HOME = %q", got)
	}
	if _, ok := backend.transport.cfg.Env["CODEX_HOME"]; ok {
		t.Fatal("platform transport inherited CODEX_HOME")
	}
	if got := backend.transport.cfg.Env["KEEP"]; got != "value" {
		t.Fatalf("cloned environment lost KEEP: %q", got)
	}
	if backend.transport.cfg.Env == nil {
		t.Fatal("platform transport must own a non-nil environment map")
	}
	if got := backend.transport.cfg.CodexVersion; got != "" {
		t.Fatalf("CodexVersion = %q, want empty", got)
	}
}

func TestPlatformAgentTransportPolicyDisablesCodexBehavior(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args")
	rpcPath := filepath.Join(tempDir, "rpc")
	codexHome := filepath.Join(tempDir, "codex-home")
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	const originalConfig = "model = \"caller-owned\"\n"
	if err := os.WriteFile(configPath, []byte(originalConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	fakePath := writeFakeCodexAppServer(t, ""+
		`printf '%s\n' "$@" > "$CAPTURE_ARGS"`+"\n"+
		`read line; printf '%s\n' "$line" >> "$CAPTURE_RPC"`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line; printf '%s\n' "$line" >> "$CAPTURE_RPC"`+"\n"+
		`read line; printf '%s\n' "$line" >> "$CAPTURE_RPC"`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-platform"}}}'`+"\n"+
		`read line; printf '%s\n' "$line" >> "$CAPTURE_RPC"`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-platform","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")
	backend := &codexBackend{
		cfg: Config{
			ExecutablePath: fakePath,
			Env: map[string]string{
				"CAPTURE_ARGS": argsPath,
				"CAPTURE_RPC":  rpcPath,
				"CODEX_HOME":   codexHome,
			},
			Logger: slog.Default(),
		},
		policy: &platformAgentAppServerPolicy,
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{
		Cwd:           tempDir,
		Model:         "codex-model",
		ThinkingLevel: "high",
		ServiceTier:   "priority",
		ExtraArgs:     []string{"--extra"},
		CustomArgs:    []string{"--custom"},
		McpConfig:     json.RawMessage(`{"mcpServers":{"server":{"command":"server"}}}`),
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	if result := <-session.Result; result.Status != "completed" {
		t.Fatalf("result = %+v", result)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(args)); !reflect.DeepEqual(got, []string{"app-server", "--listen", "stdio://"}) {
		t.Fatalf("launch args = %v, want protocol-only app-server args", got)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(config); got != originalConfig {
		t.Fatalf("platform transport modified CODEX_HOME/config.toml:\n%s", got)
	}
	rpc, err := os.ReadFile(rpcPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"codex-model", `"effort":"high"`, `"model_reasoning_effort":"high"`, `"serviceTier":"priority"`} {
		if strings.Contains(string(rpc), forbidden) {
			t.Fatalf("platform app-server payload contains Codex override %q:\n%s", forbidden, rpc)
		}
	}
}

func TestPlatformAgentTransportPolicyBrandsExecutableError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	backend := &codexBackend{cfg: Config{Logger: slog.Default()}, policy: &platformAgentAppServerPolicy}
	_, err := backend.Execute(context.Background(), "prompt", ExecOptions{})
	if err == nil {
		t.Fatal("expected missing executable error")
	}
	if got := err.Error(); !strings.Contains(got, `platform-agent-cli executable not found at "platform-agent-cli"`) {
		t.Fatalf("unexpected provider error: %q", got)
	}
}

func TestPlatformAgentTransportPolicyBrandsStartupError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	fakePath := writeFakeCodexAppServer(t, "echo 'runtime startup failed' >&2\nexit 2\n")
	backend := &codexBackend{
		cfg:    Config{ExecutablePath: fakePath, Logger: slog.Default()},
		policy: &platformAgentAppServerPolicy,
	}
	session, err := backend.Execute(context.Background(), "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if !strings.Contains(result.Error, "platform-agent-cli initialize failed") {
		t.Fatalf("startup error lacks provider identity: %q", result.Error)
	}
	if !strings.Contains(result.Error, "platform-agent-cli stderr: runtime startup failed") {
		t.Fatalf("startup stderr lacks provider identity: %q", result.Error)
	}
}

func TestPlatformAgentTransportPolicyBrandsHandshakeError(t *testing.T) {
	err := (&codexHandshakeTimeoutError{
		Method:   "initialize",
		Timeout:  5 * time.Second,
		Provider: platformAgentAppServerPolicy.provider,
	}).Error()
	want := "platform-agent-cli app-server handshake timeout: initialize did not respond after 5s"
	if err != want {
		t.Fatalf("handshake error = %q, want %q", err, want)
	}
}

func TestPlatformAgentPolicySuppressesCodexStartupRetry(t *testing.T) {
	for _, result := range []Result{
		{codexInitializeRetrySafe: true},
		{codexStartupRefreshRetrySafe: true},
	} {
		if got := appServerRetryReason(platformAgentAppServerPolicy, result); got != "" {
			t.Fatalf("platform policy scheduled Codex retry %q", got)
		}
	}
	if got := appServerRetryReason(codexAppServerPolicy, Result{codexInitializeRetrySafe: true}); got != "initialize" {
		t.Fatalf("Codex initialize retry reason = %q", got)
	}
	if got := appServerRetryReason(codexAppServerPolicy, Result{codexStartupRefreshRetrySafe: true}); got != "model_catalog_refresh" {
		t.Fatalf("Codex model-catalog retry reason = %q", got)
	}
}
