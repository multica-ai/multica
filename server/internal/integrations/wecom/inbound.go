package wecom

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// wecomRawEvent carries WeCom-specific fields the cross-platform envelope
// does not — read back only inside the WeCom resolvers and media resolver
// (aibotid routes the installation, Media feeds engine.MediaResolver; the
// core never reads Raw).
type wecomRawEvent struct {
	AIBotID string              `json:"aibotid"`
	Media   []wecomRawMediaItem `json:"media,omitempty"`
}

// wecomRawMediaItem is one image/file/video reference carried on an inbound
// callback: a top-level image/file/video msgtype, or an image embedded in a
// mixed message's msg_item[] (spec §5.5, §5.6). URL and AESKey are the
// single-use, 5-minute WeCom media download credential — they live only
// here in Raw and must never be promoted to a logged field (media.go).
type wecomRawMediaItem struct {
	MsgType  string `json:"msgtype"`
	URL      string `json:"url"`
	AESKey   string `json:"aeskey"`
	Filename string `json:"filename,omitempty"`
}

// InboundFromMsgCallback translates an aibot_msg_callback frame into the
// engine's normalized channel.InboundMessage. Returns ok=false for frames that
// must not reach the core (malformed body, unsupported msgtype, missing sender).
func InboundFromMsgCallback(f Frame) (channel.InboundMessage, bool) {
	if f.Cmd != CmdMsgCallback || len(f.Body) == 0 {
		return channel.InboundMessage{}, false
	}
	var body MsgCallbackBody
	if err := json.Unmarshal(f.Body, &body); err != nil {
		return channel.InboundMessage{}, false
	}
	if body.MsgID == "" || body.From.UserID == "" || body.AIBotID == "" {
		return channel.InboundMessage{}, false
	}

	chatType := wecomChatType(body.ChatType)
	commandText, text, msgType, ok := extractMessageContent(body, chatType)
	if !ok {
		return channel.InboundMessage{}, false
	}
	if quotePrefix := formatQuotePrefix(body.Quote); quotePrefix != "" {
		text = quotePrefix + text
	}

	chatID := body.From.UserID
	if chatType == channel.ChatTypeGroup {
		chatID = body.ChatID
		if chatID == "" {
			return channel.InboundMessage{}, false
		}
	}

	raw, _ := json.Marshal(wecomRawEvent{AIBotID: body.AIBotID, Media: mediaItemsFromCallback(body)})
	return channel.InboundMessage{
		EventID:        body.MsgID,
		MessageID:      body.MsgID,
		Type:           msgType,
		Text:           text,
		CommandText:    commandText,
		ReplyTo:        nil, // WeCom quote has no message id (spec §5.5)
		AddressedToBot: true,
		Source: channel.Source{
			ChannelType: TypeWecom,
			ChatID:      chatID,
			ChatType:    chatType,
			SenderID:    body.From.UserID,
			ThreadID:    "",
		},
		Raw: raw,
	}, true
}

// mediaItemsFromCallback extracts every downloadable media reference from
// an inbound callback body: the top-level image/file/video payload, or each
// embedded image inside a mixed message's msg_item[] (spec §5.5, §5.6). This
// runs regardless of whether the msgtype also carries text — a mixed
// message with both text and an image must surface the image to
// engine.MediaResolver, not just the text.
func mediaItemsFromCallback(body MsgCallbackBody) []wecomRawMediaItem {
	switch body.MsgType {
	case "image":
		if body.Image == nil || body.Image.URL == "" {
			return nil
		}
		return []wecomRawMediaItem{{MsgType: "image", URL: body.Image.URL, AESKey: body.Image.AESKey}}
	case "file":
		if body.File == nil || body.File.URL == "" {
			return nil
		}
		return []wecomRawMediaItem{{MsgType: "file", URL: body.File.URL, AESKey: body.File.AESKey, Filename: body.File.Filename}}
	case "video":
		if body.Video == nil || body.Video.URL == "" {
			return nil
		}
		return []wecomRawMediaItem{{MsgType: "video", URL: body.Video.URL, AESKey: body.Video.AESKey}}
	case "mixed":
		if body.Mixed == nil {
			return nil
		}
		var items []wecomRawMediaItem
		for _, part := range body.Mixed.MsgItem {
			if part.MsgType != "image" || part.Image == nil || part.Image.URL == "" {
				continue
			}
			items = append(items, wecomRawMediaItem{MsgType: "image", URL: part.Image.URL, AESKey: part.Image.AESKey})
		}
		return items
	default:
		return nil
	}
}

func wecomChatType(wire string) channel.ChatType {
	switch wire {
	case ChatTypeSingle:
		return channel.ChatTypeP2P
	case ChatTypeGroup:
		return channel.ChatTypeGroup
	default:
		return channel.ChatTypeGroup
	}
}

func extractMessageContent(body MsgCallbackBody, chatType channel.ChatType) (commandText, text string, msgType channel.MsgType, ok bool) {
	switch body.MsgType {
	case "text":
		if body.Text == nil {
			return "", "", channel.MsgTypeText, false
		}
		raw := body.Text.Content
		commandText = normalizeCommandText(raw, chatType)
		return commandText, commandText, channel.MsgTypeText, true
	case "mixed":
		if body.Mixed == nil || len(body.Mixed.MsgItem) == 0 {
			return "", "", channel.MsgTypeText, false
		}
		var parts []string
		hasImage := false
		for _, item := range body.Mixed.MsgItem {
			switch item.MsgType {
			case "text":
				if item.Text != nil && strings.TrimSpace(item.Text.Content) != "" {
					parts = append(parts, item.Text.Content)
				}
			case "image":
				hasImage = true
			}
		}
		combined := strings.TrimSpace(strings.Join(parts, ""))
		commandText = normalizeCommandText(combined, chatType)
		text = commandText
		if hasImage && text == "" {
			return commandText, mediaPlaceholder("image"), channel.MsgTypeImage, true
		}
		return commandText, text, channel.MsgTypeText, true
	case "voice":
		if body.Voice == nil || strings.TrimSpace(body.Voice.Content) == "" {
			return "", "", channel.MsgTypeText, false
		}
		commandText = strings.TrimSpace(body.Voice.Content)
		return commandText, commandText, channel.MsgTypeText, true
	case "image":
		return "", mediaPlaceholder("image"), channel.MsgTypeImage, true
	case "file":
		return "", mediaPlaceholder("file"), channel.MsgTypeFile, true
	case "video":
		return "", mediaPlaceholder("video"), channel.MsgTypeVideo, true
	default:
		return "", "", channel.MsgTypeUnknown, false
	}
}

func normalizeCommandText(raw string, chatType channel.ChatType) string {
	text := strings.TrimSpace(raw)
	if chatType == channel.ChatTypeGroup {
		text = stripLeadingAtMentions(text)
	}
	return strings.TrimSpace(text)
}

// stripLeadingAtMentions removes consecutive @token segments from the start of
// group-chat text (spec §5.1).
func stripLeadingAtMentions(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if !strings.HasPrefix(s, "@") {
			return s
		}
		rest := s[1:]
		end := strings.IndexFunc(rest, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})
		if end < 0 {
			return ""
		}
		s = rest[end+1:]
	}
}

func formatQuotePrefix(q *QuoteBody) string {
	if q == nil {
		return ""
	}
	inner := quoteInnerContent(q)
	if inner == "" {
		return ""
	}
	return "<quoted_message type=\"" + q.MsgType + "\">" + inner + "</quoted_message>\n"
}

func quoteInnerContent(q *QuoteBody) string {
	switch q.MsgType {
	case "text":
		if q.Text != nil {
			return strings.TrimSpace(q.Text.Content)
		}
	case "voice":
		if q.Voice != nil {
			return strings.TrimSpace(q.Voice.Content)
		}
	case "image":
		return mediaPlaceholder("image")
	case "file":
		return mediaPlaceholder("file")
	case "video":
		return mediaPlaceholder("video")
	}
	return ""
}

func mediaPlaceholder(kind string) string {
	switch kind {
	case "image":
		return "[图片]"
	case "file":
		return "[文件]"
	case "video":
		return "[视频]"
	default:
		return "[附件]"
	}
}
