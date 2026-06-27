package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeSlack is the channel discriminator for the Slack adapter. It is defined
// here (not in the channel core package) on purpose: registering a new platform
// must not require editing the core, so the Type value lives with its adapter.
const TypeSlack channel.Type = "slack"

// maxMessageRunes caps a single outbound chat.postMessage body. Slack hard-caps
// a message around 40k characters; we chunk below that with headroom.
const maxMessageRunes = 38000

// slackChannel is the Slack implementation of channel.Channel. One instance is
// built per channel_installation by the registered Factory. It holds only what
// Connect/Send need (the decoded credentials + an API client); the installation
// identity is resolved per message by the Router, so it is absent here — the
// same split the Feishu adapter uses.
type slackChannel struct {
	creds     credentials
	api       *slack.Client
	handler   channel.InboundHandler
	logger    *slog.Logger
	mentionRe *regexp.Regexp
}

var _ channel.Channel = (*slackChannel)(nil)

func (c *slackChannel) Type() channel.Type { return TypeSlack }

// Connect opens the Slack Socket Mode WebSocket and runs the receive loop,
// blocking until ctx is cancelled or the connection drops — the contract
// engine.Supervisor relies on to tie lease renewal to connection liveness
// (matching feishuChannel.Connect). Each decoded Events API message is
// normalized to a channel.InboundMessage and handed to the engine handler. The
// envelope is ACKed immediately on receipt (Slack expires un-ACKed envelopes in
// ~3s) so the handler's slower DB work never races the ACK.
func (c *slackChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("slack: inbound handler not configured")
	}
	if c.api == nil {
		return errors.New("slack: api client not configured")
	}
	sm := socketmode.New(c.api)

	runErr := make(chan error, 1)
	go func() { runErr <- sm.RunContext(ctx) }()

	for {
		select {
		case <-ctx.Done():
			// Graceful teardown: the Supervisor cancelled the run context.
			return nil
		case err := <-runErr:
			// The managed connection loop ended. On ctx cancellation this is a
			// graceful stop; otherwise it is a real failure the Supervisor
			// retries under backoff.
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("slack: socket mode connection closed")
		case evt, ok := <-sm.Events:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("slack: socket mode event stream closed")
			}
			if err := c.handleSocketEvent(ctx, sm, evt); err != nil {
				// A handler error is an infrastructure failure (InboundHandler
				// contract): surface it so the Supervisor tears the connection
				// down and reconnects under backoff, instead of silently
				// dropping every subsequent event. ctx cancellation is a
				// graceful stop, not a failure.
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// Disconnect is a no-op: the Socket Mode loop is torn down by ctx cancellation
// (the Supervisor cancels the run context), mirroring feishuChannel.Disconnect.
func (c *slackChannel) Disconnect(ctx context.Context) error { return nil }

// Send delivers a minimal text reply via chat.postMessage, threading into
// out.ThreadID when set so a decoupled reply lands back in the originating
// thread. Long bodies are chunked under Slack's per-message cap; the returned
// SendResult carries the timestamp of the LAST posted chunk.
func (c *slackChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
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

// Capabilities declares what the Slack adapter supports TODAY. Declaration
// only — the engine performs no degradation, and callers pick a rendering from
// these bits, so declaring a capability the Send path cannot fulfil would
// mislead them. The minimal Send delivers text into a chat or thread, so only
// CapText | CapThreadReply are declared. Block Kit (CapRichCard), file
// attachments (CapAttachment) and chat.update edits (CapMessageEdit) are
// deferred until those paths are actually wired.
func (c *slackChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapThreadReply
}

// ---- inbound ----

func (c *slackChannel) handleSocketEvent(ctx context.Context, sm *socketmode.Client, evt socketmode.Event) error {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}
		// ACK first: Slack expires un-ACKed envelopes in ~3s, far below the
		// handler's DB work. The ACK is independent of the handler outcome —
		// a handler error is surfaced to the Supervisor (reconnect/backoff),
		// not retried through the un-ACK path.
		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack failed", "error", err)
			}
		}
		return c.dispatchEventsAPI(ctx, eventsAPI)
	case socketmode.EventTypeConnecting, socketmode.EventTypeConnected, socketmode.EventTypeHello:
		c.logger.DebugContext(ctx, "slack: socket mode", "event", evt.Type)
	case socketmode.EventTypeIncomingError, socketmode.EventTypeErrorBadMessage:
		c.logger.WarnContext(ctx, "slack: socket mode error", "event", evt.Type)
	default:
		// Interactive / slash-command / other events are out of scope for the
		// minimal adapter; ACK so Slack does not retry, then ignore.
		if evt.Request != nil {
			_ = sm.Ack(*evt.Request)
		}
	}
	return nil
}

func (c *slackChannel) dispatchEventsAPI(ctx context.Context, e slackevents.EventsAPIEvent) error {
	var (
		msg channel.InboundMessage
		ok  bool
	)
	switch inner := e.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		msg, ok = c.inboundFromAppMention(e, inner)
	case *slackevents.MessageEvent:
		msg, ok = c.inboundFromMessage(e, inner)
	default:
		return nil
	}
	if !ok {
		return nil
	}
	// A non-nil handler error is an infrastructure failure; propagate it so the
	// Supervisor reconnects (InboundHandler contract). A legitimate product
	// drop (dedup hit / unbound sender / group filter) returns nil — not an
	// error — so it does not tear the connection down.
	return c.handler(ctx, msg)
}

// inboundFromMessage normalizes a Slack message event. It returns ok=false for
// events that must not reach the core: the bot's own messages and other bots'
// messages (loop guard), and edits/deletes/joins and similar subtyped system
// messages (only brand-new user messages are ingested).
//
// Group addressing policy (v1, deliberate): a group message is addressed to the
// bot only when it carries an explicit <@bot> mention. Mention-free follow-ups
// inside a thread the bot is already engaged in are NOT auto-addressed here:
// "reply to a bot message" is session state, so it belongs in the session-aware
// shared service / resolver layer (which can detect an existing bound session
// for the thread and survive reconnects) rather than in per-connection adapter
// memory. Until that lands, channel/thread continuation requires re-mentioning
// the bot. P2P (DM) ingests every message, unchanged.
func (c *slackChannel) inboundFromMessage(e slackevents.EventsAPIEvent, m *slackevents.MessageEvent) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.SubType == "bot_message" {
		return channel.InboundMessage{}, false
	}
	if m.User == "" || (c.creds.BotUserID != "" && m.User == c.creds.BotUserID) {
		return channel.InboundMessage{}, false
	}
	if !isIngestableSubtype(m.SubType) {
		return channel.InboundMessage{}, false
	}

	chatType := slackChatType(m.Channel, m.ChannelType)
	addressed := chatType == channel.ChatTypeP2P || c.mentionsBot(m.Text)
	return c.buildInbound(e, buildInboundParams{
		eventType: "message",
		subType:   m.SubType,
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  chatType,
		addressed: addressed,
	}), true
}

// inboundFromAppMention normalizes an app_mention event. An app_mention is, by
// definition, addressed to the bot and occurs in a channel (group). The same
// channel @mention also arrives as a message event with the identical ts, so
// the engine's (installation, message_id=ts) dedup collapses the pair — no
// special-casing needed here.
func (c *slackChannel) inboundFromAppMention(e slackevents.EventsAPIEvent, m *slackevents.AppMentionEvent) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.User == "" || (c.creds.BotUserID != "" && m.User == c.creds.BotUserID) {
		return channel.InboundMessage{}, false
	}
	return c.buildInbound(e, buildInboundParams{
		eventType: "app_mention",
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  channel.ChatTypeGroup,
		addressed: true,
	}), true
}

type buildInboundParams struct {
	eventType string
	subType   string
	channelID string
	userID    string
	text      string
	ts        string
	threadTS  string
	chatType  channel.ChatType
	addressed bool
}

func (c *slackChannel) buildInbound(e slackevents.EventsAPIEvent, p buildInboundParams) channel.InboundMessage {
	teamID := e.TeamID
	if teamID == "" {
		teamID = c.creds.TeamID
	}
	raw, _ := json.Marshal(slackRawEvent{
		TeamID:      teamID,
		APIAppID:    e.APIAppID,
		EventType:   p.eventType,
		SubType:     p.subType,
		ChannelType: string(p.chatType),
	})
	var reply *channel.ReplyCtx
	if p.threadTS != "" && p.threadTS != p.ts {
		reply = &channel.ReplyCtx{MessageID: p.threadTS, RootID: p.threadTS}
	}
	return channel.InboundMessage{
		EventID:        p.ts,
		MessageID:      p.ts,
		Type:           channel.MsgTypeText,
		Text:           c.cleanText(p.text),
		ReplyTo:        reply,
		AddressedToBot: p.addressed,
		Source: channel.Source{
			ChannelType: TypeSlack,
			ChatID:      p.channelID,
			ChatType:    p.chatType,
			SenderID:    p.userID,
			ThreadID:    p.threadTS,
		},
		Raw: raw,
	}
}

// slackRawEvent carries the Slack-specific fields the cross-platform envelope
// does not — read back only inside the Slack resolvers (team_id routes the
// installation; the core never reads Raw).
type slackRawEvent struct {
	TeamID      string `json:"team_id"`
	APIAppID    string `json:"api_app_id,omitempty"`
	EventType   string `json:"event_type"`
	SubType     string `json:"subtype,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
}

// cleanText strips a leading/embedded bot mention token and trims surrounding
// whitespace so the core sees the user's actual prompt, not "<@U123> hi".
func (c *slackChannel) cleanText(text string) string {
	if c.mentionRe != nil {
		text = c.mentionRe.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

// mentionsBot reports whether text contains an @-mention of this bot. Slack
// renders a mention as <@U123> or <@U123|name>.
func (c *slackChannel) mentionsBot(text string) bool {
	return c.mentionRe != nil && c.mentionRe.MatchString(text)
}

// ---- helpers ----

// slackChatType maps a Slack channel id / channel_type to the normalized
// ChatType. Only a 1:1 direct message ("im", or a "D…" channel id) is p2p;
// everything else — public/private channels AND multi-party DMs ("mpim", which
// are multi-person conversations) — is a group. A group routes through the
// engine's "must address the bot" filter, so plain chatter in a multi-party DM
// is not mistaken for a prompt to the bot.
func slackChatType(channelID, channelType string) channel.ChatType {
	switch channelType {
	case "im":
		return channel.ChatTypeP2P
	case "mpim", "channel", "group", "private_channel":
		return channel.ChatTypeGroup
	}
	if strings.HasPrefix(channelID, "D") {
		return channel.ChatTypeP2P
	}
	return channel.ChatTypeGroup
}

// isIngestableSubtype reports whether a message subtype is a brand-new user
// message the core should ingest. Empty subtype is the normal case;
// thread_broadcast and file_share are real user messages; everything else
// (message_changed, message_deleted, channel_join, …) is a system/edit event.
func isIngestableSubtype(subType string) bool {
	switch subType {
	case "", "thread_broadcast", "file_share":
		return true
	default:
		return false
	}
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

// ---- registration ----

// SlackChannelDeps bundles the shared dependencies the Slack Factory closes
// over. The inbound handler is supplied per-build by the engine via
// channel.Config.Handler, mirroring FeishuChannelDeps.
type SlackChannelDeps struct {
	// Decrypt turns the stored bot/app token ciphertext into plaintext. A nil
	// Decrypter treats stored tokens as plaintext (tests / un-encrypted dev).
	Decrypt Decrypter
	Logger  *slog.Logger
}

// RegisterSlack registers the Slack Factory on reg under TypeSlack so the
// engine.Supervisor can build a slackChannel per installation. "Adding a
// channel" is this call plus the adapter — no engine edit.
func RegisterSlack(reg *channel.Registry, deps SlackChannelDeps) {
	reg.Register(TypeSlack, newSlackFactory(deps))
}

func newSlackFactory(deps SlackChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		creds, err := decodeCredentials(cfg.Raw, deps.Decrypt)
		if err != nil {
			return nil, err
		}
		if creds.BotToken == "" || creds.AppToken == "" {
			return nil, errors.New("slack: installation config missing bot or app token")
		}
		return newSlackChannel(creds, slack.New(creds.BotToken, slack.OptionAppLevelToken(creds.AppToken)), cfg.Handler, logger), nil
	}
}

// newSlackChannel builds a slackChannel from decoded credentials and a
// configured API client. Kept separate from the Factory so tests can inject a
// client pointed at an httptest server.
func newSlackChannel(creds credentials, api *slack.Client, handler channel.InboundHandler, logger *slog.Logger) *slackChannel {
	if logger == nil {
		logger = slog.Default()
	}
	var mentionRe *regexp.Regexp
	if creds.BotUserID != "" {
		mentionRe = regexp.MustCompile(`<@` + regexp.QuoteMeta(creds.BotUserID) + `(\|[^>]*)?>`)
	}
	return &slackChannel{
		creds:     creds,
		api:       api,
		handler:   handler,
		logger:    logger,
		mentionRe: mentionRe,
	}
}
