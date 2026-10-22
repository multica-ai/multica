//go:build agentintegration

package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/chatgpt"
)

// TestCodexChatGPTRealPlanSmoke uses an explicitly selected local SIWC store.
// It does not initiate OAuth, inspect Codex credentials, or modify the selection.
// Sign in using a dedicated Multica CLI profile, then explicitly authorize:
//
//	MULTICA_RUN_REAL_AGENT_SMOKE=1 MULTICA_CHATGPT_SMOKE_STORE=/path/to/accounts.json \
//	  go test -tags=agentintegration ./pkg/agent -run '^TestCodexChatGPTRealPlanSmoke$' -count=1 -v
func TestCodexChatGPTRealPlanSmoke(t *testing.T) {
	requireRealAgentSmoke(t)
	store := os.Getenv("MULTICA_CHATGPT_SMOKE_STORE")
	if store == "" {
		t.Skip("set MULTICA_CHATGPT_SMOKE_STORE to an explicitly selected SIWC store")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal("install Codex before running the authorized smoke test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	manager := chatgpt.NewManager(store)
	account, err := manager.Status()
	if err != nil || account == nil || !account.PlanEnabled {
		t.Fatal("sign in with plan usage enabled in the selected SIWC store first")
	}
	models, err := manager.Models(ctx, account.ClientID)
	if err != nil {
		t.Fatalf("account model catalog: %v", err)
	}
	backend, err := New("codex", Config{
		ExecutablePath: path, BuiltinRuntime: true,
		// Isolate the smoke from the user's Codex auth, config, MCP, and history.
		Env:                  map[string]string{"CODEX_HOME": t.TempDir()},
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
		CodexSessionOwnerDir: t.TempDir(),
		CodexChatGPTPlan: &CodexChatGPTPlan{
			AccessToken: func(ctx context.Context) (CodexChatGPTToken, error) {
				token, err := manager.AccessToken(ctx, account.ClientID)
				return CodexChatGPTToken{AccessToken: token.AccessToken, ClientID: token.ClientID, Subject: token.Subject, Scopes: token.Scopes}, err
			},
			AssertActive: func(ctx context.Context, token CodexChatGPTToken) error {
				return manager.AssertActive(ctx, chatgpt.Token{AccessToken: token.AccessToken, ClientID: token.ClientID, Subject: token.Subject, Scopes: token.Scopes})
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	marker := "siwc-smoke-" + uuid.NewString()
	if err := os.WriteFile(filepath.Join(cwd, "probe.txt"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	opts := ExecOptions{Cwd: cwd, Model: models[0].Slug, Timeout: 90 * time.Second}
	type toolEvidence struct {
		usedTool     bool
		checkedToken bool
	}
	run := func(prompt string) (Result, toolEvidence) {
		t.Helper()
		session, err := backend.Execute(ctx, prompt, opts)
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		toolsDone := make(chan toolEvidence, 1)
		go func() {
			evidence := toolEvidence{}
			checkedCalls := map[string]bool{}
			for message := range session.Messages {
				if message.Type == MessageToolUse {
					evidence.usedTool = true
					command, _ := message.Input["command"].(string)
					if message.Tool == "exec_command" && strings.Contains(command, `test -z "$MULTICA_CHATGPT_ACCESS_TOKEN" && cat probe.txt`) {
						checkedCalls[message.CallID] = true
					}
				}
				if message.Type == MessageToolResult && checkedCalls[message.CallID] && strings.Contains(message.Output, marker) {
					evidence.checkedToken = true
				}
			}
			toolsDone <- evidence
		}()
		result := <-session.Result
		evidence := <-toolsDone
		if result.Status != "completed" || result.SessionID == "" {
			t.Fatalf("turn did not complete: status=%s error=%s", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, marker) {
			t.Fatal("turn did not return the fixture marker")
		}
		return result, evidence
	}
	first, evidence := run("Run exactly this shell command: `test -z \"$MULTICA_CHATGPT_ACCESS_TOKEN\" && cat probe.txt`. Return only the file contents. If the check fails, report CHECK_FAILED without printing any environment values.")
	if !evidence.checkedToken {
		t.Fatal("the first turn did not verify token exclusion through a real shell tool")
	}
	if err := os.Remove(filepath.Join(cwd, "probe.txt")); err != nil {
		t.Fatal(err)
	}
	opts.ResumeSessionID = first.SessionID
	second, evidence := run("Without using any tools, repeat the exact file contents from the previous turn.")
	if evidence.usedTool {
		t.Fatal("the follow-up used a tool instead of the resumed conversation")
	}
	if second.SessionID != first.SessionID {
		t.Fatal("the follow-up did not resume the same Codex thread")
	}
	t.Log("SIWC catalog, completed tool turn, shell token exclusion, and process-restart thread resume passed")
}
