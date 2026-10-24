package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// discordChannel is ONE installation's Gateway connection. Discord permits a
// single Gateway session per bot token, so the Supervisor's per-installation
// lease is also what keeps two replicas from fighting over the session.
type discordChannel struct {
	botID   string
	api     *apiClient
	gateway *gatewayConn
	handler channel.InboundHandler
	logger  *slog.Logger
}

func (c *discordChannel) Type() channel.Type { return TypeDiscord }

func (c *discordChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapQuoteReply
}

// Disconnect is a no-op: the Gateway session's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled).
func (c *discordChannel) Disconnect(ctx context.Context) error { return nil }

// Send posts an outbound reply with this installation's bot token.
func (c *discordChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return sendText(ctx, c.api, out.ChatID, out.Text, out.ReplyTo)
}

// Connect runs the Gateway receive loop until ctx is cancelled (returns nil)
// or the link fails in a way the loop cannot absorb (returns the error, and
// the Supervisor reconnects under backoff).
func (c *discordChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("discord: inbound handler not configured")
	}
	g := *c.gateway
	g.onMessage = func(ctx context.Context, m gatewayMessage) error {
		msg, ok := inboundFromGatewayMessage(c.botID, m)
		if !ok {
			return nil
		}
		if msg.Source.ChatType == channel.ChatTypeGroup && !msg.AddressedToBot {
			// Unaddressed guild chatter never reaches the pipeline: the Router
			// would drop it after a dedup claim, so skip the claim too.
			return nil
		}
		return c.handler(ctx, msg)
	}
	return g.run(ctx)
}

// ChannelDeps are the shared dependencies the Discord Factory closes over.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// APIBase overrides the REST root (tests). Empty uses production.
	APIBase string
	// GatewayURL overrides the Gateway endpoint (tests). Empty uses production.
	GatewayURL string
	// Dial overrides the Gateway dialer (tests). Nil uses gorilla.
	Dial wsDialer
}

// RegisterDiscord registers the per-installation Discord Factory so the
// engine.Supervisor builds + supervises one Gateway connection per active
// Discord installation. Same contract as RegisterSlack / RegisterTelegram.
func RegisterDiscord(reg *channel.Registry, deps ChannelDeps) {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	reg.Register(TypeDiscord, func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("discord: decode installation config: %w", err)
		}
		token, err := decryptToken(ic.BotTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("discord: decrypt bot token: %w", err)
		}
		if token == "" {
			return nil, errors.New("discord: installation has no bot token")
		}
		return &discordChannel{
			botID: ic.AppID,
			api:   newAPIClient(deps.APIBase, token, nil),
			gateway: &gatewayConn{
				botID:  ic.AppID,
				token:  token,
				url:    deps.GatewayURL,
				dial:   deps.Dial,
				logger: logger,
			},
			handler: cfg.Handler,
			logger:  logger,
		}, nil
	})
}
