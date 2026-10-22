package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestChatGPTBackendSelectionIsLocalAndProfileScoped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	original := CLIConfig{Token: "multica-login", Backends: &BackendOverrides{
		OpenClaw: &OpenClawOverride{StateDir: "/existing/openclaw"},
		Codex:    &CodexOverride{AuthMode: CodexAuthModeChatGPTPlan, ChatGPTClientID: "oaiapp_work"},
	}}
	if err := SaveCLIConfigForProfile(original, "work"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCLIConfigForProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Token != original.Token || loaded.Backends.OpenClaw.StateDir != "/existing/openclaw" {
		t.Fatal("changed unrelated login/configuration")
	}
	if loaded.Backends.Codex.AuthMode != CodexAuthModeChatGPTPlan || loaded.Backends.Codex.ChatGPTClientID != "oaiapp_work" {
		t.Fatalf("lost local ChatGPT selection: %+v", loaded.Backends.Codex)
	}
	defaults, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Backends != nil {
		t.Fatal("named profile changed default Codex configuration")
	}
	path, err := ChatGPTStorePathForProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	profileDir, _ := ProfileDir("work")
	if path != filepath.Join(profileDir, "chatgpt", "accounts.json") {
		t.Fatalf("unexpected credential path: %s", path)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path helper created credential storage: %v", err)
	}
}

func TestChatGPTBackendDoesNotChangeExistingCodexDefault(t *testing.T) {
	var config CLIConfig
	if err := json.Unmarshal([]byte(`{"backends":{"openclaw":{"state_dir":"/custom"}}}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.Backends.Codex != nil {
		t.Fatal("existing configuration opted into ChatGPT")
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if _, ok := data["backends"]["codex"]; ok {
		t.Fatal("empty Codex override was serialized")
	}
}
