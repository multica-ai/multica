package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// installationConfig is the JSON shape stored in channel_installation.config
// for a WeCom installation (spec §3.1). AppID is WeCom's aibotid — the wire's
// aibot_subscribe body names this field "bot_id", so the factory maps AppID
// onto ConnConfig.BotID. SecretEncrypted is base64 secretbox ciphertext
// produced at install time (Task 6, not yet wired). Locale selects the
// welcome/render text map in phase 2 and is otherwise inert in phase 0.
type installationConfig struct {
	AppID           string `json:"app_id"`
	SecretEncrypted string `json:"secret_encrypted"`
	Locale          string `json:"locale"`
}

// wecomChannel is one WeCom installation's long-connection lifecycle: it is
// the channel.Channel the engine.Supervisor builds via the registered
// Factory and drives through Connect/Disconnect. Phase 0 wires only the
// subscribe handshake and the pump plumbing in Conn; inbound translation
// (aibot_msg_callback -> channel.InboundMessage) and the outbound send
// queue land in later phases (spec §5.1 vs §5.3).
type wecomChannel struct {
	installationID string
	botID          string
	secret         string
	locale         string
	handler        channel.InboundHandler
	logger         *slog.Logger
	metrics        WecomMetrics
	wake           *OutboundWakeRegistry
	outbox         OutboxDeps
	retry          *RetryState
	dialURL        string

	mu     sync.Mutex
	cancel context.CancelFunc
}

var _ channel.Channel = (*wecomChannel)(nil)

// Type reports the platform discriminator.
func (c *wecomChannel) Type() channel.Type { return TypeWecom }

// Capabilities declares markdown text outbound only: WeCom's aibot_send_msg
// carries a single markdown body (spec §3.2), there is no thread/topic
// concept on the long-connection API, and phase 0 ships no streaming edit
// path — so no CapMessageEdit, no CapRichCard, no CapThreadReply.
func (c *wecomChannel) Capabilities() channel.Capability {
	return channel.CapText
}

// Connect dials the WeCom long connection for this installation (via Conn)
// and blocks until ctx is cancelled or the link drops, per the
// channel.Channel contract. It registers the installation's outbound wake
// channel and runs the outbox consumer for the connection lifetime (spec §5.3).
func (c *wecomChannel) Connect(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.cancel = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		c.cancel = nil
		c.mu.Unlock()
	}()

	var wakeCh <-chan struct{}
	if c.wake != nil {
		wakeCh = c.wake.Register(c.installationID)
		defer c.wake.Unregister(c.installationID)
	}

	conn, err := NewConn(ConnConfig{
		DialURL:         c.dialURL,
		BotID:           c.botID,
		Secret:          c.secret,
		InstallationID:  c.installationID,
		Logger:          c.logger,
		Metrics:         c.metrics,
		Retry:           c.retry,
		OnMsgCallback:   c.onMsgCallback,
		OnEventCallback: c.onEventCallback,
		OnWelcome:       c.onWelcome,
	})
	if err != nil {
		return fmt.Errorf("wecom: build conn: %w", err)
	}

	if c.outbox.Queries != nil {
		rate := c.outbox.Rate
		if rate == nil && c.outbox.Tx != nil {
			rate = NewRateGate(c.outbox.Queries, c.outbox.Tx)
		}
		consumer, cerr := NewOutboxConsumer(OutboxConsumerConfig{
			InstallationID: c.installationID,
			Locale:         c.locale,
			Queries:        c.outbox.Queries,
			Binding:        c.outbox.Binding,
			Rate:           rate,
			Conn:           conn,
			Wake:           wakeCh,
			AppURL:         c.outbox.AppURL,
			Logger:         c.logger,
			Metrics:        c.metrics,
		})
		if cerr != nil {
			return fmt.Errorf("wecom: build outbox consumer: %w", cerr)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			consumer.Run(runCtx)
		}()
		err = conn.Run(runCtx)
		// conn.Run can return without ctx being cancelled (disconnect, auth
		// failure). The outbox consumer only exits on runCtx.Done(), so we
		// must cancel it here or wg.Wait() below blocks forever.
		cancel()
		wg.Wait()
		return err
	}

	return conn.Run(runCtx)
}

func (c *wecomChannel) onMsgCallback(ctx context.Context, f Frame) {
	if c.handler == nil {
		return
	}
	msg, ok := InboundFromMsgCallback(f)
	if !ok {
		return
	}
	if err := c.handler(ctx, msg); err != nil {
		c.logger.WarnContext(ctx, "wecom: inbound handler failed",
			"installation_id", c.installationID,
			"message_id", msg.MessageID,
			"error", err,
		)
	}
}

func (c *wecomChannel) onEventCallback(ctx context.Context, f Frame) {
	eventType := peekEventType(f.Body)
	if eventType == "" {
		return
	}
	c.logger.DebugContext(ctx, "wecom: event callback received",
		"installation_id", c.installationID,
		"eventtype", eventType,
		"req_id", f.Headers.ReqID,
	)
}

// Disconnect tears the connection down by cancelling the context Connect is
// running under. It is a no-op (returns nil) when Connect is not currently
// running, and safe to call more than once.
func (c *wecomChannel) Disconnect(context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// ErrSendNotWired is returned by Send until the phase-2 outbound queue
// (spec §5.3) is wired. Wiring a direct WS send here would create a second,
// unqueued outbound path alongside the queue consumer's own SendRequest
// calls — exactly the dual-path this adapter avoids; see ws_conn.go's
// SendRequest for the (already unit-tested) wire-level send.
var ErrSendNotWired = errors.New("wecom: outbound queue not wired")

// Send is not wired in phase 0; see ErrSendNotWired.
func (c *wecomChannel) Send(context.Context, channel.OutboundMessage) (channel.SendResult, error) {
	return channel.SendResult{}, ErrSendNotWired
}

// newWecomFactory builds the channel.Factory RegisterWecom registers. It
// validates cfg.InstallationID, decodes+validates the installation config,
// decrypts the bot secret, and resolves a shared RetryState so the
// engine.Supervisor's per-attempt rebuilds (see supervisor.go's supervise
// loop) do not reset the auth-fail / kick backoff streak.
func newWecomFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := deps.Metrics
	if metrics == nil {
		metrics = NoopMetrics()
	}
	dialURL := deps.DialURL
	if dialURL == "" {
		dialURL = DefaultDialURL
	}

	return func(cfg channel.Config) (channel.Channel, error) {
		if !cfg.InstallationID.Valid {
			return nil, errors.New("wecom: channel.Config.InstallationID is required")
		}
		installationID := util.UUIDToString(cfg.InstallationID)

		var ic installationConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("wecom: decode installation config: %w", err)
		}
		if ic.AppID == "" {
			return nil, errors.New("wecom: installation config missing app_id")
		}
		if ic.SecretEncrypted == "" {
			return nil, errors.New("wecom: installation config missing secret_encrypted")
		}
		if deps.Decrypt == nil {
			return nil, errors.New("wecom: ChannelDeps.Decrypt is required")
		}
		secret, err := deps.Decrypt(ic.SecretEncrypted)
		if err != nil {
			return nil, fmt.Errorf("wecom: decrypt secret: %w", err)
		}

		var retry *RetryState
		if deps.Retries != nil {
			retry = deps.Retries.Get(installationID)
		} else {
			retry = NewRetryState()
		}

		return &wecomChannel{
			installationID: installationID,
			botID:          ic.AppID,
			secret:         string(secret),
			locale:         ic.Locale,
			handler:        cfg.Handler,
			logger:         logger,
			metrics:        metrics,
			wake:           deps.Wake,
			outbox:         deps.Outbox,
			retry:          retry,
			dialURL:        dialURL,
		}, nil
	}
}
