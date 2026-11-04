package discord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// outboundQueries is the slice of generated queries the Discord outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	SetChatMessageChannelOutboundProvenanceByTask(ctx context.Context, arg db.SetChatMessageChannelOutboundProvenanceByTaskParams) (int64, error)
	RecordChannelOutboundMessage(ctx context.Context, arg db.RecordChannelOutboundMessageParams) error
}

// Outbound delivers an agent's chat reply back to Discord — the outbound half
// of the round trip. It mirrors slack.Outbound: on EventChatDone it finds the
// Discord chat binding for the finished task's session and posts the reply into
// the originating channel. Sessions with no Discord binding are ignored, so it
// coexists with the other channel subscribers on the shared event bus.
//
// ponytail: no streaming/placeholder edits. Discord has no message-edit stream
// protocol, and one send on completion is the whole UX the other BYO adapters
// need. Add throttled edits if a live "typing answer" is requested.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	logger  *slog.Logger
	apiBase string
}

// NewOutbound builds the Discord outbound subscriber.
func NewOutbound(q outboundQueries, decrypt Decrypter, apiBase string, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{q: q, decrypt: decrypt, logger: logger, apiBase: apiBase}
}

// Register subscribes to the chat-done event on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck Discord HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "discord outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		return nil // nothing to say (empty completion)
	}
	taskID, ok := chatDoneTaskID(e)
	if !ok {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // direct Chat or another channel
		}
		return fmt.Errorf("lookup discord task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeDiscord) {
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	// A web/mobile chat task can reuse a session that originated in Discord;
	// its reply belongs only in Multica. Fail closed when the origin cannot be
	// established.
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          delivery.InstallationID,
		ChannelType: string(TypeDiscord),
	})
	if err != nil {
		return fmt.Errorf("load discord installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode discord credentials: %w", err)
	}
	chatID := outboundChatID(delivery)
	result, err := sendText(ctx, newAPIClient(o.apiBase, creds.BotToken, nil), chatID, content, "")
	if err != nil {
		return fmt.Errorf("post discord reply: %w", err)
	}
	messageIDs := result.MessageIDs
	if len(messageIDs) == 0 && result.MessageID != "" {
		messageIDs = []string{result.MessageID}
	}
	if len(messageIDs) == 0 {
		return errors.New("post discord reply: provider returned no message id")
	}
	rows, err := o.q.SetChatMessageChannelOutboundProvenanceByTask(ctx, db.SetChatMessageChannelOutboundProvenanceByTaskParams{
		ChannelType:    pgtype.Text{String: string(TypeDiscord), Valid: true},
		InstallationID: delivery.InstallationID,
		ChannelChatID:  pgtype.Text{String: chatID, Valid: true},
		MessageIds:     messageIDs,
		TaskID:         taskID,
	})
	if err != nil {
		return fmt.Errorf("record discord reply provenance: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("record discord reply provenance: updated %d assistant rows, want 1", rows)
	}
	for _, messageID := range messageIDs {
		if err := o.q.RecordChannelOutboundMessage(ctx, db.RecordChannelOutboundMessageParams{
			OutboundInstallationID: delivery.InstallationID,
			OutboundChannelType:    delivery.ChannelType,
			OutboundMessageID:      messageID,
			OutboundBindingID:      delivery.BindingID,
			OutboundRouteRevision:  delivery.RouteRevision,
			OutboundTaskID:         taskID,
			OutboundKind:           "task_reply",
		}); err != nil {
			return fmt.Errorf("record discord outbound message: %w", err)
		}
	}
	return nil
}

// outboundChatID recovers the real send target from the chat binding. The
// channel_chat_id is the Discord channel id here, but the binding config is the
// documented routing slot, so prefer it when present.
func outboundChatID(delivery db.ChannelTaskDelivery) string {
	if len(delivery.Config) > 0 {
		var cfg discordBindingConfig
		if err := json.Unmarshal(delivery.Config, &cfg); err == nil && cfg.ChannelID != "" {
			return cfg.ChannelID
		}
	}
	return delivery.ChannelChatID
}

// chatDoneTaskID extracts the task id from the event envelope or the typed/map
// payload emitted by TaskService.
func chatDoneTaskID(e events.Event) (pgtype.UUID, bool) {
	raw := e.TaskID
	if raw == "" {
		switch p := e.Payload.(type) {
		case protocol.ChatDonePayload:
			raw = p.TaskID
		case map[string]any:
			raw, _ = p["task_id"].(string)
		}
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil && id.Valid
}

// chatDoneContent extracts the reply text from an EventChatDone payload.
func chatDoneContent(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}
