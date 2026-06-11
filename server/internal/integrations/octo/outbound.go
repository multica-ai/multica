package octo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const outboundEventTimeout = 10 * time.Second

// PatcherQueries is the subset of generated queries the outbound patcher needs.
type PatcherQueries interface {
	GetOctoChatSessionBindingBySession(ctx context.Context, chatSessionID pgtype.UUID) (db.OctoChatSessionBinding, error)
	GetOctoInstallation(ctx context.Context, id pgtype.UUID) (db.OctoInstallation, error)
	CreateOctoOutboundMessage(ctx context.Context, arg db.CreateOctoOutboundMessageParams) (db.OctoOutboundMessage, error)
}

// TokenDecryptor decrypts an installation's stored bot token ciphertext. An
// interface so the patcher can be unit-tested without secretbox.
type TokenDecryptor interface {
	DecryptBotToken(inst db.OctoInstallation) (string, error)
}

// MessageSender sends an outbound message to Octo for a given installation.
// Production uses octoMessageSender (a thin wrapper over transport.HTTPClient); tests
// provide a fake. Returns the server-assigned message id/seq.
type MessageSender interface {
	Send(ctx context.Context, apiURL, botToken, channelID string, channelType transport.ChannelType, content string) (*transport.SendMessageResult, error)
}

// octoMessageSender is the production MessageSender. It constructs a per-call
// transport.HTTPClient because each installation has its own api_url + token.
type octoMessageSender struct{}

// NewMessageSender returns the production MessageSender.
func NewMessageSender() MessageSender { return octoMessageSender{} }

func (octoMessageSender) Send(ctx context.Context, apiURL, botToken, channelID string, channelType transport.ChannelType, content string) (*transport.SendMessageResult, error) {
	client := transport.NewHTTPClient(apiURL, botToken)
	return client.SendMessage(ctx, transport.SendMessageParams{
		ChannelID:   channelID,
		ChannelType: channelType,
		Content:     content,
	})
}

// Patcher subscribes to chat task events and relays agent output back to Octo.
// On chat:done it sends the agent's reply; on task:failed it sends a short error
// notice. Octo renders markdown natively, so replies go out as plain text/markdown
// (no interactive-card rendering like Lark).
type Patcher struct {
	queries   PatcherQueries
	decryptor TokenDecryptor
	sender    MessageSender
	logger    *slog.Logger
}

// NewPatcher constructs the outbound Patcher.
func NewPatcher(queries PatcherQueries, decryptor TokenDecryptor, sender MessageSender, logger *slog.Logger) *Patcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Patcher{queries: queries, decryptor: decryptor, sender: sender, logger: logger}
}

// Register subscribes the patcher to the event bus.
func (p *Patcher) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, p.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, p.handleEvent)
}

// handleEvent runs each event on its own short-lived context. Outbound delivery
// is best-effort: a failure is logged, never propagated (the chat task is
// already durable; the user simply doesn't see this particular reply).
func (p *Patcher) handleEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), outboundEventTimeout)
	defer cancel()
	if err := p.processEvent(ctx, e); err != nil {
		p.logger.Error("octo outbound: process event failed", "type", e.Type, "err", err.Error())
	}
}

func (p *Patcher) processEvent(ctx context.Context, e events.Event) error {
	taskID, chatSessionID, ok := taskAndSessionFromEvent(e)
	if !ok || !chatSessionID.Valid {
		// No task, or an issue/autopilot task with no chat session — not ours.
		return nil
	}

	binding, err := p.queries.GetOctoChatSessionBindingBySession(ctx, chatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Web-only or Lark chat session — not an Octo target.
			return nil
		}
		return fmt.Errorf("lookup chat session binding: %w", err)
	}

	inst, err := p.queries.GetOctoInstallation(ctx, binding.InstallationID)
	if err != nil {
		return fmt.Errorf("load installation: %w", err)
	}
	if InstallationStatus(inst.Status) != InstallationActive {
		// Revoked between trigger and event; nothing to send.
		return nil
	}

	token, err := p.decryptor.DecryptBotToken(inst)
	if err != nil {
		return fmt.Errorf("decrypt bot token: %w", err)
	}

	switch e.Type {
	case protocol.EventChatDone:
		return p.sendReply(ctx, inst, binding, taskID, chatDoneContent(e.Payload), token)
	case protocol.EventTaskFailed:
		msg := errorMessageFromPayload(e.Payload)
		if msg == "" {
			msg = "The agent run failed."
		}
		return p.sendReply(ctx, inst, binding, taskID, "⚠️ "+msg, token)
	}
	return nil
}

// sendReply sends content to the bound Octo channel and records the sent message
// (keyed by task) so a later streaming edit can target it. Empty content is
// dropped — better to show nothing than a bare "Done.".
func (p *Patcher) sendReply(ctx context.Context, inst db.OctoInstallation, binding db.OctoChatSessionBinding, taskID pgtype.UUID, content, token string) error {
	if content == "" {
		return nil
	}
	res, err := p.sender.Send(ctx, inst.ApiUrl, token, binding.OctoChannelID, transport.ChannelType(binding.OctoChannelType), content)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	// Record the sent message so future streaming edits can find it. Best-effort:
	// a failure here only loses the edit anchor, not the delivered message.
	var seq int64
	if res != nil {
		seq = int64(res.MessageSeq)
	}
	msgID := ""
	if res != nil {
		msgID = res.MessageID
	}
	if _, err := p.queries.CreateOctoOutboundMessage(ctx, db.CreateOctoOutboundMessageParams{
		ChatSessionID:  binding.ChatSessionID,
		TaskID:         taskID,
		OctoChannelID:  binding.OctoChannelID,
		OctoMessageID:  msgID,
		OctoMessageSeq: seq,
		Status:         "final",
	}); err != nil {
		p.logger.Warn("octo outbound: record sent message failed",
			"task_id", uuidString(taskID), "err", err.Error())
	}
	return nil
}

// taskAndSessionFromEvent extracts task_id + chat_session_id from the event,
// handling both the map payload (task events) and the ChatDonePayload struct.
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
	return taskID, chatSessionID, taskID.Valid
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
		if s, ok := m["error"].(string); ok && s != "" {
			return s
		}
		if s, ok := m["error_message"].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
