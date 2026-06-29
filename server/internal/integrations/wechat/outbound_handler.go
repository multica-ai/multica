package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type OutboundQueries interface {
	GetWechatChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.WechatChatSessionBinding, error)
	GetWechatInstallation(ctx context.Context, id pgtype.UUID) (db.WechatInstallation, error)
}

type OutboundHandler struct {
	queries  OutboundQueries
	registry *ConnectorRegistry
	logger   *slog.Logger

	mu      sync.Mutex
	streams map[string]*activeStream // keyed by taskID
}

type activeStream struct {
	botID         string
	callbackReqID string
	streamID      string
	content       strings.Builder
	lastSentAt    time.Time
	conn          *WSConnector
}

const streamThrottle = 800 * time.Millisecond

func NewOutboundHandler(queries OutboundQueries, registry *ConnectorRegistry, logger *slog.Logger) *OutboundHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboundHandler{
		queries:  queries,
		registry: registry,
		logger:   logger,
		streams:  make(map[string]*activeStream),
	}
}

func (h *OutboundHandler) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskQueued, h.handleTaskQueued)
	bus.Subscribe(protocol.EventTaskMessage, h.handleTaskMessage)
	bus.Subscribe(protocol.EventChatDone, h.handleChatDone)
}

func (h *OutboundHandler) handleTaskQueued(e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	chatSessionID, _ := payload["chat_session_id"].(string)
	taskID, _ := payload["task_id"].(string)
	if chatSessionID == "" || taskID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionUUID := pgtype.UUID{}
	if err := sessionUUID.Scan(chatSessionID); err != nil {
		return
	}

	binding, err := h.queries.GetWechatChatSessionBindingBySession(ctx, sessionUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		h.logger.Warn("wechat outbound: lookup binding failed", "error", err)
		return
	}
	if binding.LastCallbackReqID == "" {
		return
	}

	inst, err := h.queries.GetWechatInstallation(ctx, binding.InstallationID)
	if err != nil {
		return
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return
	}

	conn, ok := h.registry.Get(inst.BotID)
	if !ok {
		return
	}

	streamID := fmt.Sprintf("stream_%s_%d", taskID, time.Now().UnixMilli())

	stream := &activeStream{
		botID:         inst.BotID,
		callbackReqID: binding.LastCallbackReqID,
		streamID:      streamID,
		lastSentAt:    time.Now(),
		conn:          conn,
	}

	h.mu.Lock()
	h.streams[taskID] = stream
	h.mu.Unlock()

	if err := conn.RespondStream(binding.LastCallbackReqID, streamID, "", false); err != nil {
		h.logger.Warn("wechat outbound: initial stream frame failed", "error", err)
	}

	h.logger.Info("wechat outbound: stream started",
		"task_id", taskID,
		"bot_id", inst.BotID,
		"stream_id", streamID,
	)
}

func (h *OutboundHandler) handleTaskMessage(e events.Event) {
	payload := h.extractTaskMessagePayload(e.Payload)
	if payload == nil {
		return
	}
	if payload.Type != "text" || payload.Content == "" {
		return
	}

	h.mu.Lock()
	stream, ok := h.streams[payload.TaskID]
	h.mu.Unlock()
	if !ok {
		return
	}

	stream.content.WriteString(payload.Content)
	stream.content.WriteString("\n")

	if time.Since(stream.lastSentAt) < streamThrottle {
		return
	}

	content := stream.content.String()
	if err := stream.conn.RespondStream(stream.callbackReqID, stream.streamID, content, false); err != nil {
		h.logger.Warn("wechat outbound: stream frame failed", "error", err, "task_id", payload.TaskID)
		return
	}
	stream.lastSentAt = time.Now()
}

func (h *OutboundHandler) handleChatDone(e events.Event) {
	if e.ChatSessionID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	content := h.extractDoneContent(e.Payload)
	if content == "" {
		return
	}

	taskID := h.extractDoneTaskID(e.Payload)

	h.mu.Lock()
	stream, ok := h.streams[taskID]
	if ok {
		delete(h.streams, taskID)
	}
	h.mu.Unlock()

	if ok {
		if err := stream.conn.RespondStream(stream.callbackReqID, stream.streamID, content, true); err != nil {
			h.logger.Warn("wechat outbound: final stream frame failed", "error", err, "task_id", taskID)
		} else {
			h.logger.Info("wechat outbound: stream finished", "task_id", taskID)
		}
		return
	}

	// Fallback: no active stream (task wasn't tracked), send one-shot markdown
	h.sendFallbackMarkdown(ctx, e.ChatSessionID, content)
}

func (h *OutboundHandler) sendFallbackMarkdown(ctx context.Context, chatSessionID, content string) {
	sessionUUID := pgtype.UUID{}
	if err := sessionUUID.Scan(chatSessionID); err != nil {
		return
	}

	binding, err := h.queries.GetWechatChatSessionBindingBySession(ctx, sessionUUID)
	if err != nil {
		return
	}
	if binding.LastCallbackReqID == "" {
		return
	}

	inst, err := h.queries.GetWechatInstallation(ctx, binding.InstallationID)
	if err != nil {
		return
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return
	}

	conn, ok := h.registry.Get(inst.BotID)
	if !ok {
		return
	}

	if err := conn.RespondMarkdown(binding.LastCallbackReqID, content); err != nil {
		h.logger.Warn("wechat outbound: fallback markdown failed", "error", err)
	}
}

func (h *OutboundHandler) extractTaskMessagePayload(payload any) *protocol.TaskMessagePayload {
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var p protocol.TaskMessagePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return nil
	}
	if p.TaskID == "" {
		return nil
	}
	return &p
}

func (h *OutboundHandler) extractDoneContent(payload any) string {
	if payload == nil {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var p protocol.ChatDonePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return ""
	}
	return p.Content
}

func (h *OutboundHandler) extractDoneTaskID(payload any) string {
	if payload == nil {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	var p protocol.ChatDonePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return ""
	}
	return p.TaskID
}
