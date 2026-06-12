package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) processChannelMessageMentions(ctx context.Context, channel db.Channel, msg db.ChannelMessage, actorType, actorID string) {
	mentions := util.ParseMentions(msg.Content)
	if len(mentions) == 0 || h == nil || h.Queries == nil {
		return
	}
	h.notifyChannelMentionedMembers(ctx, channel, msg, mentions, actorType, actorID)
	h.enqueueChannelMentionedAgentTasks(ctx, channel, msg, mentions, actorType, actorID)
	h.trackMentionFrequency(ctx, channel.WorkspaceID, actorType, actorID, msg.Content)
}

func (h *Handler) notifyChannelMentionedMembers(ctx context.Context, channel db.Channel, msg db.ChannelMessage, mentions []util.Mention, actorType, actorID string) {
	recipients := map[string]bool{}
	explicit := map[string]bool{}
	hasAll := false
	for _, m := range mentions {
		switch m.Type {
		case "all":
			hasAll = true
		case "member":
			recipients[m.ID] = true
			explicit[m.ID] = true
		}
	}
	if hasAll || channel.AccessMode != "open" {
		members, err := h.Queries.ListChannelMembers(ctx, channel.ID)
		if err != nil {
			slog.Warn("channel mention: list channel members failed", "channel_id", uuidToString(channel.ID), "error", err)
			return
		}
		memberSet := make(map[string]bool, len(members))
		for _, member := range members {
			memberID := uuidToString(member.UserID)
			memberSet[memberID] = true
			if hasAll {
				recipients[memberID] = true
			}
		}
		if channel.AccessMode != "open" {
			for id := range recipients {
				if !memberSet[id] {
					delete(recipients, id)
				}
			}
		}
	}
	if len(recipients) == 0 {
		return
	}

	actorName := h.resolveChannelActorName(ctx, actorType, actorID)
	excerpt := truncateForChannelMention(msg.Content, 200)
	messageID := uuidToString(msg.ID)
	channelID := uuidToString(channel.ID)
	link := "/channels/" + channelID + "?message=" + messageID
	details := channelMentionDetails(channel, msg, actorType, actorID, actorName, excerpt, link)
	detailsJSON, _ := json.Marshal(details)
	title := "你在 #" + channel.Name + " 被提及"
	body := excerpt
	if actorName != "" {
		body = actorName + "：" + excerpt
	}

	for id := range recipients {
		if id == actorID && !explicit[id] {
			continue
		}
		recipientID, err := util.ParseUUID(id)
		if err != nil {
			continue
		}
		if !h.canSeeChannelMemberMention(ctx, channel, recipientID) {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			WorkspaceID:   channel.WorkspaceID,
			RecipientType: "member",
			RecipientID:   recipientID,
			Type:          "mentioned",
			Severity:      "info",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          util.StrToText(body),
			ActorType:     util.StrToText(actorType),
			ActorID:       optionalPGUUID(actorID),
			Details:       detailsJSON,
		})
		if err != nil {
			slog.Warn("channel mention inbox creation failed", "channel_id", channelID, "message_id", messageID, "recipient_id", id, "error", err)
			continue
		}
		h.Bus.Publish(events.Event{
			Type:        protocol.EventInboxNew,
			WorkspaceID: uuidToString(channel.WorkspaceID),
			ActorType:   actorType,
			ActorID:     actorID,
			Payload:     map[string]any{"item": inboxToResponse(item)},
		})
		h.recordChannelMentionNotification(ctx, channel, msg, recipientID, title, body, link, detailsJSON, actorType, actorID, actorName)
	}
}

func (h *Handler) canSeeChannelMemberMention(ctx context.Context, channel db.Channel, userID pgtype.UUID) bool {
	if !userID.Valid {
		return false
	}
	if channel.AccessMode != "open" {
		_, err := h.Queries.GetChannelMember(ctx, db.GetChannelMemberParams{
			ChannelID: channel.ID,
			UserID:    userID,
		})
		return err == nil
	}
	_, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: channel.WorkspaceID,
	})
	return err == nil
}

func (h *Handler) enqueueChannelMentionedAgentTasks(ctx context.Context, channel db.Channel, msg db.ChannelMessage, mentions []util.Mention, actorType, actorID string) {
	enqueued := map[string]bool{}
	enqueueAgent := func(agentID pgtype.UUID, mentionType string, mentionedID pgtype.UUID, squad db.Squad, isLeader bool) {
		resolvedID := uuidToString(agentID)
		if resolvedID == "" || enqueued[resolvedID] {
			return
		}
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          agentID,
			WorkspaceID: channel.WorkspaceID,
		})
		if err != nil || !agent.RuntimeID.Valid || agent.ArchivedAt.Valid {
			return
		}
		if !h.canTriggerPrivateAgent(ctx, agent, actorType, actorID) {
			return
		}
		hasExisting, err := h.Queries.HasChannelMentionTaskForMessageAndAgent(ctx, db.HasChannelMentionTaskForMessageAndAgentParams{
			ChannelMessageID: msg.ID,
			AgentID:          agentID,
		})
		if err != nil || hasExisting {
			return
		}
		input := service.EnqueueChannelMentionTaskInput{
			WorkspaceID:      channel.WorkspaceID,
			ChannelID:        channel.ID,
			ChannelName:      channel.Name,
			ChannelMessageID: msg.ID,
			ChannelThreadID:  msg.ThreadID,
			ChannelReplyToID: msg.ReplyToID,
			TriggerContent:   msg.Content,
			RequesterID:      optionalPGUUID(actorID),
			MentionType:      mentionType,
			MentionedID:      mentionedID,
			ResolvedAgentID:  agentID,
			IsLeaderTask:     isLeader,
		}
		if msg.ThreadID.Valid {
			if thread, err := h.Queries.GetChannelThread(ctx, msg.ThreadID); err == nil && thread.RootMessageID.Valid {
				input.ChannelThreadRootMsgID = thread.RootMessageID
			}
		}
		if squad.ID.Valid {
			input.SquadID = squad.ID
			input.SquadName = squad.Name
		}
		if _, err := h.TaskService.EnqueueChannelMentionTask(ctx, input); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				slog.Warn("channel mention task enqueue failed", "channel_id", uuidToString(channel.ID), "message_id", uuidToString(msg.ID), "agent_id", resolvedID, "error", err)
			}
			return
		}
		enqueued[resolvedID] = true
	}

	for _, m := range mentions {
		switch m.Type {
		case "agent":
			agentID, err := util.ParseUUID(m.ID)
			if err != nil {
				continue
			}
			enqueueAgent(agentID, "agent", agentID, db.Squad{}, false)
		case "squad":
			squadID, err := util.ParseUUID(m.ID)
			if err != nil {
				continue
			}
			squad, err := h.Queries.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{
				ID:          squadID,
				WorkspaceID: channel.WorkspaceID,
			})
			if err != nil || squad.ArchivedAt.Valid {
				continue
			}
			enqueueAgent(squad.LeaderID, "squad", squadID, squad, true)
		}
	}
}

func (h *Handler) recordChannelMentionNotification(ctx context.Context, channel db.Channel, msg db.ChannelMessage, recipientID pgtype.UUID, title, body, link string, details []byte, actorType, actorID, actorName string) {
	if len(details) == 0 {
		details = []byte("{}")
	}
	payloadSnapshot, err := json.Marshal(map[string]any{
		"type":        "mentioned",
		"severity":    "info",
		"title":       title,
		"summary":     body,
		"body":        body,
		"link":        link,
		"actor_type":  actorType,
		"actor_id":    actorID,
		"actor_name":  actorName,
		"render_mode": "auto",
		"details":     json.RawMessage(details),
	})
	if err != nil {
		payloadSnapshot = []byte("{}")
	}
	event, err := h.Queries.CreateNotificationEvent(ctx, db.CreateNotificationEventParams{
		WorkspaceID:     channel.WorkspaceID,
		RecipientUserID: recipientID,
		Type:            "mentioned",
		Severity:        "info",
		IssueID:         pgtype.UUID{},
		CommentID:       pgtype.UUID{},
		ActorType:       util.StrToText(actorType),
		ActorID:         optionalPGUUID(actorID),
		Title:           title,
		Body:            util.StrToText(body),
		Link:            util.StrToText(link),
		Details:         details,
	})
	if err != nil {
		slog.Warn("channel mention notification event creation failed", "channel_id", uuidToString(channel.ID), "message_id", uuidToString(msg.ID), "recipient_id", uuidToString(recipientID), "error", err)
		return
	}
	if _, err := h.Queries.CreateNotificationDelivery(ctx, db.CreateNotificationDeliveryParams{
		NotificationEventID: event.ID,
		Channel:             "inbox",
		Status:              "sent",
		AttemptCount:        1,
		LastError:           pgtype.Text{},
		PayloadSnapshot:     payloadSnapshot,
		SentAt:              pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}); err != nil {
		slog.Warn("channel mention notification delivery creation failed", "notification_event_id", uuidToString(event.ID), "error", err)
	}
}

func channelMentionDetails(channel db.Channel, msg db.ChannelMessage, actorType, actorID, actorName, excerpt, link string) map[string]any {
	details := map[string]any{
		"source_type":  "channel_message",
		"channel_id":   uuidToString(channel.ID),
		"channel_name": channel.Name,
		"message_id":   uuidToString(msg.ID),
		"actor": map[string]any{
			"type": actorType,
			"id":   actorID,
			"name": actorName,
		},
		"excerpt": excerpt,
		"link":    link,
	}
	if msg.ThreadID.Valid {
		details["thread_id"] = uuidToString(msg.ThreadID)
	}
	if msg.ReplyToID.Valid {
		details["reply_to_id"] = uuidToString(msg.ReplyToID)
	}
	return details
}

func (h *Handler) resolveChannelActorName(ctx context.Context, actorType, actorID string) string {
	if actorID == "" {
		return ""
	}
	id, err := util.ParseUUID(actorID)
	if err != nil {
		return ""
	}
	switch actorType {
	case "agent":
		if agent, err := h.Queries.GetAgent(ctx, id); err == nil {
			return agent.Name
		}
	case "member":
		if user, err := h.Queries.GetUser(ctx, id); err == nil {
			return user.Name
		}
	}
	return ""
}

func optionalPGUUID(id string) pgtype.UUID {
	if strings.TrimSpace(id) == "" {
		return pgtype.UUID{}
	}
	u, err := util.ParseUUID(id)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}

func truncateForChannelMention(content string, maxRunes int) string {
	cleaned := strings.TrimSpace(strings.Join(strings.Fields(content), " "))
	if maxRunes <= 0 {
		return cleaned
	}
	rs := []rune(cleaned)
	if len(rs) <= maxRunes {
		return cleaned
	}
	return fmt.Sprintf("%s...", string(rs[:maxRunes]))
}
