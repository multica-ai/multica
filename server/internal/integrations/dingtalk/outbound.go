package dingtalk

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

// outboundQueries is the slice of generated queries the DingTalk outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to DingTalk — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed
// it finds the DingTalk chat binding for the task's session and posts the reply
// (or failure notice) into the originating conversation. Sessions with no
// DingTalk binding are ignored, so it coexists with the Feishu and Slack
// subscribers on the shared event bus. Registered only when DingTalk is
// configured.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	client  *Client
	ack     *ackNotifier
	logger  *slog.Logger
}

// NewOutbound builds the DingTalk outbound subscriber over the generated
// queries, the AppSecret decrypter, the shared token-caching Client, and the
// optional processing-reaction lifecycle owner.
func NewOutbound(q outboundQueries, decrypt Decrypter, client *Client, ack *ackNotifier, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	if client == nil {
		client = NewClient(nil, "")
	}
	return &Outbound{q: q, decrypt: decrypt, client: client, ack: ack, logger: logger}
}

// Register subscribes to every chat-task terminal event. Task-cancelled carries
// no reply content, but still has to clear the source-message reaction.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
	bus.Subscribe(protocol.EventTaskCancelled, o.handleEvent)
	bus.Subscribe(protocol.EventAgentArchived, o.handleAgentArchived)
}

func (o *Outbound) handleAgentArchived(e events.Event) {
	if o.ack == nil {
		return
	}
	raw, err := json.Marshal(e.Payload)
	if err != nil {
		return
	}
	var payload struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return
	}
	id, err := util.ParseUUID(payload.Agent.ID)
	if err != nil || !id.Valid {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), ackCleanupTimeout)
	defer cancel()
	o.ack.onAgentArchived(ctx, id)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck DingTalk HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "dingtalk outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, sessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	content := eventContent(e)
	if content == "" && eventRetryPending(e) {
		return nil
	}
	var deliveredInput pgtype.UUID
	var deliveredInstallation engine.ResolvedInstallation
	if o.ack != nil {
		// Reactions are optional: do not spend the reply's network budget on
		// cleanup. Every terminal return attempts to settle its own input batch.
		defer func() {
			o.settleTaskReactions(ctx, taskID, sessionID)
			if deliveredInput.Valid {
				o.ack.OnReplyDelivered(ctx, deliveredInstallation, deliveredInput)
			}
		}()
	}
	if content == "" {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup dingtalk task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeDingTalk) {
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

	var input db.ChatMessage
	if task.ChatInputTaskID.Valid {
		messages, err := o.q.ListChatInputMessages(ctx, task.ChatInputTaskID)
		if err != nil {
			o.logger.WarnContext(ctx, "dingtalk: sealed input unavailable; sending without attribution", "error", err)
			messages = nil
		}
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].ChannelIngested {
				input = messages[i]
				break
			}
		}
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID: binding.InstallationID, ChannelType: string(TypeDingTalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	s := &sender{client: o.client, robotCode: creds.RobotCode, appKey: creds.AppKey, appSecret: creds.AppSecret}
	target := outboundTarget(binding)
	target.QuoteText = sealedInputQuote(input.Content)
	if _, err := s.send(ctx, target, content); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	// A failure notice is terminal, but it is not a successfully completed task.
	// Only chat:done earns the positive Done reaction.
	if o.ack != nil && e.Type == protocol.EventChatDone {
		deliveredInput = input.ID
		deliveredInstallation = engine.ResolvedInstallation{
			ID: inst.ID, WorkspaceID: inst.WorkspaceID, AgentID: inst.AgentID,
			InstallerUserID: inst.InstallerUserID, Active: true, Platform: inst,
		}
	}
	return nil
}

// Task completion owns a sealed input batch, not every pending message in the
// conversation. This also runs for cancellation and empty terminal replies.
func (o *Outbound) settleTaskReactions(ctx context.Context, taskID, sessionID pgtype.UUID) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackCleanupTimeout)
	defer cancel()
	task, err := o.q.GetAgentTask(cleanup, taskID)
	if err != nil {
		o.logger.WarnContext(cleanup, "dingtalk reaction: terminal task unavailable", "error", err)
		return
	}
	if !task.ChatInputTaskID.Valid || (task.ChatSessionID.Valid && task.ChatSessionID != sessionID) {
		return
	}
	inputs, err := o.q.ListChatInputMessages(cleanup, task.ChatInputTaskID)
	if err != nil {
		o.logger.WarnContext(cleanup, "dingtalk reaction: terminal input unavailable", "error", err)
		return
	}
	o.ack.onInputsSettled(cleanup, sessionID, inputs)
}

func eventRetryPending(e events.Event) bool {
	if e.Type != protocol.EventTaskFailed {
		return false
	}
	p, ok := e.Payload.(map[string]any)
	if !ok {
		return false
	}
	retryPending, _ := p["retry_pending"].(bool)
	return retryPending
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

// eventContent extracts the deliverable text from an EventChatDone payload
// (typed, or its map form after a serialization round trip) or an
// EventTaskFailed payload. Empty means stay silent.
//
// For task-failed the text mirrors the web transcript's failure chat_message:
// the broadcast's `error` field carries the same redacted failure text and is
// omitted while an auto-retry is pending (the retry attempt reports its own
// outcome), so error-present means deliverable.
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
