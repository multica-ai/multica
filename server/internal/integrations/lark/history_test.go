package lark

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// --- fakes ---

type historyFakeQueries struct {
	binding    ChatSessionBinding
	bindingErr error
	inst       Installation
	instErr    error
}

func (f historyFakeQueries) GetLarkChatSessionBindingBySession(_ context.Context, _ pgtype.UUID) (ChatSessionBinding, error) {
	return f.binding, f.bindingErr
}

func (f historyFakeQueries) GetLarkInstallation(_ context.Context, _ pgtype.UUID) (Installation, error) {
	return f.inst, f.instErr
}

type historyFakeCreds struct{ err error }

func (f historyFakeCreds) DecryptAppSecret(_ Installation) (string, error) {
	return "plaintext-secret", f.err
}

// historyFakeClient implements APIClient: it records ListContainerMessages
// calls and returns canned pages, and resolves names from a fixed map.
type historyFakeClient struct {
	APIClient // embedded stub for the unused methods
	calls     []ListContainerParams
	result    ListContainerResult
	listErr   error
	names     map[string]string
}

func newHistoryFakeClient() *historyFakeClient {
	return &historyFakeClient{APIClient: NewStubAPIClient(nil)}
}

func (f *historyFakeClient) ListContainerMessages(_ context.Context, _ InstallationCredentials, p ListContainerParams) (ListContainerResult, error) {
	f.calls = append(f.calls, p)
	if f.listErr != nil {
		return ListContainerResult{}, f.listErr
	}
	return f.result, nil
}

func (f *historyFakeClient) BatchGetUsers(_ context.Context, _ InstallationCredentials, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if n := f.names[id]; n != "" {
			out[id] = n
		}
	}
	return out, nil
}

func historyMsg(id, sender, senderType, createTime, threadID, body string) LarkMessage {
	return LarkMessage{
		MessageID:   id,
		MessageType: "text",
		Content:     `{"text":"` + body + `"}`,
		SenderID:    sender,
		SenderType:  senderType,
		CreateTime:  createTime,
		ThreadID:    threadID,
	}
}

func activeInstall() Installation {
	return Installation{
		Status:    string(InstallationActive),
		AppID:     "cli_app",
		BotOpenID: "ou_bot",
		Region:    "feishu",
	}
}

func newTestHistory(q historyFakeQueries, c *historyFakeClient) *History {
	return NewHistory(q, historyFakeCreds{}, c, nil)
}

// --- tests ---

func TestHistoryChannelOverview(t *testing.T) {
	client := newHistoryFakeClient()
	// Lark returns newest-first; the reader must sort oldest-first.
	client.result = ListContainerResult{
		Messages: []LarkMessage{
			historyMsg("m2", "cli_app", "app", "2000", "", "on it"),       // bot reply
			historyMsg("m1", "ou_alice", "user", "1000", "th9", "deploy"), // a topic message
		},
		PageToken: "next-tok",
	}
	client.names = map[string]string{"ou_alice": "Alice"}

	h := newTestHistory(historyFakeQueries{
		binding: ChatSessionBinding{ChannelChatID: "oc_chat"},
		inst:    activeInstall(),
	}, client)

	page, err := h.ChannelOverview(context.Background(), pgtype.UUID{}, channel.HistoryOptions{Limit: 5, Before: "cur"})
	if err != nil {
		t.Fatalf("ChannelOverview: %v", err)
	}
	if page.ChannelType != "feishu" {
		t.Errorf("ChannelType = %q, want feishu", page.ChannelType)
	}
	if page.NextCursor != "next-tok" {
		t.Errorf("NextCursor = %q, want next-tok (Lark page_token)", page.NextCursor)
	}
	if len(client.calls) != 1 || client.calls[0].ContainerType != "chat" ||
		client.calls[0].ContainerID != "oc_chat" || client.calls[0].PageToken != "cur" || client.calls[0].PageSize != 5 {
		t.Fatalf("unexpected list call: %+v", client.calls)
	}
	if len(page.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d: %+v", len(page.Messages), page.Messages)
	}
	// Oldest-first: the topic message (ts 1000) before the bot reply (ts 2000).
	first, second := page.Messages[0], page.Messages[1]
	if first.ID != "m1" || first.Role != channel.HistoryRoleUser || first.Author != "Alice" {
		t.Errorf("first message wrong: %+v", first)
	}
	if first.Text != "deploy" || first.ThreadID != "th9" {
		t.Errorf("overview did not carry topic tag / text: %+v", first)
	}
	if second.ID != "m2" || second.Role != channel.HistoryRoleAssistant || second.Author != "Bot" {
		t.Errorf("bot reply not tagged as assistant: %+v", second)
	}
}

func TestHistoryThreadCurrentTopic(t *testing.T) {
	client := newHistoryFakeClient()
	client.result = ListContainerResult{Messages: []LarkMessage{historyMsg("m1", "ou_a", "user", "1", "th1", "hi")}}

	h := newTestHistory(historyFakeQueries{
		binding: ChatSessionBinding{ChannelChatID: "oc_chat", LastThreadID: pgtype.Text{String: "th1", Valid: true}},
		inst:    activeInstall(),
	}, client)

	page, err := h.Thread(context.Background(), pgtype.UUID{}, "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].ContainerType != "thread" || client.calls[0].ContainerID != "th1" {
		t.Fatalf("expected thread read of th1, got: %+v", client.calls)
	}
	if page.ThreadID != "th1" {
		t.Errorf("page.ThreadID = %q, want th1", page.ThreadID)
	}
	// A thread read must not tag rows with ThreadID (that is overview-only).
	if len(page.Messages) != 1 || page.Messages[0].ThreadID != "" {
		t.Errorf("thread-read row should not carry ThreadID: %+v", page.Messages)
	}
}

func TestHistoryThreadFallsBackToChat(t *testing.T) {
	client := newHistoryFakeClient()
	client.result = ListContainerResult{Messages: []LarkMessage{historyMsg("m1", "ou_a", "user", "1", "", "hi")}}

	// No last_thread_id and no explicit id: fall back to the linear chat read.
	h := newTestHistory(historyFakeQueries{
		binding: ChatSessionBinding{ChannelChatID: "oc_chat"},
		inst:    activeInstall(),
	}, client)

	page, err := h.Thread(context.Background(), pgtype.UUID{}, "", channel.HistoryOptions{})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	if len(client.calls) != 1 || client.calls[0].ContainerType != "chat" || client.calls[0].ContainerID != "oc_chat" {
		t.Fatalf("expected chat fallback, got: %+v", client.calls)
	}
	if page.ThreadID != "" {
		t.Errorf("fallback page.ThreadID = %q, want empty", page.ThreadID)
	}
}

func TestHistoryThreadByID(t *testing.T) {
	client := newHistoryFakeClient()
	client.result = ListContainerResult{Messages: []LarkMessage{historyMsg("m1", "ou_a", "user", "1", "tX", "hi")}}
	h := newTestHistory(historyFakeQueries{
		binding: ChatSessionBinding{ChannelChatID: "oc_chat", LastThreadID: pgtype.Text{String: "th1", Valid: true}},
		inst:    activeInstall(),
	}, client)

	if _, err := h.Thread(context.Background(), pgtype.UUID{}, "tX", channel.HistoryOptions{}); err != nil {
		t.Fatalf("Thread: %v", err)
	}
	// An explicit id overrides the session's own topic.
	if len(client.calls) != 1 || client.calls[0].ContainerType != "thread" || client.calls[0].ContainerID != "tX" {
		t.Fatalf("expected thread read of tX, got: %+v", client.calls)
	}
}

func TestHistoryNoBinding(t *testing.T) {
	h := newTestHistory(historyFakeQueries{bindingErr: pgx.ErrNoRows}, newHistoryFakeClient())
	if _, err := h.ChannelOverview(context.Background(), pgtype.UUID{}, channel.HistoryOptions{}); !errors.Is(err, channel.ErrNoChannelSession) {
		t.Fatalf("want ErrNoChannelSession, got %v", err)
	}
}

func TestHistoryInactiveInstall(t *testing.T) {
	inst := activeInstall()
	inst.Status = string(InstallationRevoked)
	h := newTestHistory(historyFakeQueries{binding: ChatSessionBinding{ChannelChatID: "oc_chat"}, inst: inst}, newHistoryFakeClient())
	if _, err := h.ChannelOverview(context.Background(), pgtype.UUID{}, channel.HistoryOptions{}); !errors.Is(err, channel.ErrNoChannelSession) {
		t.Fatalf("revoked install should read as no-session, got %v", err)
	}
}

// A missing-scope read (Lark code 230027, e.g. group history without
// im:message.group_msg) is permanent, so it degrades to an actionable
// HistoryUnavailableError (→ 200 + note), NOT a retryable error.
func TestHistoryMissingPermissionDegrades(t *testing.T) {
	client := newHistoryFakeClient()
	client.listErr = &APIError{Op: "http 400", Code: 230027, Msg: "Lack of necessary permissions, ext=need scope: im:message.group_msg"}
	h := newTestHistory(historyFakeQueries{binding: ChatSessionBinding{ChannelChatID: "oc_chat"}, inst: activeInstall()}, client)

	_, err := h.ChannelOverview(context.Background(), pgtype.UUID{}, channel.HistoryOptions{})
	var un *channel.HistoryUnavailableError
	if !errors.As(err, &un) {
		t.Fatalf("missing scope should yield HistoryUnavailableError, got %v", err)
	}
	if !strings.Contains(un.Reason, "im:message.group_msg") {
		t.Errorf("note should surface the missing scope, got %q", un.Reason)
	}
	// The Thread path degrades the same way.
	if _, terr := h.Thread(context.Background(), pgtype.UUID{}, "", channel.HistoryOptions{}); !errors.As(terr, &un) {
		t.Fatalf("Thread should also degrade on missing scope, got %v", terr)
	}
}

// A transient / unclassified read failure stays a plain error (→ 502), never a
// note — the agent should not silently proceed on an ambiguous outage.
func TestHistoryTransientErrorStaysError(t *testing.T) {
	client := newHistoryFakeClient()
	client.listErr = errors.New("http 500: boom")
	h := newTestHistory(historyFakeQueries{binding: ChatSessionBinding{ChannelChatID: "oc_chat"}, inst: activeInstall()}, client)

	_, err := h.ChannelOverview(context.Background(), pgtype.UUID{}, channel.HistoryOptions{})
	if err == nil {
		t.Fatal("want an error")
	}
	var un *channel.HistoryUnavailableError
	if errors.As(err, &un) {
		t.Fatalf("a transient error must NOT degrade to a note: %v", err)
	}
}
