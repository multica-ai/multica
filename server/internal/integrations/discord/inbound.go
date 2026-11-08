package discord

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file holds the translation from a Discord MESSAGE_CREATE dispatch to
// the engine's normalized channel.InboundMessage. Free functions parameterized
// by the bot identity, mirroring slack/inbound.go and telegram/inbound.go, so
// the per-installation Gateway connection threads in its own bot's id.

// discordRawEvent carries the Discord-specific fields the cross-platform
// envelope does not — read back only inside the Discord resolvers.
type discordRawEvent struct {
	// BotID routes the message to its installation (config->>'app_id').
	BotID string `json:"bot_id"`
	// EventType is a coarse label for drop audits ("message").
	EventType string `json:"event_type"`
	// GuildID is set for guild channel messages, empty for DMs.
	GuildID string `json:"guild_id,omitempty"`
	// SenderName is the sender's Discord display name, carried for
	// attribution in the persisted transcript.
	SenderName string `json:"sender_name,omitempty"`
}

// inboundFromGatewayMessage normalizes one MESSAGE_CREATE. ok=false means the
// message must not reach the core: the bot's own messages and other bots'
// messages (loop guard), and empty messages (a bare attachment-only post is
// kept — its placeholder is the body).
//
// Addressing policy mirrors Slack/Telegram v1: a DM is always addressed; a
// guild channel message is addressed only when it @-mentions the bot.
//
// ponytail: Discord delivers group-DM messages without a guild_id, so a group
// DM is treated as a DM (every message addressed). Add a channel-type lookup
// (GET /channels/{id}) if group DMs become a real surface.
func inboundFromGatewayMessage(botID string, m gatewayMessage) (channel.InboundMessage, bool) {
	if m.Author.Bot || m.Author.ID == "" || m.Author.ID == botID {
		return channel.InboundMessage{}, false
	}
	chatType := channel.ChatTypeGroup
	if m.GuildID == "" {
		chatType = channel.ChatTypeP2P
	}
	text := cleanContent(m.Content, botID)
	addressed := chatType == channel.ChatTypeP2P || mentionsBot(m.Mentions, botID)
	if text == "" {
		text = attachmentPlaceholders(m.Attachments)
	}
	if text == "" {
		return channel.InboundMessage{}, false
	}
	raw, _ := json.Marshal(discordRawEvent{
		BotID:      botID,
		EventType:  "message",
		GuildID:    m.GuildID,
		SenderName: firstNonEmpty(m.Author.GlobalName, m.Author.Username),
	})
	return channel.InboundMessage{
		EventID:        m.ID,
		MessageID:      m.ID,
		Type:           channel.MsgTypeText,
		Text:           text,
		CommandText:    text,
		AddressedToBot: addressed,
		Source: channel.Source{
			ChannelType: TypeDiscord,
			ChatID:      m.ChannelID,
			ChatType:    chatType,
			SenderID:    m.Author.ID,
		},
		Raw: raw,
	}, true
}

// cleanContent strips the bot's own mention tokens and trims whitespace so the
// core sees the user's actual prompt, not "<@123> hi". Discord renders a
// mention as <@123> or <@!123>; any other mention (a person, a role) stays as
// text, which is what the agent should read.
func cleanContent(content, botID string) string {
	if botID != "" {
		content = strings.ReplaceAll(content, "<@"+botID+">", "")
		content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	}
	return strings.TrimSpace(content)
}

// mentionsBot reports whether the message's mention list includes the bot.
func mentionsBot(mentions []gatewayMention, botID string) bool {
	if botID == "" {
		return false
	}
	for _, mention := range mentions {
		if mention.ID == botID {
			return true
		}
	}
	return false
}

// attachmentPlaceholders renders a message's files as text markers. Discord
// attachments are not downloaded (see the package comment), so the agent gets
// a stable description instead of the bytes.
func attachmentPlaceholders(attachments []gatewayAttachment) string {
	if len(attachments) == 0 {
		return ""
	}
	lines := make([]string, 0, len(attachments))
	for _, a := range attachments {
		name := firstNonEmpty(a.Filename, "attachment")
		lines = append(lines, "[File: "+name+"]")
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
