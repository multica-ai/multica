//go:build agentintegration

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// TestRealAgentLocalConversationSmoke runs one local conversation per
// provider against the installed CLI and its signed-in account: a turn, a
// resumed turn that needs the first one, a native fork that keeps the
// history, and an interrupted turn. It uses the user's account, so it is
// gated by the agentintegration tag and MULTICA_RUN_REAL_AGENT_SMOKE=1.
//
// Claude needs Desktop's built adapter (pnpm --filter @multica/desktop exec
// electron-vite build); MULTICA_SMOKE_CLAUDE_ENTRY overrides its path.
// MULTICA_SMOKE_PROVIDERS limits the providers, e.g. "codex".
func TestRealAgentLocalConversationSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real agent CLI and account access")
	}
	providers := []string{"claude", "codex"}
	if v := os.Getenv("MULTICA_SMOKE_PROVIDERS"); v != "" {
		providers = strings.Split(v, ",")
	}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) { realAgentSmoke(t, provider) })
	}
}

func realAgentSmoke(t *testing.T, provider string) {
	executable, err := exec.LookPath(provider)
	if err != nil {
		t.Skipf("%s is not installed: %v", provider, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	opts := Options{Path: filepath.Join(t.TempDir(), "c.sqlite"), Logger: logger}
	switch provider {
	case "claude":
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skipf("node is not installed: %v", err)
		}
		entry := os.Getenv("MULTICA_SMOKE_CLAUDE_ENTRY")
		if entry == "" {
			entry, _ = filepath.Abs("../../../../apps/desktop/out/main/claude-host.js")
		}
		if _, err := os.Stat(entry); err != nil {
			t.Skipf("build Desktop's Claude adapter first: %v", err)
		}
		opts.Claude = func(_ context.Context, conv sqlitedb.Conversation) (ClaudeSession, StartClaudeAdapter, error) {
			fields := map[string]any{"executable": executable, "cwd": conv.Cwd, "env": ClaudeEnv(os.Environ())}
			return ClaudeSession{Fields: fields}, func(ctx context.Context, h claude.Handler) (ClaudeAdapter, error) {
				return claude.Start(ctx, claude.AdapterCommand(node, entry, os.Environ()), h, logger)
			}, nil
		}
	case "codex":
		opts.Codex = func(_ context.Context, conv sqlitedb.Conversation) (CodexSession, StartCodexServer, error) {
			return CodexSession{Cwd: conv.Cwd}, func(ctx context.Context, h codex.Handler, env []string) (CodexServer, error) {
				return codex.Start(ctx, codex.Command{Path: executable, Dir: conv.Cwd, Env: append(os.Environ(), env...)}, h, logger)
			}, nil
		}
	default:
		t.Fatalf("unknown provider %q", provider)
	}

	ctx := context.Background()
	svc, err := Open(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	scopeID, err := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "smoke", WorkspaceID: "smoke"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := svc.Create(ctx, CreateCommand{ScopeID: scopeID, ID: "smoke-" + provider, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}

	send := func(id, requestID, text string) string {
		t.Helper()
		res, err := svc.Send(ctx, SendCommand{ScopeID: scopeID, ConversationID: id, RequestID: requestID, Text: text})
		if err != nil {
			t.Fatalf("send %s: %v", requestID, err)
		}
		return res.TurnID
	}

	// A turn, then a resumed turn that only the first one can answer.
	first := waitTurn(t, svc, scopeID, conv.ID, send(conv.ID, "r1", "Remember the word PELICAN. Reply with exactly: OK"), "completed")
	t.Logf("first reply: %q", first)
	recall := waitTurn(t, svc, scopeID, conv.ID, send(conv.ID, "r2", "Which word did I ask you to remember? Reply with only the word."), "completed")
	if !strings.Contains(strings.ToUpper(recall), "PELICAN") {
		t.Fatalf("resumed turn lost the history: %q", recall)
	}

	// A native fork keeps the history in an independent provider session.
	fork, err := svc.Fork(ctx, ForkCommand{ScopeID: scopeID, SourceID: conv.ID, RequestID: "fork-1"})
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if fork.ID == conv.ID || fork.ProviderSessionID == "" || fork.ProviderSessionID == conv.ProviderSessionID {
		t.Fatalf("fork is not independent: %+v", fork)
	}
	forked := waitTurn(t, svc, scopeID, fork.ID, send(fork.ID, "f1", "Which word did I ask you to remember? Reply with only the word."), "completed")
	if !strings.Contains(strings.ToUpper(forked), "PELICAN") {
		t.Fatalf("fork lost the history: %q", forked)
	}

	// An interrupted turn ends as cancelled, and the conversation still works.
	long := send(conv.ID, "r3", "Count from 1 to 400, one number per line, with no other text.")
	waitFor(t, 2*time.Minute, func() bool { return turnHasOutput(t, svc, scopeID, conv.ID, long) })
	if err := svc.Interrupt(ctx, scopeID, conv.ID); err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	waitTurn(t, svc, scopeID, conv.ID, long, "cancelled")
	after := waitTurn(t, svc, scopeID, conv.ID, send(conv.ID, "r4", "Reply with exactly: DONE"), "completed")
	if !strings.Contains(strings.ToUpper(after), "DONE") {
		t.Fatalf("turn after the interrupt: %q", after)
	}
}

// waitTurn waits for a turn to settle with the given status and returns its
// assistant text.
func waitTurn(t *testing.T, svc *Service, scopeID int64, conversationID, turnID, status string) string {
	t.Helper()
	var view store.TurnView
	waitFor(t, 3*time.Minute, func() bool {
		v, ok := findTurn(t, svc, scopeID, conversationID, turnID)
		view = v
		return ok && v.Turn.Status != "running" && v.Turn.Status != "queued"
	})
	if view.Turn.Status != status {
		t.Fatalf("turn %s = %s (%s: %s); want %s", turnID, view.Turn.Status, view.Turn.ErrorKind, view.Turn.Error, status)
	}
	return assistantText(view)
}

func findTurn(t *testing.T, svc *Service, scopeID int64, conversationID, turnID string) (store.TurnView, bool) {
	t.Helper()
	snap, err := svc.Snapshot(context.Background(), scopeID, conversationID, 0)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, v := range snap.Turns {
		if v.Turn.ID == turnID {
			return v, true
		}
	}
	return store.TurnView{}, false
}

func turnHasOutput(t *testing.T, svc *Service, scopeID int64, conversationID, turnID string) bool {
	v, ok := findTurn(t, svc, scopeID, conversationID, turnID)
	return ok && len(v.Events) > 0 && v.Turn.Status == "running"
}

func assistantText(v store.TurnView) string {
	var parts []string
	for _, e := range v.Events {
		var p struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if e.Type == "item.completed" && json.Unmarshal([]byte(e.Payload), &p) == nil && p.Kind == "assistant_text" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func waitFor(t *testing.T, timeout time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal(fmt.Sprintf("timed out after %s", timeout))
		}
		time.Sleep(500 * time.Millisecond)
	}
}
