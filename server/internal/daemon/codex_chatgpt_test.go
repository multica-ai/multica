package daemon

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/chatgpt"
	"github.com/multica-ai/multica/server/internal/cli"
)

type fakeCodexChatGPTManager struct {
	expected string
	models   []chatgpt.Model
	err      error
}

func (m *fakeCodexChatGPTManager) AccessToken(_ context.Context, client string) (chatgpt.Token, error) {
	m.expected = client
	return chatgpt.Token{AccessToken: "private", ClientID: client, Subject: "subject", Scopes: []string{chatgpt.DirectScope}}, m.err
}
func (m *fakeCodexChatGPTManager) AssertActive(context.Context, chatgpt.Token) error { return m.err }
func (m *fakeCodexChatGPTManager) Models(_ context.Context, client string) ([]chatgpt.Model, error) {
	m.expected = client
	return m.models, m.err
}

func TestCodexChatGPTModelSelectionUsesAccountCatalog(t *testing.T) {
	m := &fakeCodexChatGPTManager{models: []chatgpt.Model{{Slug: "entitled-one", DisplayName: "One"}, {Slug: "entitled-two", DisplayName: "Two"}}}
	auth := newCodexChatGPTAuth(m, "registered-client", t.TempDir())
	ctx := context.Background()
	selected, err := auth.selectModel(ctx, taskModelSelection{ThinkingLevel: "high", ServiceTier: "priority"})
	if err != nil || selected.Model != "entitled-one" || selected.ThinkingLevel != "high" || selected.ServiceTier != "priority" {
		t.Fatalf("selection %+v: %v", selected, err)
	}
	if m.expected != "registered-client" {
		t.Fatal("selection was not bound to configured registration")
	}
	if _, err = auth.selectModel(ctx, taskModelSelection{Model: "bundled-but-not-entitled"}); err == nil {
		t.Fatal("unknown model treated as entitled")
	}
	m.models = nil
	if _, err = auth.selectModel(ctx, taskModelSelection{}); err == nil {
		t.Fatal("empty account catalog acquired a default")
	}
	m.err = errors.New("sign in again")
	if _, err = auth.selectModel(ctx, taskModelSelection{}); err == nil {
		t.Fatal("auth failure fell back to bundled models")
	}
}

func TestCodexChatGPTCredentialAdapterChecksCurrentRegistration(t *testing.T) {
	m := &fakeCodexChatGPTManager{}
	auth := newCodexChatGPTAuth(m, "registered-client", t.TempDir())
	token, err := auth.plan.AccessToken(context.Background())
	if err != nil || token.ClientID != "registered-client" || token.AccessToken != "private" {
		t.Fatalf("bad token adapter: %v", err)
	}
	m.err = errors.New("selected account changed")
	if err = auth.plan.AssertActive(context.Background(), token); err == nil {
		t.Fatal("stale token accepted")
	}
}

func TestCodexChatGPTConfigIsLocalLiveAndBuiltinOnly(t *testing.T) {
	t.Setenv(cli.TaskConfigRootEnv, t.TempDir())
	cfg := cli.CLIConfig{Backends: &cli.BackendOverrides{Codex: &cli.CodexOverride{AuthMode: cli.CodexAuthModeChatGPTPlan, ChatGPTClientID: "client-a"}}}
	if err := cli.SaveCLIConfigForProfile(cfg, "a"); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{cfg: Config{Profile: "a"}}
	current, err := d.codexAuthForProvider("codex", false)
	if err != nil || current.plan == nil || current.clientID != "client-a" {
		t.Fatalf("plan selection: %+v, %v", current, err)
	}
	for _, provider := range []string{"codex", "claude"} {
		custom, err := d.codexAuthForProvider(provider, true)
		if err != nil || custom.plan != nil || custom.ownerDir != "" {
			t.Fatalf("custom wrapper received local plan credentials: %+v %v", custom, err)
		}
	}
	cfg.Backends.Codex.ChatGPTClientID = "client-b"
	if err = cli.SaveCLIConfigForProfile(cfg, "a"); err != nil {
		t.Fatal(err)
	}
	current, err = d.codexAuthForProvider("codex", false)
	if err != nil || current.clientID != "client-b" {
		t.Fatal("running daemon retained old selection")
	}
	d.cfg.Profile = "b"
	other, err := d.codexAuthForProvider("codex", false)
	if err != nil || other.plan != nil || other.ownerDir == current.ownerDir {
		t.Fatal("profile B inherited profile A auth")
	}
	d.cfg.Profile = "a"
	path, err := cli.CLIConfigPathForProfile("a")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = d.codexAuthForProvider("codex", false); err == nil {
		t.Fatal("corrupt auth selection fell back to vendor")
	}
	if filepath.Base(filepath.Dir(current.ownerDir)) != "chatgpt" {
		t.Fatal("ownership state escaped local ChatGPT profile directory")
	}
}

func TestCodexChatGPTCredentialStaysOutOfToolShellPolicy(t *testing.T) {
	codexHome := t.TempDir()
	// Even an inherited credential must not enter the task's shell allowlist.
	// The actual plan credential is acquired later by the agent and is never
	// inserted into the explicit map passed to this policy builder.
	agentEnv := map[string]string{"MULTICA_TOKEN": "task-token", "CODEX_HOME": codexHome}
	if err := configureCodexTaskShellEnvironment("codex", codexHome, []string{"MULTICA_CHATGPT_ACCESS_TOKEN=old-credential"}, agentEnv, nil, slog.Default()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "MULTICA_CHATGPT_ACCESS_TOKEN") || strings.Contains(string(data), "old-credential") {
		t.Fatal("ChatGPT credential was authorized for tool shells")
	}
	if _, ok := agentEnv["MULTICA_CHATGPT_ACCESS_TOKEN"]; ok {
		t.Fatal("policy setup mutated task env with inference credential")
	}
}
