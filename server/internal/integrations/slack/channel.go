package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// TypeSlack is the channel discriminator for the Slack adapter. It is defined
// here (not in the channel core package) on purpose: registering a new platform
// must not require editing the core, so the Type value lives with its adapter.
const TypeSlack channel.Type = "slack"

// maxMessageRunes caps a single outbound chat.postMessage body. Slack hard-caps
// a message around 40k characters; we chunk below that with headroom.
const maxMessageRunes = 38000

// maxMarkdownBlockRunes caps a single outbound Slack markdown block payload.
// Slack enforces a 12,000-character cumulative limit across all markdown blocks
// in one chat.postMessage request; keep headroom below that ceiling.
const maxMarkdownBlockRunes = 11500

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
// SendResult carries every posted chunk timestamp and keeps MessageID as the
// timestamp of the last chunk for existing callers.
func (c *slackSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return c.send(ctx, out, slack.SlackMetadata{})
}

func (c *slackSender) SendWithMetadata(ctx context.Context, out channel.OutboundMessage, metadata slack.SlackMetadata) (channel.SendResult, error) {
	return c.send(ctx, out, metadata)
}

// SendMarkdownWithMetadata delivers an agent's standard Markdown reply using
// Slack Block Kit markdown blocks so headings, nested lists, horizontal rules,
// fenced code blocks, and CJK bold/inline code render natively. Each chunk
// also carries a formatMrkdwn plain-text fallback in MsgOptionText for push
// notifications and clients that fall back to text.
func (c *slackSender) SendMarkdownWithMetadata(ctx context.Context, out channel.OutboundMessage, metadata slack.SlackMetadata) (channel.SendResult, error) {
	if c.api == nil {
		return channel.SendResult{}, errors.New("slack: api client not configured")
	}
	threadTS := outboundThreadTS(out)
	var lastTS string
	var messageIDs []string
	for _, chunk := range chunkMarkdown(out.Text, maxMarkdownBlockRunes) {
		fallback := formatMrkdwn(chunk)
		if utf8.RuneCountInString(fallback) > maxMessageRunes {
			fallback = string([]rune(fallback)[:maxMessageRunes])
		}
		opts := []slack.MsgOption{
			slack.MsgOptionText(fallback, false),
			slack.MsgOptionBlocks(slack.NewMarkdownBlock("", chunk)),
			slack.MsgOptionDisableLinkUnfurl(),
		}
		if metadata.EventType != "" {
			opts = append(opts, slack.MsgOptionMetadata(metadata))
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, ts, err := c.api.PostMessageContext(ctx, out.ChatID, opts...)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("slack: chat.postMessage: %w", err)
		}
		lastTS = ts
		messageIDs = append(messageIDs, ts)
	}
	return channel.SendResult{MessageID: lastTS, MessageIDs: messageIDs}, nil
}

func (c *slackSender) send(ctx context.Context, out channel.OutboundMessage, metadata slack.SlackMetadata) (channel.SendResult, error) {
	if c.api == nil {
		return channel.SendResult{}, errors.New("slack: api client not configured")
	}
	threadTS := outboundThreadTS(out)
	var lastTS string
	var messageIDs []string
	// Convert the agent's standard Markdown to Slack mrkdwn before posting so
	// bold/headers/links render instead of showing literal markup.
	for _, chunk := range chunkMessage(formatMrkdwn(out.Text), maxMessageRunes) {
		opts := []slack.MsgOption{
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionDisableLinkUnfurl(),
		}
		if metadata.EventType != "" {
			opts = append(opts, slack.MsgOptionMetadata(metadata))
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, ts, err := c.api.PostMessageContext(ctx, out.ChatID, opts...)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("slack: chat.postMessage: %w", err)
		}
		lastTS = ts
		messageIDs = append(messageIDs, ts)
	}
	return channel.SendResult{MessageID: lastTS, MessageIDs: messageIDs}, nil
}

const slackOutboundMetadataEvent = "multica_channel_outbound"

func outboundMetadata(bindingID pgtype.UUID, routeRevision int64, kind string) slack.SlackMetadata {
	return slack.SlackMetadata{EventType: slackOutboundMetadataEvent, EventPayload: map[string]any{
		"binding_id": util.UUIDToString(bindingID), "route_revision": routeRevision, "kind": kind,
	}}
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

// chunkMarkdown splits Markdown into <=maxRunes-rune chunks for Slack markdown
// blocks, preferring paragraph boundaries outside fenced code blocks, then
// line boundaries, and finally rune boundaries for single oversized lines.
// Splits inside a fenced code block close the fence at the end of the chunk
// and reopen it with the same fence header in the next chunk.
func chunkMarkdown(text string, maxRunes int) []string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}
	lines := strings.Split(text, "\n")
	var chunks []string
	var cur []string
	curRunes := 0
	lastParaSplit := 0

	var fenceChar byte
	var fenceLen int
	var fenceHeader string

	closeFence := func() string {
		if fenceLen == 0 {
			return ""
		}
		return strings.Repeat(string(fenceChar), fenceLen)
	}
	reserveRunes := func() int {
		if fenceLen == 0 {
			return 0
		}
		return 1 + fenceLen // "\n" + closing fence
	}
	flush := func(linesToFlush []string, appendClose bool) {
		body := strings.Join(linesToFlush, "\n")
		if appendClose {
			if body != "" {
				body += "\n"
			}
			body += closeFence()
		}
		if strings.TrimSpace(body) == "" {
			return
		}
		chunks = append(chunks, body)
	}
	resetForNextChunk := func(reopenFence bool) {
		cur = nil
		curRunes = 0
		lastParaSplit = 0
		if reopenFence && fenceLen > 0 {
			headerRunes := utf8.RuneCountInString(fenceHeader)
			if headerRunes+1+reserveRunes() < maxRunes {
				cur = []string{fenceHeader}
				curRunes = headerRunes
			}
		}
	}

	appendLine := func(line string) {
		lineRunes := utf8.RuneCountInString(line)
		sep := 0
		if len(cur) > 0 {
			sep = 1
		}
		if curRunes+sep+lineRunes+reserveRunes() <= maxRunes {
			cur = append(cur, line)
			curRunes += sep + lineRunes
			return
		}

		onlyFenceHeader := fenceLen > 0 && len(cur) == 1 && cur[0] == fenceHeader
		if len(cur) > 0 && !onlyFenceHeader {
			if fenceLen == 0 && lastParaSplit > 0 {
				flushed := cur[:lastParaSplit]
				rest := append([]string(nil), cur[lastParaSplit:]...)
				for len(flushed) > 0 && strings.TrimSpace(flushed[len(flushed)-1]) == "" {
					flushed = flushed[:len(flushed)-1]
				}
				if len(flushed) > 0 {
					flush(flushed, false)
					cur = rest
					curRunes = joinedRuneCount(cur)
					lastParaSplit = 0
					sep = 0
					if len(cur) > 0 {
						sep = 1
					}
					if curRunes+sep+lineRunes <= maxRunes {
						cur = append(cur, line)
						curRunes += sep + lineRunes
						return
					}
				}
			}
			if len(cur) > 0 {
				flush(cur, fenceLen > 0 && curRunes+reserveRunes() <= maxRunes)
				resetForNextChunk(fenceLen > 0)
			}
		}

		sep = 0
		if len(cur) > 0 {
			sep = 1
		}
		if curRunes+sep+lineRunes+reserveRunes() <= maxRunes {
			cur = append(cur, line)
			curRunes += sep + lineRunes
			return
		}

		rem := []rune(line)
		for len(rem) > 0 {
			sep = 0
			if len(cur) > 0 {
				sep = 1
			}
			avail := maxRunes - curRunes - sep - reserveRunes()
			if avail <= 0 {
				if len(cur) > 0 {
					flush(cur, false)
					cur = nil
					curRunes = 0
					lastParaSplit = 0
					sep = 0
				}
				avail = maxRunes
			}
			n := avail
			if n > len(rem) {
				n = len(rem)
			}
			piece := string(rem[:n])
			rem = rem[n:]
			cur = append(cur, piece)
			curRunes += sep + n
			if len(rem) > 0 {
				flush(cur, fenceLen > 0 && curRunes+reserveRunes() <= maxRunes)
				resetForNextChunk(fenceLen > 0)
			}
		}
	}

	for i, line := range lines {
		if fenceLen > 0 && isClosingFence(line, fenceChar, fenceLen) {
			sep := 0
			if len(cur) > 0 {
				sep = 1
			}
			lineRunes := utf8.RuneCountInString(line)
			if curRunes+sep+lineRunes <= maxRunes {
				cur = append(cur, line)
				curRunes += sep + lineRunes
			} else if len(cur) > 0 {
				flush(cur, curRunes+reserveRunes() <= maxRunes)
				resetForNextChunk(false)
			}
			fenceChar = 0
			fenceLen = 0
			fenceHeader = ""
			continue
		}

		if fenceLen == 0 {
			if ch, n, ok := parseOpeningFence(line); ok {
				sep := 0
				if len(cur) > 0 {
					sep = 1
				}
				headerRunes := utf8.RuneCountInString(line)
				nextRunes := 1
				if i+1 < len(lines) {
					nextRunes = utf8.RuneCountInString(lines[i+1])
				}
				// Flush prose before opening a fence if the fence header plus
				// at least its first line and closing fence cannot fit here.
				if len(cur) > 0 && curRunes+sep+headerRunes+1+nextRunes+1+n > maxRunes {
					flush(cur, false)
					resetForNextChunk(false)
				}
				appendLine(line)
				fenceChar = ch
				fenceLen = n
				fenceHeader = line
				continue
			}
		}

		appendLine(line)
		if fenceLen == 0 && strings.TrimSpace(line) == "" {
			lastParaSplit = len(cur)
		}
	}

	if len(cur) > 0 {
		flush(cur, false)
	}
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func joinedRuneCount(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	n := len(lines) - 1
	for _, l := range lines {
		n += utf8.RuneCountInString(l)
	}
	return n
}

func parseOpeningFence(line string) (byte, int, bool) {
	s := trimUpToThreeSpaces(line)
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return 0, 0, false
	}
	ch := s[0]
	n := 0
	for n < len(s) && s[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	info := s[n:]
	if ch == '`' && strings.ContainsRune(info, '`') {
		return 0, 0, false
	}
	return ch, n, true
}

func isClosingFence(line string, fenceChar byte, fenceLen int) bool {
	s := trimUpToThreeSpaces(line)
	n := 0
	for n < len(s) && s[n] == fenceChar {
		n++
	}
	if n < fenceLen {
		return false
	}
	return strings.Trim(s[n:], " \t") == ""
}

func trimUpToThreeSpaces(s string) string {
	i := 0
	for i < len(s) && i < 3 && s[i] == ' ' {
		i++
	}
	return s[i:]
}
