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
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the DingTalk outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to DingTalk — the outbound
// half of the round trip. It mirrors slack.Outbound: on EventChatDone it
// finds the DingTalk chat binding for the finished task's session and posts
// the reply via the robot message API (group: openConversationId; DM: the
// staff id captured on the binding config at session creation). Sessions
// with no DingTalk binding are ignored, so it coexists with the Feishu
// Patcher and the Slack Outbound on the shared event bus.
type Outbound struct {
	q         outboundQueries
	decrypt   Decrypter
	messenger *RobotMessenger
	typing    *TypingIndicatorManager
	logger    *slog.Logger
}

// NewOutbound builds the DingTalk outbound subscriber. typing is the
// "processing" emotion manager to clear before the reply lands; nil
// disables the clear.
func NewOutbound(q outboundQueries, decrypt Decrypter, messenger *RobotMessenger, typing *TypingIndicatorManager, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{q: q, decrypt: decrypt, messenger: messenger, typing: typing, logger: logger}
}

// Register subscribes to the chat-done and task-failed events on the bus:
// chat-done delivers the reply, task-failed only clears the "processing"
// emotion (there is no failure reply on this channel).
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck DingTalk HTTP call must not
	// wedge the publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "dingtalk outbound: reply delivery failed",
			"error", err, "chat_session_id", e.ChatSessionID)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	sessionID := sessionIDFromEvent(e)
	if !sessionID.Valid {
		// Issue / autopilot tasks carry no chat_session.
		return nil
	}
	// Clear the "processing" emotion before the reply is visible so the
	// user sees a clean transition. Best-effort and a no-op for sessions
	// with no tracked emotion (other channels, task-failed after settle).
	if o.typing != nil {
		o.typing.Clear(ctx, sessionID)
	}
	if e.Type != protocol.EventChatDone {
		return nil // task-failed: the clear above is the whole job
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeDingtalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a DingTalk session (Feishu / Slack / web-only)
		}
		return fmt.Errorf("lookup dingtalk chat binding: %w", err)
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		return nil // nothing to say (empty completion)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeDingtalk),
	})
	if err != nil {
		return fmt.Errorf("load dingtalk installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeChannelCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode dingtalk credentials: %w", err)
	}
	if err := o.messenger.SendMarkdown(ctx, creds, outboundTarget(binding), content); err != nil {
		return fmt.Errorf("post dingtalk reply: %w", err)
	}
	return nil
}

// outboundTarget recovers the robot-API send target from the chat binding:
// a DM addresses the recipient by the staff id captured on the binding
// config; a group addresses the conversation id (the binding key).
func outboundTarget(b db.ChannelChatSessionBinding) RobotTarget {
	if b.ChatType == string(channel.ChatTypeP2P) {
		var cfg dingtalkBindingConfig
		if len(b.Config) > 0 {
			if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.SenderStaffID != "" {
				return RobotTarget{UserStaffID: cfg.SenderStaffID}
			}
		}
	}
	return RobotTarget{OpenConversationID: b.ChannelChatID}
}

// sessionIDFromEvent recovers the chat session id from a bus event. The
// top-level scope hint is only stamped on chat:done; task:failed goes
// through broadcastTaskEvent, which carries chat_session_id solely inside
// the payload map — so fall back to the payload like lark's
// taskAndSessionFromEvent does.
func sessionIDFromEvent(e events.Event) pgtype.UUID {
	if id, err := util.ParseUUID(e.ChatSessionID); err == nil && id.Valid {
		return id
	}
	switch p := e.Payload.(type) {
	case map[string]any:
		if s, _ := p["chat_session_id"].(string); s != "" {
			if id, err := util.ParseUUID(s); err == nil {
				return id
			}
		}
	case protocol.ChatDonePayload:
		if id, err := util.ParseUUID(p.ChatSessionID); err == nil {
			return id
		}
	}
	return pgtype.UUID{}
}

// chatDoneContent extracts the reply text from an EventChatDone payload
// (the typed payload, or its map form after a serialization round trip).
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
