package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/chatgpt"
	"github.com/multica-ai/multica/server/internal/cli"
)

type fakeChatGPTManager struct {
	account      chatgpt.Account
	accounts     []chatgpt.Account
	loginErr     error
	loginOptions chatgpt.LoginOptions
	selected     string
	logoutClient string
	revoked      bool
	modelsClient string
}

func (f *fakeChatGPTManager) Login(_ context.Context, opts chatgpt.LoginOptions) (chatgpt.Account, error) {
	f.loginOptions = opts
	if f.loginErr != nil {
		return chatgpt.Account{}, f.loginErr
	}
	if opts.Authorize != nil {
		if err := opts.Authorize(chatgpt.Authorization{URL: "https://auth.openai.com/authorize?id_token_hint=private-hint", DisplayURL: "https://auth.openai.com/authorize?state=public-state&redirect_uri=http%3A%2F%2F127.0.0.1%3A54321%2Fauth%2Fcallback"}); err != nil {
			return chatgpt.Account{}, err
		}
	}
	return f.account, nil
}
func (f *fakeChatGPTManager) Accounts() ([]chatgpt.Account, error) { return f.accounts, nil }
func (f *fakeChatGPTManager) Status() (*chatgpt.Account, error) {
	if f.account.ClientID == "" {
		return nil, nil
	}
	return &f.account, nil
}
func (f *fakeChatGPTManager) Select(_ context.Context, id string) error { f.selected = id; return nil }
func (f *fakeChatGPTManager) Logout(_ context.Context, id string) (chatgpt.LogoutResult, error) {
	f.logoutClient = id
	return chatgpt.LogoutResult{RevocationConfirmed: f.revoked}, nil
}
func (f *fakeChatGPTManager) Models(_ context.Context, id string) ([]chatgpt.Model, error) {
	f.modelsClient = id
	return []chatgpt.Model{{Slug: "model-allowed", DisplayName: "Allowed model"}}, nil
}

func setupChatGPTCommandTest(t *testing.T, fake *fakeChatGPTManager) (*bytes.Buffer, func(...string) error) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	previous := newChatGPTManager
	newChatGPTManager = func(path string) chatGPTManager { return fake }
	t.Cleanup(func() { newChatGPTManager = previous })
	output := &bytes.Buffer{}
	return output, func(args ...string) error {
		cmd := newChatGPTCommand()
		cmd.PersistentFlags().String("profile", "", "")
		cmd.SetOut(output)
		cmd.SetErr(output)
		cmd.SetArgs(args)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		return cmd.Execute()
	}
}

func TestChatGPTLoginEnablesOnlySelectedLocalProfile(t *testing.T) {
	fake := &fakeChatGPTManager{account: chatgpt.Account{ClientID: "oaiapp_work", Subject: "person", PlanEnabled: true}}
	output, run := setupChatGPTCommandTest(t, fake)
	if err := cli.SaveCLIConfigForProfile(cli.CLIConfig{Token: "existing-multica-login", Backends: &cli.BackendOverrides{OpenClaw: &cli.OpenClawOverride{StateDir: "/untouched"}}}, "work"); err != nil {
		t.Fatal(err)
	}
	if err := run("--profile", "work", "login", "--account", "oaiapp_work", "--label", "Work", "--no-browser", "--timeout", "1m"); err != nil {
		t.Fatal(err)
	}
	if fake.loginOptions.ClientID != "oaiapp_work" || fake.loginOptions.Label != "Work" {
		t.Fatalf("wrong login options: %+v", fake.loginOptions)
	}
	cfg, err := cli.LoadCLIConfigForProfile("work")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "existing-multica-login" || cfg.Backends.OpenClaw.StateDir != "/untouched" {
		t.Fatal("changed Multica login or unrelated backend")
	}
	if cfg.Backends.Codex.AuthMode != cli.CodexAuthModeChatGPTPlan || cfg.Backends.Codex.ChatGPTClientID != "oaiapp_work" {
		t.Fatalf("missing explicit auth selection: %+v", cfg.Backends.Codex)
	}
	defaults, _ := cli.LoadCLIConfig()
	if defaults.Backends != nil {
		t.Fatal("changed default profile")
	}
	if strings.Contains(output.String(), "private-hint") {
		t.Fatal("printed ID-token hint")
	}
	if !strings.Contains(output.String(), "state=public-state") || !strings.Contains(output.String(), "New Codex runs") {
		t.Fatalf("missing manual URL or account selection guidance: %s", output)
	}
	if !strings.Contains(output.String(), "http://127.0.0.1:54321/auth/callback") || !strings.Contains(output.String(), "ssh -N -L 127.0.0.1:54321:127.0.0.1:54321") {
		t.Fatalf("missing loopback callback and SSH forwarding guidance: %s", output)
	}
}

func TestChatGPTFailedLoginLeavesRuntimeSelectionUnchanged(t *testing.T) {
	fake := &fakeChatGPTManager{loginErr: errors.New("identity verification failed")}
	_, run := setupChatGPTCommandTest(t, fake)
	cfg := cli.CLIConfig{Backends: &cli.BackendOverrides{Codex: &cli.CodexOverride{AuthMode: cli.CodexAuthModeChatGPTPlan, ChatGPTClientID: "original"}}}
	if err := cli.SaveCLIConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := run("login", "--no-browser"); err == nil {
		t.Fatal("expected login failure")
	}
	actual, _ := cli.LoadCLIConfig()
	if actual.Backends.Codex.ChatGPTClientID != "original" {
		t.Fatal("failed login changed account")
	}
}

func TestChatGPTIdentityOnlyLoginDoesNotClaimInferenceReady(t *testing.T) {
	fake := &fakeChatGPTManager{account: chatgpt.Account{ClientID: "oaiapp_identity", Subject: "person", SignedIn: true, PlanEnabled: false}}
	output, run := setupChatGPTCommandTest(t, fake)
	if err := cli.SaveCLIConfig(cli.CLIConfig{Backends: &cli.BackendOverrides{Codex: &cli.CodexOverride{AuthMode: cli.CodexAuthModeChatGPTPlan, ChatGPTClientID: "oaiapp_usable"}}}); err != nil {
		t.Fatal(err)
	}
	if err := run("login", "--no-browser"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "plan usage is disabled") {
		t.Fatalf("missing disabled grant message: %s", output)
	}
	cfg, _ := cli.LoadCLIConfig()
	if cfg.Backends.Codex.AuthMode != cli.CodexAuthModeChatGPTPlan || cfg.Backends.Codex.ChatGPTClientID != "oaiapp_usable" {
		t.Fatal("disabled consent replaced the usable runtime account")
	}
	if strings.Contains(output.String(), "New Codex runs") {
		t.Fatal("identity-only login claimed runtime activation")
	}
}

func TestChatGPTUseAndLogoutKeepExplicitBillingChoice(t *testing.T) {
	fake := &fakeChatGPTManager{account: chatgpt.Account{ClientID: "oaiapp_a"}, revoked: false}
	output, run := setupChatGPTCommandTest(t, fake)
	if err := run("use", "oaiapp_b"); err != nil {
		t.Fatal(err)
	}
	if fake.selected != "oaiapp_b" {
		t.Fatal("account was not selected")
	}
	if err := run("logout", "oaiapp_b"); err == nil {
		t.Fatal("unconfirmed remote revocation must be visible as unsuccessful")
	}
	if !strings.Contains(output.String(), "locally") {
		t.Fatalf("logout did not explain local completion: %s", output)
	}
	cfg, _ := cli.LoadCLIConfig()
	if cfg.Backends.Codex.AuthMode != cli.CodexAuthModeChatGPTPlan || cfg.Backends.Codex.ChatGPTClientID != "oaiapp_b" {
		t.Fatal("logout changed billing configuration")
	}
}

func TestChatGPTDisableExplicitlyRestoresCodexDefault(t *testing.T) {
	fake := &fakeChatGPTManager{}
	_, run := setupChatGPTCommandTest(t, fake)
	if err := cli.SaveCLIConfig(cli.CLIConfig{Backends: &cli.BackendOverrides{Codex: &cli.CodexOverride{AuthMode: cli.CodexAuthModeChatGPTPlan, ChatGPTClientID: "oaiapp_a"}}}); err != nil {
		t.Fatal(err)
	}
	if err := run("disable"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := cli.LoadCLIConfig()
	if cfg.Backends.Codex != nil {
		t.Fatal("Codex override remains enabled")
	}
	if fake.logoutClient != "" {
		t.Fatal("disable unexpectedly revoked account")
	}
}

func TestChatGPTStatusAndModelsAreAccountScoped(t *testing.T) {
	fake := &fakeChatGPTManager{account: chatgpt.Account{ClientID: "oaiapp_active", Subject: "person", Email: "person@example.com", Active: true, SignedIn: true, PlanEnabled: true, CanRefresh: true, ExpiresAt: time.Now().Add(time.Hour)}}
	output, run := setupChatGPTCommandTest(t, fake)
	if err := run("status", "--output", "json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "oaiapp_active") {
		t.Fatalf("missing account identity: %s", output)
	}
	if err := run("models", "--output", "json"); err != nil {
		t.Fatal(err)
	}
	if fake.modelsClient != "oaiapp_active" {
		t.Fatal("models were not bound to selected account")
	}
	if !strings.Contains(output.String(), "model-allowed") {
		t.Fatal("missing live model")
	}
}

func TestChatGPTCommandsRejectDaemonTaskIdentity(t *testing.T) {
	for _, args := range [][]string{{"login", "--no-browser"}, {"status"}, {"accounts"}, {"use", "oaiapp_x"}, {"logout"}, {"models"}, {"disable"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			fake := &fakeChatGPTManager{}
			_, run := setupChatGPTCommandTest(t, fake)
			t.Setenv("MULTICA_TASK_ID", "task-owned")
			if err := run(args...); err == nil || !strings.Contains(err.Error(), "daemon-managed task") {
				t.Fatalf("task command allowed: %v", err)
			}
			configPath, _ := cli.CLIConfigPath()
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatal("task modified owner config")
			}
		})
	}
}
