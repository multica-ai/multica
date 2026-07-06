package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeSlack is the channel discriminator for the Slack adapter. It is defined
// here (not in the channel core package) on purpose: registering a new platform
// must not require editing the core, so the Type value lives with its adapter.
const TypeSlack channel.Type = "slack"

// maxMessageRunes caps a single outbound chat.postMessage body. Slack hard-caps
// a message around 40k characters; we chunk below that with headroom.
const maxMessageRunes = 38000

// slackSender posts agent replies back to Slack via chat.postMessage. It is the
// OUTBOUND half: it holds the per-installation bot token (xoxb-) the reply must
// be sent with (inbound runs on the per-installation Socket Mode connection in
// slack_channel.go). The installation identity (workspace / agent / installer)
// is resolved per message by the Router, so it is absent here.
type slackSender struct {
	creds  credentials
	api    *slack.Client
	logger *slog.Logger
}

// Send delivers a minimal text reply via chat.postMessage, threading into
// out.ThreadID when set so a decoupled reply lands back in the originating
// thread. Long bodies are chunked under Slack's per-message cap; the returned
// SendResult carries the timestamp of the LAST posted chunk.
func (c *slackSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.api == nil {
		return channel.SendResult{}, errors.New("slack: api client not configured")
	}
	threadTS := outboundThreadTS(out)
	var lastTS string
	// Convert the agent's standard Markdown to Slack mrkdwn before posting so
	// bold/headers/links render instead of showing literal markup.
	for _, chunk := range chunkMessage(formatMrkdwn(out.Text), maxMessageRunes) {
		opts := []slack.MsgOption{
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionDisableLinkUnfurl(),
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, ts, err := c.api.PostMessageContext(ctx, out.ChatID, opts...)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("slack: chat.postMessage: %w", err)
		}
		lastTS = ts
	}
	return channel.SendResult{MessageID: lastTS}, nil
}

// newSlackSender builds a Send-only client from decoded credentials and a
// configured API client. Kept separate from the outbound subscriber so tests
// can inject a client pointed at an httptest server.
func newSlackSender(creds credentials, api *slack.Client, logger *slog.Logger) *slackSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &slackSender{creds: creds, api: api, logger: logger}
}

// outboundThreadTS picks the Slack thread_ts for an outbound reply: an explicit
// quote target wins, else the thread the inbound message belonged to.
func outboundThreadTS(out channel.OutboundMessage) string {
	if out.ReplyTo != "" {
		return out.ReplyTo
	}
	return out.ThreadID
}

// chunkMessage splits text into <=maxRunes-rune pieces on rune boundaries so a
// long agent reply does not exceed Slack's per-message cap. An empty body
// yields a single empty chunk (Slack rejects truly empty text, but the caller
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
