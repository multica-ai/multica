package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CredentialsProvider supplies decrypted bot + corp credentials per installation.
type CredentialsProvider interface {
	BotCredentials(inst db.WecomInstallation) (botID, botSecret, corpID, corpSecret string, err error)
}

type CredentialsProviderFunc func(inst db.WecomInstallation) (botID, botSecret, corpID, corpSecret string, err error)

func (f CredentialsProviderFunc) BotCredentials(inst db.WecomInstallation) (string, string, string, string, error) {
	return f(inst)
}

// StreamRegistry tracks which installation holds an active WS connector
// on this replica. *Hub satisfies this interface.
type StreamRegistry interface {
	RegisterStreamMessenger(instID pgtype.UUID, m StreamMessenger)
	UnregisterStreamMessenger(instID pgtype.UUID, m StreamMessenger)
}

// WSConnector holds the WeCom intelligent-robot long connection.
type WSConnector struct {
	inst        db.WecomInstallation
	credentials CredentialsProvider
	resolver    *UserIDResolver
	logger      *slog.Logger
	pingEvery   time.Duration
	streams     *OutboundStreamStore
	registry    StreamRegistry

	mu       sync.Mutex
	sendConn *websocket.Conn
}

type WSConnectorConfig struct {
	Credentials CredentialsProvider
	Resolver    *UserIDResolver
	Logger      *slog.Logger
	PingEvery   time.Duration
	Streams     *OutboundStreamStore
	Registry    StreamRegistry
}

func NewWSConnector(inst db.WecomInstallation, cfg WSConnectorConfig) (*WSConnector, error) {
	if cfg.Credentials == nil {
		return nil, errors.New("wecom: WSConnector requires credentials provider")
	}
	if cfg.Resolver == nil {
		cfg.Resolver = NewUserIDResolver()
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.PingEvery == 0 {
		cfg.PingEvery = 30 * time.Second
	}
	return &WSConnector{
		inst:        inst,
		credentials: cfg.Credentials,
		resolver:    cfg.Resolver,
		logger:      log,
		pingEvery:   cfg.PingEvery,
		streams:     cfg.Streams,
		registry:    cfg.Registry,
	}, nil
}

func (c *WSConnector) Run(ctx context.Context, inst db.WecomInstallation, emit EventEmitter) error {
	botID, botSecret, corpID, corpSecret, err := c.credentials.BotCredentials(inst)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, DefaultWecomWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial wecom ws: %w", err)
	}
	defer conn.Close()
	c.setConn(conn)
	defer c.setConn(nil)
	if c.registry != nil {
		c.registry.RegisterStreamMessenger(inst.ID, c)
		defer c.registry.UnregisterStreamMessenger(inst.ID, c)
	}

	sub, err := buildSubscribeFrame(botID, botSecret)
	if err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, sub); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go c.pingLoop(pingCtx, conn)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}
		if string(data) == "ping" {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
			continue
		}
		var frame wsFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			c.logger.Warn("wecom ws: invalid frame", "error", err)
			continue
		}
		if frame.ErrCode != 0 && frame.Cmd == "" {
			c.logger.Warn("wecom ws: frame error", "errcode", frame.ErrCode, "errmsg", frame.ErrMsg)
			continue
		}
		switch frame.Cmd {
		case "aibot_msg_callback":
			if err := c.handleMessageCallback(ctx, emit, frame, corpID, corpSecret); err != nil {
				c.logger.Error("wecom ws: handle message", "error", err)
			}
		case "aibot_event_callback":
			// MVP: welcome messages handled in a follow-up.
		default:
			// subscribe ack and other control frames
		}
	}
}

func (c *WSConnector) handleMessageCallback(ctx context.Context, emit EventEmitter, frame wsFrame, corpID, corpSecret string) error {
	var body msgCallbackBody
	if err := json.Unmarshal(frame.Body, &body); err != nil {
		return err
	}
	text := extractMessageBody(body)
	if text == "" {
		return nil
	}
	plainUserid, err := c.resolver.Resolve(ctx, corpID, corpSecret, body.From.Userid)
	if err != nil {
		c.logger.Warn("wecom ws: userid resolve failed", "error", err, "raw", body.From.Userid)
		return err
	}
	chatType := ChatTypeSingle
	if body.ChatType == "group" {
		chatType = ChatTypeGroup
	}
	chatID := body.ChatID
	if chatID == "" {
		chatID = plainUserid
	}
	msg := InboundMessage{
		EventType:      "message",
		BotID:          body.AibotID,
		ChatID:         ChatID(chatID),
		ChatType:       chatType,
		MessageID:      body.MsgID,
		ReqID:          frame.Headers.ReqID,
		SenderUserid:   Userid(plainUserid),
		Body:           text,
		CommandBody:    text,
		AddressedToBot: chatType == ChatTypeSingle || containsAtBot(text),
		MessageType:    body.MsgType,
	}
	res, err := emit(ctx, msg)
	if err != nil {
		return err
	}
	return c.replyForOutcome(ctx, c.inst, frame.Headers.ReqID, msg, res, text)
}

func (c *WSConnector) replyForOutcome(ctx context.Context, inst db.WecomInstallation, reqID string, msg InboundMessage, res DispatchResult, userText string) error {
	switch res.Outcome {
	case OutcomeNeedsBinding:
		text := "请先绑定 Multica 账号后再使用机器人。"
		if res.BindingLink != "" {
			text = fmt.Sprintf("请先绑定 Multica 账号，点击链接完成绑定：%s", res.BindingLink)
		}
		return c.sendStream(reqID, text)
	case OutcomeAgentOffline:
		return c.sendStream(reqID, "智能体当前离线，消息已记录，上线后会继续处理。")
	case OutcomeAgentArchived:
		return c.sendStream(reqID, "智能体已归档，请联系管理员恢复或重新绑定。")
	case OutcomeIngested:
		streamID := newReqID()
		if err := c.SendStreamUpdate(reqID, streamID, "已收到，正在处理中…", false); err != nil {
			return err
		}
		if c.streams != nil && res.ChatSessionID.Valid {
			if err := c.streams.RecordStreaming(ctx, RecordOutboundStreamParams{
				InstallationID: inst.ID,
				ChatSessionID:  res.ChatSessionID,
				ReqID:          reqID,
				StreamID:       streamID,
				WecomChatID:    string(msg.ChatID),
				WecomChatType:  string(msg.ChatType),
			}); err != nil {
				c.logger.Warn("wecom ws: record outbound stream failed", "error", err)
			}
		}
		return nil
	case OutcomeDropped:
		return nil
	default:
		return c.sendStream(reqID, "暂时无法处理该消息。")
	}
}

func (c *WSConnector) sendStream(reqID, content string) error {
	return c.SendStreamUpdate(reqID, newReqID(), content, true)
}

// SendStreamUpdate implements StreamMessenger.
func (c *WSConnector) SendStreamUpdate(reqID, streamID, content string, finish bool) error {
	conn := c.getConn()
	if conn == nil || reqID == "" {
		return nil
	}
	payload, err := buildStreamReply(reqID, streamID, content, finish)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func (c *WSConnector) setConn(conn *websocket.Conn) {
	c.mu.Lock()
	c.sendConn = conn
	c.mu.Unlock()
}

func (c *WSConnector) getConn() *websocket.Conn {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sendConn
}

func (c *WSConnector) pingLoop(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(c.pingEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
		}
	}
}

func containsAtBot(text string) bool {
	return len(text) > 0 // group @ messages already filtered by WeCom callback
}

// NewConnectorFactory builds a ConnectorFactory for the Hub.
func NewConnectorFactory(credentials CredentialsProvider, resolver *UserIDResolver, log *slog.Logger, registry StreamRegistry, streams *OutboundStreamStore) ConnectorFactory {
	return func(inst db.WecomInstallation) (EventConnector, error) {
		return NewWSConnector(inst, WSConnectorConfig{
			Credentials: credentials,
			Resolver:    resolver,
			Logger:      log,
			Streams:     streams,
			Registry:    registry,
		})
	}
}
