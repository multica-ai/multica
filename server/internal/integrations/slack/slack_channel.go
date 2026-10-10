package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// slackChannel is ONE installation's Socket Mode connection. Under the
// bring-your-own-app (BYO) model every Slack installation carries its own Slack
// app — its own app-level token (xapp-, stored encrypted in the installation
// config) — so it gets its own connection, exactly like the stage-3
// per-installation model and like Feishu today. The engine.Supervisor builds
// one slackChannel per active Slack installation (via the registered Factory)
// and owns the lease / reconnect lifecycle; Connect blocks on the receive loop.
//
// Inbound events are translated by the shared inbound.go helpers, parameterized
// by THIS installation's bot user id, and handed to the engine router, which
// resolves the installation by the event's api_app_id — equal to this app's id,
// the per-app routing key. Outbound replies primarily flow through the
// EventChatDone subscriber (NewOutbound); Send satisfies the Channel contract
// and posts with this installation's bot token.
type slackChannel struct {
	appID       string
	botUserID   string
	appToken    string        // decrypted xapp- — authorizes the Socket Mode connection
	botAPI      *slack.Client // bot-token client for outbound Send
	handler     channel.InboundHandler
	slash       *SlashCommandProcessor // nil disables /issue and /new slash-command handling
	interactURL string                 // nil/empty disables block_actions forwarding
	logger      *slog.Logger
}

// slashCommandTimeout bounds detached `/issue`, `/new`, and `/clear` processing
// (installation + identity resolution, mutation, response_url reply). It runs
// off the socket receive loop on its own context, so a slow DB or Slack HTTP
// call cannot wedge event delivery.
const slashCommandTimeout = 10 * time.Second

// interactionTimeout bounds detached `block_actions` forwarding (the webhook
// POST to interactURL). Mirrors slashCommandTimeout: it runs off the socket
// receive loop on its own context, so a slow or unreachable sink cannot wedge
// event delivery.
const interactionTimeout = 10 * time.Second

// interactionHTTPClient is shared across every slackChannel's forwarded
// block_actions POSTs. Plain client with no client-level timeout — each
// request's context (interactionTimeout, via dispatchInteraction) already
// bounds it, the same division of responsibility as the rest of this package.
var interactionHTTPClient = &http.Client{}

func (c *slackChannel) Type() channel.Type { return TypeSlack }

func (c *slackChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapThreadReply
}

// Disconnect is a no-op: the Socket Mode connection's whole lifetime is scoped
// to Connect (it returns when the run context is cancelled), so there is no
// long-lived resource to release here. Mirrors feishuChannel.Disconnect.
func (c *slackChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts an outbound reply with this installation's bot token, reusing the
// shared slackSender (Markdown→mrkdwn, chunking, threading).
func (c *slackChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return newSlackSender(credentials{BotUserID: c.botUserID}, c.botAPI, c.logger).Send(ctx, out)
}

// Connect opens this installation's Socket Mode connection (authenticated with
// its OWN app-level token) and runs the receive loop until ctx is cancelled or
// the link drops. It mirrors the removed AppConnector.connectOnce but is
// per-installation: the bot identity is fixed (this install's bot user id)
// rather than resolved per event by team_id.
func (c *slackChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("slack: inbound handler not configured")
	}
	if c.appToken == "" {
		return errors.New("slack: app-level token not configured")
	}
	// The Socket Mode connection authenticates with the app-level token alone;
	// the bot token is only for outbound Web API calls.
	api := slack.New("", slack.OptionAppLevelToken(c.appToken))
	sm := socketmode.New(api)

	// Each connection runs under its OWN cancellable context. Every exit path
	// (handler error, event-stream close, ctx cancellation) cancels runCtx and
	// waits for the run goroutine to observe it and exit, so a transient failure
	// tears the live connection down before the supervisor reconnects — no
	// leaked socket goroutine consuming events into an unread channel.
	runCtx, runCancel := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runErr <- sm.RunContext(runCtx)
		close(done)
	}()
	defer func() {
		runCancel()
		<-done
	}()

	mentionRe := compileMentionRe(c.botUserID)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-runErr:
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
			if err := c.handleSocketEvent(ctx, sm, evt, mentionRe); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (c *slackChannel) handleSocketEvent(ctx context.Context, sm *socketmode.Client, evt socketmode.Event, mentionRe *regexp.Regexp) error {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}
		// ACK first: Slack expires un-ACKed envelopes in ~3s, far below the
		// handler's DB work. The ACK is independent of the handler outcome.
		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack failed", "error", err)
			}
		}
		return c.dispatchEventsAPI(ctx, eventsAPI, mentionRe)
	case socketmode.EventTypeSlashCommand:
		// ACK first: like Events API envelopes, Slack expires an un-ACKed slash
		// command in ~3s, well under the DB + Slack HTTP work below. The reply is
		// delivered out-of-band via the command's response_url, so an empty ACK
		// is correct. Handling never fails the connection (product outcomes are
		// ephemeral replies, not infra errors).
		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack slash command failed", "error", err)
			}
		}
		cmd, ok := evt.Data.(slack.SlashCommand)
		if ok {
			envelopeID := ""
			if evt.Request != nil {
				envelopeID = evt.Request.EnvelopeID
			}
			c.dispatchSlashCommand(cmd, envelopeID)
		}
		return nil
	case socketmode.EventTypeInteractive:
		// ACK first: like Events API and slash-command envelopes, Slack expires
		// an un-ACKed interactive envelope (button/select click) in ~3s. The
		// reply, if any, goes out-of-band via the callback's response_url, so an
		// empty ACK is correct here too.
		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack interactive failed", "error", err)
			}
		}
		cb, ok := evt.Data.(slack.InteractionCallback)
		if ok {
			c.dispatchInteraction(cb)
		}
		return nil
	case socketmode.EventTypeConnecting, socketmode.EventTypeConnected, socketmode.EventTypeHello:
		c.logger.DebugContext(ctx, "slack: socket mode", "event", evt.Type, "app_id", c.appID)
	case socketmode.EventTypeIncomingError, socketmode.EventTypeErrorBadMessage:
		c.logger.WarnContext(ctx, "slack: socket mode error", "event", evt.Type, "app_id", c.appID)
	default:
		if evt.Request != nil {
			_ = sm.Ack(*evt.Request)
		}
	}
	return nil
}

// dispatchEventsAPI translates one Events API envelope to a normalized inbound
// message and hands it to the engine. A non-nil handler error is an
// infrastructure failure; it propagates so the supervisor reconnects. A
// legitimate product drop returns nil.
func (c *slackChannel) dispatchEventsAPI(ctx context.Context, e slackevents.EventsAPIEvent, mentionRe *regexp.Regexp) error {
	var (
		msg channel.InboundMessage
		ok  bool
	)
	switch inner := e.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		msg, ok = inboundFromAppMention(e, inner, c.botUserID, mentionRe)
	case *slackevents.MessageEvent:
		msg, ok = inboundFromMessage(e, inner, c.botUserID, mentionRe)
	default:
		return nil
	}
	if !ok {
		return nil
	}
	return c.handler(ctx, msg)
}

// dispatchSlashCommand processes an already-ACKed `/issue`, `/new`, or `/clear` command
// on a detached goroutine with its own bounded context, so the mutation and
// response_url reply never block the socket receive loop (mirrors the router's
// detached outbound path). A nil processor (slash handling not wired) drops it.
func (c *slackChannel) dispatchSlashCommand(cmd slack.SlashCommand, envelopeID string) {
	if c.slash == nil {
		c.logger.Warn("slack: slash command received but no processor configured",
			"command", cmd.Command, "app_id", c.appID)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), slashCommandTimeout)
		defer cancel()
		c.slash.HandleEnvelope(ctx, cmd, envelopeID)
	}()
}

// interactionForward is the subset of a `block_actions` InteractionCallback
// forwarded to interactURL. Field names/shape are this channel's own
// contract (not a Slack API shape) — kept deliberately small, just what a
// downstream handler needs to act on the click and reply via response_url.
type interactionForward struct {
	ResponseURL string `json:"response_url"`
	ActionID    string `json:"action_id"`
	Value       string `json:"value"`
	UserID      string `json:"user_id"`
	ChannelID   string `json:"channel_id"`
	MessageTs   string `json:"message_ts"`
}

// dispatchInteraction forwards an already-ACKed `block_actions` callback to
// interactURL on a detached goroutine with its own bounded context, mirroring
// dispatchSlashCommand: the webhook POST must never block the socket receive
// loop. Interactive buttons are additive — an unconfigured interactURL, a
// non-block_actions callback (view_submission and friends are out of scope
// for now), or a click with no block action are all silently dropped, never
// affecting the mention/DM/slash-command paths this shares a connection with.
// Only the first block action is forwarded: Slack sends exactly one per
// click in every case this package handles.
func (c *slackChannel) dispatchInteraction(cb slack.InteractionCallback) {
	if c.interactURL == "" {
		return
	}
	if cb.Type != slack.InteractionTypeBlockActions || len(cb.ActionCallback.BlockActions) == 0 {
		return
	}
	action := cb.ActionCallback.BlockActions[0]
	payload := interactionForward{
		ResponseURL: cb.ResponseURL,
		ActionID:    action.ActionID,
		Value:       action.Value,
		UserID:      cb.User.ID,
		ChannelID:   cb.Channel.ID,
		MessageTs:   cb.Container.MessageTs,
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), interactionTimeout)
		defer cancel()
		if err := c.postInteraction(ctx, payload); err != nil {
			c.logger.WarnContext(ctx, "slack: interaction forward failed",
				"app_id", c.appID, "action_id", payload.ActionID, "error", err)
		}
	}()
}

// postInteraction POSTs one interactionForward to interactURL as JSON.
func (c *slackChannel) postInteraction(ctx context.Context, payload interactionForward) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode interaction payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.interactURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := interactionHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ChannelDeps are the shared dependencies the Slack Factory closes over. The
// engine inbound handler is supplied per-build via channel.Config.Handler; the
// Decrypter turns the installation's stored ciphertext tokens into plaintext.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// Slash handles `/issue` and `/new` commands delivered over Socket Mode. Nil
	// leaves slash-command handling off (the connection still serves messages
	// and @-mentions); tests that only exercise inbound messages pass nil.
	Slash *SlashCommandProcessor
	// InteractionWebhookURL receives one JSON POST (interactionForward) per
	// `block_actions` click (button/select) delivered over Socket Mode. Empty
	// leaves interactive-component handling off — the connection still serves
	// messages, @-mentions, and slash commands.
	InteractionWebhookURL string
}

// RegisterSlack registers the per-installation Slack Factory so the
// engine.Supervisor builds + supervises one slackChannel per active Slack
// installation. "Adding Slack inbound" is this call plus the adapter — no engine
// edit (the same contract as lark.RegisterFeishu).
func RegisterSlack(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeSlack, newSlackFactory(deps))
}

func newSlackFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("slack: decode installation config: %w", err)
		}
		appToken, err := decryptToken(ic.AppTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("slack: decrypt app token: %w", err)
		}
		if appToken == "" {
			return nil, errors.New("slack: installation has no app-level token")
		}
		botToken, err := decryptToken(ic.BotTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("slack: decrypt bot token: %w", err)
		}
		return &slackChannel{
			appID:       ic.AppID,
			botUserID:   ic.BotUserID,
			appToken:    appToken,
			botAPI:      slack.New(botToken),
			handler:     cfg.Handler,
			slash:       deps.Slash,
			interactURL: deps.InteractionWebhookURL,
			logger:      logger,
		}, nil
	}
}
