package telegram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeTelegram is the channel discriminator for the Telegram adapter. It is defined
// here (not in the channel core package) on purpose: registering a new platform
// must not require editing the core, so the Type value lives with its adapter.
const TypeTelegram channel.Type = "telegram"

// maxMessageRunes caps a single outbound sendMessage body. Telegram hard-caps
// a message at 4096 characters; we chunk below that with headroom.
const maxMessageRunes = 4000

// telegramSender posts agent replies back to Telegram via sendMessage. It is the
// OUTBOUND half: it holds the per-installation bot token (xoxb-) the reply must
// be sent with (inbound runs on the per-installation webhook in config.go). The
// installation identity (workspace / agent / installer) is resolved per message
// by the Router, so it is absent here.
type telegramSender struct {
	client *Client
	logger *slog.Logger
}

// Send delivers a minimal text reply via sendMessage, threading into
// out.ThreadID when set so a decoupled reply lands back in the originating
// thread. Long bodies are chunked under Telegram's per-message cap; the returned
// SendResult carries no MessageID (Telegram does not return message id on send).
func (s *telegramSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	for _, chunk := range chunkMessage(out.Text, maxMessageRunes) {
		if chunk == "" {
			continue
		}
		if err := s.client.SendMessage(ctx, out.ChatID, chunk, out.ThreadID); err != nil {
			return channel.SendResult{}, fmt.Errorf("telegram: sendMessage: %w", err)
		}
	}
	return channel.SendResult{}, nil
}

// newTelegramSender builds a Send-only client from decoded credentials and
// an optional API base URL override (for testing). Kept separate so tests can
// inject a client pointed at an httptest server.
func newTelegramSender(creds credentials, apiBase string, logger *slog.Logger) *telegramSender {
	if logger == nil {
		logger = slog.Default()
	}
	opts := []ClientOption{}
	if apiBase != "" {
		opts = append(opts, WithAPIBase(apiBase))
	}
	return &telegramSender{client: NewClient(creds.BotToken, opts...), logger: logger}
}

// chunkMessage splits text into <=maxRunes-rune pieces on rune boundaries so a
// long agent reply does not exceed Telegram's per-message cap. An empty body
// yields a single empty chunk (Telegram rejects truly empty text, but the caller
// guards against that upstream).
func chunkMessage(text string, maxRunes int) []string {
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return []string{text}
	}
	runes := []rune(text)
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
