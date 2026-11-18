package infoflow

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the InfoFlow outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error)
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelTaskDelivery(ctx context.Context, taskID pgtype.UUID) (db.ChannelTaskDelivery, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error)
}

// Outbound delivers an agent's chat reply back to InfoFlow — the outbound half
// of the round trip. On EventChatDone / EventTaskFailed / EventTaskCancelled it
// finds the InfoFlow chat binding for the task's session and posts the reply (or
// failure notice) into the originating conversation. Sessions with no InfoFlow
// binding are ignored, so it coexists with the Feishu, Slack and DingTalk
// subscribers on the shared event bus. Registered only when InfoFlow is
// configured.
type Outbound struct {
	q      outboundQueries
	box    *secretbox.Box
	logger *slog.Logger

	// objects is nil unless WithAttachments was passed, and nil means this
	// deployment cannot deliver files at all. router.go pairs it with
	// Handler.DeclareChannelFileDelivery so the capability the agent is promised
	// and the hop the process can perform are decided by one condition.
	objects mediaObjectStore

	// attachmentWG tracks in-flight attachment deliveries.
	attachmentWG sync.WaitGroup

	// senderFor builds a mediaSender from decrypted credentials. A field rather
	// than a direct call so a test can watch the wire without a network.
	senderFor func(credentials) (mediaSender, error)
}

// NewOutbound builds the InfoFlow outbound subscriber.
func NewOutbound(q outboundQueries, box *secretbox.Box, logger *slog.Logger, opts ...OutboundOption) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{q: q, box: box, logger: logger, senderFor: newSenderFor}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// Register subscribes to every chat-task terminal event.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
	bus.Subscribe(protocol.EventTaskCancelled, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck InfoFlow HTTP call must not wedge
	// the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "infoflow outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	taskID, sessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	// Nothing to say. A task that failed with a retry still queued carries no
	// user-visible text, and the engine publishes again when it settles.
	content := eventContent(e)
	// The turn's files are addressed by the assistant message, so a completed
	// turn is always worth checking even when the agent said nothing and only
	// bound an attachment.
	var messageID pgtype.UUID
	if o.mayCarryAttachments(e) {
		if raw := chatDoneMessageID(e.Payload); raw != "" {
			_ = messageID.Scan(raw)
		}
	}
	if content == "" && !messageID.Valid {
		return nil
	}
	delivery, err := o.q.GetChannelTaskDelivery(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup infoflow task delivery: %w", err)
	}
	if delivery.ChannelType != string(TypeInfoFlow) {
		return nil
	}
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
		ID: delivery.InstallationID, ChannelType: string(TypeInfoFlow),
	})
	if err != nil {
		return fmt.Errorf("load infoflow installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	if o.box == nil {
		return fmt.Errorf("infoflow: encryption box is required")
	}
	creds, err := decodeCredentials(inst.Config, o.box.Open)
	if err != nil {
		return fmt.Errorf("decode infoflow credentials: %w", err)
	}
	target := targetFromChatID(delivery.ChannelChatID)
	if content != "" {
		// A markdown answer goes out as a rich card, with the Markdown message
		// as the fallback the card path degrades to. sendReply is the only place
		// that decides, so the reply and the OutboundReplier render alike.
		replies, err := newReplySender(creds, slog.Default())
		if err != nil {
			return err
		}
		if _, err := replies.send(ctx, target, content); err != nil {
			return fmt.Errorf("post infoflow reply: %w", err)
		}
	}
	// Files go after the words, off this goroutine: the reply is already on its
	// way, and an upload must not be able to delay or lose it. This runs on the
	// publish call site's goroutine otherwise, which is exactly the thing
	// Bus.Publish must never block on.
	if messageID.Valid {
		o.spawnAttachmentDelivery(creds, target, inst.WorkspaceID, messageID, inst.ID)
	}
	return nil
}

// eventContent extracts the deliverable text from an EventChatDone payload
// (typed, or its map form after a serialization round trip) or an
// EventTaskFailed payload. Empty means stay silent.
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
