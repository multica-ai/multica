package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the Slack outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	SetChatMessageChannelOutboundProvenanceByTask(ctx context.Context, arg db.SetChatMessageChannelOutboundProvenanceByTaskParams) (int64, error)
	RecordChannelOutboundMessage(ctx context.Context, arg db.RecordChannelOutboundMessageParams) error
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// replySender posts one reply. Satisfied by *slackSender, so the outbound path
// reuses Send's Markdown->mrkdwn conversion, chunking, and threading.
type replySender interface {
	SendWithMetadata(ctx context.Context, out channel.OutboundMessage, metadata slack.SlackMetadata) (channel.SendResult, error)
}

// outboundSender adds the failure card's Block Kit post to replySender.
type outboundSender interface {
	replySender
	SendBlocks(ctx context.Context, out channel.OutboundMessage, blocks []slack.Block, metadata slack.SlackMetadata) (channel.SendResult, error)
}

// Outbound delivers an agent's chat reply back to Slack — the outbound half of
// the round trip. It mirrors the Feishu Patcher: on EventChatDone it finds the
// Slack chat binding for the finished task's session and posts the reply into
// the originating channel/thread. Sessions with no Slack binding are ignored,
// so it coexists with the Feishu Patcher on the shared event bus. It is only
// registered when Slack is configured. A terminal task failure posts a failure
// card with Retry and Open-in-Multica buttons (see interaction.go).
type Outbound struct {
	q         outboundQueries
	decrypt   Decrypter
	appURL    string
	logger    *slog.Logger
	newSender func(creds credentials) outboundSender
}

// NewOutbound builds the Slack outbound subscriber over the generated queries
// and the bot/app-token decrypter. appURL is the web app origin for the failure
// card's Open-in-Multica button; empty omits the button.
func NewOutbound(q outboundQueries, decrypt Decrypter, appURL string, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{q: q, decrypt: decrypt, appURL: strings.TrimRight(appURL, "/"), logger: logger}
	o.newSender = func(c credentials) outboundSender {
		// Only the bot token is needed to post; inbound Socket Mode uses the
		// installation's separate app-level token (see slack_channel.go).
		return newSlackSender(c, slack.New(c.BotToken), logger)
	}
	return o
}

// Register subscribes to the chat-done and task-failed events on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck Slack HTTP call must not wedge the
	// publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "slack outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	sessionID, err := util.ParseUUID(e.ChatSessionID)
	if err != nil || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	failure := e.Type == protocol.EventTaskFailed
	var content string
	if failure {
		content = taskFailureText(e.Payload)
	} else {
		content = chatDoneContent(e.Payload)
	}
	if content == "" {
		return nil // nothing to say (empty completion)
	}
	// Only bound, non-empty completions reach here, so classify the task origin
	// before loading credentials or sending. Web/mobile direct-chat tasks can
	// reuse a session that originated in Slack, but their replies belong only in
	// Multica. Outbound delivery fails closed when the origin cannot be
	// established. Sealed channel tasks own an input batch just like direct
	// tasks, so the discriminator is the immutable channel_ingested provenance
	// of that batch, not chat_input_task_id presence (which #5645 originally
	// used).
	taskID, ok := chatDoneTaskID(e)
	if !ok {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // direct Chat or another channel
		}
		return fmt.Errorf("lookup slack task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeSlack) {
		return nil
	}
	binding := slackBindingFromTaskDelivery(delivery)
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
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		return fmt.Errorf("load slack installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode slack credentials: %w", err)
	}
	channelID, threadTS := outboundTarget(binding)
	if failure {
		return o.postFailureCard(ctx, creds, inst.WorkspaceID, sessionID, delivery, taskID, channelID, threadTS, content)
	}
	result, err := o.newSender(creds).SendWithMetadata(ctx, channel.OutboundMessage{
		ChatID:   channelID,
		Text:     content,
		ThreadID: threadTS,
	}, outboundMetadata(delivery.BindingID, delivery.RouteRevision, "task_reply"))
	if err != nil {
		return fmt.Errorf("post slack reply: %w", err)
	}
	messageIDs := result.MessageIDs
	if len(messageIDs) == 0 && result.MessageID != "" {
		messageIDs = []string{result.MessageID}
	}
	if len(messageIDs) == 0 {
		return errors.New("post slack reply: provider returned no message id")
	}
	rows, err := o.q.SetChatMessageChannelOutboundProvenanceByTask(ctx, db.SetChatMessageChannelOutboundProvenanceByTaskParams{
		ChannelType:    pgtype.Text{String: string(TypeSlack), Valid: true},
		InstallationID: binding.InstallationID,
		ChannelChatID:  pgtype.Text{String: channelID, Valid: true},
		MessageIds:     messageIDs,
		TaskID:         taskID,
	})
	if err != nil {
		return fmt.Errorf("record slack reply provenance: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("record slack reply provenance: updated %d assistant rows, want 1", rows)
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
			return fmt.Errorf("record slack outbound message: %w", err)
		}
	}
	return nil
}

func slackBindingFromTaskDelivery(delivery db.ChannelTaskDelivery) db.ChannelChatSessionBinding {
	return db.ChannelChatSessionBinding{
		ID: delivery.BindingID, InstallationID: delivery.InstallationID,
		ChannelType: delivery.ChannelType, ChannelChatID: delivery.ChannelChatID,
		ChatType:      delivery.ChatType,
		LastMessageID: delivery.ChannelMessageID, LastThreadID: delivery.ChannelThreadID,
		RouteRevision: delivery.RouteRevision, Config: delivery.Config,
	}
}

// chatDoneTaskID extracts the task id from the event envelope or the typed/map
// payload emitted by TaskService. Outbound delivery fails closed when the task
// origin cannot be established.
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

// outboundTarget recovers the real send target from the chat binding. The
// channel_chat_id may be a composite "channel:threadRoot" isolation key, so the
// real channel id is read from the binding config (slackBindingConfig); the
// reply thread is the recorded last_thread_id.
func outboundTarget(b db.ChannelChatSessionBinding) (channelID, threadTS string) {
	channelID = b.ChannelChatID
	if len(b.Config) > 0 {
		var cfg slackBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.ChannelID != "" {
			channelID = cfg.ChannelID
		}
	}
	if b.LastThreadID.Valid {
		threadTS = b.LastThreadID.String
	}
	return channelID, threadTS
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

// taskFailureText returns the redacted error of a terminal failure. The error
// is omitted while an automatic retry is pending, so that attempt stays silent.
func taskFailureText(payload any) string {
	p, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	if pending, _ := p["retry_pending"].(bool); pending {
		return ""
	}
	s, _ := p["error"].(string)
	return strings.TrimSpace(s)
}

func (o *Outbound) postFailureCard(ctx context.Context, creds credentials, workspaceID, sessionID pgtype.UUID, delivery db.ChannelTaskDelivery, taskID pgtype.UUID, channelID, threadTS, errText string) error {
	chatURL := ""
	if o.appURL != "" {
		if ws, err := o.q.GetWorkspace(ctx, workspaceID); err == nil {
			chatURL = o.appURL + "/" + ws.Slug + "/chat?session=" + util.UUIDToString(sessionID)
		}
	}
	errText = truncateRunes(errText, maxFailureErrorRunes)
	result, err := o.newSender(creds).SendBlocks(ctx, channel.OutboundMessage{
		ChatID:   channelID,
		Text:     failureCardFallback + " " + errText,
		ThreadID: threadTS,
	}, failureCardBlocks(taskID, errText, chatURL), outboundMetadata(delivery.BindingID, delivery.RouteRevision, "task_failure"))
	if err != nil {
		return fmt.Errorf("post slack failure card: %w", err)
	}
	if result.MessageID == "" {
		return errors.New("post slack failure card: provider returned no message id")
	}
	if err := o.q.RecordChannelOutboundMessage(ctx, db.RecordChannelOutboundMessageParams{
		OutboundInstallationID: delivery.InstallationID,
		OutboundChannelType:    delivery.ChannelType,
		OutboundMessageID:      result.MessageID,
		OutboundBindingID:      delivery.BindingID,
		OutboundRouteRevision:  delivery.RouteRevision,
		OutboundTaskID:         taskID,
		OutboundKind:           "task_failure",
	}); err != nil {
		return fmt.Errorf("record slack failure card: %w", err)
	}
	return nil
}
