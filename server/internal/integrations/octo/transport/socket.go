package transport

import (
	"context"
	"crypto/cipher"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Connection tuning. Values match the TS reference client.
const (
	heartbeatInterval     = 60 * time.Second
	pingMaxRetry          = 3
	dialTimeout           = 30 * time.Second
	reconnectBaseDelay    = 3 * time.Second
	reconnectMaxDelay     = 60 * time.Second
	stableThreshold       = 30 * time.Second
	rapidDisconnectWindow = 5 * time.Second
	rapidDisconnectLimit  = 3

	// maxDecryptRetries caps per-message decrypt/parse failures before a poison
	// message is ack'd-and-dropped (so the server stops redelivering it forever).
	maxDecryptRetries = 3
	// maxDecryptFailEntries bounds the per-messageID failure map.
	maxDecryptFailEntries = 1000
)

// errRapidDisconnect signals the higher layer that the connection is flapping —
// usually a stale im_token that must be refreshed before reconnecting.
var errRapidDisconnect = errors.New("im: rapid disconnect detected (token may be stale)")

// ErrAlreadyStarted is returned by Connect when the socket already has a running
// connection manager. A socket runs at most one manager goroutine at a time.
var ErrAlreadyStarted = errors.New("im: socket already started")

// SocketOptions configures a WuKongIM WebSocket connection.
type SocketOptions struct {
	WSURL string
	UID   string // robot_id from register
	Token string // im_token from register

	// OnMessage receives each decoded inbound message, in order, on the protocol
	// manager goroutine. Heartbeats run on a separate goroutine, so a slow
	// handler no longer triggers a false ping-timeout teardown — but it does
	// delay processing of later frames (including RECVACKs). Handlers should
	// enqueue and return rather than do blocking work inline.
	OnMessage func(BotMessage)

	OnConnected    func()
	OnDisconnected func()
	// OnError is called for terminal conditions (kicked, rapid disconnect). After
	// a terminal error the socket stops reconnecting and the manager exits; the
	// caller may then refresh the token (SetToken) and Connect again. Do NOT call
	// Connect from within OnError — the manager has not yet exited, so it returns
	// ErrAlreadyStarted; schedule the reconnect after the callback returns.
	OnError func(error)

	// Logf is an optional structured logger hook. If nil, errors are dropped.
	Logf func(format string, args ...any)
}

// Socket is a WuKongIM bot WebSocket client implementing the binary protocol
// with per-connection DH key exchange, AES decryption, heartbeat, reconnect,
// and RECVACK. Each Socket owns one independent connection so multiple bots can
// run simultaneously.
type Socket struct {
	opts SocketOptions

	mu      sync.Mutex
	conn    *websocket.Conn
	started bool
	stopped bool
	cancel  context.CancelFunc

	// State below is owned exclusively by the manager goroutine while a manager
	// is running. The single-manager invariant (enforced by `started`) is what
	// makes unsynchronized access safe.
	needReconnect        bool
	reconnectAttempts    int
	rapidDisconnectCount int

	dh            *dhKeyPair
	serverVersion int
	aesBlock      cipher.Block
	aesIV         string

	tempBuffer  []byte
	decryptFail map[string]int

	// connected and pingRetry are shared between the manager and the heartbeat
	// goroutine, so they are atomic. writeMu serializes all websocket writes
	// (gorilla forbids concurrent writers): heartbeat PINGs and RECVACKs.
	connected atomic.Bool
	pingRetry atomic.Int32
	writeMu   sync.Mutex

	// ackHook, when non-nil, replaces the websocket RECVACK write in onRecv. It
	// lets tests drive the inbound protocol path (decrypt dispatch, poison
	// ack-and-drop, fail-map reset) without a live connection. Nil in production.
	ackHook func(messageID string, messageSeq uint32)
}

// NewSocket creates a socket. Call Connect to start.
func NewSocket(opts SocketOptions) *Socket {
	return &Socket{
		opts:        opts,
		decryptFail: make(map[string]int),
	}
}

func (s *Socket) logf(format string, args ...any) {
	if s.opts.Logf != nil {
		s.opts.Logf(format, args...)
	}
}

// SetToken updates the im_token used for the next connection. Call it after a
// token refresh, before reconnecting. It is safe to call while the socket is
// stopped or between Connect calls.
func (s *Socket) SetToken(token string) {
	s.mu.Lock()
	s.opts.Token = token
	s.mu.Unlock()
}

// Connect starts the connection manager bound to ctx. It returns immediately;
// the connection runs on a background goroutine until ctx is cancelled,
// Disconnect is called, or a terminal error occurs. It returns ErrAlreadyStarted
// if a manager is already running.
func (s *Socket) Connect(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	cctx, cancel := context.WithCancel(ctx)
	s.started = true
	s.stopped = false
	s.cancel = cancel
	s.mu.Unlock()

	go s.manager(cctx)
	return nil
}

// Disconnect gracefully stops the socket and prevents further reconnects. It is
// idempotent and safe to call multiple times.
func (s *Socket) Disconnect() {
	s.mu.Lock()
	s.stopped = true
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.mu.Unlock()
}

func (s *Socket) setConn(c *websocket.Conn) {
	s.mu.Lock()
	s.conn = c
	s.mu.Unlock()
}

func (s *Socket) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// writeMessage serializes all websocket writes so the heartbeat goroutine and
// the manager (RECVACK) never write concurrently.
func (s *Socket) writeMessage(conn *websocket.Conn, data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// manager runs the connect→serve→backoff→reconnect loop until ctx is cancelled,
// Disconnect is called, or a terminal error stops reconnection. It clears the
// started flag on exit so the socket can be reconnected.
func (s *Socket) manager(ctx context.Context) {
	defer func() {
		s.mu.Lock()
		s.started = false
		s.mu.Unlock()
	}()

	s.needReconnect = true
	for {
		if ctx.Err() != nil || s.isStopped() || !s.needReconnect {
			return
		}
		err := s.connectOnce(ctx)
		if err != nil {
			s.logf("im: connection ended: %v", err)
		}
		if ctx.Err() != nil || s.isStopped() || !s.needReconnect {
			return
		}
		delay := s.nextBackoff()
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// nextBackoff returns an exponential delay capped at reconnectMaxDelay with
// ±25% jitter to avoid a thundering herd.
func (s *Socket) nextBackoff() time.Duration {
	exp := float64(reconnectBaseDelay) * math.Pow(2, float64(s.reconnectAttempts))
	if exp > float64(reconnectMaxDelay) || exp <= 0 {
		exp = float64(reconnectMaxDelay)
	}
	jitter := 0.75 + rand.Float64()*0.5
	s.reconnectAttempts++
	return time.Duration(exp * jitter)
}

// connectOnce dials, performs the handshake, and serves the connection until it
// dies. It returns when the connection ends; the manager decides whether to
// reconnect based on needReconnect.
func (s *Socket) connectOnce(ctx context.Context) error {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = dialTimeout
	dialCtx, dialCancel := context.WithTimeout(ctx, dialTimeout)
	conn, _, err := dialer.DialContext(dialCtx, s.opts.WSURL, nil)
	dialCancel()
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	s.setConn(conn)
	s.connected.Store(false)
	s.pingRetry.Store(0)
	s.tempBuffer = s.tempBuffer[:0]

	// Send CONNECT with a fresh DH public key.
	dh, err := generateDHKeyPair()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("dh keygen: %w", err)
	}
	s.dh = dh
	connectPkt := encodeConnectPacket(connectOptions{
		version:         protoVersion,
		deviceFlag:      0, // app/bot
		deviceID:        generateDeviceID() + "W",
		uid:             s.opts.UID,
		token:           s.opts.Token,
		clientTimestamp: time.Now().UnixMilli(),
		clientKey:       dh.publicB64,
	})
	if err := s.writeMessage(conn, connectPkt); err != nil {
		_ = conn.Close()
		return fmt.Errorf("send CONNECT: %w", err)
	}

	rawCh := make(chan []byte, 16)
	readErrCh := make(chan error, 1)
	readerDone := make(chan struct{})
	pingTimeoutCh := make(chan struct{}, 1)
	// connDone is closed by the defer to stop the reader and heartbeat goroutines
	// and release a reader parked on a full rawCh, so shutdown never deadlocks.
	connDone := make(chan struct{})

	go func() {
		defer close(readerDone)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				select {
				case readErrCh <- err:
				default:
				}
				return
			}
			select {
			case rawCh <- data:
			case <-connDone:
				return
			}
		}
	}()

	// Heartbeat runs independently of frame processing so a slow OnMessage
	// handler cannot stall PINGs and trigger a false ping-timeout teardown.
	go func() {
		t := time.NewTicker(heartbeatInterval)
		defer t.Stop()
		for {
			select {
			case <-connDone:
				return
			case <-t.C:
				if !s.connected.Load() {
					continue
				}
				if s.pingRetry.Add(1) > pingMaxRetry {
					select {
					case pingTimeoutCh <- struct{}{}:
					default:
					}
					return
				}
				if err := s.writeMessage(conn, encodePingPacket()); err != nil {
					select {
					case pingTimeoutCh <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}()

	defer func() {
		close(connDone)
		_ = conn.Close()
		<-readerDone
	}()

	var connectStart time.Time
	var stableTimer *time.Timer
	var stableC <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			s.needReconnect = false
			s.markDisconnect(connectStart)
			return nil

		case data := <-rawCh:
			closeConn, err := s.handleRawData(conn, data)
			if err != nil || closeConn {
				s.markDisconnect(connectStart)
				return err
			}
			// On first successful CONNACK, record the start and arm the stability
			// timer — measured from connected, not from dial, so backoff only
			// resets after the connection proves stable.
			if s.connected.Load() && connectStart.IsZero() {
				connectStart = time.Now()
				stableTimer = time.NewTimer(stableThreshold)
				stableC = stableTimer.C
				defer stableTimer.Stop()
			}

		case err := <-readErrCh:
			s.markDisconnect(connectStart)
			return fmt.Errorf("read: %w", err)

		case <-pingTimeoutCh:
			s.markDisconnect(connectStart)
			return errors.New("ping timeout")

		case <-stableC:
			s.reconnectAttempts = 0
			s.rapidDisconnectCount = 0
		}
	}
}

// markDisconnect updates connection-stability counters and fires OnDisconnected
// if the connection had reached the connected state. Three consecutive sub-5s
// connections are treated as flapping (stale token): it stops reconnecting and
// surfaces errRapidDisconnect via OnError.
func (s *Socket) markDisconnect(connectStart time.Time) {
	if s.connected.CompareAndSwap(true, false) {
		if s.opts.OnDisconnected != nil {
			s.opts.OnDisconnected()
		}
	}
	if !connectStart.IsZero() {
		if time.Since(connectStart) < rapidDisconnectWindow {
			s.rapidDisconnectCount++
		} else {
			s.rapidDisconnectCount = 0
		}
	}
	if s.rapidDisconnectCount >= rapidDisconnectLimit {
		s.needReconnect = false
		s.rapidDisconnectCount = 0
		if s.opts.OnError != nil {
			s.opts.OnError(errRapidDisconnect)
		}
	}
}

// handleRawData appends bytes to the reassembly buffer and dispatches every
// complete packet. It returns closeConn=true (or an error) when the connection
// must be torn down (buffer overflow, malformed frame, or a CONNACK failure).
func (s *Socket) handleRawData(conn *websocket.Conn, data []byte) (closeConn bool, err error) {
	s.tempBuffer = append(s.tempBuffer, data...)
	if len(s.tempBuffer) > maxTempBufferBytes {
		s.logf("im: tempBuffer exceeded %d bytes (%d) — dropping and reconnecting", maxTempBufferBytes, len(s.tempBuffer))
		s.tempBuffer = s.tempBuffer[:0]
		return true, nil
	}

	var fatalErr error
	consumed := 0
	for {
		used, perr := unpackOne(s.tempBuffer, consumed, func(packet []byte) {
			if e := s.onPacket(conn, packet); e != nil {
				fatalErr = e
			}
		})
		if perr != nil {
			s.tempBuffer = s.tempBuffer[:0]
			return true, perr
		}
		if used == 0 {
			break
		}
		consumed += used
		if fatalErr != nil {
			break
		}
	}
	// Reset to the head when fully drained (the common, frame-aligned case) so
	// the backing array does not creep forward over a long-lived connection.
	if consumed >= len(s.tempBuffer) {
		s.tempBuffer = s.tempBuffer[:0]
	} else if consumed > 0 {
		s.tempBuffer = s.tempBuffer[consumed:]
	}
	if fatalErr != nil {
		return true, fatalErr
	}
	return false, nil
}

// onPacket parses one complete packet and dispatches by type. A decode error on
// the fixed header forces a teardown+reconnect (a truncated framed packet means
// the stream is desynced), consistent with the buffer-overflow and malformed-
// frame paths.
func (s *Socket) onPacket(conn *websocket.Conn, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	firstByte := data[0]
	packetType := PacketType(firstByte >> 4)

	if packetType == pktPong {
		s.pingRetry.Store(0)
		return nil
	}
	if packetType == pktPing {
		return nil
	}

	dec := newDecoder(data)
	if _, err := dec.ReadByte(); err != nil { // header byte
		return fmt.Errorf("read header: %w", err)
	}
	if _, err := dec.readVarLen(); err != nil { // remaining length
		return fmt.Errorf("read remaining length: %w", err)
	}

	switch packetType {
	case pktConnack:
		hasServerVersion := firstByte&0x01 > 0
		return s.onConnack(dec, hasServerVersion)
	case pktRecv:
		return s.onRecv(conn, dec)
	case pktDisconnect:
		return s.onDisconnect(dec)
	default:
		// SENDACK and others are not used by bots.
		return nil
	}
}

// onConnack derives the AES key/IV from the DH handshake. A failure here is
// terminal for this connection (returned as an error) but whether to reconnect
// depends on the reason code — set on needReconnect before returning.
func (s *Socket) onConnack(dec *decoder, hasServerVersion bool) error {
	if hasServerVersion {
		v, err := dec.ReadByte()
		if err != nil {
			return err
		}
		s.serverVersion = int(v)
	}
	if _, err := dec.ReadUint64(); err != nil { // timeDiff (unused)
		return err
	}
	reasonCode, err := dec.ReadByte()
	if err != nil {
		return err
	}
	serverKey, err := dec.ReadString()
	if err != nil {
		return err
	}
	salt, err := dec.ReadString()
	if err != nil {
		return err
	}
	if s.serverVersion >= 4 {
		if _, err := dec.ReadUint64(); err != nil { // nodeId (unused)
			return err
		}
	}

	switch reasonCode {
	case 1:
		key, iv, derr := deriveAESKeyIV(s.dh.priv, serverKey, salt)
		if derr != nil {
			// Bad salt/key → AES IV would be wrong and every message would
			// silently fail to decrypt. Reconnect for a fresh handshake.
			s.needReconnect = true
			return derr
		}
		block, berr := newAESBlock(key)
		if berr != nil {
			s.needReconnect = true
			return berr
		}
		s.aesBlock = block
		s.aesIV = iv
		s.connected.Store(true)
		if s.opts.OnConnected != nil {
			s.opts.OnConnected()
		}
		return nil
	case 0:
		s.needReconnect = false
		s.fireDisconnectAndError(errors.New("kicked by server"))
		return errors.New("kicked by server")
	default:
		s.needReconnect = false
		err := fmt.Errorf("connect failed: reasonCode=%d", reasonCode)
		s.fireDisconnectAndError(err)
		return err
	}
}

// fireDisconnectAndError reports a terminal CONNACK rejection. The TS reference
// fires onDisconnected on the kick/fail path even though the connection never
// reached the connected state, so callers can clear bot state symmetrically.
func (s *Socket) fireDisconnectAndError(err error) {
	if s.opts.OnError != nil {
		s.opts.OnError(err)
	}
	if s.opts.OnDisconnected != nil {
		s.opts.OnDisconnected()
	}
}

// onRecv parses an inbound message, decrypts the payload, acks it, and hands a
// BotMessage to the application. Decrypt happens BEFORE the RECVACK so a
// transient failure leaves the message un-acked for redelivery; a persistent
// poison message is ack'd-and-dropped after maxDecryptRetries.
func (s *Socket) onRecv(conn *websocket.Conn, dec *decoder) error {
	settingByte, err := dec.ReadByte()
	if err != nil {
		return err
	}
	setting := parseSettingByte(settingByte)
	if _, err := dec.ReadString(); err != nil { // msgKey (unused)
		return err
	}
	fromUID, err := dec.ReadString()
	if err != nil {
		return err
	}
	channelID, err := dec.ReadString()
	if err != nil {
		return err
	}
	channelType, err := dec.ReadByte()
	if err != nil {
		return err
	}
	if s.serverVersion >= 3 {
		if _, err := dec.ReadUint32(); err != nil { // expire (unused)
			return err
		}
	}
	if _, err := dec.ReadString(); err != nil { // clientMsgNo (unused)
		return err
	}
	messageIDRaw, err := dec.ReadUint64()
	if err != nil {
		return err
	}
	messageID := strconv.FormatUint(messageIDRaw, 10)
	messageSeq, err := dec.ReadUint32()
	if err != nil {
		return err
	}
	timestamp, err := dec.ReadUint32()
	if err != nil {
		return err
	}
	if setting.topic {
		if _, err := dec.ReadString(); err != nil { // topic (unused)
			return err
		}
	}
	encryptedPayload := dec.ReadRemaining()

	payload, derr := s.decryptPayload(encryptedPayload)
	if derr != nil {
		fails := s.decryptFail[messageID] + 1
		if fails >= maxDecryptRetries {
			s.logf("im: payload decrypt failed %dx for message %s — ack-and-drop (poison): %v", fails, messageID, derr)
			delete(s.decryptFail, messageID)
			s.sendRecvack(conn, messageID, messageSeq)
			return nil
		}
		if len(s.decryptFail) >= maxDecryptFailEntries {
			s.decryptFail = make(map[string]int)
		}
		s.decryptFail[messageID] = fails
		s.logf("im: payload decrypt error (attempt %d/%d) for message %s — not acking: %v", fails, maxDecryptRetries, messageID, derr)
		return nil
	}

	delete(s.decryptFail, messageID)
	s.sendRecvack(conn, messageID, messageSeq)

	msg := BotMessage{
		MessageID:   messageID,
		MessageSeq:  messageSeq,
		FromUID:     fromUID,
		ChannelID:   channelID,
		ChannelType: ChannelType(channelType),
		Timestamp:   timestamp,
		Payload:     payload,
		StreamOn:    setting.streamOn,
	}
	if s.opts.OnMessage != nil {
		s.opts.OnMessage(msg)
	}
	return nil
}

func (s *Socket) decryptPayload(encrypted []byte) (MessagePayload, error) {
	var p MessagePayload
	plain, err := aesDecrypt(encrypted, s.aesBlock, s.aesIV)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(plain, &p); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Socket) sendRecvack(conn *websocket.Conn, messageID string, messageSeq uint32) {
	if s.ackHook != nil {
		s.ackHook(messageID, messageSeq)
		return
	}
	pkt, err := encodeRecvackPacket(messageID, messageSeq)
	if err != nil {
		s.logf("im: encode RECVACK failed for %s: %v", messageID, err)
		return
	}
	if err := s.writeMessage(conn, pkt); err != nil {
		s.logf("im: send RECVACK failed for %s: %v", messageID, err)
	}
}

func (s *Socket) onDisconnect(dec *decoder) error {
	_, _ = dec.ReadByte()   // reasonCode (unused)
	_, _ = dec.ReadString() // reason (unused)
	s.needReconnect = false
	if s.opts.OnError != nil {
		s.opts.OnError(errors.New("disconnected by server"))
	}
	return errors.New("disconnected by server")
}
