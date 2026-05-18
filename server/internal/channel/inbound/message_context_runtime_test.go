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
	"strings"
	"testing"
	"time"

	chaction "github.com/multica-ai/multica/server/internal/channel/action"
	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
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
	if result.Intent.Kind != chaction.KindAddComment {
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

func TestResolveMessageContextReply_RecentHandoffLookupUsesSenderScope(t *testing.T) {
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
	}

	result, ok, err := rt.resolveMessageContextReply(context.Background(), &InboundEventRecord{ID: "evt-row-1"}, evt)
	if err != nil {
		t.Fatalf("resolveMessageContextReply: %v", err)
	}
	if !ok {
		t.Fatal("expected single scoped recent handoff to resolve")
	}
	if result.Intent.Params["issue_key"] != "STA-9" {
		t.Fatalf("issue_key = %q, want STA-9", result.Intent.Params["issue_key"])
	}
	if store.lastRecentSender != "ou-user" {
		t.Fatalf("recent handoff sender scope = %q, want ou-user", store.lastRecentSender)
	}
}

func TestGatewayReplySink_DoesNotInferHandoffFromQueryText(t *testing.T) {
	t.Parallel()

	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {
				ID:             "msg-inbound",
				ConversationID: "conv-1",
				WorkspaceID:    "11111111-1111-1111-1111-111111111111",
			},
		},
	}
	sink := NewGatewayReplySink(fakeReplyGateway{}, WithGatewayReplyConversationStore(store))
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		RuntimeEventID:      "evt-row-1",
		EventID:             "evt-1",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		MessageID:           "om-inbound",
	}

	err := sink.SendRich(context.Background(), evt, port.OutboundRichMessage{
		Title: "Issue STA-9",
		Body:  "继续展开：/timeline STA-9 /logs STA-9",
	})
	if err != nil {
		t.Fatalf("SendRich: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created messages = %d, want 1", len(store.created))
	}
	msg := store.created[0]
	if msg.HandoffKind != channelconversation.HandoffKindNone {
		t.Fatalf("handoff_kind = %q, want none", msg.HandoffKind)
	}
	var actions []string
	if err := json.Unmarshal(msg.SuggestedActions, &actions); err != nil {
		t.Fatalf("unmarshal suggested_actions: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("suggested_actions = %#v, want empty", actions)
	}
}

func TestGatewayReplySink_RecordsStructuredHandoff(t *testing.T) {
	t.Parallel()

	store := &fakeConversationStore{
		byInbound: map[string]channelconversation.Message{
			"evt-row-1": {ID: "msg-inbound", ConversationID: "conv-1"},
		},
	}
	sink := NewGatewayReplySink(fakeReplyGateway{}, WithGatewayReplyConversationStore(store))
	evt := port.InboundEvent{
		ChannelName:         "feishu",
		ChannelConnectionID: "conn-1",
		RuntimeEventID:      "evt-row-1",
		EventID:             "evt-1",
		ChatID:              "chat-1",
		ChatType:            port.ChatTypeGroup,
		SenderID:            "ou-user",
		MessageID:           "om-inbound",
	}

	err := sink.SendText(context.Background(), evt, port.OutboundMessage{
		Text:             "Agent 失败，可重试。",
		HandoffKind:      channelconversation.HandoffKindFailure,
		SuggestedActions: []string{"retry", "comment"},
	})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("created messages = %d, want 1", len(store.created))
	}
	msg := store.created[0]
	if msg.HandoffKind != channelconversation.HandoffKindFailure {
		t.Fatalf("handoff_kind = %q, want failure", msg.HandoffKind)
	}
	var actions []string
	if err := json.Unmarshal(msg.SuggestedActions, &actions); err != nil {
		t.Fatalf("unmarshal suggested_actions: %v", err)
	}
	if got, want := strings.Join(actions, ","), "retry,comment"; got != want {
		t.Fatalf("suggested_actions = %q, want %q", got, want)
	}
}

type fakeConversationStore struct {
	byPlatform       map[string]channelconversation.Message
	byInbound        map[string]channelconversation.Message
	refs             map[string][]channelconversation.EntityRef
	recentRefs       []channelconversation.EntityRef
	recent           []channelconversation.Message
	latestTurn       channelconversation.Turn
	latestTurnOK     bool
	latestReset      channelconversation.Turn
	latestResetOK    bool
	created          []channelconversation.Message
	upsertedTurns    []channelconversation.Turn
	mergedTurnStates []json.RawMessage
	lastRecentSender string
	lastContextSince time.Time
	lastPendingSince time.Time
}

func (f *fakeConversationStore) EnsureConversation(context.Context, channelconversation.Conversation) (channelconversation.Conversation, error) {
	return channelconversation.Conversation{}, nil
}

func (f *fakeConversationStore) CreateMessage(_ context.Context, msg channelconversation.Message) (channelconversation.Message, error) {
	if msg.ID == "" {
		msg.ID = "created-message"
	}
	f.created = append(f.created, msg)
	return msg, nil
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

func (f *fakeConversationStore) ListRecentContextEntityRefs(_ context.Context, _, _, _, _ string, since time.Time, _ int) ([]channelconversation.EntityRef, error) {
	f.lastContextSince = since
	if f.latestResetOK && !f.latestReset.CompletedAt.IsZero() && !since.Before(f.latestReset.CompletedAt) {
		return nil, nil
	}
	return append([]channelconversation.EntityRef(nil), f.recentRefs...), nil
}

func (f *fakeConversationStore) ListRecentHandoffMessages(_ context.Context, _, _, senderExternalID, _ string, _ time.Time, _ int) ([]channelconversation.Message, error) {
	f.lastRecentSender = senderExternalID
	return append([]channelconversation.Message(nil), f.recent...), nil
}

func (f *fakeConversationStore) FindLatestCompletedTurn(_ context.Context, _, _, _, _ string, since time.Time) (channelconversation.Turn, bool, error) {
	f.lastPendingSince = since
	if f.latestTurnOK && !f.latestTurn.CompletedAt.IsZero() && f.latestTurn.CompletedAt.Before(since) {
		return channelconversation.Turn{}, false, nil
	}
	return f.latestTurn, f.latestTurnOK, nil
}

func (f *fakeConversationStore) FindLatestContextReset(context.Context, string, string, string, string, time.Time) (channelconversation.Turn, bool, error) {
	return f.latestReset, f.latestResetOK, nil
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

func (f *fakeConversationStore) MergeTurnResultForInboundEvent(_ context.Context, _ string, payload json.RawMessage) error {
	f.mergedTurnStates = append(f.mergedTurnStates, append(json.RawMessage(nil), payload...))
	return nil
}

var _ channelconversation.Store = (*fakeConversationStore)(nil)

type fakeReplyGateway struct{}

func (fakeReplyGateway) SendText(context.Context, string, port.OutboundMessage) (port.SendResult, error) {
	return port.SendResult{PlatformMessageID: "om-outbound"}, nil
}

func (fakeReplyGateway) SendRich(context.Context, string, port.OutboundRichMessage) (port.SendResult, error) {
	return port.SendResult{PlatformMessageID: "om-outbound"}, nil
}

func (fakeReplyGateway) GetChatInfo(context.Context, string, string) (port.ChatInfo, error) {
	return port.ChatInfo{}, nil
}

func (fakeReplyGateway) GetUserInfo(context.Context, string, string) (port.UserInfo, error) {
	return port.UserInfo{}, nil
}

func (fakeReplyGateway) FileDownloader(string) (port.FileDownloader, bool) {
	return nil, false
}
