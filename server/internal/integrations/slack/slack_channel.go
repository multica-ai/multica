package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"

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
	appID     string
	botUserID string
	appToken  string        // decrypted xapp- — authorizes the Socket Mode connection
	botAPI    *slack.Client // bot-token client for outbound Send
	handler   channel.InboundHandler
	logger    *slog.Logger
}

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

// ChannelDeps are the shared dependencies the Slack Factory closes over. The
// engine inbound handler is supplied per-build via channel.Config.Handler; the
// Decrypter turns the installation's stored ciphertext tokens into plaintext.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
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
			appID:     ic.AppID,
			botUserID: ic.BotUserID,
			appToken:  appToken,
			botAPI:    slack.New(botToken),
			handler:   cfg.Handler,
			logger:    logger,
		}, nil
	}
}
