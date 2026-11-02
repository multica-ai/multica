package lark

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

type contextMedia struct {
	items    []ContextMedia
	occupied string
	next     int
}

func (c *contextMedia) marker(res larkMediaResource) string {
	label := contextMediaLabel(res.kind)
	if res.messageID == "" || res.key == "" {
		return fmt.Sprintf("[%s: unavailable]", label)
	}
	for i := range c.items {
		item := &c.items[i]
		if item.MessageID == res.messageID && item.FetchType == res.fetchType && item.ResourceKey == res.key {
			item.Occurrences++
			return item.Placeholder
		}
	}
	var marker string
	for {
		c.next++
		marker = fmt.Sprintf("[%s %d: unavailable]", label, c.next)
		if !strings.Contains(c.occupied, marker) {
			break
		}
	}
	c.items = append(c.items, ContextMedia{MessageID: res.messageID, ResourceKey: res.key,
		Type: res.kind, FetchType: res.fetchType, Filename: res.filename, MimeType: res.mimeType,
		Placeholder: marker, Occurrences: 1})
	return marker
}

func (c *contextMedia) collides(body string) bool {
	for _, item := range c.items {
		if strings.Count(body, item.Placeholder) != item.Occurrences {
			return true
		}
	}
	return false
}

func contextMediaLabel(kind channel.MsgType) string {
	switch kind {
	case channel.MsgTypeImage:
		return "Image"
	case channel.MsgTypeAudio:
		return "Audio"
	case channel.MsgTypeVideo:
		return "Video"
	default:
		return "File"
	}
}

// Collect at rendering time, after topic filtering, deletion checks and forward
// truncation, so only context actually selected for this turn is downloaded.
func (e *inboundEnricher) flattenContextMessage(m LarkMessage, media *contextMedia, resourceMessageID string) string {
	if m.Deleted {
		return "[deleted message]"
	}
	messageID := m.MessageID
	// Forwarded child IDs are not valid resource download owners. Feishu
	// addresses their resource keys through the top-level forward container.
	if resourceMessageID != "" {
		messageID = resourceMessageID
	}
	marker := func(res larkMediaResource) string {
		res.messageID = messageID
		text := media.marker(res)
		// Retain names in the source transcript even when a download fails.
		if res.filename != "" {
			text += " (" + res.filename + ")"
		}
		return text
	}
	var body string
	if m.MessageType == "post" {
		body = flattenPostContentWithMedia(m.Content, marker)
	} else if resources := currentMediaResources(InboundMessage{MessageID: messageID, MessageType: m.MessageType, Content: m.Content}); len(resources) > 0 {
		body = marker(resources[0])
	} else {
		switch m.MessageType {
		case "image":
			body = "[Image: unavailable]"
		case "file":
			body = "[File: unavailable]"
		case "audio":
			body = "[Audio: unavailable]"
		case "media", "video":
			body = "[Video: unavailable]"
		default:
			return e.flattenMessage(m)
		}
	}
	return resolveMentions(body, restMentionsToEvent(m.Mentions), "", "")
}
