package engine

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/chattitle"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// DeriveChatTitle is the deterministic fallback shared by channel-created
// Chats. It measures Unicode code points, including the trailing ellipsis.
func DeriveChatTitle(body string) string {
	return chattitle.Derive(body)
}

func deriveFirstMessageTitle(body string, hasMedia bool) string {
	if hasMedia {
		body = withoutMediaPlaceholderLines(body)
	}
	return DeriveChatTitle(body)
}

func withoutMediaPlaceholderLines(body string) string {
	lines := make([]string, 0, strings.Count(body, "\n")+1)
	for _, line := range strings.Split(body, "\n") {
		switch strings.TrimSpace(line) {
		case "[Image]", "[File]", "[Audio]", "[Video]":
			continue
		default:
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func mediaTypeTitle(kind channel.MsgType) string {
	switch kind {
	case channel.MsgTypeImage:
		return "Image chat"
	case channel.MsgTypeAudio:
		return "Audio chat"
	case channel.MsgTypeVideo:
		return "Video chat"
	default:
		return "File chat"
	}
}
