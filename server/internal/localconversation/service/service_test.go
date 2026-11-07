package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

type serviceFixture struct {
	svc      *Service
	path     string
	scopeID  int64
	mu       sync.Mutex
	adapters []*fakeAdapter
	// reply, when set, answers requests of adapters started after it.
	reply func(a *fakeAdapter, typ string) (json.RawMessage, error)
	// beforeLaunch, when set, runs while the provider CLI is being resolved.
	beforeLaunch func()
}

func newServiceFixture(t *testing.T, path string) *serviceFixture {
	t.Helper()
	return newServiceFixtureWith(t, path, Options{})
}

// newServiceFixtureWith opens the service with opts plus the fixture's
// path and Claude launcher.
func newServiceFixtureWith(t *testing.T, path string, opts Options) *serviceFixture {
	t.Helper()
	f := &serviceFixture{path: path}
	opts.Path, opts.Claude = path, f.launch
	svc, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close(context.Background()) })
	f.svc = svc
	f.scopeID, err = svc.Store().EnsureScope(context.Background(), store.Scope{APIOrigin: "https://api.multica.ai", UserID: "u", WorkspaceID: "w"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *serviceFixture) launch(_ context.Context, conv sqlitedb.Conversation) (ClaudeSession, StartClaudeAdapter, error) {
	f.mu.Lock()
	before := f.beforeLaunch
	f.mu.Unlock()
	if before != nil {
		before()
	}
	return ClaudeSession{Fields: map[string]any{"executable": "/bin/claude", "cwd": conv.Cwd}}, func(_ context.Context, h claude.Handler) (ClaudeAdapter, error) {
		a := &fakeAdapter{handler: h, done: make(chan struct{})}
		f.mu.Lock()
		if reply := f.reply; reply != nil {
			a.reply = func(typ string) (json.RawMessage, error) { return reply(a, typ) }
		}
		f.adapters = append(f.adapters, a)
		f.mu.Unlock()
		return a, nil
	}, nil
}

func (f *serviceFixture) create(t *testing.T, id, provider string) {
	t.Helper()
	if _, err := f.svc.Store().CreateConversation(context.Background(), store.NewConversation{ID: id, ScopeID: f.scopeID, Provider: provider, Cwd: "/repo", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

func TestServiceSendIsIdempotentPerRequestID(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	cmd := SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "req-1", Text: "hi"}
	first, err := f.svc.Send(ctx, cmd)
	if err != nil || first.Replayed {
		t.Fatalf("first send = %+v, %v", first, err)
	}
	again, err := f.svc.Send(ctx, cmd)
	if err != nil || !again.Replayed || again.TurnID != first.TurnID {
		t.Fatalf("retry = %+v, %v; want replay of %s", again, err, first.TurnID)
	}
	if got := f.adapters[0].sent(); fmt.Sprint(got) != "[start send]" {
		t.Fatalf("retry reached the adapter: %v", got)
	}
	cmd.Text = "different"
	if _, err := f.svc.Send(ctx, cmd); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("reused id with new content = %v", err)
	}
}

func TestDrainRefusesNewTurnsAndReportsRunningOnes(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	f.create(t, "conv-2", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	busy, err := f.svc.Drain(ctx)
	if err != nil || busy != 1 {
		t.Fatalf("Drain = %d, %v; want the running turn", busy, err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-2", RequestID: "r2", Text: "hi"}); !errors.Is(err, ErrDraining) {
		t.Fatalf("send while draining = %v", err)
	}
	if err := f.svc.Undrain(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-2", RequestID: "r2", Text: "hi"}); err != nil {
		t.Fatalf("send after undrain = %v", err)
	}
}

func TestServiceChecksScopeAndProvider(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	f.create(t, "conv-codex", "codex")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID + 1, ConversationID: "conv-1", RequestID: "r", Text: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("send from another scope = %v", err)
	}
	if _, err := f.svc.Snapshot(ctx, f.scopeID+1, "conv-1", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("snapshot from another scope = %v", err)
	}
	if _, _, err := f.svc.Subscribe(ctx, f.scopeID+1, "conv-1", 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("subscribe from another scope = %v", err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-codex", RequestID: "r", Text: "x"}); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("send to codex = %v", err)
	}
}

func TestServicePublishesCommittedEventsToSubscribers(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	sub, _, err := f.svc.Subscribe(ctx, f.scopeID, "conv-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "req-1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	f.adapters[0].emit(t, `{"type":"result","subtype":"success","is_error":false,"result":""}`)

	deadline, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var types []string
	for len(types) == 0 || types[len(types)-1] != "turn.completed" {
		events, err := sub.Next(deadline)
		if err != nil {
			t.Fatalf("Next: %v (got %v)", err, types)
		}
		for _, e := range events {
			types = append(types, e.Type)
		}
	}
	if fmt.Sprint(types) != "[user.message turn.started turn.completed]" {
		t.Fatalf("subscriber saw %v", types)
	}
}

func TestOpenClosesTurnsInterruptedByARestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "c.sqlite")
	ctx := context.Background()
	// A previous daemon left a running turn with a half-streamed answer, an
	// unfinished command and one finished item.
	st, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	scopeID, _ := st.EnsureScope(ctx, store.Scope{APIOrigin: "o", UserID: "u", WorkspaceID: "w"})
	if _, err := st.CreateConversation(ctx, store.NewConversation{ID: "conv-1", ScopeID: scopeID, Provider: "claude", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w := st.NewWriter(store.WriterOptions{})
	ev := func(item, typ string, final bool, payload string) store.NewEvent {
		return store.NewEvent{TurnID: "turn-1", ItemID: item, Type: typ, Final: final, Payload: json.RawMessage(payload), CreatedAt: time.Now()}
	}
	_, err = w.Submit(ctx, store.Batch{ConversationID: "conv-1", StartTurn: &store.NewTurn{ID: "turn-1", StartedAt: time.Now()}, Events: []store.NewEvent{
		ev("m1:0", "item.started", false, `{"kind":"assistant_text"}`),
		ev("m1:0", "item.delta", false, `{"kind":"assistant_text","field":"text","text":"half "}`),
		ev("m1:0", "item.delta", false, `{"kind":"assistant_text","field":"text","text":"answer"}`),
		ev("tu-1", "item.started", false, `{"kind":"command","command":"sleep 9"}`),
		ev("tu-2", "item.started", false, `{"kind":"tool_call","tool":"Read"}`),
		ev("tu-2", "item.completed", true, `{"kind":"tool_call","status":"completed","tool":"Read","output":"ok"}`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	_ = st.Close()

	f := newServiceFixture(t, path)
	events, err := f.svc.Store().TurnEvents(ctx, "conv-1", "turn-1")
	if err != nil {
		t.Fatal(err)
	}
	var tail []string
	for _, e := range events[6:] {
		var p map[string]any
		_ = json.Unmarshal([]byte(e.Payload), &p)
		tail = append(tail, fmt.Sprintf("%s %s %v %v", e.Type, e.ItemID.String, p["status"], p["text"]))
	}
	want := []string{"item.completed m1:0 failed half answer", "item.completed tu-1 failed <nil>", "turn.completed  failed <nil>"}
	if fmt.Sprint(tail) != fmt.Sprint(want) {
		t.Fatalf("reconciled events = %v, want %v", tail, want)
	}
	conv, _ := f.svc.Store().Conversation(ctx, "conv-1")
	if conv.Status != "failed" {
		t.Fatalf("conversation status = %s", conv.Status)
	}
	if running, _ := f.svc.Store().RunningTurns(ctx); len(running) != 0 {
		t.Fatalf("turns still running: %+v", running)
	}
}

func TestServiceUpdateSettingsPersistsAndAppliesToTheLiveAdapter(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	opus, auto := "claude-opus-5-5", "auto"
	// Before any adapter runs, only the stored settings change.
	if err := f.svc.UpdateSettings(ctx, f.scopeID, "conv-1", SettingsUpdate{Model: &opus}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	a := f.adapters[0]
	if a.fields[0]["model"] != opus {
		t.Fatalf("adapter started with %v, want model %s", a.fields[0], opus)
	}

	if err := f.svc.UpdateSettings(ctx, f.scopeID, "conv-1", SettingsUpdate{ClearModel: true, PermissionMode: &auto}); err != nil {
		t.Fatal(err)
	}
	if got := a.sent(); fmt.Sprint(got) != "[start send set_permission_mode set_model apply_flag_settings]" {
		t.Fatalf("adapter requests = %v", got)
	}
	if a.fields[3]["model"] != nil || a.fields[2]["mode"] != "auto" {
		t.Fatalf("applied fields = %v / %v", a.fields[3], a.fields[2])
	}
	conv, _ := f.svc.Store().Conversation(ctx, "conv-1")
	if conv.Model.Valid || conv.PermissionMode != "auto" {
		t.Fatalf("stored settings = model %v permission %s", conv.Model, conv.PermissionMode)
	}

	bad := "yolo"
	if err := f.svc.UpdateSettings(ctx, f.scopeID, "conv-1", SettingsUpdate{PermissionMode: &bad}); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("invalid permission mode = %v", err)
	}
	if err := f.svc.UpdateSettings(ctx, f.scopeID+1, "conv-1", SettingsUpdate{Model: &opus}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update from another scope = %v", err)
	}
}

func TestServiceGivesAdaptersTheContextTools(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv-1", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	start := f.adapters[0].fields[0]
	entry := start["mcp_servers"].(map[string]any)[contextmcp.Name].(map[string]any)
	if fmt.Sprint(start["allowed_tools"]) != "[mcp__"+contextmcp.Name+"__*]" || entry["type"] != "http" {
		t.Fatalf("start fields = %v", start)
	}
	url := entry["url"].(string)
	auth := entry["headers"].(map[string]any)["Authorization"].(string)

	call := func() map[string]any {
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"current_context"}}`
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		req.Header.Set("Authorization", auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return out
	}
	out := call()
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var current map[string]any
	_ = json.Unmarshal([]byte(text), &current)
	if current["conversation_id"] != "conv-1" || current["turn_id"] != "turn-r1" || current["workspace_id"] != "w" {
		t.Fatalf("current_context = %v", current)
	}

	// The route ends with the adapter.
	f.adapters[0].exit(errors.New("gone"))
	deadline := time.Now().Add(2 * time.Second)
	for {
		if res := call(); res["result"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("context route still served after the adapter exited")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestServiceGivesCloudContextAPrivateDirectory(t *testing.T) {
	t.Parallel()

	f := &serviceFixture{}
	dirs := make(chan string, 1)
	svc, err := Open(context.Background(), Options{Path: filepath.Join(t.TempDir(), "c.sqlite"), Claude: f.launch,
		Cloud: func(scope store.Scope, dir string) contextmcp.Cloud {
			if scope.WorkspaceID != "w" {
				t.Errorf("cloud scope = %+v", scope)
			}
			dirs <- dir
			return nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close(context.Background())
	f.svc = svc
	ctx := context.Background()
	f.scopeID, _ = svc.Store().EnsureScope(ctx, store.Scope{APIOrigin: "https://api.multica.ai", UserID: "u", WorkspaceID: "w"})
	f.create(t, "conv-1", "claude")
	if _, err := svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	dir := <-dirs
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("run dir %s: %v", dir, err)
	}
	f.adapters[0].exit(errors.New("gone"))
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("run directory kept after the adapter exited")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSystemPromptDescribesTheExecution(t *testing.T) {
	t.Parallel()

	withCloud, offline := systemPrompt("ws-1", true), systemPrompt("ws-1", false)
	for _, want := range []string{`workspace ID is "ws-1"`, "ask_user_question", "fenced svg code block", "Markdown image syntax"} {
		if !strings.Contains(withCloud, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if !strings.Contains(offline, "currently unavailable") || strings.Contains(withCloud, "currently unavailable") {
		t.Fatal("cloud availability is not described")
	}
}

func TestSystemPromptRecommendsSideWithoutRestrictingExternalBrowsers(t *testing.T) {
	t.Parallel()
	for _, cloud := range []bool{false, true} {
		prompt := systemPrompt("ws-1", cloud)
		for _, want := range []string{
			"Side workspace tools can read and control only tabs still open in this conversation's Side workspace",
			"Prefer the built-in Side browser",
			"External browsers and browser automation tools",
			"are allowed when the user requests them or they better fit the task",
		} {
			if !strings.Contains(prompt, want) {
				t.Errorf("cloud=%v: browser guidance lacks %q", cloud, want)
			}
		}
		for _, obsolete := range []string{
			"The current conversation can read and control only tabs",
			"Never open a separate browser",
			"Side webpage tasks continue to use side_browser_*",
		} {
			if strings.Contains(prompt, obsolete) {
				t.Errorf("cloud=%v: browser guidance still restricts tool selection: %q", cloud, obsolete)
			}
		}
	}
}

func TestReferencesAreCurrentContextWhileTheTurnRuns(t *testing.T) {
	t.Parallel()

	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	refs := []Reference{{Kind: "session", ID: "conv-2"}, {Kind: "issue", ID: createTaskMention}}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r1", Text: "hi", References: refs}); err != nil {
		t.Fatal(err)
	}
	f.svc.mu.Lock()
	r := f.svc.runners["conv-1"].(*ClaudeRunner)
	f.svc.mu.Unlock()
	if got := r.currentContext()["references"]; !reflect.DeepEqual(got, []Reference{{Kind: "session", ID: "conv-2"}}) {
		t.Fatalf("current references = %+v", got)
	}
	bad := make([]Reference, 6)
	for i := range bad {
		bad[i] = Reference{Kind: "agent", ID: "a"}
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "r2", Text: "x", References: bad}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("too many references = %v", err)
	}
}

func TestMemberReferenceReachesProviderAndCurrentContext(t *testing.T) {
	t.Parallel()
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	ctx := context.Background()
	f.create(t, "conv-1", "claude")
	text := "Assign to [@Alex](mention://workspace-member/user-b?workspaceId=w&userId=user-b)"
	refs := []Reference{{Kind: "member", ID: "user-b", WorkspaceID: "w"}}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "member-send", Text: text, References: refs}); err != nil {
		t.Fatal(err)
	}
	if got := f.sends(t); len(got) != 1 || got[0] != text {
		t.Fatalf("provider text = %v", got)
	}
	f.svc.mu.Lock()
	r := f.svc.runners["conv-1"].(*ClaudeRunner)
	f.svc.mu.Unlock()
	if got := r.currentContext()["references"]; !reflect.DeepEqual(got, refs) {
		t.Fatalf("current references = %+v", got)
	}
	for _, workspaceID := range []string{"", "other-workspace"} {
		_, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv-1", RequestID: "invalid-" + workspaceID, Text: text, References: []Reference{{Kind: "member", ID: "user-b", WorkspaceID: workspaceID}}})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("workspace %q: %v", workspaceID, err)
		}
	}
	if !strings.Contains(systemPrompt("w", true), "assignee_type=member and assignee_id=user_id") {
		t.Fatal("member assignment identity is not described")
	}
}
