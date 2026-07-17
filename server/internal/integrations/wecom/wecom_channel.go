package wecom

// wecom_channel.go — the Channel + Factory the engine.Supervisor drives, plus
// the WebSocket run loop for one aibot smart-bot connection. WeChat allows
// only one active connection per bot; the Supervisor's WS lease enforces
// that same "at most one per replica" invariant at the process layer, so the
// combination gives us a single global connection per installation without
// wecom-specific coordination.
//
// The read loop lives on this file (rather than in a shared connector as
// with lark/ws_connector.go) because the aibot protocol is small enough
// that a per-installation loop is clearer than an EventConnector abstraction.
// Slack takes the same shape in slack_channel.go — the per-installation
// receive loop is inlined into Channel.Connect.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"

	cryptorand "crypto/rand"
	"encoding/hex"

	"github.com/gorilla/websocket"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// DefaultWSURL is the aibot long-connection endpoint. WeChat publishes a
// single global endpoint for every bot; the (bot_id, secret) pair carried in
// the aibot_subscribe frame after the WS handshake identifies which bot the
// connection belongs to.
const DefaultWSURL = "wss://openws.work.weixin.qq.com"

// pingInterval is the client-driven heartbeat cadence. WeChat's docs
// prescribe 30s; below that they may kill the socket, above that we spam.
const pingInterval = 30 * time.Second

// subscribeTimeout caps the wait between "sent aibot_subscribe" and
// "received the errcode 0 ack". The server responds within a few hundred
// milliseconds in practice; this bound protects against a silent socket.
const subscribeTimeout = 10 * time.Second

// readDeadline is refreshed on every successful read. If no bytes arrive
// within this window we assume the socket is dead and force-close it — the
// Supervisor then handles reconnect. It MUST exceed pingInterval by a
// comfortable margin so a pong is not late enough to trigger a false trip.
const readDeadline = 90 * time.Second

// writeDeadline caps a single frame's write budget. Below this a genuinely
// slow socket is preferable to an infinitely stuck goroutine.
const writeDeadline = 10 * time.Second

// handshakeTimeout bounds the initial TCP + WS handshake dial.
const handshakeTimeout = 15 * time.Second

// wecomChannel is one installation's aibot smart-bot WebSocket connection.
// The engine.Supervisor builds one per active installation via the
// registered Factory and drives lease / reconnect lifecycle; Connect blocks
// on the receive loop until ctx is cancelled or the link drops.
type wecomChannel struct {
	installationID string
	botID          string
	secret         string
	handler        channel.InboundHandler
	dialer         Dialer
	wsURL          string
	logger         *slog.Logger

	// send is the WS write handle a running connection installs on itself
	// so wecomChannel.Send (called concurrently by the engine) can push an
	// aibot_send_msg without opening a second connection. Nil while the
	// receive loop is not running; guarded by sendMu.
	sendMu sync.Mutex
	send   *wsSender
}

var _ channel.Channel = (*wecomChannel)(nil)

func (c *wecomChannel) Type() channel.Type { return TypeWecom }

// Capabilities declares what the aibot adapter supports today. Text is the
// only fully wired capability; attachments arrive as MsgTypeImage / File /
// Audio / Video but we do not yet download the media (WeChat's aibot API
// requires an additional aibot_upload_media_* dance to send back media).
func (c *wecomChannel) Capabilities() channel.Capability {
	return channel.CapText
}

// Disconnect is a no-op: the WS connection's whole lifetime is scoped to
// Connect (it returns when the run context is cancelled), so there is no
// long-lived resource to release here. Mirrors feishuChannel / slackChannel.
func (c *wecomChannel) Disconnect(ctx context.Context) error { return nil }

// Connect dials the aibot long-connection endpoint, sends the subscribe
// frame, and runs the read loop until ctx is cancelled or the link drops.
// Every exit path cancels the derived runCtx and waits for the read
// goroutine to observe it, so a transient failure tears the live connection
// down before the Supervisor reconnects — no leaked socket goroutine
// consuming events into an unread channel.
func (c *wecomChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("wecom: inbound handler not configured")
	}
	if c.botID == "" || c.secret == "" {
		return errors.New("wecom: bot_id / secret not configured")
	}

	wsURL := c.wsURL
	if wsURL == "" {
		wsURL = DefaultWSURL
	}
	if _, err := url.Parse(wsURL); err != nil {
		return fmt.Errorf("wecom: parse ws url: %w", err)
	}

	dialer := c.dialer
	if dialer == nil {
		dialer = defaultDialer
	}

	log := c.logger
	if log == nil {
		log = slog.Default()
	}
	log = log.With("installation_id", c.installationID, "bot_id", c.botID)

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("wecom: dial %s: %w", wsURL, err)
	}
	defer conn.Close()

	sender := newWSSender(conn, log)

	// Watchdog: bridges ctx cancellation to the blocking ReadMessage() call.
	// gorilla's ReadMessage does not observe ctx; cancelling our ctx flips
	// ctx.Done but does not touch the read syscall. We close the socket on
	// ctx.Done so the in-flight Read returns immediately with a
	// "use of closed connection" error. The watchdog also runs on any other
	// exit path (via `done`) so we never leak this goroutine, and close is
	// idempotent so double-close on a normal exit is safe.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	// Subscribe — auth the connection. Any error here yields the loop back
	// to the Supervisor for backoff + retry.
	if err := c.subscribe(ctx, conn, sender, log); err != nil {
		return err
	}
	log.Info("wecom: subscribe ok")

	// Install the sender so concurrent Send() calls can use this connection.
	c.sendMu.Lock()
	c.send = sender
	c.sendMu.Unlock()
	defer func() {
		c.sendMu.Lock()
		c.send = nil
		c.sendMu.Unlock()
	}()

	// Heartbeat — WeChat kills silent sockets past ~90s. We ping every 30s
	// via the shared writer mutex so it interleaves cleanly with other
	// outbound frames.
	pingCtx, pingCancel := context.WithCancel(ctx)
	defer pingCancel()
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		c.pingLoop(pingCtx, sender, log)
	}()
	defer func() { <-pingDone }()

	// Read loop. Every frame comes back through the same decode → dispatch
	// → (maybe) reply path. A single bad frame does NOT tear the socket
	// down — only transport / handler errors escalate.
	_ = conn.SetReadDeadline(time.Now().Add(readDeadline))
	for {
		if ctx.Err() != nil {
			return nil
		}
		typ, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wecom: read: %w", err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(readDeadline))
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}

		var env frameEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			log.Warn("wecom: bad frame envelope", "error", err, "size", len(payload))
			continue
		}
		if err := c.dispatchFrame(ctx, env, sender, log); err != nil {
			return err
		}
	}
}

// subscribe sends the aibot_subscribe frame and waits (up to
// subscribeTimeout) for the server's ack. The ack shape is a frame with
// echoed headers.req_id + errcode; errcode == 0 means good, anything else
// is fatal (bad credentials / bot doesn't exist).
func (c *wecomChannel) subscribe(ctx context.Context, conn wsConn, sender *wsSender, log *slog.Logger) error {
	reqID := newReqID()
	if err := sender.write(map[string]any{
		"cmd":     cmdSubscribe,
		"headers": frameHeaders{ReqID: reqID},
		"body":    subscribeBody(c.botID, c.secret),
	}); err != nil {
		return fmt.Errorf("wecom: send subscribe: %w", err)
	}

	// Wait for the matching ack — the server writes it as a frame with
	// cmd empty (or absent) and headers.req_id equal to ours. Any other
	// frame that arrives first is dropped (subscribe is the very first
	// exchange, so this is rare in practice).
	deadline := time.Now().Add(subscribeTimeout)
	_ = conn.SetReadDeadline(deadline)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		typ, payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("wecom: subscribe read: %w", err)
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}
		var env frameEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			continue
		}
		if env.Headers.ReqID != reqID {
			continue
		}
		if env.ErrCode != 0 {
			return fmt.Errorf("wecom: subscribe rejected errcode=%d errmsg=%s", env.ErrCode, env.ErrMsg)
		}
		return nil
	}
}

// dispatchFrame routes one server frame. Only aibot_msg_callback ever
// escalates back to the loop's caller (as a handler infra failure);
// events are logged + acked and everything else is silently dropped.
func (c *wecomChannel) dispatchFrame(ctx context.Context, env frameEnvelope, sender *wsSender, log *slog.Logger) error {
	switch env.Cmd {
	case cmdMsgCallback:
		var mc aibotMsgCallback
		if err := json.Unmarshal(env.Body, &mc); err != nil {
			log.Warn("wecom: bad aibot_msg_callback body", "error", err)
			return nil
		}
		if mc.MsgType != "text" {
			// Media types arrive but iteration 1 only routes text; drop
			// the rest silently so the handler pipeline is not spammed.
			log.Debug("wecom: dropped non-text message", "msg_type", mc.MsgType, "msg_id", mc.MsgID)
			return nil
		}
		msg := channelMessageFromCallback(c.botID, mc, env.Headers.ReqID)
		if err := c.handler(ctx, msg); err != nil {
			return err
		}
		return nil
	case cmdEventCallback:
		var ec aibotEventCallback
		if err := json.Unmarshal(env.Body, &ec); err != nil {
			log.Warn("wecom: bad aibot_event_callback body", "error", err)
			return nil
		}
		switch ec.Event.EventType {
		case eventDisconnected:
			// Another connection displaced ours. Return so the Supervisor
			// can backoff and reconnect (which will in turn displace THAT
			// one — the last writer wins).
			return errors.New("wecom: received disconnected_event (superseded)")
		default:
			log.Debug("wecom: event", "type", ec.Event.EventType)
			return nil
		}
	case cmdServerPing:
		// Server-initiated ping (rare per the docs, but handle defensively).
		if err := sender.write(map[string]any{
			"cmd":     cmdPong,
			"headers": frameHeaders{ReqID: env.Headers.ReqID},
		}); err != nil {
			return fmt.Errorf("wecom: pong: %w", err)
		}
		return nil
	case cmdPong:
		// Ack for our client-initiated ping — no-op.
		return nil
	default:
		// Includes anonymous ack frames (empty cmd) for our writes.
		return nil
	}
}

// pingLoop sends heartbeat frames every pingInterval until ctx is
// cancelled. A write failure surfaces on the next ReadMessage error path;
// we log it here but do not tear the loop down ourselves.
func (c *wecomChannel) pingLoop(ctx context.Context, sender *wsSender, log *slog.Logger) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := sender.write(map[string]any{
				"cmd":     cmdPing,
				"headers": frameHeaders{ReqID: newReqID()},
			}); err != nil {
				log.Warn("wecom: ping write failed", "error", err)
			}
		}
	}
}

// Send is the outbound Channel entry the engine calls with a normalized
// OutboundMessage. Iteration 1 always uses aibot_send_msg (WeCom's
// "proactive push" cmd) rather than aibot_respond_msg — send_msg has no 5s
// deadline and works regardless of whether the message ever ties back to a
// specific inbound frame. The one caveat is chat_type: aibot_send_msg needs
// to know whether the ChatID is a single-user id or a group id. We piggy-
// back on the length heuristic used by internal-customer-service (chat ids
// are ≥33 chars, userids are shorter), which is stable in practice.
//
// The Channel is not the primary outbound path in the multica engine — the
// EventChatDone subscriber and the OutboundReplier handle most sends — but
// Channel.Send is still the contract that lets the engine deliver ad-hoc
// replies, so we implement it here for parity with feishuChannel /
// slackChannel.
func (c *wecomChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if out.ChatID == "" {
		return channel.SendResult{}, errors.New("wecom: send requires chat_id")
	}
	c.sendMu.Lock()
	sender := c.send
	c.sendMu.Unlock()
	if sender == nil {
		return channel.SendResult{}, errors.New("wecom: connection not ready")
	}
	chatType := chatTypeSingleInt
	if len(out.ChatID) > 32 {
		chatType = chatTypeGroupInt
	}
	body, err := sendMsgTextBody(out.ChatID, chatType, out.Text)
	if err != nil {
		return channel.SendResult{}, err
	}
	if err := sender.write(map[string]any{
		"cmd":     cmdSendMsg,
		"headers": frameHeaders{ReqID: newReqID()},
		"body":    body,
	}); err != nil {
		return channel.SendResult{}, fmt.Errorf("wecom: send_msg: %w", err)
	}
	return channel.SendResult{}, nil
}

// ---- factory ----

// ChannelDeps bundles the shared dependencies the wecom Factory closes
// over. The engine inbound handler is supplied per-build via
// channel.Config.Handler; the CredentialsResolver decrypts the stored
// secret.
type ChannelDeps struct {
	Credentials CredentialsResolver
	Logger      *slog.Logger

	// Dialer overrides the default gorilla dialer. Tests point it at an
	// httptest server; production leaves this nil.
	Dialer Dialer

	// WSURL overrides DefaultWSURL. Same test-only intent as Dialer.
	WSURL string
}

// RegisterWecom registers the per-installation wecom smart-bot Factory so
// the engine.Supervisor builds + supervises one wecomChannel per active
// installation. "Adding wecom smart-bot inbound" is this call plus the
// adapter — no engine edit (same contract as lark.RegisterFeishu /
// slack.RegisterSlack).
func RegisterWecom(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeWecom, newWecomFactory(deps))
}

func newWecomFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		if deps.Credentials == nil {
			return nil, errors.New("wecom: credentials resolver missing")
		}
		var ic installConfig
		if len(cfg.Raw) > 0 {
			if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
				return nil, fmt.Errorf("wecom: decode installation config: %w", err)
			}
		}
		if ic.BotID == "" {
			return nil, errors.New("wecom: installation config missing bot_id")
		}
		inst := Installation{BotID: ic.BotID, SecretEncrypted: ic.SecretEncrypted}
		creds, err := deps.Credentials.Credentials(inst)
		if err != nil {
			return nil, fmt.Errorf("wecom: decrypt secret: %w", err)
		}
		return &wecomChannel{
			botID:   creds.BotID,
			secret:  creds.Secret,
			handler: cfg.Handler,
			dialer:  deps.Dialer,
			wsURL:   deps.WSURL,
			logger:  logger,
		}, nil
	}
}

// ---- request id ----

// newReqID returns a random correlation id for a WebSocket frame's
// headers.req_id. The server echoes it back on each ack so the client can
// pair replies with requests.
func newReqID() string {
	var buf [8]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		return fmt.Sprintf("wecom-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
