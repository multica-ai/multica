package slack

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/slack-go/slack/slackevents"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file holds the platform-neutral translation from a Slack Events API
// payload to the engine's normalized channel.InboundMessage. These are free
// functions parameterized by the bot identity rather than methods on the
// channel, so the per-installation Socket Mode connection (slack_channel.go)
// threads in its own installed bot's user id when translating each event.

// slackRawEvent carries the Slack-specific fields the cross-platform envelope
// does not — read back only inside the Slack resolvers (team_id routes the
// installation; the core never reads Raw).
type slackRawEvent struct {
	TeamID      string `json:"team_id"`
	APIAppID    string `json:"api_app_id,omitempty"`
	EventType   string `json:"event_type"`
	SubType     string `json:"subtype,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
}

// compileMentionRe builds the regexp that matches an @-mention of botUserID.
// Slack renders a mention as <@U123> or <@U123|name>. An empty botUserID
// (installation not found / not yet known) yields nil — mention detection is
// then a no-op, which is safe: DMs and app_mention events do not rely on it,
// and an un-routable team is dropped at installation resolution anyway.
func compileMentionRe(botUserID string) *regexp.Regexp {
	if botUserID == "" {
		return nil
	}
	return regexp.MustCompile(`<@` + regexp.QuoteMeta(botUserID) + `(\|[^>]*)?>`)
}

// inboundFromMessage normalizes a Slack message event. It returns ok=false for
// events that must not reach the core: the bot's own messages and other bots'
// messages (loop guard), and edits/deletes/joins and similar subtyped system
// messages (only brand-new user messages are ingested).
//
// Group addressing policy (v1, deliberate): a group message is addressed to the
// bot only when it carries an explicit <@bot> mention. Mention-free follow-ups
// inside a thread the bot is already engaged in are NOT auto-addressed here:
// "reply to a bot message" is session state, so it belongs in the session-aware
// shared service / resolver layer rather than in per-connection adapter memory.
// Until that lands, channel/thread continuation requires re-mentioning the bot.
// P2P (DM) ingests every message, unchanged.
func inboundFromMessage(e slackevents.EventsAPIEvent, m *slackevents.MessageEvent, botUserID string, mentionRe *regexp.Regexp) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.SubType == "bot_message" {
		return channel.InboundMessage{}, false
	}
	if m.User == "" || (botUserID != "" && m.User == botUserID) {
		return channel.InboundMessage{}, false
	}
	if !isIngestableSubtype(m.SubType) {
		return channel.InboundMessage{}, false
	}

	chatType := slackChatType(m.Channel, m.ChannelType)
	addressed := chatType == channel.ChatTypeP2P || mentionsBot(m.Text, mentionRe)
	return buildInbound(e, buildInboundParams{
		eventType: "message",
		subType:   m.SubType,
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  chatType,
		addressed: addressed,
	}, mentionRe), true
}

// inboundFromAppMention normalizes an app_mention event. An app_mention is, by
// definition, addressed to the bot and occurs in a channel (group). The same
// channel @mention also arrives as a message event with the identical ts, so
// the engine's (installation, message_id=ts) dedup collapses the pair — no
// special-casing needed here.
func inboundFromAppMention(e slackevents.EventsAPIEvent, m *slackevents.AppMentionEvent, botUserID string, mentionRe *regexp.Regexp) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.User == "" || (botUserID != "" && m.User == botUserID) {
		return channel.InboundMessage{}, false
	}
	return buildInbound(e, buildInboundParams{
		eventType: "app_mention",
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  channel.ChatTypeGroup,
		addressed: true,
	}, mentionRe), true
}

type buildInboundParams struct {
	eventType string
	subType   string
	channelID string
	userID    string
	text      string
	ts        string
	threadTS  string
	chatType  channel.ChatType
	addressed bool
}

func buildInbound(e slackevents.EventsAPIEvent, p buildInboundParams, mentionRe *regexp.Regexp) channel.InboundMessage {
	raw, _ := json.Marshal(slackRawEvent{
		TeamID:      e.TeamID,
		APIAppID:    e.APIAppID,
		EventType:   p.eventType,
		SubType:     p.subType,
		ChannelType: string(p.chatType),
	})
	var reply *channel.ReplyCtx
	if p.threadTS != "" && p.threadTS != p.ts {
		reply = &channel.ReplyCtx{MessageID: p.threadTS, RootID: p.threadTS}
	}
	return channel.InboundMessage{
		EventID:        p.ts,
		MessageID:      p.ts,
		Type:           channel.MsgTypeText,
		Text:           cleanText(p.text, mentionRe),
		ReplyTo:        reply,
		AddressedToBot: p.addressed,
		Source: channel.Source{
			ChannelType: TypeSlack,
			ChatID:      p.channelID,
			ChatType:    p.chatType,
			SenderID:    p.userID,
			ThreadID:    p.threadTS,
		},
		Raw: raw,
	}
}

// cleanText strips a leading/embedded bot mention token and trims surrounding
// whitespace so the core sees the user's actual prompt, not "<@U123> hi".
func cleanText(text string, mentionRe *regexp.Regexp) string {
	if mentionRe != nil {
		text = mentionRe.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

// mentionsBot reports whether text contains an @-mention of this bot.
func mentionsBot(text string, mentionRe *regexp.Regexp) bool {
	return mentionRe != nil && mentionRe.MatchString(text)
}

// slackChatType maps a Slack channel id / channel_type to the normalized
// ChatType. Only a 1:1 direct message ("im", or a "D…" channel id) is p2p;
// everything else — public/private channels AND multi-party DMs ("mpim", which
// are multi-person conversations) — is a group. A group routes through the
// engine's "must address the bot" filter, so plain chatter in a multi-party DM
// is not mistaken for a prompt to the bot.
func slackChatType(channelID, channelType string) channel.ChatType {
	switch channelType {
	case "im":
		return channel.ChatTypeP2P
	case "mpim", "channel", "group", "private_channel":
		return channel.ChatTypeGroup
	}
	if strings.HasPrefix(channelID, "D") {
		return channel.ChatTypeP2P
	}
	return channel.ChatTypeGroup
}

// isIngestableSubtype reports whether a message subtype is a brand-new user
// message the core should ingest. Empty subtype is the normal case;
// thread_broadcast and file_share are real user messages; everything else
// (message_changed, message_deleted, channel_join, …) is a system/edit event.
func isIngestableSubtype(subType string) bool {
	switch subType {
	case "", "thread_broadcast", "file_share":
		return true
	default:
		return false
	}
}
