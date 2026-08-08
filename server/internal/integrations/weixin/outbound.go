package weixin

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type Outbound struct {
	q       *db.Queries
	senders *SendersRegistry
	logger  *slog.Logger
}

func NewOutbound(q *db.Queries, senders *SendersRegistry, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{q: q, senders: senders, logger: logger}
}

func (o *Outbound) Register(bus *events.Bus) { bus.Subscribe(protocol.EventChatDone, o.handleEvent) }

func (o *Outbound) handleEvent(event events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := o.process(ctx, event); err != nil {
		o.logger.WarnContext(ctx, "weixin: outbound reply failed", "error", err, "chat_session_id", event.ChatSessionID)
	}
}

func (o *Outbound) process(ctx context.Context, event events.Event) error {
	sessionID, err := util.ParseUUID(event.ChatSessionID)
	if err != nil || !sessionID.Valid {
		return nil
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID, ChannelType: channelTypeWeixin,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	content := chatDoneContent(event.Payload)
	if content == "" {
		return nil
	}
	// A web/mobile turn can reuse a session that originated in Weixin. Only
	// immutable channel-ingested task provenance authorizes an external reply.
	taskID, ok := chatDoneTaskID(event)
	if !ok {
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return err
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil || !deliver {
		return err
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{ID: binding.InstallationID, ChannelType: channelTypeWeixin})
	if err != nil || inst.Status != string(InstallationActive) {
		return err
	}
	if o.senders == nil {
		return errors.New("weixin: sender registry unavailable")
	}
	sender := o.senders.get(binding.InstallationID)
	if sender == nil {
		return errors.New("weixin: connection not ready on this replica")
	}
	_, err = sender.send(ctx, binding.ChannelChatID, content)
	return err
}

func chatDoneTaskID(event events.Event) (pgtype.UUID, bool) {
	raw := event.TaskID
	if raw == "" {
		switch payload := event.Payload.(type) {
		case protocol.ChatDonePayload:
			raw = payload.TaskID
		case map[string]any:
			raw, _ = payload["task_id"].(string)
		}
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil && id.Valid
}

func chatDoneContent(payload any) string {
	switch value := payload.(type) {
	case protocol.ChatDonePayload:
		return value.Content
	case map[string]any:
		text, _ := value["content"].(string)
		return text
	default:
		return ""
	}
}
