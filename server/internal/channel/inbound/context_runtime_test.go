package inbound

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/conversationctx"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/channel/replyctx"
)

// ---------------------------------------------------------------------------
// TC-10.1 / TC-10.3: ThreadID 透传
// ---------------------------------------------------------------------------

func TestApplyReplyContext_ThreadID(t *testing.T) {
	t.Parallel()
	store := &fakeReplyContextStore{}
	rt := NewRuntime(RuntimeConfig{ReplyContext: store})
	evt := port.InboundEvent{
		ChannelName: "feishu",
		ChatType:    port.ChatTypeDirect,
		SenderID:    "ou_1",
		ThreadID:    "m_root_1",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, "m_root_1")
	}
}

func TestApplyReplyContext_NonThreadMessage(t *testing.T) {
	t.Parallel()
	store := &fakeReplyContextStore{}
	rt := NewRuntime(RuntimeConfig{ReplyContext: store})
	evt := port.InboundEvent{
		ChannelName: "feishu",
		ChatType:    port.ChatTypeDirect,
		SenderID:    "ou_1",
		ThreadID:    "",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ThreadID != "" {
		t.Fatalf("ThreadID = %q, want empty", got.ThreadID)
	}
}

// ---------------------------------------------------------------------------
// TC-8.1 / TC-9.1: 显式信号透传（quote + reply-to）
// ---------------------------------------------------------------------------

func TestApplyReplyContext_ExplicitSignals(t *testing.T) {
	t.Parallel()
	store := &fakeReplyContextStore{}
	rt := NewRuntime(RuntimeConfig{ReplyContext: store})
	evt := port.InboundEvent{
		ChannelName:      "feishu",
		ChatType:         port.ChatTypeDirect,
		SenderID:         "ou_1",
		ThreadID:         "m_root",
		QuotedMessageID:  "m_quoted",
		QuotedText:       "quoted text",
		ReplyToMessageID: "m_parent",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ThreadID != "m_root" {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, "m_root")
	}
	if got.QuotedMessageID != "m_quoted" {
		t.Fatalf("QuotedMessageID = %q, want %q", got.QuotedMessageID, "m_quoted")
	}
	if got.QuotedText != "quoted text" {
		t.Fatalf("QuotedText = %q, want %q", got.QuotedText, "quoted text")
	}
	if got.ReplyToMessageID != "m_parent" {
		t.Fatalf("ReplyToMessageID = %q, want %q", got.ReplyToMessageID, "m_parent")
	}
}

// ---------------------------------------------------------------------------
// TC-9.5: 非飞书 inbound 事件，新字段自然留空
// ---------------------------------------------------------------------------

func TestApplyReplyContext_NonFeishuEvent(t *testing.T) {
	t.Parallel()
	store := &fakeReplyContextStore{}
	rt := NewRuntime(RuntimeConfig{ReplyContext: store})
	evt := port.InboundEvent{
		ChannelName: "slack",
		ChatType:    port.ChatTypeDirect,
		SenderID:    "u_1",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ThreadID != "" {
		t.Fatalf("ThreadID = %q, want empty", got.ThreadID)
	}
	if got.QuotedMessageID != "" {
		t.Fatalf("QuotedMessageID = %q, want empty", got.QuotedMessageID)
	}
	if got.QuotedText != "" {
		t.Fatalf("QuotedText = %q, want empty", got.QuotedText)
	}
	if got.ReplyToMessageID != "" {
		t.Fatalf("ReplyToMessageID = %q, want empty", got.ReplyToMessageID)
	}
}

// ---------------------------------------------------------------------------
// TC-10.4: thread 消息但 context_entries store 未 wire-up（TODO 占位期）
// ---------------------------------------------------------------------------

func TestApplyReplyContext_ThreadIDWithoutContextStore(t *testing.T) {
	t.Parallel()
	// ReplyContext is nil but ThreadID should still be passed through.
	rt := NewRuntime(RuntimeConfig{})
	evt := port.InboundEvent{
		ChannelName: "feishu",
		ChatType:    port.ChatTypeDirect,
		SenderID:    "ou_1",
		ThreadID:    "m_root_1",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, "m_root_1")
	}
}

func TestApplyReplyContext_ConversationContextEntities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := conversationctx.NewFakeStore()
	scope := conversationctx.Scope{
		ConnectionID: "conn-1",
		WorkspaceID:  "ws-1",
		ChatID:       "chat-1",
		SenderID:     "ou_1",
		ThreadID:     "thread-1",
	}
	if err := store.Upsert(ctx, conversationctx.ConversationContext{
		Scope: scope,
		Entities: []conversationctx.EntityRef{{
			Key:         "STA-12",
			Type:        conversationctx.EntityTypeIssue,
			MentionedAt: time.Now(),
		}},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	rt := NewRuntime(RuntimeConfig{ConversationCtx: store})
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChannelName:         "feishu",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_1",
		ThreadID:            "thread-1",
	}
	req := chintent.IntentRequest{WorkspaceID: "ws-1"}
	got := rt.applyReplyContext(ctx, req, evt, &ChatBindingContext{WorkspaceID: "ws-1"})
	if len(got.ContextEntities) != 1 || got.ContextEntities[0].Key != "STA-12" {
		t.Fatalf("ContextEntities = %+v, want STA-12", got.ContextEntities)
	}
	if got.ContextIssueKey != "STA-12" {
		t.Fatalf("ContextIssueKey = %q, want STA-12", got.ContextIssueKey)
	}
	if got.ContextMode != "conversation" {
		t.Fatalf("ContextMode = %q, want conversation", got.ContextMode)
	}
}

func TestApplyReplyContext_QuotedTextEntityTakesPriority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := conversationctx.NewFakeStore()
	scope := conversationctx.Scope{
		ConnectionID: "conn-1",
		WorkspaceID:  "ws-1",
		ChatID:       "chat-1",
		SenderID:     "ou_1",
		ThreadID:     "",
	}
	if err := store.Upsert(ctx, conversationctx.ConversationContext{
		Scope: scope,
		Entities: []conversationctx.EntityRef{{
			Key:         "STA-12",
			Type:        conversationctx.EntityTypeIssue,
			MentionedAt: time.Now(),
		}},
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	rt := NewRuntime(RuntimeConfig{ConversationCtx: store})
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChannelName:         "feishu",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_1",
		QuotedText:          "STA-99 已经创建",
	}
	req := chintent.IntentRequest{WorkspaceID: "ws-1"}
	got := rt.applyReplyContext(ctx, req, evt, &ChatBindingContext{WorkspaceID: "ws-1"})
	if got.ContextIssueKey != "STA-99" {
		t.Fatalf("ContextIssueKey = %q, want quoted STA-99", got.ContextIssueKey)
	}
	if len(got.ExplicitEntities) != 1 || got.ExplicitEntities[0].Key != "STA-99" {
		t.Fatalf("ExplicitEntities = %+v, want quoted STA-99", got.ExplicitEntities)
	}
	if len(got.ContextEntities) != 1 || got.ContextEntities[0].Key != "STA-12" {
		t.Fatalf("ContextEntities = %+v, want temporal STA-12", got.ContextEntities)
	}
}

func TestApplyRequestContextToIntentResult_ExplicitEntityTakesPriority(t *testing.T) {
	t.Parallel()
	req := chintent.IntentRequest{
		ExplicitEntities: []conversationctx.EntityRef{{
			Key:  "STA-99",
			Type: conversationctx.EntityTypeIssue,
		}},
		ContextEntities: []conversationctx.EntityRef{{
			Key:  "STA-12",
			Type: conversationctx.EntityTypeIssue,
		}},
	}
	result := chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:   chintent.IntentSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToIntentResult(result, req)
	if got.Intent.Params["issue_key"] != "STA-99" {
		t.Fatalf("issue_key = %q, want explicit STA-99", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToIntentResult_FillsSingleContextIssue(t *testing.T) {
	t.Parallel()
	req := chintent.IntentRequest{
		ContextIssueKey: "STA-12",
		ContextEntities: []conversationctx.EntityRef{{
			Key:  "STA-12",
			Type: conversationctx.EntityTypeIssue,
		}},
	}
	result := chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:   chintent.IntentSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToIntentResult(result, req)
	if got.Intent.Params["issue_key"] != "STA-12" {
		t.Fatalf("issue_key = %q, want STA-12", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToIntentResult_DoesNotGuessMultipleContextIssues(t *testing.T) {
	t.Parallel()
	req := chintent.IntentRequest{
		ContextEntities: []conversationctx.EntityRef{
			{Key: "STA-12", Type: conversationctx.EntityTypeIssue},
			{Key: "STA-13", Type: conversationctx.EntityTypeIssue},
		},
	}
	result := chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:   chintent.IntentSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToIntentResult(result, req)
	if got.Intent.Params["issue_key"] != "" {
		t.Fatalf("issue_key = %q, want empty", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToIntentResult_DoesNotFallbackWhenExplicitAmbiguous(t *testing.T) {
	t.Parallel()
	req := chintent.IntentRequest{
		ExplicitEntities: []conversationctx.EntityRef{
			{Key: "STA-99", Type: conversationctx.EntityTypeIssue},
			{Key: "STA-100", Type: conversationctx.EntityTypeIssue},
		},
		ContextEntities: []conversationctx.EntityRef{
			{Key: "STA-12", Type: conversationctx.EntityTypeIssue},
		},
	}
	result := chintent.IntentResult{
		Matched: true,
		Intent: chintent.Intent{
			Kind:   chintent.IntentSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToIntentResult(result, req)
	if got.Intent.Params["issue_key"] != "" {
		t.Fatalf("issue_key = %q, want empty for ambiguous explicit context", got.Intent.Params["issue_key"])
	}
}

// ---------------------------------------------------------------------------
// 旧逻辑兼容：replyctx.Lookup 仍填充 ContextIssueKey / ContextMode
// ---------------------------------------------------------------------------

func TestApplyReplyContext_LegacyReplyContext(t *testing.T) {
	t.Parallel()
	store := &fakeReplyContextStore{
		replyCtx: replyctx.Context{
			IssueIdentifier: "STA-78",
			WorkspaceID:     pgtype.UUID{},
		},
		ok: true,
	}
	rt := NewRuntime(RuntimeConfig{ReplyContext: store})
	evt := port.InboundEvent{
		ChannelName:     "feishu",
		ChatType:        port.ChatTypeDirect,
		SenderID:        "ou_1",
		ThreadID:        "m_root_1",
		QuotedMessageID: "m_q",
	}
	req := chintent.IntentRequest{}
	chatCtx := ChatBindingContext{}
	got := rt.applyReplyContext(context.Background(), req, evt, &chatCtx)
	if got.ContextIssueKey != "STA-78" {
		t.Fatalf("ContextIssueKey = %q, want STA-78", got.ContextIssueKey)
	}
	if got.ContextMode != "reply" {
		t.Fatalf("ContextMode = %q, want reply", got.ContextMode)
	}
	if got.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want m_root_1", got.ThreadID)
	}
	if got.QuotedMessageID != "m_q" {
		t.Fatalf("QuotedMessageID = %q, want m_q", got.QuotedMessageID)
	}
}

type fakeReplyContextStore struct {
	replyCtx replyctx.Context
	ok       bool
	err      error
}

func (s *fakeReplyContextStore) Lookup(ctx context.Context, connectionID, senderID, chatID string, at time.Time) (replyctx.Context, bool, error) {
	return s.replyCtx, s.ok, s.err
}

func (s *fakeReplyContextStore) Save(ctx context.Context, connectionID, senderID, issueIdentifier string) error {
	return nil
}

func (s *fakeReplyContextStore) Clear(ctx context.Context, connectionID, senderID, chatID string) error {
	return nil
}

func (s *fakeReplyContextStore) Upsert(ctx context.Context, item replyctx.Context) error {
	return nil
}
