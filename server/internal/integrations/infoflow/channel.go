package infoflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/dolfly/infoflow"
	"github.com/dolfly/infoflow/bot/events"
	"github.com/dolfly/infoflow/bot/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// ChannelDeps carries the process-wide dependencies the factory needs. It is
// built once in router.go and captured by every per-installation channel the
// factory creates.
type ChannelDeps struct {
	Box    *secretbox.Box
	Logger *slog.Logger
}

// infoflowChannel is ONE installation's InfoFlow WebSocket session. Every
// installation carries its own robot — its own AppKey plus encrypted AppSecret
// in the installation config — so it gets its own connection, exactly like the
// per-installation DingTalk and Slack adapters. The engine.Supervisor builds one
// infoflowChannel per active installation (via the registered Factory) and owns
// the lease / reconnect lifecycle; Connect blocks until the run context is
// cancelled.
//
// Inbound events are translated by inbound.go, parameterized by THIS
// installation's AppKey so the engine router can resolve the installation (the
// InfoFlow event carries no robot identity). Outbound replies flow through the
// EventChatDone subscriber (outbound.go) and the OutboundReplier; Send satisfies
// the Channel contract for a direct reply.
type infoflowChannel struct {
	appKey  string
	creds   credentials
	handler channel.InboundHandler
	logger  *slog.Logger
}

// RegisterInfoFlow registers the InfoFlow factory with the channel registry.
func RegisterInfoFlow(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeInfoFlow, func(cfg channel.Config) (channel.Channel, error) {
		if deps.Box == nil {
			return nil, errors.New("infoflow: encryption box is required")
		}
		creds, err := decodeCredentials(cfg.Raw, deps.Box.Open)
		if err != nil {
			return nil, err
		}
		logger := deps.Logger
		if logger == nil {
			logger = slog.Default()
		}
		return &infoflowChannel{
			appKey:  creds.AppKey,
			creds:   creds,
			handler: cfg.Handler,
			logger:  logger,
		}, nil
	})
}

func (c *infoflowChannel) Type() channel.Type { return TypeInfoFlow }

func (c *infoflowChannel) Capabilities() channel.Capability {
	return channel.CapText
}

// Disconnect is a no-op: the SDK session returns from Connect on context
// cancellation, and the engine.Supervisor owns the lifecycle.
func (c *infoflowChannel) Disconnect(context.Context) error { return nil }

// Connect opens this installation's InfoFlow WebSocket session (authenticated
// with its own AppKey/AppSecret) and blocks until ctx is cancelled. The SDK owns
// the socket and its reconnect loop; the engine.Supervisor owns the
// per-installation lease, so an error here just triggers a supervised redial.
func (c *infoflowChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("infoflow: inbound handler not configured")
	}
	if c.creds.AppSecret == "" {
		return errors.New("infoflow: app secret not configured")
	}
	client, err := infoflow.NewClient(infoflow.Config{AppKey: c.creds.AppKey, AppSecret: c.creds.AppSecret})
	if err != nil {
		return fmt.Errorf("infoflow: build client: %w", err)
	}
	handle := func(ctx context.Context, ev events.Event) error {
		return c.onEvent(ctx, ev)
	}
	// Register both namespaces; the dispatcher routes by ChatType.
	if err := client.Bot.Events.On("private.*", handle); err != nil {
		return fmt.Errorf("infoflow: register private events: %w", err)
	}
	if err := client.Bot.Events.On("group.*", handle); err != nil {
		return fmt.Errorf("infoflow: register group events: %w", err)
	}
	session, err := client.Bot.WebSocket.NewSession(websocket.SessionOptions{})
	if err != nil {
		return fmt.Errorf("infoflow: build session: %w", err)
	}
	return session.Connect(ctx)
}

// onEvent normalizes one SDK event and hands it to the engine. Non-message
// lifecycle frames and unroutable events are dropped quietly.
func (c *infoflowChannel) onEvent(ctx context.Context, ev events.Event) error {
	in, ok := inboundFromEvent(c.appKey, ev)
	if !ok {
		return nil
	}
	if err := c.handler(ctx, in); err != nil {
		return fmt.Errorf("infoflow: handle inbound: %w", err)
	}
	return nil
}

// Send posts a direct reply into out.ChatID with this installation's robot. It
// satisfies the Channel contract; the primary reply paths (EventChatDone
// subscriber and OutboundReplier) build their own sender with a full target.
func (c *infoflowChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	client, err := newSDKClient(c.creds)
	if err != nil {
		return channel.SendResult{}, err
	}
	return sendText(ctx, client, targetFromChatID(out.ChatID), out.Text)
}

// newSDKClient builds an InfoFlow SDK client from decrypted credentials.
func newSDKClient(creds credentials) (*infoflow.Client, error) {
	return infoflow.NewClient(infoflow.Config{AppKey: creds.AppKey, AppSecret: creds.AppSecret})
}

// sendText delivers one text message through a client, choosing the user or
// group endpoint from the target kind.
func sendText(ctx context.Context, client *infoflow.Client, target sendTarget, text string) (channel.SendResult, error) {
	if text == "" {
		return channel.SendResult{}, errors.New("infoflow: empty message")
	}
	switch target.ChatType {
	case channel.ChatTypeGroup:
		groupID, err := groupIDInt64(target.ChatID)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("infoflow: invalid group id %q: %w", target.ChatID, err)
		}
		res, err := client.Bot.IM.Message.SendToGroup(ctx, groupID, text)
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageID, 10)}, nil
	default:
		res, err := client.Bot.IM.Message.SendToUser(ctx, target.ChatID, text)
		if err != nil {
			return channel.SendResult{}, err
		}
		return channel.SendResult{MessageID: strconv.FormatInt(res.MessageKey, 10)}, nil
	}
}
