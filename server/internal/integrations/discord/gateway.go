package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// This file hand-rolls the Discord Gateway (v10) WebSocket client, mirroring
// dingtalk/ws_connector.go: one blocking run owns one socket session.
// Reconnect/backoff/lease live in the shared engine.Supervisor, so run()
// returns — nil on a graceful close, an error on a broken connection — and the
// supervisor decides when to redial.
//
// ponytail: no Gateway RESUME. A reconnect re-IDENTIFYs instead of replaying
// from the last sequence; the engine's (installation, message_id) dedup
// absorbs any redelivery, so the extra session state buys nothing yet.

const (
	// defaultGatewayURL is the production Gateway endpoint.
	defaultGatewayURL = "wss://gateway.discord.gg/?v=10&encoding=json"

	gatewayWriteTimeout = 10 * time.Second
	gatewayReadLimit    = 1 << 20 // 1 MiB: message content is capped at 2000 chars
	gatewayMaxMissedAck = 2

	// Gateway intents. GUILDS + GUILD_MESSAGES + DIRECT_MESSAGES give guild
	// channel and DM messages; MESSAGE_CONTENT is privileged and must be
	// enabled in the Discord developer portal for guild message bodies (DMs
	// and @-mentions carry content regardless).
	intentGuilds         = 1 << 0
	intentGuildMessages  = 1 << 9
	intentDirectMessages = 1 << 12
	intentMessageContent = 1 << 15
	gatewayIntents       = intentGuilds | intentGuildMessages | intentDirectMessages | intentMessageContent

	// Gateway opcodes used here.
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatAck   = 11
)

// gatewayPayload is the envelope every Gateway frame uses.
type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// gatewayHello is the op 10 payload.
type gatewayHello struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// gatewayReady is the op 0 READY payload (only the fields the adapter reads).
type gatewayReady struct {
	SessionID string `json:"session_id"`
}

// gatewayMessage is the MESSAGE_CREATE dispatch payload, reduced to the fields
// the adapter needs.
type gatewayMessage struct {
	ID          string              `json:"id"`
	ChannelID   string              `json:"channel_id"`
	GuildID     string              `json:"guild_id,omitempty"`
	Content     string              `json:"content"`
	Author      gatewayAuthor       `json:"author"`
	Mentions    []gatewayMention    `json:"mentions"`
	Attachments []gatewayAttachment `json:"attachments"`
}

type gatewayAuthor struct {
	ID         string `json:"id"`
	Bot        bool   `json:"bot"`
	GlobalName string `json:"global_name,omitempty"`
	Username   string `json:"username,omitempty"`
}

type gatewayMention struct {
	ID string `json:"id"`
}

type gatewayAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
}

// wsDialer opens a WebSocket for a dial URL. The production dialer wraps
// gorilla; tests inject one pointed at an httptest server.
type wsDialer func(ctx context.Context, url string) (*websocket.Conn, error)

func gorillaDial(ctx context.Context, url string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	return conn, err
}

// gatewayConn runs one installation's Gateway session.
type gatewayConn struct {
	botID  string
	token  string
	url    string
	dial   wsDialer
	logger *slog.Logger
	// onMessage receives every MESSAGE_CREATE dispatch.
	onMessage func(ctx context.Context, m gatewayMessage) error
}

func (g *gatewayConn) withDefaults() {
	if g.url == "" {
		g.url = defaultGatewayURL
	}
	if g.dial == nil {
		g.dial = gorillaDial
	}
	if g.logger == nil {
		g.logger = slog.Default()
	}
}

// run opens the Gateway connection and services frames until ctx is cancelled
// (returns nil), Discord asks for a reconnect (returns an error so the
// supervisor backs off), or the socket breaks.
func (g *gatewayConn) run(ctx context.Context) error {
	g.withDefaults()
	conn, err := g.dial(ctx, g.url)
	if err != nil {
		return fmt.Errorf("discord gateway: dial: %w", err)
	}
	defer conn.Close()
	conn.SetReadLimit(gatewayReadLimit)

	// One writer goroutine serialises heartbeats and identify frames; gorilla
	// forbids concurrent writers.
	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(gatewayWriteTimeout))
		return conn.WriteJSON(v)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var seqMu sync.Mutex
	var seq *int64
	setSeq := func(s *int64) {
		if s == nil {
			return
		}
		seqMu.Lock()
		v := *s
		seq = &v
		seqMu.Unlock()
	}
	currentSeq := func() *int64 {
		seqMu.Lock()
		defer seqMu.Unlock()
		return seq
	}

	// Read HELLO before anything else: it carries the heartbeat cadence.
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, first, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("discord gateway: read hello: %w", err)
	}
	var hello gatewayPayload
	if err := json.Unmarshal(first, &hello); err != nil || hello.Op != opHello {
		return errors.New("discord gateway: first frame is not HELLO")
	}
	var h gatewayHello
	if err := json.Unmarshal(hello.D, &h); err != nil || h.HeartbeatInterval <= 0 {
		return errors.New("discord gateway: HELLO has no heartbeat_interval")
	}

	if err := writeJSON(map[string]any{
		"op": opIdentify,
		"d": map[string]any{
			"token":   g.token,
			"intents": gatewayIntents,
			"properties": map[string]any{
				"os":      "linux",
				"browser": "multica",
				"device":  "multica",
			},
		},
	}); err != nil {
		return fmt.Errorf("discord gateway: identify: %w", err)
	}

	// Heartbeat loop. Discord closes the session when it misses heartbeats, so
	// a write failure ends the run rather than logging and continuing.
	hbDone := make(chan struct{})
	defer close(hbDone)
	var missedAcks int32
	go func() {
		ticker := time.NewTicker(time.Duration(h.HeartbeatInterval) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-hbDone:
				return
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if atomic.LoadInt32(&missedAcks) > gatewayMaxMissedAck {
					g.logger.WarnContext(runCtx, "discord gateway: heartbeat unacknowledged; reconnecting", "bot_id", g.botID)
					cancel()
					return
				}
				if err := writeJSON(map[string]any{"op": opHeartbeat, "d": currentSeq()}); err != nil {
					g.logger.WarnContext(runCtx, "discord gateway: heartbeat write failed", "bot_id", g.botID, "error", err)
					cancel()
					return
				}
				atomic.AddInt32(&missedAcks, 1)
			}
		}
	}()

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil || runCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("discord gateway: read: %w", err)
		}
		var p gatewayPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		setSeq(p.S)
		switch p.Op {
		case opDispatch:
			if p.T == "MESSAGE_CREATE" {
				var m gatewayMessage
				if err := json.Unmarshal(p.D, &m); err != nil {
					continue
				}
				if err := g.onMessage(ctx, m); err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return err
				}
			}
		case opHeartbeat:
			// Discord asks for an immediate heartbeat.
			if err := writeJSON(map[string]any{"op": opHeartbeat, "d": currentSeq()}); err != nil {
				return fmt.Errorf("discord gateway: heartbeat: %w", err)
			}
		case opHeartbeatAck:
			atomic.StoreInt32(&missedAcks, 0)
		case opReconnect:
			return errors.New("discord gateway: server requested reconnect")
		case opInvalidSession:
			return errors.New("discord gateway: invalid session")
		}
	}
}
