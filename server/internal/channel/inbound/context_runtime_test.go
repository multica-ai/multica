package inbound

import (
	"context"
	"encoding/json"
	"testing"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/port"
	chturn "github.com/multica-ai/multica/server/internal/channel/turn"
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
	got := rt.applyMessageContext(context.Background(), chturn.Request{}, evt)
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
	got := rt.applyMessageContext(context.Background(), chturn.Request{}, evt)
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
	req := chturn.Request{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
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

func TestApplyMessageContext_FiltersRecentEntitiesByWorkspacePrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		recentRefs: []channelconversation.EntityRef{
			{
				EntityType: channelconversation.EntityTypeIssue,
				EntityKey:  "AC-2",
				Role:       channelconversation.EntityRoleMentioned,
			},
			{
				EntityType: channelconversation.EntityTypeIssue,
				EntityKey:  "STA-82",
				Role:       channelconversation.EntityRoleMentioned,
			},
			{
				EntityType: channelconversation.EntityTypeIssue,
				EntityKey:  "AC-5",
				Role:       channelconversation.EntityRoleMentioned,
			},
		},
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
	req := chturn.Request{InboundEventID: "evt-row-1", WorkspaceID: "ws-1", IssuePrefix: "STA"}
	got := rt.applyMessageContext(ctx, req, evt)
	if len(got.ContextEntities) != 1 || got.ContextEntities[0].EntityKey != "STA-82" {
		t.Fatalf("ContextEntities = %+v, want only STA-82", got.ContextEntities)
	}
	if got.ContextIssueKey != "STA-82" {
		t.Fatalf("ContextIssueKey = %q, want STA-82", got.ContextIssueKey)
	}
}

func TestApplyMessageContext_IncludesPendingActionFromLatestTurn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	state, _ := json.Marshal(chturn.StatePayload{
		PendingAction: &chturn.PendingAction{
			Kind:       string(chaction.KindSetStatus),
			Params:     map[string]string{"status": "cancelled"},
			Missing:    []string{"issue_key"},
			Candidates: []string{"STA-82"},
			Question:   "Which issue should I cancel?",
		},
	})
	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ConversationID: "00000000-0000-0000-0000-000000000001"},
		},
		latestTurn: channelconversation.Turn{
			ID:            "turn-1",
			ResultPayload: state,
		},
		latestTurnOK: true,
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
	req := chturn.Request{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
	got := rt.applyMessageContext(ctx, req, evt)
	if got.PendingAction == nil {
		t.Fatal("PendingAction is nil, want prior cancellation clarification")
	}
	if got.PendingAction.Kind != string(chaction.KindSetStatus) || got.PendingAction.Params["status"] != "cancelled" {
		t.Fatalf("PendingAction = %+v, want SetStatus cancelled", got.PendingAction)
	}
	if len(got.PendingAction.Candidates) != 1 || got.PendingAction.Candidates[0] != "STA-82" {
		t.Fatalf("PendingAction candidates = %+v, want STA-82", got.PendingAction.Candidates)
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
	req := chturn.Request{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
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
	req := chturn.Request{InboundEventID: "evt-row-1", WorkspaceID: "ws-1"}
	got := rt.applyMessageContext(ctx, req, evt)
	if len(got.ExplicitEntities) != 1 || got.ExplicitEntities[0].EntityKey != "STA-88" {
		t.Fatalf("ExplicitEntities = %+v, want quoted message STA-88", got.ExplicitEntities)
	}
	if got.ContextIssueKey != "STA-88" {
		t.Fatalf("ContextIssueKey = %q, want STA-88", got.ContextIssueKey)
	}
}

func TestApplyRequestContextToCommandResult_ExplicitEntityTakesPriority(t *testing.T) {
	t.Parallel()
	req := chturn.Request{
		ExplicitEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-99",
			EntityType: channelconversation.EntityTypeIssue,
		}},
		ContextEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-12",
			EntityType: channelconversation.EntityTypeIssue,
		}},
	}
	result := chaction.Result{
		Matched: true,
		Intent: chaction.Intent{
			Kind:   chaction.KindSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToCommandResult(result, req)
	if got.Intent.Params["issue_key"] != "STA-99" {
		t.Fatalf("issue_key = %q, want explicit STA-99", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToCommandResult_FillsSingleContextIssue(t *testing.T) {
	t.Parallel()
	req := chturn.Request{
		ContextIssueKey: "STA-12",
		ContextEntities: []channelconversation.EntityRef{{
			EntityKey:  "STA-12",
			EntityType: channelconversation.EntityTypeIssue,
		}},
	}
	result := chaction.Result{
		Matched: true,
		Intent: chaction.Intent{
			Kind:   chaction.KindSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToCommandResult(result, req)
	if got.Intent.Params["issue_key"] != "STA-12" {
		t.Fatalf("issue_key = %q, want STA-12", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToCommandResult_DoesNotGuessMultipleContextIssues(t *testing.T) {
	t.Parallel()
	req := chturn.Request{
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-12", EntityType: channelconversation.EntityTypeIssue},
			{EntityKey: "STA-13", EntityType: channelconversation.EntityTypeIssue},
		},
	}
	result := chaction.Result{
		Matched: true,
		Intent: chaction.Intent{
			Kind:   chaction.KindSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToCommandResult(result, req)
	if got.Intent.Params["issue_key"] != "" {
		t.Fatalf("issue_key = %q, want empty", got.Intent.Params["issue_key"])
	}
}

func TestApplyRequestContextToCommandResult_DoesNotFallbackWhenExplicitAmbiguous(t *testing.T) {
	t.Parallel()
	req := chturn.Request{
		ExplicitEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-99", EntityType: channelconversation.EntityTypeIssue},
			{EntityKey: "STA-100", EntityType: channelconversation.EntityTypeIssue},
		},
		ContextEntities: []channelconversation.EntityRef{
			{EntityKey: "STA-12", EntityType: channelconversation.EntityTypeIssue},
		},
	}
	result := chaction.Result{
		Matched: true,
		Intent: chaction.Intent{
			Kind:   chaction.KindSetStatus,
			Params: map[string]string{"status": "done"},
		},
	}
	got := applyRequestContextToCommandResult(result, req)
	if got.Intent.Params["issue_key"] != "" {
		t.Fatalf("issue_key = %q, want empty for ambiguous explicit context", got.Intent.Params["issue_key"])
	}
}
