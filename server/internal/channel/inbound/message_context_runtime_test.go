// Package inbound tests message-context reply resolution for channel handoff
// turns.
//
// Responsibilities:
//   - Verify short user replies can resolve explicit channel message context.
//   - Verify ordinary short chat is not promoted into an issue comment.
//
// Boundaries:
//   - Uses an in-memory conversation store fake.
//   - Does not exercise provider sending or database migrations.
package inbound

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	chintent "github.com/multica-ai/multica/server/internal/channel/intent"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestResolveMessageContextReply_QuotedHandoffCreatesCommentIntent(t *testing.T) {
	t.Parallel()

	agentID := "11111111-1111-1111-1111-111111111111"
	store := &fakeConversationStore{
		byPlatform: map[string]channelconversation.Message{
			"conn-1\x00om-1": {
				ID:                "msg-target",
				ConnectionID:      "conn-1",
				ConversationID:    "conv-1",
				PlatformMessageID: "om-1",
				Direction:         channelconversation.DirectionOutbound,
				MessageType:       channelconversation.MessageTypeNotification,
				HandoffKind:       channelconversation.HandoffKindFailure,
			},
		},
		refs: map[string][]channelconversation.EntityRef{
			"msg-target": {
				{EntityType: channelconversation.EntityTypeIssue, EntityKey: "STA-9", Role: channelconversation.EntityRolePrimary},
				{EntityType: channelconversation.EntityTypeAgent, EntityID: agentID, Display: "ReviewBot", Role: channelconversation.EntityRoleHandoffTarget},
			},
		},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		Text:                "重试",
		ReplyToMessageID:    "om-1",
	}

	result, ok, err := rt.resolveMessageContextReply(context.Background(), &InboundEventRecord{ID: "evt-row-1"}, evt)
	if err != nil {
		t.Fatalf("resolveMessageContextReply: %v", err)
	}
	if !ok {
		t.Fatal("expected quoted handoff reply to resolve")
	}
	if result.Intent.Kind != chintent.IntentAddComment {
		t.Fatalf("intent kind = %s, want AddComment", result.Intent.Kind)
	}
	if result.Intent.Params["issue_key"] != "STA-9" {
		t.Fatalf("issue_key = %q, want STA-9", result.Intent.Params["issue_key"])
	}
	comment := result.Intent.Params["comment"]
	if comment != "重试一次 [@ReviewBot](mention://agent/"+agentID+")" {
		t.Fatalf("comment = %q", comment)
	}
}

func TestResolveMessageContextReply_ExplicitMissingContextDoesNotFallbackToRecent(t *testing.T) {
	t.Parallel()

	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ID: "msg-inbound", ConversationID: "conv-1"},
		},
		recent: []channelconversation.Message{
			{
				ID:             "msg-recent",
				ConversationID: "conv-1",
				Direction:      channelconversation.DirectionOutbound,
				MessageType:    channelconversation.MessageTypeNotification,
				HandoffKind:    channelconversation.HandoffKindApproval,
			},
		},
		refs: map[string][]channelconversation.EntityRef{
			"msg-recent": {{EntityType: channelconversation.EntityTypeIssue, EntityKey: "STA-9"}},
		},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		Text:                "OK",
		ReplyToMessageID:    "provider-message-that-was-not-recorded",
	}

	_, ok, err := rt.resolveMessageContextReply(context.Background(), &InboundEventRecord{ID: "evt-row-1"}, evt)
	if err != nil {
		t.Fatalf("resolveMessageContextReply: %v", err)
	}
	if ok {
		t.Fatal("explicit quote/reply miss must not fallback to recent handoff")
	}
}

func TestResolveMessageContextReply_NonHandoffTargetDoesNotResolve(t *testing.T) {
	t.Parallel()

	store := &fakeConversationStore{
		byPlatform: map[string]channelconversation.Message{
			"conn-1\x00om-1": {
				ID:                "msg-target",
				ConnectionID:      "conn-1",
				ConversationID:    "conv-1",
				PlatformMessageID: "om-1",
				Direction:         channelconversation.DirectionInbound,
				MessageType:       channelconversation.MessageTypeUser,
				HandoffKind:       channelconversation.HandoffKindNone,
				Text:              "STA-9",
			},
		},
		refs: map[string][]channelconversation.EntityRef{
			"msg-target": {{EntityType: channelconversation.EntityTypeIssue, EntityKey: "STA-9"}},
		},
	}
	rt := NewRuntime(RuntimeConfig{ConversationStore: store})
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		Text:                "同意",
		ReplyToMessageID:    "om-1",
	}

	_, ok, err := rt.resolveMessageContextReply(context.Background(), &InboundEventRecord{ID: "evt-row-1"}, evt)
	if err != nil {
		t.Fatalf("resolveMessageContextReply: %v", err)
	}
	if ok {
		t.Fatal("plain issue-bearing messages must not become handoff reply targets")
	}
}

func TestResolveMessageContextReply_PlainShortReplyWithoutContextDoesNotResolve(t *testing.T) {
	t.Parallel()

	rt := NewRuntime(RuntimeConfig{ConversationStore: &fakeConversationStore{}})
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		Type:                port.EventTypeMessageReceived,
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		Text:                "OK",
	}

	_, ok, err := rt.resolveMessageContextReply(context.Background(), &InboundEventRecord{ID: "evt-row-1"}, evt)
	if err != nil {
		t.Fatalf("resolveMessageContextReply: %v", err)
	}
	if ok {
		t.Fatal("plain short reply without explicit or recent handoff context must not resolve")
	}
}

type fakeConversationStore struct {
	byPlatform    map[string]channelconversation.Message
	byInbound     map[string]channelconversation.Message
	refs          map[string][]channelconversation.EntityRef
	recentRefs    []channelconversation.EntityRef
	recent        []channelconversation.Message
	upsertedTurns []channelconversation.Turn
}

func (f *fakeConversationStore) EnsureConversation(context.Context, channelconversation.Conversation) (channelconversation.Conversation, error) {
	return channelconversation.Conversation{}, nil
}

func (f *fakeConversationStore) CreateMessage(context.Context, channelconversation.Message) (channelconversation.Message, error) {
	return channelconversation.Message{}, nil
}

func (f *fakeConversationStore) UpdateMessageForInboundEvent(context.Context, string, channelconversation.Message) error {
	return nil
}

func (f *fakeConversationStore) AddEntityRefs(context.Context, string, []channelconversation.EntityRef) error {
	return nil
}

func (f *fakeConversationStore) FindMessageByPlatformID(_ context.Context, connectionID, platformMessageID string) (channelconversation.Message, bool, error) {
	if f.byPlatform == nil {
		return channelconversation.Message{}, false, nil
	}
	msg, ok := f.byPlatform[connectionID+"\x00"+platformMessageID]
	return msg, ok, nil
}

func (f *fakeConversationStore) FindMessageByInboundEventID(_ context.Context, inboundEventID string) (channelconversation.Message, bool, error) {
	if f.byInbound == nil {
		return channelconversation.Message{}, false, nil
	}
	msg, ok := f.byInbound[inboundEventID]
	return msg, ok, nil
}

func (f *fakeConversationStore) ListEntityRefsByMessageID(_ context.Context, messageID string) ([]channelconversation.EntityRef, error) {
	return append([]channelconversation.EntityRef(nil), f.refs[messageID]...), nil
}

func (f *fakeConversationStore) ListRecentContextEntityRefs(context.Context, string, string, string, string, time.Time, int) ([]channelconversation.EntityRef, error) {
	return append([]channelconversation.EntityRef(nil), f.recentRefs...), nil
}

func (f *fakeConversationStore) ListRecentHandoffMessages(context.Context, string, string, string, time.Time, int) ([]channelconversation.Message, error) {
	return append([]channelconversation.Message(nil), f.recent...), nil
}

func (f *fakeConversationStore) CreateTurn(context.Context, channelconversation.Turn) (channelconversation.Turn, error) {
	return channelconversation.Turn{}, nil
}

func (f *fakeConversationStore) UpsertTurn(_ context.Context, turn channelconversation.Turn) (channelconversation.Turn, error) {
	f.upsertedTurns = append(f.upsertedTurns, turn)
	return turn, nil
}

func (f *fakeConversationStore) CompleteTurn(context.Context, string, string, string, json.RawMessage, string) error {
	return nil
}

func (f *fakeConversationStore) CompleteTurnForInboundEvent(context.Context, string, string, string, json.RawMessage, string) error {
	return nil
}

var _ channelconversation.Store = (*fakeConversationStore)(nil)
