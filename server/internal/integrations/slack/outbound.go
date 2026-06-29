package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the Slack outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// replySender posts one reply. Satisfied by *slackChannel, so the outbound path
// reuses Send's Markdown->mrkdwn conversion, chunking, and threading.
type replySender interface {
	Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error)
}

// Outbound delivers an agent's chat reply back to Slack — the outbound half of
// the round trip. It mirrors the Feishu Patcher: on EventChatDone it finds the
// Slack chat binding for the finished task's session and posts the reply into
// the originating channel/thread. Sessions with no Slack binding are ignored,
// so it coexists with the Feishu Patcher on the shared event bus. It is only
// registered when Slack is configured.
type Outbound struct {
	q         outboundQueries
	decrypt   Decrypter
	logger    *slog.Logger
	newSender func(creds credentials) replySender
}

// NewOutbound builds the Slack outbound subscriber over the generated queries
// and the bot/app-token decrypter.
func NewOutbound(q outboundQueries, decrypt Decrypter, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Outbound{q: q, decrypt: decrypt, logger: logger}
	o.newSender = func(c credentials) replySender {
		// Only the bot token is needed to post; the app token is a Socket Mode
		// (inbound) credential.
		return newSlackChannel(c, slack.New(c.BotToken), nil, logger)
	}
	return o
}

// Register subscribes to the chat-done event on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
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
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a Slack session (Feishu / web-only)
		}
		return fmt.Errorf("lookup slack chat binding: %w", err)
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		return nil // nothing to say (empty completion)
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
	if _, err := o.newSender(creds).Send(ctx, channel.OutboundMessage{
		ChatID:   channelID,
		Text:     content,
		ThreadID: threadTS,
	}); err != nil {
		return fmt.Errorf("post slack reply: %w", err)
	}
	return nil
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
