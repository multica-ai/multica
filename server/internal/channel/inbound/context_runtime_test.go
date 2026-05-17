package inbound

import (
	"context"
	"testing"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestApplyMessageContext_ThreadID(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(RuntimeConfig{})
	evt := port.InboundEvent{
		ChannelName: "feishu",
		ChatType:    port.ChatTypeDirect,
		SenderID:    "ou_1",
		ThreadID:    "m_root_1",
	}
	got := rt.applyMessageContext(context.Background(), chintent.IntentRequest{}, evt)
	if got.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, "m_root_1")
	}
}

func TestApplyMessageContext_ExplicitSignals(t *testing.T) {
	t.Parallel()
	rt := NewRuntime(RuntimeConfig{})
	evt := port.InboundEvent{
		ChannelName:      "feishu",
		ChatType:         port.ChatTypeDirect,
		SenderID:         "ou_1",
		ThreadID:         "m_root",
		QuotedMessageID:  "m_quoted",
		QuotedText:       "quoted text STA-99",
		ReplyToMessageID: "m_parent",
	}
	got := rt.applyMessageContext(context.Background(), chintent.IntentRequest{}, evt)
	if got.ThreadID != "m_root" {
		t.Fatalf("ThreadID = %q, want %q", got.ThreadID, "m_root")
	}
	if got.QuotedMessageID != "m_quoted" {
		t.Fatalf("QuotedMessageID = %q, want %q", got.QuotedMessageID, "m_quoted")
	}
	if got.QuotedText != "quoted text STA-99" {
		t.Fatalf("QuotedText = %q, want %q", got.QuotedText, "quoted text STA-99")
	}
	if got.ReplyToMessageID != "m_parent" {
		t.Fatalf("ReplyToMessageID = %q, want %q", got.ReplyToMessageID, "m_parent")
	}
	if len(got.ExplicitEntities) != 1 || got.ExplicitEntities[0].EntityKey != "STA-99" {
		t.Fatalf("ExplicitEntities = %+v, want quoted STA-99", got.ExplicitEntities)
	}
}

func TestApplyMessageContext_RecentMessageEntities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		recentRefs: []channelconversation.EntityRef{{
			EntityType: channelconversation.EntityTypeIssue,
			EntityKey:  "STA-12",
			Role:       channelconversation.EntityRoleMentioned,
		}},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChannelName:         "feishu",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_1",
		ThreadID:            "thread-1",
	}
	req := chintent.IntentRequest{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
	got := rt.applyMessageContext(ctx, req, evt)
	if len(got.ContextEntities) != 1 || got.ContextEntities[0].EntityKey != "STA-12" {
		t.Fatalf("ContextEntities = %+v, want STA-12", got.ContextEntities)
	}
	if got.ContextIssueKey != "STA-12" {
		t.Fatalf("ContextIssueKey = %q, want STA-12", got.ContextIssueKey)
	}
	if got.ContextMode != "message" {
		t.Fatalf("ContextMode = %q, want message", got.ContextMode)
	}
}

func TestApplyMessageContext_QuotedTextEntityTakesPriority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		recentRefs: []channelconversation.EntityRef{{
			EntityType: channelconversation.EntityTypeIssue,
			EntityKey:  "STA-12",
			Role:       channelconversation.EntityRoleMentioned,
		}},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChannelName:         "feishu",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_1",
		QuotedText:          "STA-99 已经创建",
	}
	req := chintent.IntentRequest{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
	got := rt.applyMessageContext(ctx, req, evt)
	if got.ContextIssueKey != "STA-99" {
		t.Fatalf("ContextIssueKey = %q, want quoted STA-99", got.ContextIssueKey)
	}
	if len(got.ExplicitEntities) != 1 || got.ExplicitEntities[0].EntityKey != "STA-99" {
		t.Fatalf("ExplicitEntities = %+v, want quoted STA-99", got.ExplicitEntities)
	}
	if len(got.ContextEntities) != 1 || got.ContextEntities[0].EntityKey != "STA-12" {
		t.Fatalf("ContextEntities = %+v, want temporal STA-12", got.ContextEntities)
	}
}

func TestApplyMessageContext_QuotedMessageEntityRefsAreExplicit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeConversationStore{
		byPlatform: map[string]channelconversation.Message{
			"conn-1\x00msg-card": {ID: "msg-1"},
		},
		refs: map[string][]channelconversation.EntityRef{
			"msg-1": {{
				EntityType: channelconversation.EntityTypeIssue,
				EntityKey:  "STA-88",
				Role:       channelconversation.EntityRolePrimary,
			}},
		},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelConnectionID: "conn-1",
		ChannelName:         "feishu",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou_1",
		QuotedMessageID:     "msg-card",
		QuotedText:          "Review PASS",
	}
	req := chintent.IntentRequest{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
	got := rt.applyMessageContext(ctx, req, evt)
	if len(got.ExplicitEntities) != 1 || got.ExplicitEntities[0].EntityKey != "STA-88" {
		t.Fatalf("ExplicitEntities = %+v, want quoted message STA-88", got.ExplicitEntities)
	}
	if got.ContextIssueKey != "STA-88" {
		t.Fatalf("ContextIssueKey = %q, want STA-88", got.ContextIssueKey)
	}
}

func TestApplyRequestContextToIntentResult_ExplicitEntityTakesPriority(t *testing.T) {
	t.Parallel()
	req := chintent.IntentRequest{
		ExplicitEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-99",
			EntityType: channelconversation.EntityTypeIssue,
		}},
		ContextEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-12",
			EntityType: channelconversation.EntityTypeIssue,
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
		ContextEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-12",
			EntityType: channelconversation.EntityTypeIssue,
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
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-12", EntityType: channelconversation.EntityTypeIssue},
			{EntityKey: "STA-13", EntityType: channelconversation.EntityTypeIssue},
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
		ExplicitEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-99", EntityType: channelconversation.EntityTypeIssue},
			{EntityKey: "STA-100", EntityType: channelconversation.EntityTypeIssue},
		},
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-12", EntityType: channelconversation.EntityTypeIssue},
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
