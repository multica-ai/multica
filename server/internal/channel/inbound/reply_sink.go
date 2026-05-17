package inbound

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

type ChannelReplySink interface {
	SendText(ctx context.Context, evt port.InboundEvent, msg port.OutboundMessage) error
	SendRich(ctx context.Context, evt port.InboundEvent, msg port.OutboundRichMessage) error
}

type GatewayReplySink struct {
	gateway port.ChannelGateway
	store   channelconversation.Store
}

type GatewayReplySinkOption func(*GatewayReplySink)

func NewGatewayReplySink(gateway port.ChannelGateway, opts ...GatewayReplySinkOption) *GatewayReplySink {
	s := &GatewayReplySink{gateway: gateway}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// WithGatewayReplyConversationStore records successful provider sends as
// channel messages.
func WithGatewayReplyConversationStore(store channelconversation.Store) GatewayReplySinkOption {
	return func(s *GatewayReplySink) {
		s.store = store
	}
}

func (s *GatewayReplySink) SendText(ctx context.Context, evt port.InboundEvent, msg port.OutboundMessage) error {
	if s == nil || s.gateway == nil || msg.Text == "" {
		return nil
	}
	msg.Target = defaultReplyTarget(evt, msg.Target)
	result, err := s.gateway.SendText(ctx, evt.ConnectionID(), msg)
	if err == nil {
		s.recordSentText(ctx, evt, msg, result)
	}
	return err
}

func (s *GatewayReplySink) SendRich(ctx context.Context, evt port.InboundEvent, msg port.OutboundRichMessage) error {
	if s == nil || s.gateway == nil || (msg.Title == "" && msg.Body == "") {
		return nil
	}
	msg.Target = defaultReplyTarget(evt, msg.Target)
	result, err := s.gateway.SendRich(ctx, evt.ConnectionID(), msg)
	if err == nil {
		s.recordSentRich(ctx, evt, msg, result)
	}
	return err
}

func (s *GatewayReplySink) recordSentText(ctx context.Context, evt port.InboundEvent, msg port.OutboundMessage, result port.SendResult) {
	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: msg.Text})
	if err != nil {
		slog.Error("channel reply sink: marshal text message body failed", "error", err)
		return
	}
	s.recordSentMessage(ctx, evt, outboundReplyRecord{
		Target:             msg.Target,
		Text:               msg.Text,
		Body:               body,
		ContentFormat:      channelconversation.ContentFormatPlain,
		PlatformMessageID:  result.PlatformMessageID,
		HandoffText:        msg.Text,
		SuggestedActionSet: suggestedActionsForText(msg.Text),
	})
}

func (s *GatewayReplySink) recordSentRich(ctx context.Context, evt port.InboundEvent, msg port.OutboundRichMessage, result port.SendResult) {
	body, err := json.Marshal(struct {
		Title    string                 `json:"title"`
		Body     string                 `json:"body"`
		Actions  []port.OutboundAction  `json:"actions,omitempty"`
		Mentions []port.OutboundMention `json:"mentions,omitempty"`
	}{Title: msg.Title, Body: msg.Body, Actions: msg.Actions, Mentions: msg.Mentions})
	if err != nil {
		slog.Error("channel reply sink: marshal rich message body failed", "error", err)
		return
	}
	text := strings.TrimSpace(msg.Body)
	if text == "" {
		text = msg.Title
	}
	s.recordSentMessage(ctx, evt, outboundReplyRecord{
		Target:             msg.Target,
		Text:               text,
		Body:               body,
		ContentFormat:      channelconversation.ContentFormatCard,
		PlatformMessageID:  result.PlatformMessageID,
		HandoffText:        strings.TrimSpace(msg.Title + "\n" + msg.Body),
		SuggestedActionSet: suggestedActionsForText(msg.Title + "\n" + msg.Body),
	})
}

type outboundReplyRecord struct {
	Target             port.OutboundTarget
	Text               string
	Body               json.RawMessage
	ContentFormat      string
	PlatformMessageID  string
	HandoffText        string
	SuggestedActionSet []string
}

func (s *GatewayReplySink) recordSentMessage(ctx context.Context, evt port.InboundEvent, record outboundReplyRecord) {
	if s == nil || s.store == nil {
		return
	}
	inboundMsg, hasInbound, err := s.store.FindMessageByInboundEventID(ctx, evt.RuntimeEventID)
	if err != nil {
		slog.Error("channel reply sink: lookup inbound message failed", "runtime_event_id", evt.RuntimeEventID, "error", err)
		return
	}
	conversationID := inboundMsg.ConversationID
	workspaceID := inboundMsg.WorkspaceID
	chatID := replyChatID(evt, record.Target)
	chatType := normalizedRuntimeChatType(evt)
	if chatType == "" {
		chatType = string(port.ChatTypeGroup)
	}
	conversationType := chatType
	if strings.TrimSpace(evt.ThreadID) != "" {
		conversationType = channelconversation.ConversationTypeThread
	}
	if conversationID == "" {
		conv, err := s.store.EnsureConversation(ctx, channelconversation.Conversation{
			Provider:         evt.ChannelName,
			ConnectionID:     evt.ConnectionID(),
			ConversationKey:  ConversationKey(evt),
			ChatID:           chatID,
			ChatType:         chatType,
			ConversationType: conversationType,
			ExternalThreadID: evt.ThreadID,
			LastMessageAt:    time.Now().UTC(),
		})
		if err != nil {
			slog.Error("channel reply sink: ensure conversation failed", "connection_id", evt.ConnectionID(), "chat_id", chatID, "error", err)
			return
		}
		conversationID = conv.ID
		workspaceID = conv.WorkspaceID
	}
	metadata, err := json.Marshal(struct {
		SourceInboundEventID string `json:"source_inbound_event_id,omitempty"`
		SourceEventID        string `json:"source_event_id,omitempty"`
	}{SourceInboundEventID: evt.RuntimeEventID, SourceEventID: evt.EventID})
	if err != nil {
		slog.Error("channel reply sink: marshal metadata failed", "error", err)
		return
	}
	handoffKind := handoffKindForText(record.HandoffText)
	suggestedActions, err := json.Marshal(record.SuggestedActionSet)
	if err != nil {
		slog.Error("channel reply sink: marshal suggested actions failed", "error", err)
		return
	}
	msg, err := s.store.CreateMessage(ctx, channelconversation.Message{
		Provider:                 evt.ChannelName,
		ConnectionID:             evt.ConnectionID(),
		ConversationID:           conversationID,
		WorkspaceID:              workspaceID,
		ChatID:                   chatID,
		ChatType:                 chatType,
		ThreadID:                 evt.ThreadID,
		PlatformMessageID:        record.PlatformMessageID,
		Direction:                channelconversation.DirectionOutbound,
		MessageType:              channelconversation.MessageTypeBot,
		SenderType:               channelconversation.SenderTypeBot,
		Text:                     record.Text,
		Body:                     record.Body,
		ContentFormat:            record.ContentFormat,
		ReplyToPlatformMessageID: evt.MessageID,
		ReplyToMessageID:         inboundMessageID(hasInbound, inboundMsg.ID),
		HandoffKind:              handoffKind,
		SuggestedActions:         suggestedActions,
		Metadata:                 metadata,
		OccurredAt:               time.Now().UTC(),
	})
	if err != nil {
		slog.Error("channel reply sink: create outbound message failed", "runtime_event_id", evt.RuntimeEventID, "platform_message_id", record.PlatformMessageID, "error", err)
		return
	}
	if refs := entityRefsFromReplyText(workspaceID, record.HandoffText); len(refs) > 0 {
		if err := s.store.AddEntityRefs(ctx, msg.ID, refs); err != nil {
			slog.Error("channel reply sink: add outbound entity refs failed", "message_id", msg.ID, "error", err)
		}
	}
	if evt.RuntimeEventID != "" {
		resultPayload, _ := json.Marshal(struct {
			PlatformMessageID string `json:"platform_message_id,omitempty"`
		}{PlatformMessageID: record.PlatformMessageID})
		if err := s.store.CompleteTurnForInboundEvent(ctx, evt.RuntimeEventID, msg.ID, channelconversation.TurnStatusCompleted, resultPayload, ""); err != nil {
			slog.Error("channel reply sink: complete turn failed", "runtime_event_id", evt.RuntimeEventID, "message_id", msg.ID, "error", err)
		}
	}
}

func inboundMessageID(ok bool, id string) string {
	if !ok {
		return ""
	}
	return id
}

func entityRefsFromReplyText(workspaceID, text string) []channelconversation.EntityRef {
	entities := channelconversation.ExtractIssueEntityRefs(workspaceID, text, channelconversation.EntityRoleMentioned)
	if len(entities) == 0 {
		return nil
	}
	refs := make([]channelconversation.EntityRef, 0, len(entities))
	for _, entity := range entities {
		key := strings.ToUpper(strings.TrimSpace(entity.EntityKey))
		if key == "" {
			continue
		}
		refs = append(refs, channelconversation.EntityRef{
			WorkspaceID: workspaceID,
			EntityType:  channelconversation.EntityTypeIssue,
			EntityKey:   key,
			Display:     firstNonEmpty(entity.Display, key),
			Role:        channelconversation.EntityRoleMentioned,
		})
	}
	return refs
}

func handoffKindForText(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case strings.Contains(lower, "429") || strings.Contains(lower, "error") || strings.Contains(text, "失败") || strings.Contains(text, "重试"):
		return channelconversation.HandoffKindFailure
	case strings.Contains(text, "等待用户审批") || strings.Contains(text, "审批") || strings.Contains(strings.ToUpper(text), "PASS"):
		return channelconversation.HandoffKindApproval
	case strings.Contains(text, "继续推进") || strings.Contains(text, "继续"):
		return channelconversation.HandoffKindContinue
	default:
		return channelconversation.HandoffKindNone
	}
}

func suggestedActionsForText(text string) []string {
	switch handoffKindForText(text) {
	case channelconversation.HandoffKindFailure:
		return []string{"retry", "comment"}
	case channelconversation.HandoffKindApproval, channelconversation.HandoffKindContinue:
		return []string{"approve", "continue", "comment"}
	default:
		return []string{}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func defaultReplyTarget(evt port.InboundEvent, target port.OutboundTarget) port.OutboundTarget {
	if target.ID != "" {
		return target
	}
	if evt.ChatType == port.ChatTypeDirect {
		return port.TargetUser(evt.SenderID)
	}
	return port.TargetChat(evt.ChatID)
}

func replyChatID(evt port.InboundEvent, target port.OutboundTarget) string {
	if target.Type == port.OutboundTargetChat && strings.TrimSpace(target.ID) != "" {
		return target.ID
	}
	return evt.ChatID
}

var _ ChannelReplySink = (*GatewayReplySink)(nil)
