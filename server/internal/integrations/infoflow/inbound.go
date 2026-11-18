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
	// Media is the downloadable half of the message, normalized at decode
	// time. It is persisted rather than re-derived because the engine's
	// MediaResolver contract requires HasMedia to be a pure in-memory check
	// (it runs synchronously on the connector ACK path), and because the
	// resolver must never have to parse the platform payload twice.
	Media []mediaDescriptor `json:"media,omitempty"`
}

// mediaDescriptor is one artifact on an inbound message that the media
// resolver can turn into a Multica attachment.
//
// InfoFlow delivers images, files and voice the same way: as a chat file the
// robot resolves by FID (im.File.Download). An image key (im.File.GetURL) is
// the fallback for a payload that carries one instead, which is what the
// outbound image API speaks.
type mediaDescriptor struct {
	Kind channel.MsgType `json:"kind"`
	// FID is the platform chat-file id. Set for anything the robot can read
	// back through the file API.
	FID string `json:"fid,omitempty"`
	// ObjectKey is a BOS object key, the form an outbound image carries.
	ObjectKey string `json:"object_key,omitempty"`
	// Ref is a reference whose kind the payload does not pin down — a group
	// body part carries its identifier in "content", which is an FID for a chat
	// file and an object key for an image. The resolver tries both routes rather
	// than guessing which one the deployment's platform speaks.
	Ref       string `json:"ref,omitempty"`
	Filename  string `json:"filename,omitempty"`
	MimeType  string `json:"mime_type,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

// fetchable reports whether this descriptor carries anything to download.
// A descriptor without either reference is dropped at decode rather than
// persisted, so a ref that can only ever fail never reaches the resolver.
func (d mediaDescriptor) fetchable() bool {
	return d.Kind != "" && (d.FID != "" || d.ObjectKey != "" || d.Ref != "")
}

// references returns every reference this descriptor offers, in the order the
// resolver should try them: an explicit FID is unambiguous, an explicit object
// key is the outbound form, and Ref is tried both ways.
func (d mediaDescriptor) references() []string {
	var out []string
	seen := map[string]bool{}
	add := func(ref string) {
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		out = append(out, ref)
	}
	add(d.FID)
	add(d.ObjectKey)
	add(d.Ref)
	return out
}

// inboundFromEvent translates one InfoFlow event into the normalized
// InboundMessage the engine consumes. It returns ok=false only for events that
// must not reach the core at all: lifecycle frames, non-message events, and
// messages without a sender or message id (system / bot-authored). Media-only
// messages are NOT dropped: they reach the core as a placeholder line the
// engine's media resolver replaces with attachments.
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

	msgType := normalizeMsgType(ev.MessageType)
	media := extractMedia(payload)
	text := extractText(payload, chatType == channel.ChatTypeGroup)
	if len(media) > 0 {
		// A placeholder per attachment, in platform order, so the message the
		// agent reads names what it is missing if the resolver cannot fetch it.
		// The media resolver binds the same refs onto this same chat_message, and
		// the engine substitutes them into this text.
		if text == "" {
			text = strings.Join(mediaPlaceholderLines(media), "\n")
		} else {
			text = text + "\n" + strings.Join(mediaPlaceholderLines(media), "\n")
		}
	}
	if text == "" {
		return channel.InboundMessage{}, false
	}

	raw := rawEvent{
		AppID:       appKey,
		MessageID:   messageID,
		ChatID:      formatChatID(chatType, rawChatID),
		ChatType:    string(chatType),
		SenderID:    senderID,
		MessageType: string(msgType),
		Text:        text,
		Media:       media,
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

// mediaPlaceholder* is what the agent reads when a media message's bytes could
// not be bound, and what it reads before the resolver runs at all. English,
// like every other user-facing string this adapter writes; the sibling DingTalk
// adapter uses the same "[Image]" shape.
const (
	mediaImagePlaceholder = "[Image]"
	mediaFilePlaceholder  = "[File]"
)

// mediaPlaceholderLines renders one line per attachment, naming the file when
// the platform told us its name. The name is part of the prompt: "[File]" tells
// the agent nothing about what was sent, while "[File: spec.pdf]" often tells it
// everything.
func mediaPlaceholderLines(media []mediaDescriptor) []string {
	lines := make([]string, 0, len(media))
	for _, d := range media {
		lines = append(lines, mediaPlaceholderLine(d))
	}
	return lines
}

func mediaPlaceholderLine(d mediaDescriptor) string {
	name := strings.TrimSpace(d.Filename)
	switch {
	case d.Kind == channel.MsgTypeImage:
		return mediaImagePlaceholder
	case d.Kind == channel.MsgTypeAudio:
		if name != "" {
			return "[Audio: " + name + "]"
		}
		return "[Audio]"
	case d.Kind == channel.MsgTypeVideo:
		if name != "" {
			return "[Video: " + name + "]"
		}
		return "[Video]"
	default:
		if name != "" {
			return "[File: " + name + "]"
		}
		return mediaFilePlaceholder
	}
}

// extractMedia collects every downloadable artifact on one message, in platform
// order. A private message names its body in a type-keyed map ("image",
// "file"); a group message carries an ordered body array of typed parts, so a
// mixed text-and-image message yields one descriptor per part. Descriptors that
// carry no reference the robot could resolve are dropped here rather than
// persisted.
func extractMedia(payload map[string]any) []mediaDescriptor {
	if message, ok := payload["message"].(map[string]any); ok {
		if parts, ok := message["body"].([]any); ok {
			var out []mediaDescriptor
			for _, item := range parts {
				part, ok := item.(map[string]any)
				if !ok {
					continue
				}
				kind := mediaKindForType(firstString(part, "type", "Type"))
				if kind == channel.MsgTypeUnknown {
					continue
				}
				if d := mediaDescriptorFromMap(part, kind); d.fetchable() {
					out = append(out, d)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	var out []mediaDescriptor
	for _, key := range []string{"image", "file", "voice", "audio", "video"} {
		m, ok := payload[key].(map[string]any)
		if !ok {
			continue
		}
		kind := mediaKindForKey(key)
		if d := mediaDescriptorFromMap(m, kind); d.fetchable() {
			out = append(out, d)
			continue
		}
		// Some payloads carry only the body's metadata and put the reference
		// beside it at the top level; fall back to the message envelope itself
		// before giving up on the attachment.
		if d := mediaDescriptorFromMap(payload, kind); d.fetchable() {
			out = append(out, d)
		}
	}
	return out
}

// mediaDescriptorFromMap reads one artifact's reference and metadata. The key
// spellings are deliberately wide: the SDK classifies a message by MsgType but
// does not pin the body shape, and both the WebSocket and webhook transports
// carry the same protocol-open payload with the provider's own casing.
func mediaDescriptorFromMap(m map[string]any, kind channel.MsgType) mediaDescriptor {
	return mediaDescriptor{
		Kind:      kind,
		FID:       firstString(m, "fid", "Fid", "FID", "file_id", "fileId", "fileID"),
		ObjectKey: firstString(m, "object_key", "objectKey", "image_key", "imageKey", "imageKeyId", "key"),
		Ref:       firstString(m, "content", "Content"),
		Filename:  firstString(m, "filename", "fileName", "FileName", "name", "Name"),
		MimeType:  firstString(m, "mime_type", "mimeType", "MimeType", "content_type", "contentType", "ContentType"),
		SizeBytes: firstInt64(m, "size", "FileSize", "fileSize", "file_size"),
	}
}

// mediaKindForType maps a group body part type onto the closed media set. Parts
// that are text (TEXT / MD / RICHTEXT / LINK) return MsgTypeUnknown and are left
// to extractText.
func mediaKindForType(raw string) channel.MsgType {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "IMAGE":
		return channel.MsgTypeImage
	case "FILE":
		return channel.MsgTypeFile
	case "VOICE", "AUDIO":
		return channel.MsgTypeAudio
	case "VIDEO":
		return channel.MsgTypeVideo
	default:
		return channel.MsgTypeUnknown
	}
}

// mediaKindForKey maps a private-message body key onto the closed media set.
func mediaKindForKey(key string) channel.MsgType {
	switch strings.ToLower(key) {
	case "image":
		return channel.MsgTypeImage
	case "voice", "audio":
		return channel.MsgTypeAudio
	case "video":
		return channel.MsgTypeVideo
	default:
		return channel.MsgTypeFile
	}
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

// firstInt64 returns the first numeric value among keys, accepting both
// json.Number (the SDK's decoder setting) and float64.
func firstInt64(payload map[string]any, keys ...string) int64 {
	for _, key := range keys {
		v, ok := payload[key]
		if !ok {
			continue
		}
		switch n := v.(type) {
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i
			}
		case float64:
			return int64(n)
		case string:
			if i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
				return i
			}
		}
	}
	return 0
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
