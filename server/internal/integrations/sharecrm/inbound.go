package sharecrm

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// botMessageData is the Gateway v1.2+ message event payload (data field).
type botMessageData struct {
	MessageID       string          `json:"message_id"`
	ChatID          string          `json:"chat_id"`
	ChatType        string          `json:"chat_type"`
	From            botSender       `json:"from"`
	Text            string          `json:"text"`
	Date            int64           `json:"date"`
	Message         *botTextMessage `json:"message"`
	Timestamp       int64           `json:"timestamp"`
	Env             int             `json:"env"`
	EA              string          `json:"ea"`
	SessionID       string          `json:"session_id"`
	ParentSessionID *string         `json:"parent_session_id"`
	BotFullID       string          `json:"bot_full_id"`
	MessageType     string          `json:"message_type"`
	ReplyMessageID  *int64          `json:"reply_message_id"`
	HistoryMessages []historyMsg    `json:"history_messages"`
}

type botSender struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type botTextMessage struct {
	Type    string        `json:"type"`
	Content string        `json:"content"`
	Images  []botImageRef `json:"images"`
}

type botImageRef struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Size     int64  `json:"size"`
}

type historyMsg struct {
	MessageID        string `json:"message_id"`
	MessageType      string `json:"message_type"`
	SenderID         string `json:"sender_id"`
	FullSenderID     string `json:"full_sender_id"`
	Content          string `json:"content"`
	MessageTimestamp int64  `json:"message_timestamp"`
}

// sharecrmRawEvent carries platform-only fields in InboundMessage.Raw.
type sharecrmRawEvent struct {
	AppID          string        `json:"app_id"`
	BotFullID      string        `json:"bot_full_id,omitempty"`
	SessionID      string        `json:"session_id,omitempty"`
	ReplyMessageID *int64        `json:"reply_message_id,omitempty"`
	History        []historyMsg  `json:"history_messages,omitempty"`
	EA             string        `json:"ea,omitempty"`
	Images         []botImageRef `json:"images,omitempty"`
}

// inboundFromEvent normalizes a Gateway message into channel.InboundMessage.
// appID is stamped by the receiving connection (routing key). ok=false for
// events that must not reach the engine (no sender / no chat).
//
// Group messages that reach the bot have already been @-filtered by the
// ShareCRM / 企信 side, so AddressedToBot is true for both direct and group.
func inboundFromEvent(data *botMessageData, appID, botFullID string) (channel.InboundMessage, bool) {
	if data == nil {
		return channel.InboundMessage{}, false
	}
	senderID := normalizeSenderID(data.From.ID, data.EA)
	if senderID == "" || strings.TrimSpace(data.ChatID) == "" {
		return channel.InboundMessage{}, false
	}

	chatType := channel.ChatTypeP2P
	if strings.EqualFold(strings.TrimSpace(data.ChatType), "group") {
		chatType = channel.ChatTypeGroup
	}

	caption := strings.TrimSpace(data.Text)
	if data.Message != nil && strings.TrimSpace(data.Message.Content) != "" {
		caption = strings.TrimSpace(data.Message.Content)
	}
	// Strip a leading @bot mention leftover in group text (OpenClaw parity)
	// before placeholders, so /issue CommandText stays the user's words.
	if chatType == channel.ChatTypeGroup {
		caption = stripLeadingMention(caption, firstNonEmpty(data.BotFullID, botFullID))
	}
	images := collectInboundImages(data)
	text := joinNonEmpty("\n", caption, inboundImagePlaceholders(len(images)))

	msgID := strings.TrimSpace(data.MessageID)
	raw := sharecrmRawEvent{
		AppID:          appID,
		BotFullID:      firstNonEmpty(data.BotFullID, botFullID),
		SessionID:      data.SessionID,
		ReplyMessageID: data.ReplyMessageID,
		History:        data.HistoryMessages,
		EA:             data.EA,
		Images:         images,
	}
	rawJSON, _ := json.Marshal(raw)

	msgType := channel.MsgTypeText
	if caption == "" && len(images) > 0 {
		msgType = channel.MsgTypeImage
	}
	msg := channel.InboundMessage{
		EventID:        msgID,
		MessageID:      msgID,
		AddressedToBot: true,
		Type:           msgType,
		Text:           text,
		CommandText:    caption,
		Source: channel.Source{
			ChannelType: TypeShareCRM,
			ChatID:      data.ChatID,
			ChatType:    chatType,
			SenderID:    senderID,
		},
		Raw: rawJSON,
	}
	normalizeShareCRMFreshControlLayout(&msg, len(images))
	if data.ReplyMessageID != nil && *data.ReplyMessageID > 0 {
		msg.ReplyTo = &channel.ReplyCtx{MessageID: strconv.FormatInt(*data.ReplyMessageID, 10)}
	}
	return msg, true
}

// normalizeShareCRMFreshControlLayout keeps image placeholders on a /clear
// turn before the shared Router handles it. Router treats CommandText="/clear"
// as a bare pending-fresh sentinel and otherwise overwrites Text with the
// command body, which would drop adapter-generated [Image] markers.
func normalizeShareCRMFreshControlLayout(msg *channel.InboundMessage, imageCount int) {
	control, ok := engine.ParseControlCommand(msg.CommandText)
	if !ok || control.Kind != engine.ControlCommandFreshSession {
		return
	}
	if control.Body == "" && imageCount == 0 {
		return
	}
	msg.ForceFresh = true
	msg.Text = joinNonEmpty("\n", control.Body, inboundImagePlaceholders(imageCount))
	if control.Body == "" {
		// Media-bearing `/clear` is a real turn. ForceFresh carries the
		// already-consumed directive so Router does not take the bare path.
		msg.CommandText = msg.Text
	}
}

// normalizeSenderID prefers the full E.{ea}.{id} form used as the binding key.
func normalizeSenderID(rawID, ea string) string {
	id := strings.TrimSpace(rawID)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "E.") {
		return id
	}
	ea = strings.TrimSpace(ea)
	if ea != "" {
		return "E." + ea + "." + id
	}
	return id
}

func stripLeadingMention(text, botFullID string) string {
	normalized := strings.TrimSpace(text)
	if normalized == "" || botFullID == "" {
		return normalized
	}
	candidates := []string{botFullID}
	if i := strings.LastIndex(botFullID, "."); i >= 0 && i+1 < len(botFullID) {
		candidates = append(candidates, botFullID[i+1:])
	}
	lower := strings.ToLower(normalized)
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// Match @candidate or candidate at the start, optional separators.
		for _, prefix := range []string{"@" + c, c} {
			pl := strings.ToLower(prefix)
			if !strings.HasPrefix(lower, pl) {
				continue
			}
			rest := strings.TrimSpace(normalized[len(prefix):])
			rest = strings.TrimLeft(rest, ",:：， \t-")
			return strings.TrimSpace(rest)
		}
	}
	return normalized
}

func collectInboundImages(data *botMessageData) []botImageRef {
	if data == nil || data.Message == nil {
		return nil
	}
	var out []botImageRef
	for _, image := range data.Message.Images {
		url := strings.TrimSpace(image.URL)
		if url == "" {
			continue
		}
		name := strings.TrimSpace(image.Filename)
		if name == "" {
			name = "image"
		}
		out = append(out, botImageRef{
			URL:      url,
			Filename: name,
			Width:    image.Width,
			Height:   image.Height,
			Size:     image.Size,
		})
	}
	return out
}

// sharecrmImagePlaceholder is the durable body marker the engine replaces
// with a stored attachment link. Same token DingTalk uses, so mixed
// caption+image messages keep a stable inline position even if one fetch fails.
const sharecrmImagePlaceholder = "[Image]"

func inboundImagePlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = sharecrmImagePlaceholder
	}
	return strings.Join(parts, "\n")
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, sep)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
