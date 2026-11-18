package infoflow

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/dolfly/infoflow/bot/events"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Chat id prefixes keep the outbound path self-describing. The engine treats
// Source.ChatID as an opaque binding key and hands it back verbatim as
// OutboundMessage.ChatID, so the adapter encodes the conversation kind into the
// id rather than adding a platform field to the cross-platform envelope.
const (
	chatIDPrefixP2P   = "u:"
	chatIDPrefixGroup = "g:"
)

// rawEvent is the adapter-private envelope persisted in
// channel.InboundMessage.Raw. It carries the receiving installation's AppKey
// (the routing key, since an InfoFlow event does not name its robot) plus the
// normalized fields the resolvers and outbound path need.
type rawEvent struct {
	AppID       string `json:"app_id"`
	MessageID   string `json:"message_id,omitempty"`
	ChatID      string `json:"chat_id,omitempty"`
	ChatType    string `json:"chat_type,omitempty"`
	SenderID    string `json:"sender_id,omitempty"`
	MessageType string `json:"message_type,omitempty"`
	Text        string `json:"text,omitempty"`
}

// inboundFromEvent translates one InfoFlow event into the normalized
// InboundMessage the engine consumes. It returns ok=false only for events that
// must not reach the core at all: lifecycle frames, non-message events, and
// messages without a sender or message id (system / bot-authored).
func inboundFromEvent(appKey string, ev events.Event) (channel.InboundMessage, bool) {
	payload := ev.Payload()
	if len(payload) == 0 {
		return channel.InboundMessage{}, false
	}
	chatType := channel.ChatTypeP2P
	if ev.ChatType == events.ChatGroup {
		chatType = channel.ChatTypeGroup
	}
	senderID := firstString(payload, "FromUserId", "fromuserid", "fromUserId", "senderid", "senderId")
	messageID := firstString(payload, "MsgId", "msgid", "messageId", "messageid")
	rawChatID := senderID
	if chatType == channel.ChatTypeGroup {
		header := firstMap(payload, "message", "header")
		senderID = firstNonEmpty(senderID, firstString(header, "FromUserId", "fromuserid", "senderid", "userid"))
		messageID = firstNonEmpty(messageID, firstString(header, "messageid", "msgid"))
		rawChatID = firstString(payload, "groupid", "groupId")
		if rawChatID == "" {
			rawChatID = firstString(header, "groupid", "groupId")
		}
	}
	if senderID == "" || messageID == "" {
		return channel.InboundMessage{}, false
	}
	if chatType == channel.ChatTypeGroup && rawChatID == "" {
		return channel.InboundMessage{}, false
	}

	text := extractText(payload, chatType == channel.ChatTypeGroup)
	if text == "" {
		// No text to feed the agent. Media-only messages are dropped until
		// InfoFlow media resolution lands; Capability() advertises text only.
		return channel.InboundMessage{}, false
	}
	msgType := normalizeMsgType(ev.MessageType)

	raw := rawEvent{
		AppID:       appKey,
		MessageID:   messageID,
		ChatID:      formatChatID(chatType, rawChatID),
		ChatType:    string(chatType),
		SenderID:    senderID,
		MessageType: string(msgType),
		Text:        text,
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return channel.InboundMessage{}, false
	}

	return channel.InboundMessage{
		EventID: messageID,
		// InfoFlow routes group messages only to the robot they address, so every
		// delivered group message is addressed by construction.
		AddressedToBot: true,
		MessageID:      messageID,
		Source:         channel.Source{ChannelType: TypeInfoFlow, ChatID: raw.ChatID, ChatType: chatType, SenderID: senderID},
		Type:           msgType,
		Text:           text,
		CommandText:    text,
		Raw:            encoded,
	}, true
}

// extractText flattens the platform's message body into agent-readable text.
// InfoFlow's webhook and WebSocket transports preserve the same protocol-open
// payload, whose content location differs between direct and group messages.
func extractText(payload map[string]any, group bool) string {
	if group {
		if message, ok := payload["message"].(map[string]any); ok {
			if body, ok := message["body"].([]any); ok {
				var b strings.Builder
				for _, item := range body {
					part, ok := item.(map[string]any)
					if !ok {
						continue
					}
					kind := strings.ToUpper(firstString(part, "type", "Type"))
					if kind != "TEXT" && kind != "MD" {
						continue
					}
					if content := firstString(part, "content", "Content"); content != "" {
						if b.Len() > 0 {
							b.WriteByte('\n')
						}
						b.WriteString(content)
					}
				}
				if b.Len() > 0 {
					return strings.TrimSpace(b.String())
				}
			}
			return strings.TrimSpace(firstString(message, "content", "Content"))
		}
		return strings.TrimSpace(firstString(payload, "content", "Content"))
	}
	if text, ok := payload["text"].(map[string]any); ok {
		if content := firstString(text, "content", "Content"); content != "" {
			return strings.TrimSpace(content)
		}
	}
	if md, ok := payload["md"].(map[string]any); ok {
		if content := firstString(md, "content", "Content"); content != "" {
			return strings.TrimSpace(content)
		}
	}
	if content := firstString(payload, "content", "Content"); content != "" {
		return strings.TrimSpace(content)
	}
	return ""
}

// normalizeMsgType maps the platform message type onto the closed channel set.
func normalizeMsgType(messageType string) channel.MsgType {
	switch strings.ToLower(strings.TrimSpace(messageType)) {
	case "text", "md", "richtext", "markdown", "":
		return channel.MsgTypeText
	case "image":
		return channel.MsgTypeImage
	case "file":
		return channel.MsgTypeFile
	case "voice", "audio":
		return channel.MsgTypeAudio
	case "video":
		return channel.MsgTypeVideo
	default:
		return channel.MsgTypeUnknown
	}
}

// formatChatID encodes the conversation kind into the binding key.
func formatChatID(chatType channel.ChatType, rawID string) string {
	if chatType == channel.ChatTypeGroup {
		return chatIDPrefixGroup + rawID
	}
	return chatIDPrefixP2P + rawID
}

// splitChatID decodes a binding key back into its conversation kind and raw
// platform id. An unprefixed id is treated as a direct message for backward
// compatibility with rows written before prefixes existed.
func splitChatID(chatID string) (channel.ChatType, string) {
	if rest := strings.TrimPrefix(chatID, chatIDPrefixGroup); rest != chatID {
		return channel.ChatTypeGroup, rest
	}
	if rest := strings.TrimPrefix(chatID, chatIDPrefixP2P); rest != chatID {
		return channel.ChatTypeP2P, rest
	}
	return channel.ChatTypeP2P, chatID
}

// sendTarget is the decoded outbound destination recovered from a binding key
// (and optionally its persisted config).
type sendTarget struct {
	ChatType channel.ChatType
	ChatID   string
}

// targetFromChatID decodes a binding key into a send target.
func targetFromChatID(chatID string) sendTarget {
	kind, raw := splitChatID(chatID)
	return sendTarget{ChatType: kind, ChatID: raw}
}

// firstString returns the first non-empty string value among keys.
func firstString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
			if n, ok := v.(json.Number); ok {
				return n.String()
			}
		}
	}
	return ""
}

// firstScalar walks a nested path of string keys and returns the first
// non-empty scalar. It is used for the group message envelope, where the id
// fields live under message.header.
func firstMap(payload map[string]any, path ...string) map[string]any {
	current := payload
	for i, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			if i == len(path)-1 {
				return nil
			}
			return nil
		}
		current = next
	}
	return current
}

// firstNonEmpty returns the first non-empty candidate.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// groupIDInt64 parses a group id for the SDK's SendToGroup, which requires a
// positive int64.
func groupIDInt64(raw string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, err
	}
	if id <= 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}
