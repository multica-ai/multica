package sharecrm

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

type outboundQueries interface {
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers agent chat replies back to ShareCRM.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	client  *Client
	logger  *slog.Logger
}

func NewOutbound(q outboundQueries, decrypt Decrypter, client *Client, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil)
	}
	return &Outbound{q: q, decrypt: decrypt, client: client, logger: logger}
}

func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "sharecrm outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, _, ok := taskAndSessionFromEvent(e)
	if !ok {
		return nil
	}
	content := eventContent(e)
	if content == "" {
		return nil
	}
	// A missing snapshot means the task came from Web/Desktop/Mobile and
	// must not reach ShareCRM, even if its Chat once had a ShareCRM route.
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup sharecrm task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeShareCRM) {
		return nil
	}
	binding := sharecrmBindingFromTaskDelivery(delivery)
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeShareCRM),
	})
	if err != nil {
		return fmt.Errorf("load sharecrm installation: %w", err)
	}
	if inst.Status != "active" {
		return nil
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode sharecrm credentials: %w", err)
	}
	chatID := outboundChatID(binding)
	if _, err := o.client.SendMessage(ctx, creds.GatewayBaseURL, creds.AppID, creds.AppSecret, chatID, stripMarkdown(content), nil); err != nil {
		return fmt.Errorf("post sharecrm reply: %w", err)
	}
	return nil
}

func sharecrmBindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType: delivery.ChatType, LastMessageID: delivery.ChannelMessageID,
		LastThreadID: delivery.ChannelThreadID, RouteRevision: delivery.RouteRevision,
		Config: delivery.Config,
	}
}

// eventContent extracts the deliverable text from an EventChatDone payload
// (typed, or its map form after a serialization round trip) or an
// EventTaskFailed payload. Empty means stay silent.
//
// For task-failed the text mirrors the web transcript's failure chat_message:
// the broadcast's `error` field carries the same redacted failure text and is
// omitted while an auto-retry is pending (the retry attempt reports its own
// outcome), so error-present means deliverable. retry_pending must silence
// even if a mixed-version publisher accidentally includes an error string
// (same guard as dingtalk/outbound.go).
func eventContent(e events.Event) string {
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if e.Type == protocol.EventTaskFailed {
			if retryPending, _ := p["retry_pending"].(bool); retryPending {
				return ""
			}
			if s, _ := p["error"].(string); s != "" {
				return "⚠️ " + s
			}
			return ""
		}
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

func taskAndSessionFromEvent(e events.Event) (taskID, sessionID pgtype.UUID, ok bool) {
	if e.TaskID != "" {
		_ = taskID.Scan(e.TaskID)
	}
	if e.ChatSessionID != "" {
		_ = sessionID.Scan(e.ChatSessionID)
	}
	switch p := e.Payload.(type) {
	case protocol.ChatDonePayload:
		if !taskID.Valid {
			_ = taskID.Scan(p.TaskID)
		}
		if !sessionID.Valid {
			_ = sessionID.Scan(p.ChatSessionID)
		}
	case map[string]any:
		if !taskID.Valid {
			if raw, _ := p["task_id"].(string); raw != "" {
				_ = taskID.Scan(raw)
			}
		}
		if !sessionID.Valid {
			if raw, _ := p["chat_session_id"].(string); raw != "" {
				_ = sessionID.Scan(raw)
			}
		}
	}
	return taskID, sessionID, taskID.Valid
}
