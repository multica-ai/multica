package wecom

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	StreamStatusStreaming = "streaming"
	StreamStatusFinal     = "final"
	StreamStatusError     = "error"
)

// StreamMessenger sends aibot_respond_msg stream frames on an active
// installation WS connection. WSConnector satisfies this while Run
// is active; the Hub routes patcher calls to the registered messenger.
type StreamMessenger interface {
	SendStreamUpdate(reqID, streamID, content string, finish bool) error
}

// StreamSender is the outbound transport the Patcher uses. *Hub
// satisfies it by delegating to the active StreamMessenger for the
// installation (if any).
type StreamSender interface {
	SendStreamReply(ctx context.Context, installationID pgtype.UUID, reqID, streamID, content string, finish bool) error
}

// ErrNoActiveWecomConnection is returned when the patcher tries to
// push a reply but this replica does not hold the installation's WS
// lease (or the connector is between reconnects).
var ErrNoActiveWecomConnection = errors.New("wecom: no active ws connection for installation")

// PatcherQueries is the narrow subset of *db.Queries the Patcher needs.
type PatcherQueries interface {
	GetWecomInstallation(ctx context.Context, id pgtype.UUID) (db.WecomInstallation, error)
	GetWecomChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.WecomChatSessionBinding, error)
	GetWecomOutboundStreamByTask(ctx context.Context, taskID pgtype.UUID) (db.WecomOutboundStream, error)
	GetWecomOutboundStreamByChatSession(ctx context.Context, chatSessionID pgtype.UUID) (db.WecomOutboundStream, error)
	UpdateWecomOutboundStreamStatus(ctx context.Context, arg db.UpdateWecomOutboundStreamStatusParams) error
}

// PatcherConfig tunes the outbound Patcher.
type PatcherConfig struct {
	Logger *slog.Logger
}

func (c PatcherConfig) withDefaults() PatcherConfig {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// Patcher reacts to task-lifecycle events and forwards agent chat
// replies to WeCom as stream message updates on the inbound req_id.
// It is the outbound side of the ingested path: the WS connector
// opens a streaming reply ("已收到，正在处理中…", finish=false) and
// records req_id/stream_id in wecom_outbound_stream; this patcher
// completes the same stream when EventChatDone fires.
type Patcher struct {
	queries PatcherQueries
	sender  StreamSender
	cfg     PatcherConfig
}

func NewPatcher(queries PatcherQueries, sender StreamSender, cfg PatcherConfig) *Patcher {
	cfg = cfg.withDefaults()
	return &Patcher{
		queries: queries,
		sender:  sender,
		cfg:     cfg,
	}
}

func (p *Patcher) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventTaskFailed, p.handleEvent)
	bus.Subscribe(protocol.EventChatDone, p.handleEvent)
}

func (p *Patcher) handleEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.processEvent(ctx, e); err != nil {
		p.cfg.Logger.Warn("wecom patcher: event handling failed",
			"event_type", e.Type,
			"task_id", e.TaskID,
			"chat_session_id", e.ChatSessionID,
			"error", err,
		)
	}
}

func (p *Patcher) processEvent(ctx context.Context, e events.Event) error {
	taskID, chatSessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !chatSessionID.Valid {
		return nil
	}

	binding, err := p.queries.GetWecomChatSessionBindingBySession(ctx, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup chat session binding: %w", err)
	}

	inst, err := p.queries.GetWecomInstallation(ctx, binding.InstallationID)
	if err != nil {
		return fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		return nil
	}

	stream, err := p.lookupOutboundStream(ctx, taskID, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	switch e.Type {
	case protocol.EventChatDone:
		content := chatDoneContent(e.Payload)
		if content == "" {
			return nil
		}
		return p.completeStream(ctx, inst.ID, stream, content, StreamStatusFinal)
	case protocol.EventTaskFailed:
		msg := "运行失败。"
		if detail := errorMessageFromPayload(e.Payload); detail != "" {
			msg = "运行失败：" + detail
		}
		return p.completeStream(ctx, inst.ID, stream, msg, StreamStatusError)
	}
	return nil
}

func (p *Patcher) lookupOutboundStream(ctx context.Context, taskID, chatSessionID pgtype.UUID) (db.WecomOutboundStream, error) {
	if taskID.Valid {
		stream, err := p.queries.GetWecomOutboundStreamByTask(ctx, taskID)
		if err == nil {
			return stream, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return db.WecomOutboundStream{}, err
		}
	}
	return p.queries.GetWecomOutboundStreamByChatSession(ctx, chatSessionID)
}

func (p *Patcher) completeStream(ctx context.Context, installationID pgtype.UUID, stream db.WecomOutboundStream, content, status string) error {
	if p.sender == nil {
		return errors.New("wecom patcher: stream sender missing")
	}
	if stream.ReqID == "" || stream.StreamID == "" {
		return errors.New("wecom patcher: outbound stream missing req_id or stream_id")
	}
	if err := p.sender.SendStreamReply(ctx, installationID, stream.ReqID, stream.StreamID, content, true); err != nil {
		if errors.Is(err, ErrNoActiveWecomConnection) {
			p.cfg.Logger.Warn("wecom patcher: no active ws to deliver reply",
				"installation_id", uuidString(installationID),
				"stream_id", stream.StreamID,
			)
			return nil
		}
		return fmt.Errorf("send stream reply: %w", err)
	}
	if err := p.queries.UpdateWecomOutboundStreamStatus(ctx, db.UpdateWecomOutboundStreamStatusParams{
		ID:     stream.ID,
		Status: status,
	}); err != nil {
		return fmt.Errorf("update outbound stream status: %w", err)
	}
	return nil
}

func taskAndSessionFromEvent(e events.Event) (taskID, chatSessionID pgtype.UUID, ok bool) {
	if e.TaskID != "" {
		_ = taskID.Scan(e.TaskID)
	}
	if e.ChatSessionID != "" {
		_ = chatSessionID.Scan(e.ChatSessionID)
	}
	switch pl := e.Payload.(type) {
	case map[string]any:
		if !taskID.Valid {
			if s, _ := pl["task_id"].(string); s != "" {
				_ = taskID.Scan(s)
			}
		}
		if !chatSessionID.Valid {
			if s, _ := pl["chat_session_id"].(string); s != "" {
				_ = chatSessionID.Scan(s)
			}
		}
	case protocol.ChatDonePayload:
		if !taskID.Valid {
			_ = taskID.Scan(pl.TaskID)
		}
		if !chatSessionID.Valid {
			_ = chatSessionID.Scan(pl.ChatSessionID)
		}
	}
	return taskID, chatSessionID, taskID.Valid || chatSessionID.Valid
}

func chatDoneContent(payload any) string {
	switch pl := payload.(type) {
	case protocol.ChatDonePayload:
		return pl.Content
	case map[string]any:
		if s, ok := pl["content"].(string); ok {
			return s
		}
	}
	return ""
}

func errorMessageFromPayload(payload any) string {
	if m, ok := payload.(map[string]any); ok {
		if s, ok := m["error"].(string); ok {
			return s
		}
		if s, ok := m["error_message"].(string); ok {
			return s
		}
	}
	return ""
}
