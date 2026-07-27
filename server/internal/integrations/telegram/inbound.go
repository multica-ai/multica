package telegram

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// This file holds the platform-neutral translation from a Telegram webhook
// Update to the engine's normalized channel.InboundMessage. inboundFromUpdate
// is a free function parameterized by the bot identity (rather than a method
// on the channel) so the per-installation webhook handler threads in its own
// installed bot's id/username when translating each update.

// Update is a Telegram webhook update. Only the fields the adapter needs are
// modeled; everything else Telegram sends is ignored.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Message is a Telegram message, modeled with only the fields inbound
// normalization and downstream reply threading need.
type Message struct {
	MessageID       int64    `json:"message_id"`
	From            *User    `json:"from"`
	Chat            Chat     `json:"chat"`
	Text            string   `json:"text"`
	Entities        []Entity `json:"entities"`
	ReplyToMessage  *Message `json:"reply_to_message"`
	MessageThreadID int64    `json:"message_thread_id"`
}

// User is a Telegram user/bot account.
type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

// Chat is a Telegram chat. Type is one of "private", "group", "supergroup",
// or "channel".
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Entity is a Telegram message entity (mention, bot_command, …) describing a
// styled/semantic substring of Text by byte offset/length (UTF-16 code units
// per the Telegram API, but treated as rune-based here since offsets are only
// used to slice out an @mention token, not for exact-byte accounting).
type Entity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

// telegramRawEvent carries the Telegram-specific fields the cross-platform
// envelope does not — read back only inside the Telegram resolvers (bot_id
// routes the installation; the core never reads Raw).
type telegramRawEvent struct {
	BotID     string `json:"bot_id"`
	ChatType  string `json:"chat_type"`
	EventType string `json:"event_type"`
}

// telegramChatType maps a Telegram chat.type to the normalized ChatType.
// "channel" (a broadcast channel, not a conversation) has no ChatType
// mapping; callers must drop it before calling this for that case, but as a
// defensive default it is treated as a group.
func telegramChatType(t string) channel.ChatType {
	switch t {
	case "private":
		return channel.ChatTypeP2P
	case "group", "supergroup":
		return channel.ChatTypeGroup
	default:
		return channel.ChatTypeGroup
	}
}

// inboundFromUpdate normalizes a Telegram webhook Update. It returns
// ok=false for updates that must not reach the core: non-message updates,
// the bot's own messages (loop guard), empty text, and channel posts
// (broadcast channels are not conversations).
//
// Group addressing policy (v1, deliberate, mirrors Slack): a group message
// is addressed to the bot only when it carries an explicit @botusername
// mention entity, or is a reply to a message the bot itself sent.
// Mention-free follow-ups are not auto-addressed here.
func inboundFromUpdate(u Update, botID, botUsername string) (channel.InboundMessage, bool) {
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || strings.TrimSpace(m.Text) == "" {
		return channel.InboundMessage{}, false
	}
	if m.Chat.Type == "channel" {
		return channel.InboundMessage{}, false
	}

	chatType := telegramChatType(m.Chat.Type)
	addressed := chatType == channel.ChatTypeP2P || mentionsBot(m, botUsername) || repliesToBot(m, botUsername)

	text := cleanText(m.Text, botUsername)
	if text == "" {
		return channel.InboundMessage{}, false
	}

	var threadID string
	if m.MessageThreadID != 0 {
		threadID = strconv.FormatInt(m.MessageThreadID, 10)
	}

	var reply *channel.ReplyCtx
	if m.ReplyToMessage != nil {
		replyID := strconv.FormatInt(m.ReplyToMessage.MessageID, 10)
		reply = &channel.ReplyCtx{MessageID: replyID, RootID: replyID}
	}

	raw, _ := json.Marshal(telegramRawEvent{
		BotID:     botID,
		ChatType:  m.Chat.Type,
		EventType: "message",
	})

	messageID := strconv.FormatInt(m.MessageID, 10)
	return channel.InboundMessage{
		EventID:        messageID,
		MessageID:      messageID,
		Type:           channel.MsgTypeText,
		Text:           text,
		ReplyTo:        reply,
		AddressedToBot: addressed,
		Source: channel.Source{
			ChannelType: TypeTelegram,
			ChatID:      strconv.FormatInt(m.Chat.ID, 10),
			ChatType:    chatType,
			SenderID:    strconv.FormatInt(m.From.ID, 10),
			ThreadID:    threadID,
		},
		Raw: raw,
	}, true
}

// mentionsBot reports whether m.Text carries an explicit @botUsername
// mention entity: a "mention" entity whose substring equals @botUsername, or
// a "bot_command" entity (e.g. "/issue@acme_bot") whose substring contains
// @botUsername.
func mentionsBot(m *Message, botUsername string) bool {
	if botUsername == "" {
		return false
	}
	target := "@" + botUsername
	runes := []rune(m.Text)
	for _, e := range m.Entities {
		sub := entitySubstring(runes, e)
		switch e.Type {
		case "mention":
			if sub == target {
				return true
			}
		case "bot_command":
			if strings.Contains(sub, target) {
				return true
			}
		}
	}
	return false
}

// repliesToBot reports whether m is a reply to a message the bot itself
// sent, the other group-addressing signal alongside an explicit mention.
func repliesToBot(m *Message, botUsername string) bool {
	r := m.ReplyToMessage
	return r != nil && r.From != nil && r.From.IsBot && r.From.Username == botUsername
}

// entitySubstring slices out the text an Entity covers. Offset/Length are
// rune-indexed here for simplicity (Telegram specifies UTF-16 code units,
// which coincide with rune counts for the ASCII bot usernames/commands this
// adapter matches against).
func entitySubstring(runes []rune, e Entity) string {
	start := e.Offset
	end := e.Offset + e.Length
	if start < 0 || end > len(runes) || start > end {
		return ""
	}
	return string(runes[start:end])
}

// cleanText strips a leading "@botusername" mention token and normalizes a
// leading "/cmd@botusername" to "/cmd" so engine.ParseIssueCommand matches,
// then trims surrounding whitespace.
func cleanText(text string, botUsername string) string {
	if botUsername == "" {
		return strings.TrimSpace(text)
	}
	mention := "@" + botUsername
	text = strings.TrimSpace(text)

	// Strip a leading standalone @mention token, e.g. "@acme_bot /issue Ship it".
	if strings.HasPrefix(text, mention) {
		rest := text[len(mention):]
		if rest == "" || rest[0] == ' ' || rest[0] == '\n' || rest[0] == '\t' {
			text = strings.TrimSpace(rest)
		}
	}

	// Normalize a leading "/cmd@botusername" to "/cmd".
	if strings.HasPrefix(text, "/") {
		if sp := strings.IndexAny(text, " \n\t"); sp >= 0 {
			cmd, rest := text[:sp], text[sp:]
			if strings.Contains(cmd, mention) {
				text = strings.Replace(cmd, mention, "", 1) + rest
			}
		} else if strings.Contains(text, mention) {
			text = strings.Replace(text, mention, "", 1)
		}
	}

	return strings.TrimSpace(text)
}
