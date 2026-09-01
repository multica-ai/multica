package mattermost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// This file delivers the agent's chat reply back to Mattermost — the outbound
// half of the round trip. It is modeled on slack/outbound.go: on EventChatDone
// it finds the Mattermost chat binding for the finished task's session and
// posts the reply into the originating channel or thread. Sessions with no
// Mattermost binding are ignored, so it coexists with every other adapter's
// subscriber on the shared event bus. It is only registered when Mattermost is
// configured.
//
// Deliberately NOT streaming. Mattermost supports PUT /posts/{id}, so a
// throttled edit loop like Telegram's is possible, but Telegram needs ~1100
// lines of rate-limit scheduling for it and a self-hosted server imposes no
// comparable limits. One post on completion is the v1 contract.

// outboundQueries is the slice of generated queries the subscriber needs.
// *db.Queries satisfies it.
type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	SetChatMessageChannelOutboundProvenanceByTask(ctx context.Context, arg db.SetChatMessageChannelOutboundProvenanceByTaskParams) (int64, error)
	RecordChannelOutboundMessage(ctx context.Context, arg db.RecordChannelOutboundMessageParams) error
}

// replySender posts one reply. Satisfied by *sender, and injectable so the
// delivery path is testable without a live Mattermost server.
type replySender interface {
	Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error)
}

// Outbound delivers an agent's chat reply back to Mattermost.
type Outbound struct {
	q         outboundQueries
	decrypt   Decrypter
	logger    *slog.Logger
	newSender func(creds credentials) replySender
}

// outboundTimeout bounds one delivery. Bus delivery is synchronous, so a stuck
// HTTP call must not wedge the publish call site.
const outboundTimeout = 15 * time.Second

// NewOutbound builds the Mattermost outbound subscriber over the generated
// queries and the token decrypter.
func NewOutbound(q outboundQueries, decrypt Decrypter, client *http.Client, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{q: q, decrypt: decrypt, logger: logger}
	o.newSender = func(c credentials) replySender {
		return newSender(newRESTClient(c.ServerURL, c.AccessToken, client), logger)
	}
	return o
}

// Register subscribes to the chat-done event on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), outboundTimeout)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "mattermost outbound: reply delivery failed",
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
	// Only bound, non-empty completions reach here, so classify the task origin
	// before loading credentials or sending. Web/mobile direct-chat tasks can
	// reuse a session that originated in Mattermost, but their replies belong
	// only in Multica. Delivery fails closed when the origin cannot be
	// established.
	taskID, ok := eventTaskID(e)
	if !ok {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // direct Chat or another channel
		}
		return fmt.Errorf("lookup mattermost task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeMattermost) {
		return nil
	}
	binding := bindingFromTaskDelivery(delivery)
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load agent task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("classify task input origin: %w", err)
	}
	if !deliver {
		return nil
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeMattermost),
	})
	if err != nil {
		return fmt.Errorf("load mattermost installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode mattermost credentials: %w", err)
	}
	channelID, threadRoot := outboundTarget(binding)
	result, err := o.newSender(creds).Send(ctx, channel.OutboundMessage{
		ChatID:   channelID,
		Text:     content,
		ThreadID: threadRoot,
	})
	// A partial send still delivered posts the user can see, so record
	// provenance for those before reporting the failure.
	if recErr := o.recordProvenance(ctx, delivery, binding, taskID, channelID, result); recErr != nil && err == nil {
		return recErr
	}
	if err != nil {
		return fmt.Errorf("post mattermost reply: %w", err)
	}
	return nil
}

// recordProvenance links the delivered posts back to the assistant message and
// the outbound ledger, so a later read knows which Mattermost posts carried
// which Multica turn.
func (o *Outbound) recordProvenance(
	ctx context.Context,
	delivery db.ChannelTaskDelivery,
	binding db.ChannelChatSessionBinding,
	taskID pgtype.UUID,
	channelID string,
	result channel.SendResult,
) error {
	messageIDs := result.MessageIDs
	if len(messageIDs) == 0 && result.MessageID != "" {
		messageIDs = []string{result.MessageID}
	}
	if len(messageIDs) == 0 {
		return errors.New("post mattermost reply: provider returned no post id")
	}
	rows, err := o.q.SetChatMessageChannelOutboundProvenanceByTask(ctx, db.SetChatMessageChannelOutboundProvenanceByTaskParams{
		ChannelType:    pgtype.Text{String: string(TypeMattermost), Valid: true},
		InstallationID: binding.InstallationID,
		ChannelChatID:  pgtype.Text{String: channelID, Valid: true},
		MessageIds:     messageIDs,
		TaskID:         taskID,
	})
	if err != nil {
		return fmt.Errorf("record mattermost reply provenance: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("record mattermost reply provenance: updated %d assistant rows, want 1", rows)
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
			return fmt.Errorf("record mattermost outbound message: %w", err)
		}
	}
	return nil
}

func bindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType:      delivery.ChatType,
		LastMessageID: delivery.ChannelMessageID, LastThreadID: delivery.ChannelThreadID,
		RouteRevision: delivery.RouteRevision, Config: delivery.Config,
	}
}

// eventTaskID extracts the task id from the event envelope or the typed/map
// payload emitted by TaskService. Delivery fails closed when the task origin
// cannot be established.
func eventTaskID(e events.Event) (pgtype.UUID, bool) {
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

// outboundTarget recovers the real send target from the chat binding. The
// channel_chat_id may be a composite "channel:threadRoot" isolation key, so the
// real channel id is read from the binding config; the reply thread is the
// recorded last_thread_id.
func outboundTarget(b db.ChannelChatSessionBinding) (channelID, threadRoot string) {
	channelID = b.ChannelChatID
	if len(b.Config) > 0 {
		var cfg mattermostBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.ChannelID != "" {
			channelID = cfg.ChannelID
		}
	}
	if b.LastThreadID.Valid {
		threadRoot = b.LastThreadID.String
	}
	return channelID, threadRoot
}

// chatDoneContent extracts the reply text from an EventChatDone payload (the
// typed payload, or its map form after a serialization round trip).
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
