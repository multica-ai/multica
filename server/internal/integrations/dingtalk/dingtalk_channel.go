package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeDingtalk is the channel discriminator for the DingTalk adapter. It is
// defined here (not in the channel core package) on purpose: registering a
// new platform must not require editing the core, so the Type value lives
// with its adapter. It MUST equal channelTypeDingTalk (the store-side
// constant) — both name the same channel_type column value.
const TypeDingtalk channel.Type = channelTypeDingTalk

// Decrypter turns the installation's stored ciphertext client_secret into
// plaintext (secretbox.Box.Open in production).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

var errEmptyRaw = errors.New("dingtalk: inbound message Raw is empty")

// channelCredentials is the decrypted per-installation identity the
// adapter's transport needs.
type channelCredentials struct {
	ClientID     string
	ClientSecret string
}

// decodeChannelCredentials reads the installation config blob (the same
// dingtalkInstallConfig shape the store writes) and decrypts the secret.
func decodeChannelCredentials(raw json.RawMessage, decrypt Decrypter) (channelCredentials, error) {
	var cfg dingtalkInstallConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return channelCredentials{}, fmt.Errorf("dingtalk: decode installation config: %w", err)
	}
	if cfg.AppID == "" {
		return channelCredentials{}, errors.New("dingtalk: installation has no app_id")
	}
	sealed, err := decodeSecret(cfg.AppSecretEncrypted)
	if err != nil {
		return channelCredentials{}, fmt.Errorf("dingtalk: decode app_secret_encrypted: %w", err)
	}
	if len(sealed) == 0 {
		return channelCredentials{}, errors.New("dingtalk: installation has no client_secret")
	}
	if decrypt == nil {
		return channelCredentials{}, errors.New("dingtalk: decrypter not configured")
	}
	plain, err := decrypt(sealed)
	if err != nil {
		return channelCredentials{}, fmt.Errorf("dingtalk: decrypt client_secret: %w", err)
	}
	return channelCredentials{ClientID: cfg.AppID, ClientSecret: string(plain)}, nil
}

// dingtalkChannel is ONE installation's Stream Mode connection. Every
// installation carries its own app (minted by the scan-to-create device
// flow), so it gets its own connection — the engine.Supervisor builds one
// dingtalkChannel per active installation via the registered Factory and
// owns the lease / reconnect lifecycle; Connect blocks on the receive loop.
type dingtalkChannel struct {
	creds       channelCredentials
	openAPIBase string
	httpClient  *http.Client
	handler     channel.InboundHandler
	messenger   *RobotMessenger
	logger      *slog.Logger
}

func (c *dingtalkChannel) Type() channel.Type { return TypeDingtalk }

func (c *dingtalkChannel) Capabilities() channel.Capability {
	return channel.CapText
}

// Disconnect is a no-op: the connection's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled), so there is no
// long-lived resource to release here. Mirrors slackChannel.Disconnect.
func (c *dingtalkChannel) Disconnect(ctx context.Context) error { return nil }

// Send delivers an outbound reply via the robot message API. The engine's
// OutboundMessage carries the conversation id; for DM conversations the
// robot API wants the recipient's staff id instead, which the outbound
// subscriber resolves from the session binding — so ChatID here may be
// either an openConversationId (group) or a staff id prefixed target the
// messenger understands (see RobotTarget).
func (c *dingtalkChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.messenger == nil {
		return channel.SendResult{}, errors.New("dingtalk: messenger not configured")
	}
	return channel.SendResult{}, c.messenger.SendMarkdown(ctx, c.creds, RobotTarget{OpenConversationID: out.ChatID}, out.Text)
}

// Connect opens this installation's Stream Mode connection (authenticated
// with its OWN client credentials) and runs the receive loop until ctx is
// cancelled or the link drops.
func (c *dingtalkChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("dingtalk: inbound handler not configured")
	}
	ep, err := openStreamEndpoint(ctx, c.httpClient, c.openAPIBase, c.creds.ClientID, c.creds.ClientSecret)
	if err != nil {
		return err
	}
	conn, err := dialStream(ctx, ep)
	if err != nil {
		return err
	}
	defer conn.Close()
	c.logger.Info("dingtalk stream: connected", "client_id", c.creds.ClientID)

	// Close the socket when ctx is cancelled so the blocking ReadMessage
	// below unblocks immediately (gorilla reads are not ctx-aware).
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	// Transport-level pong refreshes the idle deadline too — DingTalk's
	// server pings at the app layer (SYSTEM/ping), but be tolerant of
	// either keepalive style.
	_ = conn.SetReadDeadline(time.Now().Add(streamReadIdleTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(streamReadIdleTimeout))
	})

	for {
		msgType, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("dingtalk stream: read: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(streamReadIdleTimeout))
		if msgType != websocket.TextMessage {
			continue
		}
		var frame streamFrame
		if err := json.Unmarshal(payload, &frame); err != nil {
			c.logger.Warn("dingtalk stream: undecodable frame", "error", err, "client_id", c.creds.ClientID)
			continue
		}
		reconnect, err := c.handleFrame(ctx, conn, &frame)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if reconnect {
			// Server-initiated disconnect: return an error so the
			// supervisor redials (a fresh gateway grant is required —
			// tickets are one-shot).
			return errors.New("dingtalk stream: server requested disconnect")
		}
	}
}

// handleFrame ACKs one frame and dispatches bot messages to the engine.
// The ACK goes out FIRST (mirroring Slack's ack-before-handle): DingTalk
// redelivers un-ACKed callbacks, and the engine's dedup layer absorbs any
// redelivery that races the handler anyway.
func (c *dingtalkChannel) handleFrame(ctx context.Context, conn *websocket.Conn, frame *streamFrame) (reconnect bool, err error) {
	switch frame.Type {
	case streamFrameTypeSystem:
		switch frame.topic() {
		case streamTopicPing:
			// Pong mirrors the ping's data payload (SDK OnPing behavior).
			if err := writeStreamResponse(conn, newStreamAck(frame, frame.Data)); err != nil {
				return false, fmt.Errorf("dingtalk stream: pong: %w", err)
			}
		case streamTopicDisconnect:
			return true, nil
		default:
			_ = writeStreamResponse(conn, newStreamAck(frame, ""))
		}
		return false, nil

	case streamFrameTypeCallback:
		if frame.topic() != streamTopicBotMessage {
			_ = writeStreamResponse(conn, newStreamAck(frame, `{"response":{}}`))
			return false, nil
		}
		if err := writeStreamResponse(conn, newStreamAck(frame, `{"response":{}}`)); err != nil {
			return false, fmt.Errorf("dingtalk stream: ack callback: %w", err)
		}
		var data botCallbackData
		if err := json.Unmarshal([]byte(frame.Data), &data); err != nil {
			c.logger.Warn("dingtalk stream: undecodable bot callback", "error", err, "client_id", c.creds.ClientID)
			return false, nil
		}
		msg, ok := inboundFromBotCallback(data, c.creds.ClientID)
		if !ok {
			return false, nil
		}
		// A non-nil handler error is an infrastructure failure; it
		// propagates so the supervisor reconnects (product drops return nil).
		if err := c.handler(ctx, msg); err != nil {
			return false, err
		}
		return false, nil

	default: // EVENT — not subscribed today; ACK so the server stops redelivering.
		_ = writeStreamResponse(conn, newStreamAck(frame, `{"status":"SUCCESS"}`))
		return false, nil
	}
}

// ChannelDeps are the shared dependencies the DingTalk Factory closes over.
// The engine inbound handler is supplied per-build via channel.Config.Handler.
type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger
	// OpenAPIBase overrides https://api.dingtalk.com (tests/proxies).
	OpenAPIBase string
	// Messenger delivers Channel.Send outbound messages. Optional: nil
	// leaves Send unconfigured (the EventChatDone subscriber owns the
	// production reply path and constructs its own messenger).
	Messenger *RobotMessenger
}

// RegisterDingTalk registers the per-installation DingTalk Factory so the
// engine.Supervisor builds + supervises one dingtalkChannel per active
// installation. "Adding DingTalk inbound" is this call plus the adapter —
// no engine edit (the same contract as lark.RegisterFeishu / slack.RegisterSlack).
func RegisterDingTalk(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeDingtalk, newDingTalkFactory(deps))
}

func newDingTalkFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	base := deps.OpenAPIBase
	if base == "" {
		base = defaultOpenAPIBase
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		creds, err := decodeChannelCredentials(cfg.Raw, deps.Decrypt)
		if err != nil {
			return nil, err
		}
		return &dingtalkChannel{
			creds:       creds,
			openAPIBase: base,
			httpClient:  &http.Client{Timeout: 30 * time.Second},
			handler:     cfg.Handler,
			messenger:   deps.Messenger,
			logger:      logger,
		}, nil
	}
}
