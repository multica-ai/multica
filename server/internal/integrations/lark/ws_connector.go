package lark

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// WSLongConnConnector is the production EventConnector that holds the
// Lark long-conn WebSocket open, decodes the binary Frame envelope
// the open-platform server pushes, and forwards normalized inbound
// events to the Hub's Dispatcher.
//
// Protocol layer (aligned with `larksuite/oapi-sdk-go/v3/ws`):
//
//  1. EndpointFetcher does the POST /callback/ws/endpoint bootstrap.
//     Lark returns a single-use wss URL with `device_id` + `service_id`
//     query parameters acting as the credential. The `service_id` is
//     extracted and used as Frame.Service on every outbound frame.
//  2. Every WebSocket frame is a binary protobuf Frame. JSON envelopes
//     are wrapped inside Frame.Payload for data events; control,
//     ping, pong, ack frames are pure-binary.
//  3. The client emits an app-layer ping frame (NewPingFrame) on the
//     PingInterval the server returned in the bootstrap ClientConfig.
//     WebSocket protocol-level PING is NOT used — Lark's server
//     ignores it. The server can also push pings at any time; we
//     reply with NewPongFrame.
//  4. Every data frame requires an ACK back. The ACK reuses the
//     inbound frame's Headers verbatim (Lark correlates by
//     message_id) and writes a JSON Response{code:200, ...} as the
//     Payload. We send ACK 200 on successful Dispatcher emit, 500
//     when the Dispatcher reported an infra failure so Lark retries.
//
// Ownership of the §4.4 invariant (ctx cancel breaks blocking read):
//
// gorilla/websocket.ReadMessage blocks on the TCP socket and does NOT
// observe a context. Cancelling our ctx flips ctx.Done but does not
// touch the read syscall. We bridge ctx → read interrupt with a
// watchdog goroutine that calls conn.Close once ctx fires;
// gorilla.Close causes any in-flight Read to return immediately with
// a "use of closed connection" error. The watchdog also runs on a
// normal exit (so we never leak a goroutine) and is idempotent
// because conn.Close is safe to call multiple times. This invariant
// MUST be preserved by any future protocol change: without it,
// renewLeaseUntil's cancelRun is meaningless and two replicas can
// process the same installation during a healthy-but-silent socket.
//
// PersonalAgent compatibility risk: the official Feishu docs describe
// long-conn as "supports 企业自建应用 only". PersonalAgent device-flow
// apps are not listed as supported. If the bootstrap call returns a
// structured error from Lark, this connector exits Run with the error
// wrapped and the Hub's backoff loop logs it on every retry — making
// the misconfiguration visible. See MUL-2671 review thread for the
// smoke-test path.
type WSLongConnConnector struct {
	cfg WSConnectorConfig
}

// WSConnectorConfig wires the connector's dependencies. All injected
// interfaces are required; nil dependencies cause NewWSLongConnConnector
// to return an error rather than producing a connector that would panic
// at first use. Time / logger fields default at construction.
type WSConnectorConfig struct {
	// Dialer opens the WebSocket transport. Defaults to gorilla's
	// DefaultDialer with a bounded HandshakeTimeout. Tests inject a
	// fake that points at an httptest server.
	Dialer WSDialer

	// EndpointFetcher resolves the per-installation WS URL + server
	// config (ping interval, service id) via the bootstrap POST. The
	// connector calls it once per Run, so a transient failure here
	// causes a Hub-level backoff retry rather than an in-Run reconnect
	// storm.
	EndpointFetcher EndpointFetcher

	// FrameDecoder turns a single decoded Frame into either a
	// normalized InboundMessage (to be emitted upstream) or a
	// "control / heartbeat / unknown" signal that the connector
	// drops silently. Errors from the decoder do NOT exit the loop
	// — they log + drop — because one malformed Lark event payload
	// should not tear down the entire connection.
	FrameDecoder FrameDecoder

	// Enricher optionally expands a decoded message's body with the
	// context the user explicitly attached (quoted reply / forwarded
	// bundle) before it is emitted to the dispatcher. It runs on the
	// inbound read loop, so it is bounded by EnrichTimeout to protect
	// the Lark long-conn ACK budget; on timeout / fetch failure the
	// enricher degrades to a placeholder rather than blocking. Nil
	// disables enrichment (the decoded body is emitted as-is).
	Enricher Enricher

	// EnrichTimeout caps a single message's enrichment (at most two
	// GetMessage calls). It MUST stay well under Lark's ~3s long-conn
	// ACK window, since enrichment runs before the frame is ACKed.
	// Zero defaults to 2 seconds.
	EnrichTimeout time.Duration

	// CredentialsProvider returns the InstallationCredentials the
	// EndpointFetcher needs. Typically wraps
	// InstallationService.DecryptAppSecret so the plaintext secret
	// never sits on the LarkInstallation row in memory.
	CredentialsProvider CredentialsProvider

	// PingInterval is the fallback cadence for the app-layer ping.
	// In production it is overridden per-installation by the
	// PingInterval Lark returns in the bootstrap ClientConfig.
	// Zero defaults to 2 minutes (matches the SDK default in
	// `larksuite/oapi-sdk-go/v3/ws/client.go`).
	PingInterval time.Duration

	// ReadDeadline bounds a single ReadMessage call. Re-armed before
	// each read; expiry yields a transient read error which the
	// connector logs and uses to exit, deferring to the Hub's
	// reconnect backoff. Zero defaults to 6 minutes so a healthy
	// connection with the 2-minute default ping never trips it.
	ReadDeadline time.Duration

	// WriteTimeout bounds a single WriteMessage. Zero defaults to 10s.
	WriteTimeout time.Duration

	// ChunkTTL bounds how long the chunk assembler holds a partial
	// multi-frame event before discarding the buffered chunks. Mirrors
	// the SDK's 5-second default — long enough to absorb pacing across
	// several chunks, short enough that an abandoned multi-frame event
	// does not leak memory. Zero defaults to 5 seconds.
	ChunkTTL time.Duration

	// Now is overridable for deterministic tests. Defaults to time.Now.
	Now func() time.Time

	// Logger optional; defaults to slog.Default.
	Logger *slog.Logger
}

func (c WSConnectorConfig) withDefaults() WSConnectorConfig {
	if c.PingInterval == 0 {
		c.PingInterval = 2 * time.Minute
	}
	if c.ReadDeadline == 0 {
		c.ReadDeadline = 6 * time.Minute
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 10 * time.Second
	}
	if c.ChunkTTL == 0 {
		c.ChunkTTL = 5 * time.Second
	}
	if c.EnrichTimeout == 0 {
		c.EnrichTimeout = 2 * time.Second
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// NewWSLongConnConnector validates the supplied config and returns a
// reusable connector.
func NewWSLongConnConnector(cfg WSConnectorConfig) (*WSLongConnConnector, error) {
	if cfg.Dialer == nil {
		return nil, errors.New("lark ws connector: Dialer is required")
	}
	if cfg.EndpointFetcher == nil {
		return nil, errors.New("lark ws connector: EndpointFetcher is required")
	}
	if cfg.FrameDecoder == nil {
		return nil, errors.New("lark ws connector: FrameDecoder is required")
	}
	if cfg.CredentialsProvider == nil {
		return nil, errors.New("lark ws connector: CredentialsProvider is required")
	}
	return &WSLongConnConnector{cfg: cfg.withDefaults()}, nil
}

// Run satisfies EventConnector. Opens one WebSocket session, reads
// binary Frame envelopes until either the ctx is cancelled or the
// connection errors, and returns. Nil return = clean exit; non-nil
// return = connection failed (Hub steps up backoff).
func (c *WSLongConnConnector) Run(ctx context.Context, inst db.LarkInstallation, emit EventEmitter) error {
	log := c.cfg.Logger.With(
		"installation_id", uuidString(inst.ID),
		"app_id", inst.AppID,
	)

	creds, err := c.cfg.CredentialsProvider.Credentials(ctx, inst)
	if err != nil {
		return fmt.Errorf("resolve credentials: %w", err)
	}

	endpoint, err := c.cfg.EndpointFetcher.Endpoint(ctx, creds)
	if err != nil {
		return fmt.Errorf("resolve ws endpoint: %w", err)
	}

	// Server-pushed PingInterval beats the static default; this is the
	// SDK behaviour. A zero (server omitted the field) falls back to
	// our static default so we never degenerate to "ping every 0s".
	pingInterval := endpoint.PingInterval
	if pingInterval <= 0 {
		pingInterval = c.cfg.PingInterval
	}

	conn, _, err := c.cfg.Dialer.DialContext(ctx, endpoint.URL, endpoint.Headers)
	if err != nil {
		return fmt.Errorf("dial ws: %w", err)
	}

	// runCtx fans out cancellation to the watchdog + ping goroutines
	// on EVERY Run exit, not just on outer-ctx cancel. A read error or
	// emit-infra failure would otherwise leave the ping goroutine
	// ticking on the outer ctx — and the deferred join would deadlock.
	runCtx, runCancel := context.WithCancel(ctx)

	var closeOnce sync.Once
	closeConn := func() {
		closeOnce.Do(func() {
			_ = conn.Close()
		})
	}

	// Watchdog: ctx fires → close the socket so blocking ReadMessage
	// returns immediately. Also runs on any other exit path so we
	// never leak the goroutine.
	done := make(chan struct{})
	go func() {
		select {
		case <-runCtx.Done():
			closeConn()
		case <-done:
		}
	}()

	// writeMu serializes WriteMessage from the read loop (ACK send)
	// and the ping goroutine. gorilla/websocket forbids concurrent
	// writers; without this, ack + ping interleaving would corrupt
	// the binary frame stream.
	var writeMu sync.Mutex

	// Per-Run chunk assembler. State does not need to outlive a single
	// long-conn session — Lark re-sends multi-frame events from chunk 0
	// after a reconnect — so the assembler is built here and dropped
	// when Run returns, which also releases any partial buffers held by
	// an abandoned event.
	assembler := newChunkAssembler(c.cfg.ChunkTTL, c.cfg.Now)

	// Ping loop: app-layer binary ping frames at the server's PingInterval.
	pingDone := make(chan struct{})
	go c.pingLoop(runCtx, conn, &writeMu, endpoint.ServiceID, pingInterval, log, pingDone)

	defer func() {
		runCancel()
		closeConn()
		close(done)
		<-pingDone
	}()

	log.Info("lark ws connector: connected",
		"service_id", endpoint.ServiceID,
		"ping_interval", pingInterval.String(),
	)

	for {
		// Re-arm the read deadline before every Read so a stalled
		// connection eventually unblocks the syscall.
		if err := conn.SetReadDeadline(c.cfg.Now().Add(c.cfg.ReadDeadline)); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("set read deadline: %w", err)
		}

		msgType, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				log.Info("lark ws connector: ctx cancelled, read returned",
					"close_err", err.Error(),
				)
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Info("lark ws connector: server closed connection", "err", err.Error())
				return nil
			}
			return fmt.Errorf("read message: %w", err)
		}
		// Lark only sends binary frames. A text frame is a Lark-side
		// schema regression — log + drop to be safe.
		if msgType != websocket.BinaryMessage {
			log.Warn("lark ws connector: dropped non-binary frame",
				"type", msgType,
				"len", len(raw),
			)
			continue
		}

		frame, err := UnmarshalFrame(raw)
		if err != nil {
			log.Warn("lark ws connector: frame protobuf decode failed",
				"err", err.Error(),
				"raw_len", len(raw),
			)
			continue
		}

		// Control frames carry ping / pong / config updates. We
		// only have to act on pings (reply with a pong); pongs and
		// config payloads are accepted silently.
		if frame.Method == FrameMethodControl {
			if frame.HeaderValue(FrameHeaderTypeKey) == FrameHeaderTypePing {
				if werr := c.writeFrame(&writeMu, conn, NewPongFrame(endpoint.ServiceID)); werr != nil {
					log.Warn("lark ws connector: pong write failed", "err", werr.Error())
				}
			}
			continue
		}

		// Multi-frame events: stash the chunk and skip ACK until the
		// full payload has arrived. This mirrors the SDK's combine()
		// behaviour — the SDK does NOT ACK partial chunks; Lark
		// reconciles delivery on its side using sum/seq, so ACKing
		// partials would tell Lark "we got it" before we actually
		// have the assembled payload.
		sum, seq, msgID := parseChunkHeaders(frame)
		payload := frame.Payload
		if sum > 1 {
			assembled, complete := assembler.admit(msgID, sum, seq, frame.Payload)
			if !complete {
				log.Debug("lark ws connector: partial chunk buffered",
					"message_id", msgID,
					"seq", seq,
					"sum", sum,
					"pending", assembler.pendingCount(),
				)
				continue
			}
			payload = assembled
			log.Debug("lark ws connector: chunk reassembly complete",
				"message_id", msgID,
				"chunks", sum,
				"bytes", len(payload),
			)
		}

		// Data frames: hand the (possibly reassembled) JSON payload to
		// the decoder, emit if it resolved to a message, and ACK back.
		msg, ok, derr := c.cfg.FrameDecoder.Decode(payload, inst)
		if derr != nil {
			log.Warn("lark ws connector: frame decode failed",
				"err", derr.Error(),
				"payload_len", len(frame.Payload),
			)
			// A decode failure still gets a 200 ACK: the message is
			// valid wire-wise, we just can't act on it. NACKing would
			// trigger a Lark-side retry storm of a payload we've
			// already proven we can't parse.
			if werr := c.writeFrame(&writeMu, conn, NewAckFrame(frame, true)); werr != nil {
				log.Warn("lark ws connector: ack-after-decode-error write failed", "err", werr.Error())
				return fmt.Errorf("write ack: %w", werr)
			}
			continue
		}
		if !ok {
			// Heartbeat / unhandled event type. ACK 200 so the server
			// stops sending it; the decoder owns the "what we handle"
			// policy.
			if werr := c.writeFrame(&writeMu, conn, NewAckFrame(frame, true)); werr != nil {
				log.Warn("lark ws connector: ack-after-drop write failed", "err", werr.Error())
				return fmt.Errorf("write ack: %w", werr)
			}
			continue
		}

		// Enrich the decoded body with explicitly-attached context
		// (quoted reply / forwarded bundle) before emitting. This runs
		// before the frame ACK, so it is bounded by EnrichTimeout and
		// degrades to a placeholder on failure rather than blocking the
		// pipeline. Most messages need no enrichment and return
		// immediately without any network call.
		if c.cfg.Enricher != nil {
			enrichCtx, cancelEnrich := context.WithTimeout(ctx, c.cfg.EnrichTimeout)
			msg = c.cfg.Enricher.Enrich(enrichCtx, msg, creds)
			cancelEnrich()
		}

		_, emitErr := emit(ctx, msg)
		if emitErr != nil {
			// Infra failure from Dispatcher (DB down, etc.). NACK so
			// Lark retries this event on a healthy replica; then
			// return so the Hub backs off and reconnects.
			if werr := c.writeFrame(&writeMu, conn, NewAckFrame(frame, false)); werr != nil {
				log.Warn("lark ws connector: nack write failed", "err", werr.Error())
			}
			log.Error("lark ws connector: emit infra error",
				"event_id", msg.EventID,
				"err", emitErr.Error(),
			)
			return fmt.Errorf("dispatch: %w", emitErr)
		}
		if werr := c.writeFrame(&writeMu, conn, NewAckFrame(frame, true)); werr != nil {
			log.Warn("lark ws connector: ack write failed", "err", werr.Error())
			return fmt.Errorf("write ack: %w", werr)
		}
	}
}

// writeFrame serializes a Frame and writes it as a binary WebSocket
// message under the connector's write mutex. Caller MUST NOT hold
// writeMu when calling.
func (c *WSLongConnConnector) writeFrame(mu *sync.Mutex, conn WSConn, f *Frame) error {
	payload := f.Marshal()
	mu.Lock()
	defer mu.Unlock()
	deadline := c.cfg.Now().Add(c.cfg.WriteTimeout)
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}

// pingLoop sends a periodic app-layer ping frame on the cadence Lark
// asked for. The previous implementation used WebSocket protocol PING
// (WriteControl), which the SDK source confirms Lark ignores.
func (c *WSLongConnConnector) pingLoop(ctx context.Context, conn WSConn, writeMu *sync.Mutex, serviceID int32, interval time.Duration, log *slog.Logger, done chan<- struct{}) {
	defer close(done)
	if interval <= 0 {
		// A zero / negative interval would tick infinitely; bail out
		// quietly. We logged the chosen interval at connect time.
		<-ctx.Done()
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.writeFrame(writeMu, conn, NewPingFrame(serviceID)); err != nil {
				log.Warn("lark ws connector: ping write failed", "err", err.Error())
				// Don't tear down here — the read loop will exit on
				// its own when the conn dies. Closing here would race
				// with the read loop's own cleanup.
				return
			}
		}
	}
}

// WSDialer is the dialer surface this connector consumes. *websocket.Dialer
// satisfies it directly (via DialContext); tests inject a fake.
type WSDialer interface {
	DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WSConn, *http.Response, error)
}

// WSConn is the subset of *websocket.Conn this connector uses.
type WSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Close() error
}

// EndpointFetcher resolves the per-installation bootstrap response.
// The implementation is responsible for the POST /callback/ws/endpoint
// call and surfacing the server-pushed ClientConfig.
type EndpointFetcher interface {
	Endpoint(ctx context.Context, creds InstallationCredentials) (WSEndpoint, error)
}

// WSEndpoint is the resolved transport target plus the server-pushed
// runtime configuration the connector needs to honor (ping cadence,
// reconnect hints). ServiceID is parsed out of the wss URL's
// `service_id` query parameter — it identifies which Lark backend
// service ID our outbound frames belong to.
type WSEndpoint struct {
	URL               string
	Headers           http.Header
	ServiceID         int32
	PingInterval      time.Duration
	ReconnectInterval time.Duration
	ReconnectNonce    time.Duration
	ReconnectCount    int
}

// FrameDecoder turns the JSON payload of a data Frame into either an
// InboundMessage (ok=true) or a no-op (ok=false). The connector
// treats a decoder error as per-frame: log + drop, do not tear down
// the connection. The decoder receives the JSON payload bytes — the
// outer binary Frame envelope is stripped by the connector.
type FrameDecoder interface {
	Decode(payload []byte, inst db.LarkInstallation) (msg InboundMessage, ok bool, err error)
}

// CredentialsProvider supplies the plaintext InstallationCredentials a
// connector needs for its EndpointFetcher call.
type CredentialsProvider interface {
	Credentials(ctx context.Context, inst db.LarkInstallation) (InstallationCredentials, error)
}

// CredentialsProviderFunc adapts a free function.
type CredentialsProviderFunc func(ctx context.Context, inst db.LarkInstallation) (InstallationCredentials, error)

func (f CredentialsProviderFunc) Credentials(ctx context.Context, inst db.LarkInstallation) (InstallationCredentials, error) {
	return f(ctx, inst)
}

// EndpointFetcherFunc adapts a plain function to EndpointFetcher.
type EndpointFetcherFunc func(ctx context.Context, creds InstallationCredentials) (WSEndpoint, error)

func (f EndpointFetcherFunc) Endpoint(ctx context.Context, creds InstallationCredentials) (WSEndpoint, error) {
	return f(ctx, creds)
}

// FrameDecoderFunc adapts a plain function to FrameDecoder.
type FrameDecoderFunc func(payload []byte, inst db.LarkInstallation) (InboundMessage, bool, error)

func (f FrameDecoderFunc) Decode(payload []byte, inst db.LarkInstallation) (InboundMessage, bool, error) {
	return f(payload, inst)
}

// GorillaDialer is the production WSDialer.
type GorillaDialer struct {
	Dialer *websocket.Dialer
}

func NewGorillaDialer() *GorillaDialer {
	return &GorillaDialer{
		Dialer: &websocket.Dialer{
			HandshakeTimeout: 15 * time.Second,
		},
	}
}

func (g *GorillaDialer) DialContext(ctx context.Context, urlStr string, requestHeader http.Header) (WSConn, *http.Response, error) {
	d := g.Dialer
	if d == nil {
		d = websocket.DefaultDialer
	}
	c, resp, err := d.DialContext(ctx, urlStr, requestHeader)
	if err != nil {
		return nil, resp, err
	}
	return c, resp, nil
}
