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
	// Media is the one file this message carries, for the media resolver to
	// fetch off the ACK path. Nil for text-only messages.
	Media *inboundMedia `json:"media,omitempty"`
}

// inboundMedia is what the media resolver needs to fetch a message's file:
// the getFile handle plus the display metadata Telegram already told us.
type inboundMedia struct {
	Kind         channel.MsgType `json:"kind"`
	FileID       string          `json:"file_id"`
	FileUniqueID string          `json:"file_unique_id,omitempty"`
	FileName     string          `json:"file_name,omitempty"`
	MimeType     string          `json:"mime_type,omitempty"`
	FileSize     int64           `json:"file_size,omitempty"`
	// Placeholder is the exact marker in the message body the bound
	// attachment replaces with its Markdown link. It stays as plain text when
	// the fetch fails, so the agent still knows a file was there.
	Placeholder string `json:"placeholder"`
	// PlaceholderIndex is which occurrence of Placeholder in the body is the
	// sender's: quoted and recent context go in front of it, and a member
	// who typed the same marker there must not receive the file.
	PlaceholderIndex int `json:"placeholder_index,omitempty"`
}

// mediaFromMessage picks the message's downloadable file, or nil. Photos come
// in several renditions; the largest is the one worth keeping.
func mediaFromMessage(m *Message) *inboundMedia {
	switch {
	case len(m.Photo) > 0:
		best := m.Photo[0]
		for _, p := range m.Photo[1:] {
			if p.Width*p.Height >= best.Width*best.Height {
				best = p
			}
		}
		return &inboundMedia{Kind: channel.MsgTypeImage, FileID: best.FileID, FileUniqueID: best.FileUniqueID,
			MimeType: "image/jpeg", FileSize: best.FileSize, Placeholder: "[Image]"}
	case m.Document != nil:
		return fileRefMedia(channel.MsgTypeFile, m.Document, "[File: "+firstNonEmpty(m.Document.FileName, "attachment")+"]")
	case m.Video != nil:
		return fileRefMedia(channel.MsgTypeVideo, m.Video, "[Video]")
	case m.VideoNote != nil:
		return fileRefMedia(channel.MsgTypeVideo, m.VideoNote, "[Video note]")
	case m.Animation != nil:
		return fileRefMedia(channel.MsgTypeVideo, m.Animation, "[Animation]")
	case m.Audio != nil:
		return fileRefMedia(channel.MsgTypeAudio, m.Audio, "[Audio]")
	case m.Voice != nil:
		return fileRefMedia(channel.MsgTypeAudio, m.Voice, "[Voice message]")
	}
	return nil
}

func fileRefMedia(kind channel.MsgType, f *FileRef, placeholder string) *inboundMedia {
	if f.FileID == "" {
		return nil
	}
	return &inboundMedia{Kind: kind, FileID: f.FileID, FileUniqueID: f.FileUniqueID,
		FileName: f.FileName, MimeType: f.MimeType, FileSize: f.FileSize, Placeholder: placeholder}
}

// inboundFromUpdate normalizes one Telegram update without any recent group
// context. See inboundFromUpdateWithContext.
func inboundFromUpdate(u Update, botID int64, botUsername string) (channel.InboundMessage, bool) {
	return inboundFromUpdateWithContext(u, botID, botUsername, nil)
}

// inboundFromUpdateWithContext normalizes one Telegram update. ok=false means
// the update must not reach the core: bot/self messages, channel posts, edits
// (excluded via allowed_updates already), or unsupported media (the caller
// decides whether to send an "unsupported" notice for p2p).
//
// Group addressing policy mirrors Slack v1: a group message is addressed to
// the bot only when it carries an explicit @bot mention or directly replies to
// one of the bot's messages. Telegram only withholds unaddressed group
// chatter while the bot's privacy mode is ON and the bot is not a group admin;
// the recent-context feature actively invites operators to turn privacy off,
// so this check — not Telegram — is what keeps unaddressed chatter from
// starting a turn.
//
// recent, when non-nil, supplies the preceding messages of the same
// chat/topic. They are inlined as a <recent_context> block ahead of an
// addressed group message (never p2p, never an unaddressed one, and never
// for /new — a new Chat must not inherit the previous Chat's ambient
// context), matching Lark's enricher composition: recent → quoted → own.
// Ambient context is not "selected" context: HasSelectedContext stays tied
// to the explicitly quoted reply.
func inboundFromUpdateWithContext(u Update, botID int64, botUsername string, recent recentContextSource) (channel.InboundMessage, bool) {
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
	media := mediaFromMessage(m)

	mentioned := mentionsBot(m, botUsername)
	repliedToBot := m.ReplyToMessage != nil && m.ReplyToMessage.From != nil && m.ReplyToMessage.From.ID == botID
	addressed := chatType == channel.ChatTypeP2P || mentioned || repliedToBot

	cleaned := normalizeText(text, botUsername)
	commandText := cleaned
	forceFresh := false
	startChat := false
	if control, ok := engine.ParseControlCommand(cleaned); ok {
		cleaned = control.Body
		forceFresh = control.Kind == engine.ControlCommandFreshSession
		startChat = control.Kind == engine.ControlCommandNewChat
	}
	agentText := cleaned
	own := ""
	if media != nil {
		// The placeholder leads the sender's own text, so a caption reads as
		// the caption of the file above it once the resolver swaps the marker
		// for the attachment link. Quoted and recent context still go in front.
		own = media.Placeholder
		if cleaned != "" {
			own += "\n" + cleaned
		}
		agentText = own
	}
	quotedHuman := m.ReplyToMessage != nil && m.ReplyToMessage.From != nil && !m.ReplyToMessage.From.IsBot
	hasSelectedContext := chatType == channel.ChatTypeGroup && mentioned && quotedHuman
	if hasSelectedContext {
		agentText = enrichWithQuotedHumanMessage(agentText, m.Chat.ID, m.ReplyToMessage)
	}
	if recent != nil && chatType == channel.ChatTypeGroup && addressed && !startChat {
		agentText = enrichWithRecentContext(agentText, m, recent)
	}
	if media != nil {
		// The engine replaces the placeholder by occurrence, and the context
		// enrichers only ever prepend, so the sender's own segment is the
		// suffix. Count every occurrence ahead of it — user-typed literals
		// included — so a member who wrote "[Image]" in the window does not
		// receive the sender's file. Same rule as DingTalk's quoted block.
		media.PlaceholderIndex = strings.Count(agentText[:len(agentText)-len(own)], media.Placeholder)
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
		Media:      media,
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
		MessageID:          messageKey(m.Chat.ID, m.MessageID),
		Type:               msgType,
		Text:               agentText,
		CommandText:        commandText,
		HasSelectedContext: hasSelectedContext,
		ReplyTo:            reply,
		AddressedToBot:     addressed,
		ForceFresh:         forceFresh,
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

// classifyMessage maps the message payload to the normalized MsgType. Text and
// the downloadable media kinds are ingested; MsgTypeUnknown (stickers,
// locations, polls, …) is what the caller answers with the unsupported notice.
func classifyMessage(m *Message) channel.MsgType {
	switch {
	case m.Text != "":
		return channel.MsgTypeText
	case len(m.Photo) > 0:
		return channel.MsgTypeImage
	case m.Voice != nil, m.Audio != nil:
		return channel.MsgTypeAudio
	case m.Video != nil, m.VideoNote != nil, m.Animation != nil:
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
// such as /clear and /issue for the engine's command parser.
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

// enrichWithRecentContext prepends the chat/topic's preceding messages to the
// instruction. The trigger itself and an explicitly quoted parent are excluded
// from the window: the first is the instruction, the second is already
// rendered as <quoted_message>. An empty window leaves the text untouched.
func enrichWithRecentContext(instruction string, m *Message, recent recentContextSource) string {
	exclude := []int64{m.MessageID}
	if m.ReplyToMessage != nil {
		exclude = append(exclude, m.ReplyToMessage.MessageID)
	}
	var threadID int64
	if m.IsTopicMessage {
		threadID = m.MessageThreadID
	}
	entries := recent.Snapshot(m.Chat.ID, threadID, exclude...)
	if len(entries) == 0 {
		return instruction
	}
	block := renderRecentContextBlock(entries)
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
