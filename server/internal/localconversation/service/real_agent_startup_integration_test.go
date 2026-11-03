//go:build agentintegration

package service

import (
	"context"
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
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
)

// TestRealAgentStartupSmoke drives spares, preheating and permission changes
// against the installed CLIs. It uses the user's account, so it is gated like
// TestRealAgentLocalConversationSmoke.
func TestRealAgentStartupSmoke(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow real agent CLI and account access")
	}
	providers := []string{"claude", "codex"}
	if v := os.Getenv("MULTICA_SMOKE_PROVIDERS"); v != "" {
		providers = strings.Split(v, ",")
	}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) { realStartupSmoke(t, provider) })
	}
}

func realStartupSmoke(t *testing.T, provider string) {
	executable, err := exec.LookPath(provider)
	if err != nil {
		t.Skipf("%s is not installed: %v", provider, err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	root := t.TempDir()
	timingPath := filepath.Join(root, "timing.jsonl")
	opts := Options{Path: filepath.Join(root, "c.sqlite"), Logger: logger, TimingPath: timingPath}
	conditions := func(context.Context) SpareConditions {
		return SpareConditions{Ready: true, Executable: executable, Binary: executable, Identity: "smoke"}
	}
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
		start := func(ctx context.Context, h claude.Handler) (ClaudeAdapter, error) {
			return claude.Start(ctx, claude.AdapterCommand(node, entry, os.Environ()), h, logger)
		}
		opts.Claude = func(_ context.Context, conv sqlitedb.Conversation) (ClaudeSession, StartClaudeAdapter, error) {
			return ClaudeSession{Fields: map[string]any{"executable": executable, "cwd": conv.Cwd, "env": ClaudeEnv(os.Environ())}}, start, nil
		}
		opts.ClaudeSpare = &ClaudeSpareLauncher{Conditions: conditions, Start: start, Fields: func(c SpareConditions) map[string]any {
			return map[string]any{"executable": c.Executable, "env": ClaudeEnv(os.Environ()), "allowed_tools": []string{"mcp__multica_desktop_context__*"}}
		}}
	case "codex":
		opts.Codex = func(_ context.Context, conv sqlitedb.Conversation) (CodexSession, StartCodexServer, error) {
			return CodexSession{Cwd: conv.Cwd}, func(ctx context.Context, h codex.Handler, env []string) (CodexServer, error) {
				return codex.Start(ctx, codex.Command{Path: executable, Dir: conv.Cwd, Env: append(os.Environ(), env...)}, h, logger)
			}, nil
		}
		spareDir := filepath.Join(root, "spare")
		if err := os.Mkdir(spareDir, 0o700); err != nil {
			t.Fatal(err)
		}
		opts.CodexSpare = &CodexSpareLauncher{Conditions: conditions, Start: func(ctx context.Context, c SpareConditions, h codex.Handler, env []string) (CodexServer, error) {
			return codex.Start(ctx, codex.Command{Path: c.Executable, Dir: spareDir, Env: append(os.Environ(), env...)}, h, logger)
		}}
	}

	ctx := context.Background()
	svc, err := Open(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	disconnected := svc.ClientConnected("smoke")
	t.Cleanup(disconnected)
	scopeID, err := svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "smoke", WorkspaceID: "smoke"})
	if err != nil {
		t.Fatal(err)
	}
	parked := func() {
		t.Helper()
		slot := svc.spares.slots[provider]
		waitFor(t, 2*time.Minute, func() bool {
			slot.mu.Lock()
			defer slot.mu.Unlock()
			return slot.spare != nil
		})
	}
	marks := func() string {
		data, _ := os.ReadFile(timingPath)
		return string(data)
	}
	create := func(id string, permission string) sqlitedb.Conversation {
		t.Helper()
		conv, err := svc.Create(ctx, CreateCommand{ScopeID: scopeID, ID: id, Provider: provider})
		if err != nil {
			t.Fatal(err)
		}
		update := SettingsUpdate{PermissionMode: &permission}
		if provider == "claude" {
			model := "haiku"
			update.Model = &model
		} else {
			effort := "low"
			update.ReasoningEffort = &effort
		}
		if err := svc.UpdateSettings(ctx, scopeID, id, update); err != nil {
			t.Fatal(err)
		}
		return conv
	}
	send := func(id, requestID, text string) (string, store.TurnView) {
		t.Helper()
		res, err := svc.Send(ctx, SendCommand{ScopeID: scopeID, ConversationID: id, RequestID: requestID, Text: text})
		if err != nil {
			t.Fatalf("send %s: %v", requestID, err)
		}
		reply := waitTurn(t, svc, scopeID, id, res.TurnID, "completed")
		view, _ := findTurn(t, svc, scopeID, id, res.TurnID)
		return reply, view
	}
	touch := func(id, requestID, name string) bool {
		t.Helper()
		// Outside $TMPDIR, which workspace-write sandboxes may write to.
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(home, ".multica-smoke-"+filepath.Base(root)+"-"+name)
		t.Cleanup(func() { _ = os.Remove(file) })
		res, err := svc.Send(ctx, SendCommand{ScopeID: scopeID, ConversationID: id, RequestID: requestID, Text: "Run exactly this shell command once: touch " + file + " . Do not retry or work around a failure. Then reply done."})
		if err != nil {
			t.Fatalf("send %s: %v", requestID, err)
		}
		waitFor(t, 3*time.Minute, func() bool {
			v, ok := findTurn(t, svc, scopeID, id, res.TurnID)
			return ok && v.Turn.Status != "running" && v.Turn.Status != "queued"
		})
		_, statErr := os.Stat(file)
		return statErr == nil
	}

	// A spare serves a new conversation, and its first turn reaches context tools.
	parked()
	conv := create("smoke-spare", "default")
	reply, _ := send(conv.ID, "s1", "Remember the word PELICAN. Call the current_context tool of the multica_desktop_context MCP server once, then reply with only the current conversation ID it returned.")
	t.Logf("spare reply: %q", reply)
	// Only the tool knows this ID, and context tool calls are not shown as items.
	if !strings.Contains(reply, conv.ID) {
		t.Fatalf("the first turn on a spare did not reach the context tool: %q", reply)
	}
	waitFor(t, 30*time.Second, func() bool {
		return strings.Contains(marks(), timing.PrepareSpare) && strings.Contains(marks(), "spare.claimed")
	})

	// A reclaimed conversation is preheated when focused, and keeps its history.
	svc.mu.Lock()
	r := svc.runners[conv.ID]
	svc.mu.Unlock()
	evictable, ok := r.(interface{ Evict(context.Context) bool })
	if !ok || !evictable.Evict(ctx) {
		t.Fatal("the idle runner was not evicted")
	}
	if err := svc.SetFocus(ctx, scopeID, conv.ID, "smoke", true, timing.Caller{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, time.Minute, func() bool { return strings.Contains(marks(), "preheat.finished") })
	recall, _ := send(conv.ID, "s2", "Which word did I ask you to remember? Reply with only the word.")
	if !strings.Contains(strings.ToUpper(recall), "PELICAN") {
		t.Fatalf("preheated turn lost the history: %q", recall)
	}
	waitFor(t, 30*time.Second, func() bool { return strings.Contains(marks(), timing.PreparePreheated) })
	if err := svc.SetFocus(ctx, scopeID, conv.ID, "smoke", false, timing.Caller{}); err != nil {
		t.Fatal(err)
	}

	// Permissions follow the settings, including on spares and after a change.
	parked()
	full := create("smoke-full", "full-access")
	if !touch(full.ID, "p1", "full-1") {
		t.Fatal("full access could not write outside the project")
	}
	auto := "auto"
	if err := svc.UpdateSettings(ctx, scopeID, full.ID, SettingsUpdate{PermissionMode: &auto}); err != nil {
		t.Fatal(err)
	}
	if provider == "codex" && touch(full.ID, "p2", "lowered") {
		t.Fatal("lowering from full access still wrote outside the project")
	}
	if provider == "claude" {
		parked()
		plain := create("smoke-default", "default")
		if touch(plain.ID, "p3", "default-1") {
			t.Fatal("default mode on a spare skipped the permission check")
		}
	}
}
