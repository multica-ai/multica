package wechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// outboundQueries is the slice of generated queries the WeChat outbound
// subscriber needs. *db.Queries satisfies it.
type outboundQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

// Outbound delivers an agent's chat reply back to WeChat — the outbound half of
// the round trip. On EventChatDone it finds the WeChat chat binding for the
// finished task's session, recovers the context_token + peer from the binding's
// config (the core iLink quirk), and posts the reply via iLink sendmessage.
// Sessions with no WeChat binding are ignored, so it coexists with the Feishu
// Patcher and Slack Outbound on the shared event bus. It is only registered when
// WeChat is configured.
type Outbound struct {
	q       outboundQueries
	decrypt Decrypter
	client  *iLinkClient
	logger  *slog.Logger
}

// NewOutbound builds the WeChat outbound subscriber over the generated queries
// and the bot-token decrypter. baseURL seeds the iLink client (per-account
// base_url from the installation config overrides it for the actual call).
func NewOutbound(q outboundQueries, decrypt Decrypter, baseURL string, logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{
		q:       q,
		decrypt: decrypt,
		client:  newILinkClient(baseURL, logger),
		logger:  logger,
	}
}

// Register subscribes to the chat-done event on the bus.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	// Bus delivery is synchronous, so a stuck iLink HTTP call must not wedge the
	// publish call site: use a fresh ctx with a tight timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "wechat outbound: reply delivery failed",
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
		ChannelType:   string(TypeWechat),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // not a WeChat session (Feishu / Slack / web-only)
		}
		return fmt.Errorf("lookup wechat chat binding: %w", err)
	}
	content := chatDoneContent(e.Payload)
	if content == "" {
		return nil // nothing to say (empty completion)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeWechat),
	})
	if err != nil {
		return fmt.Errorf("load wechat installation: %w", err)
	}
	if inst.Status != "active" {
		return nil // revoked between trigger and reply
	}
	creds, err := decodeCredentials(inst.Config, o.decrypt)
	if err != nil {
		return fmt.Errorf("decode wechat credentials: %w", err)
	}
	cfg := extractBindingConfig(binding.Config)
	if cfg.ContextToken == "" {
		// The context_token is the iLink reply-association key. Without it the
		// reply cannot be delivered; surface a resend prompt so the user is not
		// left waiting. This is the documented fallback for a stale/empty token.
		o.logger.WarnContext(ctx, "wechat outbound: binding has no context_token; cannot reply",
			"chat_session_id", sessionID)
		return nil
	}
	peer := cfg.PeerUserID
	if peer == "" {
		return errors.New("wechat outbound: binding has no peer user id")
	}
	if _, err := o.client.sendMessage(ctx, creds.BotToken, creds.BaseURL, cfg.ContextToken, peer, content); err != nil {
		return fmt.Errorf("post wechat reply: %w", err)
	}
	return nil
}

// chatDoneContent extracts the reply text from an EventChatDone payload (the
// typed payload, or its map form after a serialization round trip). Mirrors the
// Slack helper.
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
