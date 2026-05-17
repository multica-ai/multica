// Package outbound records durable outbound notification sends into the
// channel conversation model.
//
// Responsibilities:
//   - Convert successfully sent outbox cards into channel_message facts.
//   - Attach issue, comment, inbox item, and agent references to the message.
//
// Boundaries:
//   - Does not send provider messages.
//   - Does not interpret inbound user replies or dispatch issue actions.
package outbound

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	channelconversation "github.com/multica-ai/multica/server/internal/channel/conversation"
	"github.com/multica-ai/multica/server/internal/channel/port"
	"github.com/multica-ai/multica/server/internal/util"
)

type ConversationMessageRecorder struct {
	store channelconversation.Store
}

// NewConversationMessageRecorder creates an outbox send recorder backed by
// the channel conversation store.
func NewConversationMessageRecorder(store channelconversation.Store) *ConversationMessageRecorder {
	return &ConversationMessageRecorder{store: store}
}

// RecordSentNotification persists the provider-assigned message id and the
// business context behind a delivered outbox notification card.
func (r *ConversationMessageRecorder) RecordSentNotification(ctx context.Context, group notificationGroup, payload RetryPayload, result port.SendResult) error {
	if r == nil || r.store == nil || len(group.items) == 0 {
		return nil
	}
	chatID, chatType, conversationType := notificationConversationShape(group.target)
	workspaceID := commonWorkspaceID(group.items)
	conv, err := r.store.EnsureConversation(ctx, channelconversation.Conversation{
		Provider:         group.provider,
		ConnectionID:     group.connectionID,
		ConversationKey:  notificationConversationKey(group.connectionID, group.target),
		ChatID:           chatID,
		ChatType:         chatType,
		ConversationType: conversationType,
		WorkspaceID:      workspaceID,
		LastMessageAt:    time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	body, err := marshalNotificationMessageBody(group, payload)
	if err != nil {
		return err
	}
	metadata, err := marshalNotificationMessageMetadata(group)
	if err != nil {
		return err
	}
	representedAgentID := commonActorAgentID(group.items)
	handoffKind, suggestedActions := notificationHandoff(group.items)
	msg, err := r.store.CreateMessage(ctx, channelconversation.Message{
		Provider:               group.provider,
		ConnectionID:           group.connectionID,
		ConversationID:         conv.ID,
		WorkspaceID:            workspaceID,
		ChatID:                 chatID,
		ChatType:               chatType,
		PlatformMessageID:      result.PlatformMessageID,
		OutboundNotificationID: singleNotificationID(group.items),
		Direction:              channelconversation.DirectionOutbound,
		MessageType:            channelconversation.MessageTypeNotification,
		SenderType:             channelconversation.SenderTypeBot,
		RepresentedAgentID:     representedAgentID,
		Text:                   payload.Body,
		Body:                   body,
		ContentFormat:          channelconversation.ContentFormatCard,
		HandoffKind:            handoffKind,
		SuggestedActions:       suggestedActions,
		Metadata:               metadata,
		OccurredAt:             time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return r.store.AddEntityRefs(ctx, msg.ID, notificationEntityRefs(group.items))
}

func notificationConversationShape(target port.OutboundTarget) (chatID, chatType, conversationType string) {
	chatID = strings.TrimSpace(target.ID)
	if target.Type == port.OutboundTargetUser {
		return chatID, string(port.ChatTypeDirect), channelconversation.ConversationTypeDirect
	}
	return chatID, string(port.ChatTypeGroup), channelconversation.ConversationTypeGroup
}

func notificationConversationKey(connectionID string, target port.OutboundTarget) string {
	targetID := strings.TrimSpace(target.ID)
	if target.Type == port.OutboundTargetUser {
		return strings.Join([]string{connectionID, "direct", targetID}, ":")
	}
	return strings.Join([]string{connectionID, "group", targetID}, ":")
}

func marshalNotificationMessageBody(group notificationGroup, payload RetryPayload) (json.RawMessage, error) {
	items := make([]map[string]string, 0, len(group.items))
	for _, item := range group.items {
		items = append(items, map[string]string{
			"id":               uuidText(item.ID),
			"event_kind":       item.EventKind,
			"title":            item.Title,
			"issue_identifier": item.IssueIdentifier,
		})
	}
	return json.Marshal(struct {
		Title    string                 `json:"title"`
		Body     string                 `json:"body"`
		Target   port.OutboundTarget    `json:"target"`
		Mentions []port.OutboundMention `json:"mentions,omitempty"`
		Items    []map[string]string    `json:"items"`
	}{Title: payload.Title, Body: payload.Body, Target: group.target, Mentions: payload.Mentions, Items: items})
}

func marshalNotificationMessageMetadata(group notificationGroup) (json.RawMessage, error) {
	ids := make([]string, 0, len(group.items))
	eventKinds := make([]string, 0, len(group.items))
	for _, item := range group.items {
		ids = append(ids, uuidText(item.ID))
		eventKinds = append(eventKinds, item.EventKind)
	}
	return json.Marshal(struct {
		OutboundNotificationIDs []string `json:"outbound_notification_ids"`
		EventKinds              []string `json:"event_kinds"`
		TargetUserID            string   `json:"target_user_id,omitempty"`
	}{OutboundNotificationIDs: ids, EventKinds: eventKinds, TargetUserID: uuidText(group.targetUserID)})
}

func commonWorkspaceID(items []OutboxNotification) string {
	var workspaceID string
	for _, item := range items {
		id := uuidText(item.WorkspaceID)
		if id == "" {
			continue
		}
		if workspaceID == "" {
			workspaceID = id
			continue
		}
		if workspaceID != id {
			return ""
		}
	}
	return workspaceID
}

func commonActorAgentID(items []OutboxNotification) string {
	var agentID string
	for _, item := range items {
		if item.ActorType != "agent" {
			return ""
		}
		id := uuidText(item.ActorID)
		if id == "" {
			return ""
		}
		if agentID == "" {
			agentID = id
			continue
		}
		if agentID != id {
			return ""
		}
	}
	return agentID
}

func singleNotificationID(items []OutboxNotification) string {
	if len(items) != 1 {
		return ""
	}
	return uuidText(items[0].ID)
}

func notificationHandoff(items []OutboxNotification) (string, json.RawMessage) {
	actions := []string{}
	handoffKind := channelconversation.HandoffKindNone
	for _, item := range items {
		text := strings.ToLower(item.Title + "\n" + item.Body + "\n" + item.EventKind)
		switch {
		case strings.Contains(text, "task_failed") || strings.Contains(text, "failed") || strings.Contains(text, "error") || strings.Contains(item.Body, "失败"):
			handoffKind = channelconversation.HandoffKindFailure
			actions = append(actions, "retry", "comment")
		case item.EventKind == "status_in_review" || strings.Contains(strings.ToUpper(item.Body), "PASS") || strings.Contains(item.Body, "审批"):
			if handoffKind == channelconversation.HandoffKindNone {
				handoffKind = channelconversation.HandoffKindReviewPass
			}
			actions = append(actions, "approve", "continue", "comment")
		case item.Replyable:
			if handoffKind == channelconversation.HandoffKindNone {
				handoffKind = channelconversation.HandoffKindNeedInput
			}
			actions = append(actions, "comment")
		}
	}
	raw, _ := json.Marshal(dedupeStrings(actions))
	return handoffKind, raw
}

func notificationEntityRefs(items []OutboxNotification) []channelconversation.EntityRef {
	refs := make([]channelconversation.EntityRef, 0, len(items)*4)
	for _, item := range items {
		workspaceID := uuidText(item.WorkspaceID)
		if item.IssueID.Valid || strings.TrimSpace(item.IssueIdentifier) != "" {
			refs = append(refs, channelconversation.EntityRef{
				WorkspaceID: workspaceID,
				EntityType:  channelconversation.EntityTypeIssue,
				EntityID:    uuidText(item.IssueID),
				EntityKey:   strings.ToUpper(strings.TrimSpace(item.IssueIdentifier)),
				Display:     firstNonEmpty(item.IssueTitle, item.IssueIdentifier),
				Role:        channelconversation.EntityRolePrimary,
			})
		}
		if item.SourceCommentID.Valid {
			refs = append(refs, channelconversation.EntityRef{
				WorkspaceID: workspaceID,
				EntityType:  channelconversation.EntityTypeIssueComment,
				EntityID:    uuidText(item.SourceCommentID),
				Role:        channelconversation.EntityRoleSource,
			})
		}
		if item.InboxItemID.Valid {
			refs = append(refs, channelconversation.EntityRef{
				WorkspaceID: workspaceID,
				EntityType:  channelconversation.EntityTypeInboxItem,
				EntityID:    uuidText(item.InboxItemID),
				Role:        channelconversation.EntityRoleSource,
			})
		}
		for _, mention := range util.ParseMentions(item.Title + "\n" + item.Body) {
			if mention.Type != "agent" {
				continue
			}
			refs = append(refs, channelconversation.EntityRef{
				WorkspaceID: workspaceID,
				EntityType:  channelconversation.EntityTypeAgent,
				EntityID:    mention.ID,
				Display:     "Agent",
				Role:        channelconversation.EntityRoleHandoffTarget,
			})
		}
		if item.ActorType == "agent" && item.ActorID.Valid {
			refs = append(refs, channelconversation.EntityRef{
				WorkspaceID: workspaceID,
				EntityType:  channelconversation.EntityTypeAgent,
				EntityID:    uuidText(item.ActorID),
				Display:     "Agent",
				Role:        channelconversation.EntityRoleSource,
			})
		}
	}
	return refs
}

func dedupeStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuidStr(id)
}
