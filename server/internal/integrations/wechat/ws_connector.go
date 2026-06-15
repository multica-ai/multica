package wechat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const wsEndpoint = "wss://openws.work.weixin.qq.com"

// WSCommand is the WeCom long-connection JSON frame envelope.
type WSCommand struct {
	Cmd     string          `json:"cmd"`
	Headers *WSHeaders      `json:"headers,omitempty"`
	Body    json.RawMessage `json:"body,omitempty"`
}

type WSHeaders struct {
	ReqID string `json:"req_id,omitempty"`
}

type SubscribeBody struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

type CallbackBody struct {
	MsgID    string       `json:"msgid"`
	AiBotID  string       `json:"aibotid"`
	ChatID   string       `json:"chatid"`
	ChatType string       `json:"chattype"`
	From     CallbackFrom `json:"from"`
	MsgType  string       `json:"msgtype"`
	Text     CallbackText `json:"text"`
}

type CallbackFrom struct {
	UserID string `json:"userid"`
	Name   string `json:"name"`
}

type CallbackText struct {
	Content string `json:"content"`
}

// Stream reply types for aibot_respond_msg.
type RespondStreamMsg struct {
	MsgType string      `json:"msgtype"`
	Stream  *StreamBody `json:"stream"`
}

type StreamBody struct {
	ID       string          `json:"id"`
	Finish   bool            `json:"finish"`
	Content  string          `json:"content"`
	Feedback *StreamFeedback `json:"feedback,omitempty"`
}

type StreamFeedback struct {
	ID string `json:"id"`
}

type RespondMarkdownMsg struct {
	MsgType  string           `json:"msgtype"`
	Markdown *MarkdownContent `json:"markdown"`
}

type MarkdownContent struct {
	Content string `json:"content"`
}

// WSConnectorConfig tunes the WeCom connector.
type WSConnectorConfig struct {
	InstallationService *InstallationService
	Logger              *slog.Logger
}

// WSConnector implements EventConnector for the WeCom long-connection protocol.
type WSConnector struct {
	installSvc *InstallationService
	logger     *slog.Logger

	mu   sync.Mutex
	conn *websocket.Conn
}

func NewWSConnector(cfg WSConnectorConfig) *WSConnector {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &WSConnector{
		installSvc: cfg.InstallationService,
		logger:     log,
	}
}

func (c *WSConnector) Run(ctx context.Context, inst db.WechatInstallation, emit EventEmitter) error {
	secret, err := c.installSvc.DecryptSecret(inst)
	if err != nil {
		return fmt.Errorf("decrypt secret: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsEndpoint, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
	}()

	if err := c.subscribe(conn, inst.BotID, secret); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	c.logger.Info("wechat connector: connected", "bot_id", inst.BotID)

	// Ping loop
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sendPing(conn)
			}
		}
	}()

	// Context-cancel watchdog: closes the conn when ctx fires so the
	// blocking ReadMessage returns immediately.
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	// Read loop
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		c.logger.Debug("wechat ws recv", "bot_id", inst.BotID, "raw", string(message))
		c.handleMessage(ctx, inst, message, emit)
	}
}

// RespondStream sends a stream frame reply using the callback req_id.
func (c *WSConnector) RespondStream(callbackReqID, streamID, content string, finish bool) error {
	body := RespondStreamMsg{
		MsgType: "stream",
		Stream: &StreamBody{
			ID:      streamID,
			Finish:  finish,
			Content: content,
		},
	}
	return c.sendRespond(callbackReqID, body)
}

// RespondMarkdown sends a markdown reply using the callback req_id.
func (c *WSConnector) RespondMarkdown(callbackReqID, content string) error {
	body := RespondMarkdownMsg{
		MsgType:  "markdown",
		Markdown: &MarkdownContent{Content: content},
	}
	return c.sendRespond(callbackReqID, body)
}

func (c *WSConnector) sendRespond(callbackReqID string, body any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	msg := WSCommand{
		Cmd:     "aibot_respond_msg",
		Headers: &WSHeaders{ReqID: callbackReqID},
		Body:    b,
	}
	return c.conn.WriteJSON(msg)
}

func (c *WSConnector) subscribe(conn *websocket.Conn, botID, secret string) error {
	subBody := SubscribeBody{BotID: botID, Secret: secret}
	b, _ := json.Marshal(subBody)
	msg := WSCommand{
		Cmd:     "aibot_subscribe",
		Headers: &WSHeaders{ReqID: fmt.Sprintf("sub_%d", time.Now().UnixMilli())},
		Body:    b,
	}
	return conn.WriteJSON(msg)
}

func (c *WSConnector) sendPing(conn *websocket.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	msg := WSCommand{
		Cmd:     "ping",
		Headers: &WSHeaders{ReqID: fmt.Sprintf("ping_%d", time.Now().UnixMilli())},
	}
	conn.WriteJSON(msg)
}

func (c *WSConnector) handleMessage(ctx context.Context, inst db.WechatInstallation, data []byte, emit EventEmitter) {
	var cmd WSCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		c.logger.Error("wechat: unmarshal ws message failed", "error", err)
		return
	}

	switch cmd.Cmd {
	case "aibot_msg_callback":
		c.handleCallback(ctx, inst, cmd.Headers, cmd.Body, emit)
	case "pong", "":
		// heartbeat response or subscribe/respond ack
	default:
		c.logger.Debug("wechat: unhandled ws command", "cmd", cmd.Cmd)
	}
}

func (c *WSConnector) handleCallback(ctx context.Context, inst db.WechatInstallation, headers *WSHeaders, data json.RawMessage, emit EventEmitter) {
	var cb CallbackBody
	if err := json.Unmarshal(data, &cb); err != nil {
		c.logger.Error("wechat: unmarshal callback failed", "error", err)
		return
	}

	callbackReqID := ""
	if headers != nil {
		callbackReqID = headers.ReqID
	}

	c.logger.Info("wechat: received message",
		"bot_id", inst.BotID,
		"from", cb.From.UserID,
		"chat_id", cb.ChatID,
		"content", cb.Text.Content,
		"callback_req_id", callbackReqID,
	)

	chatType := ChatTypeGroup
	if cb.ChatType == "single" {
		chatType = ChatTypeSingle
	}

	msg := InboundMessage{
		MessageID:      cb.MsgID,
		BotID:          inst.BotID,
		ChatID:         cb.ChatID,
		ChatType:       chatType,
		SenderUserID:   cb.From.UserID,
		SenderName:     cb.From.Name,
		Body:           cb.Text.Content,
		MsgType:        cb.MsgType,
		CallbackReqID:  callbackReqID,
		AddressedToBot: chatType == ChatTypeSingle,
	}

	result, err := emit(ctx, msg)
	if err != nil {
		c.logger.Error("wechat: dispatch failed", "error", err, "msg_id", msg.MessageID)
	} else {
		c.logger.Info("wechat: dispatch result", "outcome", result.Outcome, "msg_id", msg.MessageID)
	}
}
