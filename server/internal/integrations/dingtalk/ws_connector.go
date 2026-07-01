package dingtalk

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// This file hand-rolls the DingTalk Stream WebSocket connection, replacing the
// vendor stream SDK. It mirrors the Lark ws_connector: a single blocking run()
// owns exactly one socket session. Reconnect/backoff/lease live in the shared
// engine.Supervisor, so run() just returns — nil on a graceful close, an error
// on a broken connection — and the supervisor decides when to redial.

const (
	streamPingInterval = 30 * time.Second
	streamReadDeadline = 90 * time.Second
	streamWriteTimeout = 10 * time.Second
)

// wsConn is the slice of *websocket.Conn the connector uses, extracted so tests
// can inject a fake in-memory socket.
type wsConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	SetPongHandler(h func(appData string) error)
	Close() error
}

// wsDialer opens a wsConn for a dial URL. The production dialer wraps gorilla;
// tests inject a fake.
type wsDialer interface {
	dial(ctx context.Context, dialURL string) (wsConn, error)
}

type gorillaDialer struct{}

func (gorillaDialer) dial(ctx context.Context, dialURL string) (wsConn, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, dialURL, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// wsConnector runs one installation's Stream session.
type wsConnector struct {
	httpClient *http.Client
	apiBase    string
	appKey     string
	appSecret  string

	dialer    wsDialer
	open      func(ctx context.Context) (string, error) // dial-URL fetch; defaults to openConnection
	onMessage func(ctx context.Context, data *botCallbackData) error
	logger    *slog.Logger

	pingInterval time.Duration
	readDeadline time.Duration
	writeTimeout time.Duration
}

func (c *wsConnector) withDefaults() {
	if c.dialer == nil {
		c.dialer = gorillaDialer{}
	}
	if c.open == nil {
		c.open = func(ctx context.Context) (string, error) {
			return openConnection(ctx, c.httpClient, c.apiBase, c.appKey, c.appSecret)
		}
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	if c.pingInterval == 0 {
		c.pingInterval = streamPingInterval
	}
	if c.readDeadline == 0 {
		c.readDeadline = streamReadDeadline
	}
	if c.writeTimeout == 0 {
		c.writeTimeout = streamWriteTimeout
	}
}

// run opens the connection and services frames until ctx is cancelled (returns
// nil), the gateway sends a SYSTEM disconnect (returns nil), or the socket
// breaks (returns the error, so the supervisor reconnects under backoff).
func (c *wsConnector) run(ctx context.Context) error {
	c.withDefaults()

	dialURL, err := c.open(ctx)
	if err != nil {
		return fmt.Errorf("dingtalk stream: open connection: %w", err)
	}
	conn, err := c.dialer.dial(ctx, dialURL)
	if err != nil {
		return fmt.Errorf("dingtalk stream: dial: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)

	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { _ = conn.Close() }) }

	var writeMu sync.Mutex
	writeResponse := func(resp dataFrameResponse) error {
		b, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(c.writeTimeout))
		return conn.WriteMessage(websocket.TextMessage, b)
	}

	done := make(chan struct{})
	// Watchdog: gorilla's ReadMessage does not observe ctx, so closing the socket
	// is what unblocks a read stuck in the syscall when the run ctx is cancelled.
	go func() {
		select {
		case <-runCtx.Done():
			closeConn()
		case <-done:
		}
	}()

	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		ticker := time.NewTicker(c.pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.writeTimeout))
				writeMu.Unlock()
				if err != nil {
					return // the read loop owns teardown
				}
			}
		}
	}()

	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(c.readDeadline))
	})

	defer func() {
		cancel()
		closeConn()
		close(done)
		<-pingDone
	}()

	for {
		if err := conn.SetReadDeadline(time.Now().Add(c.readDeadline)); err != nil {
			return fmt.Errorf("dingtalk stream: set read deadline: %w", err)
		}
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if runCtx.Err() != nil {
				return nil // ctx cancelled → graceful stop
			}
			return fmt.Errorf("dingtalk stream: read: %w", err)
		}
		if msgType != websocket.TextMessage {
			continue
		}
		var frame dataFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			c.logger.WarnContext(runCtx, "dingtalk stream: malformed frame", "error", err)
			continue
		}

		switch {
		case frame.Type == frameTypeSystem && frame.topic() == systemTopicPing:
			if err := writeResponse(newPongResponse(frame.messageID(), frame.Data)); err != nil {
				if runCtx.Err() != nil {
					return nil
				}
				return fmt.Errorf("dingtalk stream: write pong: %w", err)
			}
		case frame.Type == frameTypeSystem && frame.topic() == systemTopicDisconnect:
			// Gateway asks us to reconnect; return cleanly and let the supervisor redial.
			return nil
		case frame.Type == frameTypeCallback && frame.topic() == botMessageTopic:
			c.dispatchCallback(runCtx, &frame, writeResponse)
		default:
			c.logger.WarnContext(runCtx, "dingtalk stream: unhandled frame",
				"type", frame.Type, "topic", frame.topic())
		}
	}
}

// dispatchCallback decodes a bot-message callback, hands it to onMessage, and
// always ACKs (echoing the frame's messageId). A decode or handler error is
// logged, never surfaced: DingTalk expires un-ACKed frames fast and the engine's
// (installation, msgId) dedup guards any redelivery — matching the prior SDK
// callback's always-ACK behavior.
func (c *wsConnector) dispatchCallback(ctx context.Context, frame *dataFrame, writeResponse func(dataFrameResponse) error) {
	var payload botCallbackData
	if err := json.Unmarshal([]byte(frame.Data), &payload); err != nil {
		c.logger.WarnContext(ctx, "dingtalk stream: decode callback", "error", err)
	} else if err := c.onMessage(ctx, &payload); err != nil {
		c.logger.WarnContext(ctx, "dingtalk stream: handler error", "error", err)
	}
	if err := writeResponse(newAckResponse(frame.messageID())); err != nil {
		c.logger.WarnContext(ctx, "dingtalk stream: write ack", "error", err)
	}
}
