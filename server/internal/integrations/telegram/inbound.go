package telegram

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// This file holds the translation from a Telegram Update to the engine's
// normalized channel.InboundMessage. Free functions parameterized by the bot
// identity, mirroring slack/inbound.go, so the per-installation polling loop
// threads in its own bot's id and username.

// telegramRawEvent carries the Telegram-specific fields the cross-platform
// envelope does not — read back only inside the Telegram resolvers.
type telegramRawEvent struct {
	// BotID routes the message to its installation (config->>'app_id').
	BotID string `json:"bot_id"`
	// EventType is a coarse label for drop audits ("message").
	EventType string `json:"event_type"`
	// SenderName is the sender's Telegram display name, carried for
	// group-context attribution.
	SenderName string `json:"sender_name,omitempty"`
}

// inboundFromUpdate normalizes one Telegram update. ok=false means the update
// must not reach the core: bot/self messages, channel posts, edits (excluded
// via allowed_updates already), or unsupported media (the caller decides
// whether to send an "unsupported" notice for p2p).
//
// Group addressing policy mirrors Slack v1: a group message is addressed to
// the bot only when it carries an explicit @bot mention or directly replies to
// one of the bot's messages. Privacy mode is left ON, so Telegram already
// withholds unaddressed group chatter from the bot; this check is the
// defense-in-depth for bots whose privacy mode was disabled in BotFather.
func inboundFromUpdate(u Update, botID int64, botUsername string) (channel.InboundMessage, bool) {
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || m.From.ID == botID {
		return channel.InboundMessage{}, false
	}
	chatType, ok := telegramChatType(m.Chat.Type)
	if !ok {
		return channel.InboundMessage{}, false
	}

	text := m.Text
	if text == "" {
		text = m.Caption
	}
	msgType := classifyMessage(m)

	mentioned := mentionsBot(m, botUsername)
	repliedToBot := m.ReplyToMessage != nil && m.ReplyToMessage.From != nil && m.ReplyToMessage.From.ID == botID
	addressed := chatType == channel.ChatTypeP2P || mentioned || repliedToBot

	cleaned := normalizeText(text, botUsername)
	commandText := cleaned
	forceFresh := false
	if body, fresh := engine.ParseFreshSessionCommand(cleaned); fresh {
		cleaned = body
		forceFresh = true
	}
	agentText := cleaned
	quotedHuman := m.ReplyToMessage != nil && m.ReplyToMessage.From != nil && !m.ReplyToMessage.From.IsBot
	if chatType == channel.ChatTypeGroup && mentioned && quotedHuman {
		agentText = enrichWithQuotedHumanMessage(cleaned, m.Chat.ID, m.ReplyToMessage)
	}

	senderID := strconv.FormatInt(m.From.ID, 10)
	chatID := strconv.FormatInt(m.Chat.ID, 10)
	threadID := ""
	if m.IsTopicMessage && m.MessageThreadID != 0 {
		threadID = strconv.FormatInt(m.MessageThreadID, 10)
	}

	raw, _ := json.Marshal(telegramRawEvent{
		BotID:      strconv.FormatInt(botID, 10),
		EventType:  "message",
		SenderName: senderDisplayName(m.From),
	})

	var reply *channel.ReplyCtx
	if m.ReplyToMessage != nil {
		reply = &channel.ReplyCtx{
			MessageID: messageKey(m.Chat.ID, m.ReplyToMessage.MessageID),
			RootID:    threadID,
		}
	}

	return channel.InboundMessage{
		EventID: strconv.FormatInt(u.UpdateID, 10),
		// Telegram message ids are only unique per chat, so the dedup key
		// (installation, message_id) uses the composite chat:message form.
		MessageID:      messageKey(m.Chat.ID, m.MessageID),
		Type:           msgType,
		Text:           agentText,
		CommandText:    commandText,
		ReplyTo:        reply,
		AddressedToBot: addressed,
		ForceFresh:     forceFresh,
		Source: channel.Source{
			ChannelType: TypeTelegram,
			ChatID:      chatID,
			ChatType:    chatType,
			SenderID:    senderID,
			// Telegram user ids are global, so the per-installation id doubles as
			// the cross-installation stable id.
			SenderStableID: senderID,
			ThreadID:       threadID,
		},
		Raw: raw,
	}, true
}

// messageKey builds the per-installation-unique message id "chat:message".
func messageKey(chatID, messageID int64) string {
	return strconv.FormatInt(chatID, 10) + ":" + strconv.FormatInt(messageID, 10)
}

// telegramChatType maps Telegram's chat.type. Channel posts (broadcast
// channels, no interactive sender context) are not ingested.
func telegramChatType(t string) (channel.ChatType, bool) {
	switch t {
	case "private":
		return channel.ChatTypeP2P, true
	case "group", "supergroup":
		return channel.ChatTypeGroup, true
	default:
		return "", false
	}
}

// classifyMessage maps the message payload to the normalized MsgType. Only
// text is actionable in v1 (aligned with Feishu/Slack); media kinds are
// reported so the caller can reply "unsupported" rather than stay silent.
func classifyMessage(m *Message) channel.MsgType {
	switch {
	case m.Text != "":
		return channel.MsgTypeText
	case len(m.Photo) > 0:
		return channel.MsgTypeImage
	case m.Voice != nil:
		return channel.MsgTypeAudio
	case m.Video != nil:
		return channel.MsgTypeVideo
	case m.Document != nil:
		return channel.MsgTypeFile
	default:
		return channel.MsgTypeUnknown
	}
}

// mentionsBot reports whether the message text contains "@botusername".
// Telegram marks mentions with entities, but matching the literal token is
// equivalent for bot usernames (they are globally unique and always start
// with "@" in text) and keeps the check entity-order independent.
func mentionsBot(m *Message, botUsername string) bool {
	if botUsername == "" {
		return false
	}
	wantMention := "@" + botUsername
	for _, source := range []struct {
		text     string
		entities []MessageEntity
	}{
		{text: m.Text, entities: m.Entities},
		{text: m.Caption, entities: m.CaptionEntities},
	} {
		for _, entity := range source.entities {
			if entity.Type != "mention" && entity.Type != "bot_command" {
				continue
			}
			value, ok := messageEntityText(source.text, entity)
			if !ok {
				continue
			}
			if strings.EqualFold(value, wantMention) || commandTargetsBot(value, botUsername) {
				return true
			}
		}
	}
	// Telegram normally supplies entities. Keep a boundary-aware fallback for
	// old fixtures and defensive compatibility with incomplete gateways.
	return containsBotMention(m.Text, botUsername) || containsBotMention(m.Caption, botUsername)
}

// normalizeText strips the bot mention token while retaining shared commands
// such as /new and /issue for the engine's command parser.
func normalizeText(text, botUsername string) string {
	cleaned := text
	if botUsername != "" {
		cleaned = removeBotMentions(cleaned, botUsername)
	}
	return strings.TrimSpace(cleaned)
}

// enrichWithQuotedHumanMessage prepends only the message a group member
// explicitly selected by replying and mentioning the bot. Ambient group
// history never enters the agent context. CommandText remains the sender's own
// cleaned instruction so commands inside the quoted message stay historical.
func enrichWithQuotedHumanMessage(instruction string, chatID int64, quoted *Message) string {
	quotedText := quoted.Text
	if quotedText == "" {
		quotedText = quoted.Caption
	}
	if strings.TrimSpace(quotedText) == "" {
		quotedText = "[empty or non-text message]"
	}
	sender := "Unknown user"
	if quoted.From != nil {
		if name := senderDisplayName(quoted.From); name != "" {
			sender = name
		}
	}
	msgType := classifyMessage(quoted)
	block := fmt.Sprintf("<quoted_message message_id=%q sender=%q type=%q>\n%s\n</quoted_message>",
		messageKey(chatID, quoted.MessageID), sender, msgType, quotedText)
	if instruction == "" {
		return block
	}
	return block + "\n\n" + instruction
}

func commandTargetsBot(command, botUsername string) bool {
	at := strings.LastIndexByte(command, '@')
	return at >= 0 && strings.EqualFold(command[at+1:], botUsername)
}

func messageEntityText(text string, entity MessageEntity) (string, bool) {
	if entity.Offset < 0 || entity.Length <= 0 {
		return "", false
	}
	units := utf16.Encode([]rune(text))
	end := entity.Offset + entity.Length
	if entity.Offset > len(units) || end < entity.Offset || end > len(units) {
		return "", false
	}
	return string(utf16.Decode(units[entity.Offset:end])), true
}

func containsBotMention(text, botUsername string) bool {
	token := "@" + strings.ToLower(botUsername)
	lower := strings.ToLower(text)
	for start := 0; ; {
		i := strings.Index(lower[start:], token)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(token)
		if end == len(lower) || !isTelegramUsernameByte(lower[end]) {
			return true
		}
		start = end
	}
}

func removeBotMentions(text, botUsername string) string {
	token := "@" + strings.ToLower(botUsername)
	lower := strings.ToLower(text)
	var out strings.Builder
	for start := 0; start < len(text); {
		i := strings.Index(lower[start:], token)
		if i < 0 {
			out.WriteString(text[start:])
			break
		}
		i += start
		end := i + len(token)
		if end < len(lower) && isTelegramUsernameByte(lower[end]) {
			out.WriteString(text[start:end])
			start = end
			continue
		}
		out.WriteString(text[start:i])
		start = end
	}
	return out.String()
}

func isTelegramUsernameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// senderDisplayName renders "First Last" or the username as fallback.
func senderDisplayName(u *User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name != "" {
		return name
	}
	return u.Username
}

// containsFold is a case-insensitive strings.Contains (bot usernames are
// case-insensitive on Telegram).
