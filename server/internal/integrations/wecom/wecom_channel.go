package wecom

// wecom_channel.go — the Channel + Factory the engine.Supervisor drives, plus
// the WebSocket run loop for one aibot smart-bot connection. WeCom allows
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
	"time"

	cryptorand "crypto/rand"
	"encoding/hex"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
)

// DefaultWSURL is the aibot long-connection endpoint. WeCom publishes a
// single global endpoint for every bot; the (bot_id, secret) pair carried in
// the aibot_subscribe frame after the WS handshake identifies which bot the
// connection belongs to.
const DefaultWSURL = "wss://openws.work.weixin.qq.com"

// pingInterval is the client-driven heartbeat cadence. WeCom's docs
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
	installationID pgtype.UUID
	botID          string
	secret         string
	// botDisplayName is what this bot is called in a chat, from the
	// installation config. Empty on every installation that has not filled it
	// in; see stripLeadingMentions for what an empty name falls back to.
	botDisplayName string
	handler        channel.InboundHandler
	dialer         Dialer
	wsURL          string
	logger         *slog.Logger
	// senders is the package-level installation→wsSender registry (see
	// senders_registry.go). We hold a reference so Connect can register
	// itself on entry and clear on exit. nil in tests that don't exercise
	// the OutboundReplier path.
	senders *sendersRegistry
}

var _ channel.Channel = (*wecomChannel)(nil)

func (c *wecomChannel) Type() channel.Type { return TypeWecom }

// Capabilities declares what the aibot adapter supports today. Text is the
// only fully wired capability; attachments arrive as MsgTypeImage / File /
// Audio / Video but we do not yet download the media (WeCom's aibot API
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
func (c *wecomChannel) Connect(ctx context.Context) (err error) {
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
	log = log.With("installation_id", util.UUIDToString(c.installationID), "bot_id", c.botID)

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

	// Install the sender on the package-level registry so the OutboundReplier
	// (created at boot, not per-installation) can locate this connection by
	// installation id and push aibot_send_msg over the same socket. Cleared
	// on exit so a stale sender for a dead connection is never dispatched to.
	if c.senders != nil && c.installationID.Valid {
		c.senders.set(c.installationID, sender)
		defer c.senders.clear(c.installationID, sender)
	}

	// Heartbeat — WeCom kills silent sockets past ~90s. We ping every 30s
	// via the shared writer mutex so it interleaves cleanly with other
	// outbound frames.
	pingCtx, pingCancel := context.WithCancel(ctx)
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		c.pingLoop(pingCtx, sender, log)
	}()
	// Cancel and wait in ONE defer, in that order. Two separate defers would
	// run LIFO — the wait first, the cancel second — and pingLoop only returns
	// on pingCtx.Done. On any exit path where the parent ctx is still live
	// (read error, dispatch error) nothing would ever cancel pingCtx, so the
	// wait would block forever and Connect would never return to the
	// Supervisor for reconnect.
	defer func() {
		pingCancel()
		<-pingDone
	}()

	// Inbound callbacks run on their own worker, not on the read loop. The
	// read loop is the sole deliverer of server verdicts, so anything that
	// handles a callback inline cannot also wait for the ack of a frame it
	// writes — it would be waiting on itself. DingTalk's adapter is already
	// shaped this way.
	//
	// ONE worker, not a pool: WeCom delivers a chat's messages in order, and
	// the engine's dedup and turn batching assume that order survives.
	//
	// A full queue BLOCKS the read loop rather than dropping. Backpressure
	// costs a reconnect; dropping costs a user's message with nothing to say
	// so. That only holds while somebody is still receiving, so the read
	// loop's send also watches cbDone — see the send site below.
	callbacks := make(chan frameEnvelope, callbackQueueDepth)
	cbDone := make(chan struct{})
	var cbErr error
	go func() {
		defer close(cbDone)
		for env := range callbacks {
			if e := c.dispatchFrame(ctx, env, sender, log); e != nil {
				cbErr = e
				// Wake the read loop if it is parked in ReadMessage; a
				// cancelled context alone will not move it. A read loop
				// parked on the queue send is woken by cbDone instead —
				// closing a socket does not move a channel send.
				_ = conn.Close()
				return
			}
		}
	}()
	defer func() {
		close(callbacks)
		<-cbDone
		// The worker's error is the real cause; the read error that followed
		// it is just the socket we closed to get here. Only on a live ctx: a
		// shutdown or a lease-loss cancel that catches a callback mid-flight
		// is an ordinary stop, and promoting that callback's error would
		// report a spurious "connection exited with error".
		if cbErr != nil && ctx.Err() == nil {
			err = cbErr
		}
	}()

	// Read loop. Every frame comes back through the same decode → dispatch
	// → (maybe) reply path. A single bad frame does NOT tear the socket
	// down — only transport / handler errors escalate.
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Armed immediately before the read, and nowhere else.
		//
		// It used to be armed after ReadMessage returned, which put
		// everything the loop then did inside the window. Only the server's
		// pong resets the deadline on a quiet bot and our ping goes out every
		// 30s, so on a loaded pool the next read could time out on a socket
		// that was perfectly healthy. The idle window should measure idleness.
		//
		// The error is no longer discarded: a socket that refuses a deadline
		// is not one to keep reading from.
		if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
			// The shutdown path closes the socket, and a closed socket
			// refuses a deadline. That is an ordinary stop, not a failure to
			// report to the Supervisor.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wecom: set read deadline: %w", err)
		}
		typ, payload, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("wecom: read: %w", err)
		}
		if typ != websocket.TextMessage && typ != websocket.BinaryMessage {
			continue
		}

		var env frameEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			log.Warn("wecom: bad frame envelope", "error", err, "size", len(payload))
			continue
		}
		traceIn(log, env)
		switch env.Cmd {
		case cmdMsgCallback, cmdEventCallback:
			select {
			case callbacks <- env:
			case <-cbDone:
				// The worker has stopped, so this send has no receiver — and
				// no closer either: the queue is closed by a defer that
				// cannot run until this loop returns. With a full queue that
				// is a permanent park, because the worker's conn.Close()
				// wakes a read loop sitting in ReadMessage, not one sitting
				// on a send, and ctx stays live on this path. Nothing would
				// ever reconnect: the Supervisor would keep renewing the
				// lease for a connection that had stopped reading.
				//
				// Return and let the deferred handler substitute the
				// worker's error, which is the real cause.
				return nil
			case <-ctx.Done():
				return nil
			}
		default:
			// Acks, pings and pongs stay on the read loop: they are the
			// frames the worker's own writes are waiting for.
			if err := c.dispatchFrame(ctx, env, sender, log); err != nil {
				return err
			}
		}
	}
}

// callbackQueueDepth is how far the callback worker may fall behind the read
// loop before the read loop blocks. Past this the socket stops being drained,
// WeCom notices, and the connection is replaced — which is the correct
// outcome: a replica that cannot keep up should hand the bot to one that can,
// not quietly discard the messages it could not reach.
const callbackQueueDepth = 64

// subscribe sends the aibot_subscribe frame and waits (up to
// subscribeTimeout) for the server's ack. The ack shape is a frame with
// echoed headers.req_id + errcode; errcode == 0 means good.
//
// A non-zero errcode goes through classifySubscribeAck — the same function the
// install-time credential probe uses on the same ack, so the two cannot answer
// the same code differently. 40001 / 40013 come back as ErrCredentialsRejected:
// the refusal that repeats identically on every backoff until somebody fixes
// the installation. Every other non-zero code is ErrCredentialsUnverifiable,
// because a throttle (45009, 45033) or a platform-side failure clears on its
// own, and counting one as a credential failure would page an operator about a
// tenant whose bot is fine. Both sentinels are exported, so channel/engine —
// which is what receives this error out of Connect() — can branch on them.
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
		// Traced before the req_id filter: a subscribe that is rejected, or
		// answered on a req_id we never sent, is exactly the failure an
		// operator turns tracing on to see.
		traceIn(log, env)
		if env.Headers.ReqID != reqID {
			continue
		}
		if env.ErrCode != 0 {
			return classifySubscribeAck(log, env.ErrCode, env.ErrMsg)
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
		traceInbound(log, mc, mc.Text.Content)
		msg := channelMessageFromCallback(c.botID, c.botDisplayName, mc, env.Headers.ReqID)
		if mc.MsgType != "text" {
			// Iteration 1 routes only text. Rather than drop other types
			// (voice / image / file) silently — which reads as a broken bot —
			// answer the same chat with a one-line "text only" receipt, then
			// stop. Media routing, and dedup'd receipts that never double-answer
			// a WeCom delivery retry, are a follow-up. Best-effort: a send
			// failure degrades to the prior silent drop.
			log.Debug("wecom: non-text message, replying text-only", "msg_type", mc.MsgType, "msg_id", mc.MsgID)
			if err := sender.sendText(msg.Source.ChatID, aibotChatTypeFromChannel(msg.Source.ChatType), "抱歉，我目前只能处理文字消息。"); err != nil {
				log.Debug("wecom: text-only receipt send failed", "error", err, "msg_id", mc.MsgID)
			}
			return nil
		}
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
		// Anonymous ack frames (empty cmd) for our writes. Most are
		// errcode=0 no-ops, but aibot_send_msg / aibot_respond_msg /
		// aibot_upload_media_* can reject with a non-zero errcode
		// (e.g. wrong msgtype, rate limit, chat not writable). Log the
		// error so a failed outbound is visible without having to
		// packet-capture the socket.
		// Hand it to whoever wrote the frame, if anybody is waiting. An
		// unclaimed ack is not an error — the pushes that do not wait for a
		// verdict share this connection.
		if sender.routeResponse(env) {
			return nil
		}
		if env.ErrCode != 0 {
			log.Warn("wecom: server ack error",
				"errcode", env.ErrCode,
				"errmsg", env.ErrMsg,
				"req_id", env.Headers.ReqID,
			)
		}
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
	// Not used. Outbound for wecom goes through OutboundReplier / Outbound
	// (EventChatDone + EventInboxNew), which know the message's real
	// Source.ChatType and address the correct chat. The generic
	// Channel.Send seam is never invoked by the engine for this channel; a
	// stub here previously guessed single-vs-group from len(ChatID) > 32,
	// the only chat-type inference in the package. Return not-supported
	// rather than keep a second, heuristic outbound path alive.
	_ = ctx
	_ = out
	return channel.SendResult{}, ErrSendNotSupported
}

// ErrSendNotSupported is returned by wecomChannel.Send. WeCom's generic
// Channel.Send seam has no honest implementation — channel.OutboundMessage
// carries no chat_type, and outbound already flows through OutboundReplier /
// Outbound, which read the chat type off the inbound frame.
var ErrSendNotSupported = errors.New("wecom: Channel.Send is not supported; outbound goes through OutboundReplier/Outbound")

// ---- factory ----

// ChannelDeps bundles the shared dependencies the wecom Factory closes
// over. The engine inbound handler is supplied per-build via
// channel.Config.Handler; the CredentialsResolver decrypts the stored
// secret.
type ChannelDeps struct {
	Credentials CredentialsResolver
	Logger      *slog.Logger

	// Senders is the package-level installation→wsSender registry.
	// OutboundReplier and Outbound both look up the live wsSender through
	// it. Boot passes ONE registry instance shared with the OutboundReplier
	// constructor. Nil in tests that don't exercise outbound.
	Senders *sendersRegistry

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
			installationID: cfg.ID,
			botID:          creds.BotID,
			secret:         creds.Secret,
			botDisplayName: ic.BotDisplayName,
			handler:        cfg.Handler,
			dialer:         deps.Dialer,
			wsURL:          deps.WSURL,
			logger:         logger,
			senders:        deps.Senders,
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
