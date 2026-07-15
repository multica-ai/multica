package execenv

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func mustReadOpenclawJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return value
}

func readOpenclawTaskFiles(t *testing.T, wrapperPath, envRoot string) []byte {
	t.Helper()
	var combined []byte
	for _, path := range []string{wrapperPath, filepath.Join(envRoot, openclawUserSnapshotFile)} {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		combined = append(combined, raw...)
	}
	return combined
}

func writeOwnerOpenclawConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write owner config: %v", err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", path)
	return path
}

func TestPrepareOpenclawConfigDoesNotExecuteTaskSelectedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}
	writeOwnerOpenclawConfig(t, `{"agents":{"defaults":{"model":"openai/gpt-5"}}}`)
	envRoot := t.TempDir()
	marker := filepath.Join(t.TempDir(), "invoked")
	fakeBin := filepath.Join(t.TempDir(), "openclaw")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\nprintf invoked > "+marker+"\nexit 73\n"), 0o755); err != nil {
		t.Fatalf("write malicious binary: %v", err)
	}

	_, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{OpenclawBin: fakeBin})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("task-selected OpenClaw binary was invoked: %v", err)
	}
}

func TestPrepareOpenclawConfigExcludesOwnerCommandMCP(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{
		"agents":{"defaults":{"model":"openai/gpt-5"}},
		"mcp":{"servers":{"owner-admin":{"command":"/Users/owner/bin/owner-mcp","args":["--admin"]}}}
	}`)
	envRoot := t.TempDir()
	result, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	combined := readOpenclawTaskFiles(t, result.ConfigPath, envRoot)
	for _, forbidden := range []string{"owner-admin", "owner-mcp", "--admin"} {
		if bytes.Contains(combined, []byte(forbidden)) {
			t.Fatalf("owner MCP leaked through %q: %s", forbidden, combined)
		}
	}
}

func TestPrepareOpenclawConfigContainsNoOwnerAbsolutePaths(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{
		"agents":{"defaults":{"workspace":"/Users/owner/.openclaw/workspace","model":"openai/gpt-5"}},
		"plugins":{"load":{"paths":["/Users/owner/.openclaw/plugins/admin"]}},
		"tools":{"admin":{"command":"/Users/owner/bin/admin-tool"}},
		"gateway":{"port":18789}
	}`)
	envRoot := t.TempDir()
	result, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	combined := readOpenclawTaskFiles(t, result.ConfigPath, envRoot)
	if bytes.Contains(combined, []byte("/Users/owner")) {
		t.Fatalf("owner absolute path leaked: %s", combined)
	}
}

func TestPrepareOpenclawConfigWritesAllowlistedSnapshot(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{
		"models":{"default":"openai/gpt-5"},
		"providers":{"openai":{"apiKey":"secret"}},
		"gateway":{"host":"127.0.0.1","port":18789},
		"agents":{"defaults":{"model":"openai/gpt-5","tools":{"admin":true}},"list":[{"id":"coder","name":"Coder","model":"openai/gpt-5","prompt":"owner orders"}]},
		"plugins":{"enabled":true},"hooks":{"command":"owner-hook"},"channels":{"admin":true}
	}`)
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	snapshotPath := filepath.Join(envRoot, openclawUserSnapshotFile)
	snapshot := mustReadOpenclawJSON(t, snapshotPath)
	for _, forbidden := range []string{"mcp", "plugins", "hooks", "channels", "tools"} {
		if _, ok := snapshot[forbidden]; ok {
			t.Fatalf("snapshot retained %q: %v", forbidden, snapshot)
		}
	}
	if snapshot["models"] == nil || snapshot["gateway"] == nil {
		t.Fatalf("snapshot lost allowlisted model or gateway data: %v", snapshot)
	}
	if _, ok := snapshot["providers"]; ok {
		t.Fatalf("credential-only provider data was retained: %v", snapshot)
	}
	agents := snapshot["agents"].(map[string]any)
	if agents["defaults"].(map[string]any)["workspace"] != workDir {
		t.Fatalf("defaults workspace not isolated: %v", agents)
	}
	entry := agents["list"].([]any)[0].(map[string]any)
	if entry["workspace"] != workDir || entry["id"] != "coder" {
		t.Fatalf("agent metadata not projected safely: %v", entry)
	}
	if _, ok := entry["prompt"]; ok {
		t.Fatalf("owner prompt leaked: %v", entry)
	}
	wrapper := mustReadOpenclawJSON(t, result.ConfigPath)
	include := wrapper["$include"].([]any)
	if len(include) != 1 || include[0] != snapshotPath {
		t.Fatalf("wrapper include = %v, want private snapshot", include)
	}
	info, err := os.Lstat(snapshotPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, err = %v", info, err)
	}
}

func TestPrepareOpenclawConfigProjectsPublicProviderMetadata(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{
		"providers":{
			"openai":{
				"apiKey":"openai-secret",
				"API_KEY":"uppercase-secret",
				"x-api-key":"/Users/owner/provider-key",
				"accessKey":"access-key-secret",
				"accountMaterial":"novel-secret",
				"baseUrl":"https://api.openai.example/v1",
				"organization":"org-example",
				"auth":{"access_token":"nested-access-secret","oauthToken":"oauth-secret","Authorization":"Bearer auth-secret","scheme":"Bearer"},
				"fallbacks":[
					{"region":"us-east-1","clientSecret":"array-client-secret"},
					{"region":"eu-west-1","credential":{"type":"service-account","private-key":"array-private-secret","sharedSecret":"shared-secret"}}
				]
			},
			"local":{"endpoint":"http://127.0.0.1:11434","enabled":true,"key":"local-key-secret"}
		}
	}`)
	envRoot := t.TempDir()
	result, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	combined := readOpenclawTaskFiles(t, result.ConfigPath, envRoot)
	for _, secret := range []string{
		"openai-secret", "uppercase-secret", "/Users/owner/provider-key", "access-key-secret",
		"novel-secret", "nested-access-secret", "oauth-secret", "auth-secret",
		"array-client-secret", "array-private-secret", "shared-secret", "local-key-secret",
	} {
		if bytes.Contains(combined, []byte(secret)) {
			t.Errorf("provider credential %q leaked: %s", secret, combined)
		}
	}

	snapshot := mustReadOpenclawJSON(t, filepath.Join(envRoot, openclawUserSnapshotFile))
	providers := snapshot["providers"].(map[string]any)
	want := map[string]any{
		"openai": map[string]any{
			"baseUrl":      "https://api.openai.example/v1",
			"organization": "org-example",
		},
		"local": map[string]any{
			"endpoint": "http://127.0.0.1:11434",
			"enabled":  true,
		},
	}
	if !reflect.DeepEqual(providers, want) {
		t.Fatalf("providers = %#v, want public projection %#v", providers, want)
	}
}

func TestPrepareOpenclawConfigManagedMCPIsTaskAuthoritative(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{"mcp":{"servers":{"owner":{"command":"owner-tool"}}}}`)
	envRoot := t.TempDir()
	managed := json.RawMessage(`{"mcpServers":{"task":{"command":"task-tool","args":["serve"]}}}`)
	result, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{McpConfig: managed})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	raw := readOpenclawTaskFiles(t, result.ConfigPath, envRoot)
	if !bytes.Contains(raw, []byte("task-tool")) || bytes.Contains(raw, []byte("owner-tool")) {
		t.Fatalf("managed MCP isolation failed: %s", raw)
	}
}

func TestPrepareOpenclawConfigFailsClosedOnUnsupportedOwnerConfig(t *testing.T) {
	cases := map[string]string{
		"json5 comment":      "// comment\n{}",
		"trailing comma":     `{"gateway":{"port":18789,}}`,
		"include":            `{"$include":["base.json"]}`,
		"env substitution":   `{"providers":{"openai":{"apiKey":"${OPENAI_API_KEY}"}}}`,
		"absolute allowlist": `{"providers":{"local":{"baseUrl":"/Users/owner/provider.sock"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			writeOwnerOpenclawConfig(t, body)
			envRoot := t.TempDir()
			_, err := prepareOpenclawConfig(envRoot, filepath.Join(envRoot, "workdir"), OpenclawConfigPrep{})
			if err == nil {
				t.Fatal("prepare succeeded, want fail closed")
			}
			if _, statErr := os.Stat(filepath.Join(envRoot, openclawConfigFile)); !os.IsNotExist(statErr) {
				t.Fatalf("wrapper written after failure: %v", statErr)
			}
		})
	}
}

func TestPrepareOpenclawConfigRejectsSymlinkSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture may require privileges")
	}
	target := filepath.Join(t.TempDir(), "owner.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCLAW_CONFIG_PATH", link)
	_, err := prepareOpenclawConfig(t.TempDir(), t.TempDir(), OpenclawConfigPrep{})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want symlink rejection", err)
	}
}

func TestPrepareOpenclawConfigFreshInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_HOME", "")
	t.Setenv("OPENCLAW_CONFIG_PATH", "")
	t.Setenv("CLAWDBOT_CONFIG_PATH", "")
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("CLAWDBOT_STATE_DIR", "")
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{})
	if err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}
	wrapper := mustReadOpenclawJSON(t, result.ConfigPath)
	if _, ok := wrapper["$include"]; ok {
		t.Fatalf("fresh install has include: %v", wrapper)
	}
	if wrapper["agents"].(map[string]any)["defaults"].(map[string]any)["workspace"] != workDir {
		t.Fatalf("workspace not pinned: %v", wrapper)
	}
}

func TestPrepareEnvironmentOpenclawWiresPrivateConfigOnly(t *testing.T) {
	writeOwnerOpenclawConfig(t, `{"agents":{"defaults":{"model":"openai/gpt-5"}},"tools":{"command":"owner-tool"}}`)
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws-1",
		TaskID: "11111111-2222-3333-4444-555555555555", AgentName: "scout",
		Provider: "openclaw", OpenclawBin: "/malicious/not-invoked",
		Task: TaskContextForEnv{IssueID: "issue-1"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if env.OpenclawConfigPath == "" || !strings.HasPrefix(env.OpenclawConfigPath, env.RootDir+string(os.PathSeparator)) {
		t.Fatalf("OpenclawConfigPath = %q, root = %q", env.OpenclawConfigPath, env.RootDir)
	}
	for _, value := range []string{env.RootDir, env.HomeDir, env.ConfigDir, env.WorkDir, env.OpenclawConfigPath} {
		if strings.Contains(value, "/Users/owner") {
			t.Fatalf("environment retained owner path: %+v", env)
		}
	}
}

func TestBuildPerTaskOpenclawConfigGateway(t *testing.T) {
	config := buildPerTaskOpenclawConfig("", "/workdir", nil, false, OpenclawGatewayPin{
		Host: "gw.internal", Port: 18789, Token: "secret-token", TLS: true,
	})
	gateway := config["gateway"].(map[string]any)
	if gateway["host"] != "gw.internal" || gateway["port"] != 18789 || gateway["tls"] != true {
		t.Fatalf("gateway pin = %v", gateway)
	}
	auth := gateway["auth"].(map[string]any)
	if auth["mode"] != "token" || auth["token"] != "secret-token" {
		t.Fatalf("gateway auth = %v", auth)
	}
}
