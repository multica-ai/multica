package discord

import (
	"context"
	"fmt"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// maxMessageRunes caps a single outbound Discord message body. Discord hard
// caps content at 2000 characters; 1900 leaves headroom.
const maxMessageRunes = 1900

// sendText posts text as one or more messages and returns every delivered id.
// Long bodies are chunked on rune boundaries. replyTo quotes the triggering
// message (first chunk only).
func sendText(ctx context.Context, api *apiClient, channelID, text, replyTo string) (channel.SendResult, error) {
	if api == nil {
		return channel.SendResult{}, fmt.Errorf("discord: api client not configured")
	}
	var ids []string
	for i, chunk := range chunkMessage(text, maxMessageRunes) {
		ref := ""
		if i == 0 {
			ref = replyTo
		}
		id, err := api.SendMessage(ctx, channelID, chunk, ref)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("discord: send message: %w", err)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return channel.SendResult{}, fmt.Errorf("discord: nothing to send")
	}
	return channel.SendResult{MessageID: ids[len(ids)-1], MessageIDs: ids}, nil
}

// chunkMessage splits text into <=maxRunes-rune pieces on rune boundaries so a
// long agent reply does not exceed Discord's per-message cap.
func chunkMessage(text string, maxRunes int) []string {
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return []string{text}
	}
	var chunks []string
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
