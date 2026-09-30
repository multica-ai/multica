package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func chatGPTTestConfig(t *testing.T) Config {
	t.Helper()
	return Config{CodexSessionOwnerDir: t.TempDir(), CodexChatGPTPlan: &CodexChatGPTPlan{
		AccessToken: func(context.Context) (CodexChatGPTToken, error) {
			return CodexChatGPTToken{AccessToken: "opaque-private-token", ClientID: "client-a", Subject: "subject-a", Scopes: []string{"chatgpt.tokens.use.direct"}}, nil
		},
		AssertActive: func(context.Context, CodexChatGPTToken) error { return nil },
	}}
}

func chatGPTFake(t *testing.T, capture, status string) string {
	t.Helper()
	return writeFakeCodexAppServer(t, `printf '%s\n' "$@" > '`+capture+`.args'
printf '%s' "$MULTICA_CHATGPT_ACCESS_TOKEN" > '`+capture+`.token'
read line; printf '%s\n' "$line" > '`+capture+`.rpc'
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read line
read line; printf '%s\n' "$line" >> '`+capture+`.rpc'
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thread-plan"}}}'
read line; printf '%s\n' "$line" >> '`+capture+`.rpc'
echo '{"jsonrpc":"2.0","id":3,"result":{}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thread-plan","turn":{"id":"turn-plan","status":"`+status+`"}}}'
`)
}

func TestCodexChatGPTPlanLaunchAndAccountIsolation(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake CLI")
	}
	cfg := chatGPTTestConfig(t)
	capture := filepath.Join(t.TempDir(), "capture")
	fake := chatGPTFake(t, capture, "completed")
	opts := ExecOptions{Model: "account-model", ResumeSessionID: "old-vendor-thread", Timeout: 30 * time.Second}
	result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, opts, 45*time.Second)
	if result.Status != "completed" {
		t.Fatalf("result: %+v", result)
	}
	args, _ := os.ReadFile(capture + ".args")
	rpc, _ := os.ReadFile(capture + ".rpc")
	for _, value := range []string{`model_provider="multica_chatgpt_`, `base_url="https://api.openai.com/v1"`, `requires_openai_auth=false`, `supports_websockets=false`} {
		if !strings.Contains(string(args), value) {
			t.Errorf("missing %s in launch", value)
		}
	}
	if strings.Contains(string(args)+string(rpc), "opaque-private-token") {
		t.Fatal("token entered argv or JSON-RPC")
	}
	token, _ := os.ReadFile(capture + ".token")
	if string(token) != "opaque-private-token" {
		t.Fatal("missing child-only token")
	}
	if !strings.Contains(string(rpc), `"name":"Multica"`) || !strings.Contains(string(rpc), `"modelProvider":"multica_chatgpt_`) {
		t.Fatalf("incorrect SIWC identity or provider: %s", rpc)
	}
	if strings.Contains(string(rpc), "thread/resume") {
		t.Fatal("resumed unowned vendor thread")
	}
	opts.ResumeSessionID = result.SessionID
	result, _ = executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, opts, 45*time.Second)
	if result.Status != "completed" {
		t.Fatalf("same account resume: %+v", result)
	}
	rpc, _ = os.ReadFile(capture + ".rpc")
	if !strings.Contains(string(rpc), "thread/resume") {
		t.Fatal("same account did not resume")
	}
	cfg.CodexChatGPTPlan.AccessToken = func(context.Context) (CodexChatGPTToken, error) {
		return CodexChatGPTToken{AccessToken: "token-b", ClientID: "client-b", Subject: "subject-b", Scopes: []string{"chatgpt.tokens.use.direct"}}, nil
	}
	fakeSource, _ := os.ReadFile(fake)
	if err := os.WriteFile(fake, []byte(strings.ReplaceAll(string(fakeSource), "thread-plan", "thread-b")), 0700); err != nil {
		t.Fatal(err)
	}
	result, _ = executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, opts, 45*time.Second)
	if result.Status != "completed" {
		t.Fatalf("new account: %+v", result)
	}
	opts.ResumeSessionID = result.SessionID
	rpc, _ = os.ReadFile(capture + ".rpc")
	if strings.Contains(string(rpc), "thread/resume") {
		t.Fatal("resumed another registration's thread")
	}
	cfg.CodexChatGPTPlan = nil
	result, _ = executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, opts, 45*time.Second)
	rpc, _ = os.ReadFile(capture + ".rpc")
	if strings.Contains(string(rpc), "thread/resume") {
		t.Fatal("vendor auth resumed SIWC thread")
	}
	if result.Status != "completed" {
		t.Fatalf("vendor new thread: %+v", result)
	}
}

func TestCodexChatGPTPlanRequiresCompletedAndActiveToken(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake CLI")
	}
	for _, status := range []string{"incomplete", ""} {
		t.Run(status, func(t *testing.T) {
			capture := filepath.Join(t.TempDir(), "capture")
			result, _ := executeFakeCodexCollectingMessagesWithConfig(t, chatGPTFake(t, capture, status), chatGPTTestConfig(t), ExecOptions{Model: "available", Timeout: 30 * time.Second}, 45*time.Second)
			if result.Status != "failed" || !strings.Contains(result.Error, "did not complete") {
				t.Fatalf("noncompleted succeeded: %+v", result)
			}
		})
	}
	cfg := chatGPTTestConfig(t)
	cfg.CodexChatGPTPlan.AssertActive = func(context.Context, CodexChatGPTToken) error {
		return errors.New("ChatGPT account changed; select it again")
	}
	capture := filepath.Join(t.TempDir(), "capture")
	result, _ := executeFakeCodexCollectingMessagesWithConfig(t, chatGPTFake(t, capture, "completed"), cfg, ExecOptions{Model: "available", Timeout: 30 * time.Second}, 45*time.Second)
	if result.Status != "failed" || !strings.Contains(result.Error, "account changed") {
		t.Fatalf("stale token accepted: %+v", result)
	}
	rpc, _ := os.ReadFile(capture + ".rpc")
	if strings.Contains(string(rpc), "turn/start") {
		t.Fatal("stale token sent a turn")
	}
}

func TestCodexChatGPTRefreshRestartsAndResumesWithoutExposingToken(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake CLI")
	}
	cfg := chatGPTTestConfig(t)
	var log bytes.Buffer
	cfg.Logger = slog.New(slog.NewTextHandler(&log, nil))
	loaded := 0
	var providerIDs []string
	cfg.CodexChatGPTPlan.AccessToken = func(context.Context) (CodexChatGPTToken, error) {
		loaded++
		return CodexChatGPTToken{AccessToken: fmt.Sprintf("opaque-renewed-%d", loaded), ClientID: "client-a", Subject: "subject-a", Scopes: []string{"chatgpt.tokens.use.direct"}}, nil
	}
	cfg.CodexChatGPTPlan.AssertActive = func(_ context.Context, token CodexChatGPTToken) error {
		if token.AccessToken != fmt.Sprintf("opaque-renewed-%d", loaded) {
			return errors.New("old token reached dispatch")
		}
		return nil
	}
	capture := filepath.Join(t.TempDir(), "capture")
	fake := chatGPTFake(t, capture, "completed")
	opts := ExecOptions{Model: "account-model", Timeout: 30 * time.Second, CustomArgs: []string{"-c", `model_provider="wrong-provider"`, "-c", `model_providers.openai_chatgpt_plan={base_url="https://example.invalid",experimental_bearer_token="wrong-token"}`}}
	for i := 1; i <= 2; i++ {
		result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, opts, 45*time.Second)
		if result.Status != "completed" {
			t.Fatalf("turn %d: %+v", i, result)
		}
		opts.ResumeSessionID = result.SessionID
		args, _ := os.ReadFile(capture + ".args")
		provider := regexp.MustCompile(`model_provider="(multica_chatgpt_[a-f0-9]+)"`).FindStringSubmatch(string(args))
		if len(provider) != 2 {
			t.Fatalf("missing isolated provider: %s", args)
		}
		providerIDs = append(providerIDs, provider[1])
		token, _ := os.ReadFile(capture + ".token")
		if string(token) != fmt.Sprintf("opaque-renewed-%d", i) {
			t.Fatal("new process did not receive renewed credential")
		}
	}
	rpc, _ := os.ReadFile(capture + ".rpc")
	if !strings.Contains(string(rpc), "thread/resume") {
		t.Fatal("token renewal lost conversation")
	}
	args, _ := os.ReadFile(capture + ".args")
	if strings.LastIndex(string(args), `base_url="https://api.openai.com/v1"`) < strings.LastIndex(string(args), `base_url="https://example.invalid"`) {
		t.Fatal("custom provider overrides official route")
	}
	if !strings.Contains(string(args), `shell_environment_policy.exclude=["MULTICA_CHATGPT_ACCESS_TOKEN"]`) {
		t.Fatal("shell tools can inherit token")
	}
	if strings.Contains(log.String(), "opaque-renewed-") {
		t.Fatal("token entered daemon logs")
	}
	for _, path := range []string{codexChatGPTOwnerPath(cfg.CodexSessionOwnerDir, opts.ResumeSessionID)} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "client-a") || strings.Contains(string(data), "subject-a") || strings.Contains(string(data), "opaque") {
			t.Fatal("ownership marker stores identity or token")
		}
	}
	if len(providerIDs) != 2 || providerIDs[0] == providerIDs[1] {
		t.Fatal("reused a provider ID from previous process configuration")
	}
	if loaded != 2 {
		t.Fatalf("got %d token acquisitions for two turns", loaded)
	}
}

func TestCodexChatGPTDeniesMissingScopeBeforeLaunch(t *testing.T) {
	cfg := chatGPTTestConfig(t)
	cfg.CodexChatGPTPlan.AccessToken = func(context.Context) (CodexChatGPTToken, error) {
		return CodexChatGPTToken{AccessToken: "identity-only", ClientID: "client", Subject: "subject"}, nil
	}
	backend, err := New("codex", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = backend.Execute(context.Background(), "prompt", ExecOptions{Model: "model"}); err == nil || !strings.Contains(err.Error(), "chatgpt.tokens.use.direct") {
		t.Fatalf("identity grant launched CLI: %v", err)
	}
}

func TestCodexChatGPTLegacyTerminalCannotClaimCompletion(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake CLI")
	}
	capture := filepath.Join(t.TempDir(), "capture")
	fake := chatGPTFake(t, capture, "completed")
	source, _ := os.ReadFile(fake)
	script := strings.Replace(string(source), `"method":"turn/completed","params":{"threadId":"thread-plan","turn":{"id":"turn-plan","status":"completed"}}`, `"method":"codex/event/task_complete","params":{"id":"turn-plan","msg":{"type":"task_complete"}}`, 1)
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	result, _ := executeFakeCodexCollectingMessagesWithConfig(t, fake, chatGPTTestConfig(t), ExecOptions{Model: "account-model", Timeout: 30 * time.Second}, 45*time.Second)
	if result.Status != "failed" || !strings.Contains(result.Error, "turn/completed") {
		t.Fatalf("unverified completion accepted: %+v", result)
	}
}

func TestCodexChatGPTRedactsOpaqueErrorsAndDoesNotReplayFailure(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX fake CLI")
	}
	cfg := chatGPTTestConfig(t)
	var log bytes.Buffer
	cfg.Logger = slog.New(slog.NewTextHandler(&log, nil))
	capture := filepath.Join(t.TempDir(), "capture")
	fake := chatGPTFake(t, capture, "failed")
	source, _ := os.ReadFile(fake)
	script := strings.Replace(string(source), `"status":"failed"`, `"status":"failed","error":{"message":"subscription_sharing_usage_limit opaque-private-token"}`, 1)
	script = strings.Replace(script, "read line;", `echo 'opaque-private-token' >&2`+"\nread line;", 1)
	if err := os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	result, messages := executeFakeCodexCollectingMessagesWithConfig(t, fake, cfg, ExecOptions{Model: "account-model", Timeout: 30 * time.Second}, 45*time.Second)
	if result.Status != "failed" || !strings.Contains(result.Error, "subscription_sharing_usage_limit") {
		t.Fatalf("failure was lost: %+v", result)
	}
	if strings.Contains(fmt.Sprint(result, messages, log.String()), "opaque-private-token") {
		t.Fatal("opaque token leaked from child diagnostics")
	}
	if strings.Count(log.String(), "phase=spawn") != 1 {
		t.Fatal("terminal usage error replayed the turn")
	}
	token, _ := cfg.CodexChatGPTPlan.AccessToken(context.Background())
	if strings.Contains(fmt.Sprintf("%s %+v %#v", token, token, token), "opaque-private-token") {
		t.Fatal("token formatting is not redacted")
	}
}
