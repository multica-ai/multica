package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// This file is the Telegram ResolverSet: the platform-specific seams the
// channel-agnostic engine.Router runs the inbound pipeline through. Built
// entirely on the generic channel_* queries plus the shared engine.ChatSession
// — no new query, no schema change — mirroring slack/resolvers.go.

// originTelegramChat is the issue.origin_type label for issues created via the
// Telegram /issue command.
const originTelegramChat = "telegram_chat"

// NewTelegramResolverSet assembles the Telegram ResolverSet. Pass a nil
// replier to disable outbound verdict notices; typing (sendChatAction) is
// enabled when deps carry a decrypter.
func NewTelegramResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier, typing engine.TypingNotifier) engine.ResolverSet {
	return engine.ResolverSet{
		Installation: &installationResolver{q: q},
		Identity:     &identityResolver{q: q},
		Dedup:        &deduper{q: q},
		Session: &sessionBinder{session: engine.NewChatSession(q, tx, TypeTelegram, engine.SessionTitles{
			Group:    "Telegram group",
			Direct:   "Telegram direct message",
			Fallback: "Telegram chat",
		})},
		Audit:      &auditor{q: q},
		Replier:    replier,
		Typing:     typing,
		OriginType: originTelegramChat,
	}
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
	_ engine.TypingNotifier       = (*typingNotifier)(nil)
)

// telegramBindingConfig is the opaque outbound routing persisted on the chat
// binding's config: the real chat id survives even when the binding key is a
// composite (forum-topic thread isolation).
type telegramBindingConfig struct {
	ChatID string `json:"chat_id"`
}

// telegramSessionRouting derives the session-isolation key, the binding
// config, and the reply thread from one inbound message. A private chat is one
// continuous session (key = chat id). A group is one session per chat, except
// forum topics, which isolate per topic (key = "chat:thread") — the closest
// analog of Feishu thread isolation. Pure function, unit-tested without a DB.
func telegramSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte, replyThread string) {
	chatID := msg.Source.ChatID
	cfg, _ := json.Marshal(telegramBindingConfig{ChatID: chatID})
	if msg.Source.ChatType == channel.ChatTypeGroup && msg.Source.ThreadID != "" {
		return chatID + ":" + msg.Source.ThreadID, cfg, msg.Source.ThreadID
	}
	return chatID, cfg, msg.Source.ThreadID
}

func decodeTelegramRaw(msg channel.InboundMessage) (telegramRawEvent, error) {
	var raw telegramRawEvent
	if len(msg.Raw) == 0 {
		return telegramRawEvent{}, errors.New("telegram: inbound message Raw is empty")
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return telegramRawEvent{}, fmt.Errorf("decode telegram inbound raw: %w", err)
	}
	return raw, nil
}

func nullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// ---- installation routing ----

type installationResolver struct{ q *db.Queries }

func (r *installationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	raw, err := decodeTelegramRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	inst, err := r.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeTelegram),
		// Route by the bot id: each polling loop serves exactly one bot, and the
		// (channel_type, app_id) unique index maps it to its installation.
		AppID: raw.BotID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
		}
		return engine.ResolvedInstallation{}, err
	}
	return engine.ResolvedInstallation{
		ID:              inst.ID,
		WorkspaceID:     inst.WorkspaceID,
		AgentID:         inst.AgentID,
		InstallerUserID: inst.InstallerUserID,
		Active:          inst.Status == "active",
		Platform:        inst,
	}, nil
}

// ---- identity ----

type identityResolver struct{ q *db.Queries }

func (r *identityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	binding, err := r.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  msg.Source.SenderID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		return engine.ResolvedIdentity{}, err
	}
	// Binding existence no longer proves membership (no FK); re-check.
	if _, err := r.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.MulticaUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
		}
		return engine.ResolvedIdentity{}, err
	}
	return engine.ResolvedIdentity{UserID: binding.MulticaUserID}, nil
}

// ---- dedup ----

type deduper struct{ q *db.Queries }

func (r *deduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	claim, err := r.q.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrDuplicate
		}
		return pgtype.UUID{}, err
	}
	return claim.ClaimToken, nil
}

func (r *deduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.q.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

func (r *deduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.q.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

// ---- session bind / append ----

// chatSession is the shared engine session surface used by the Telegram
// adapter. Keeping the adapter behind this narrow seam makes its parameter
// mapping testable without a database.
type chatSession interface {
	EnsureSession(ctx context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error)
	MarkPendingFresh(ctx context.Context, sessionID pgtype.UUID, messageID string) error
	AppendUserMessage(ctx context.Context, in engine.AppendInput) (engine.AppendResult, error)
	BindMediaRefs(ctx context.Context, in engine.BindMediaInput) error
}

type sessionBinder struct{ session chatSession }

func (r *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	bindingKey, config, _ := telegramSessionRouting(p.Message)
	return r.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    p.Installation.WorkspaceID,
		AgentID:        p.Installation.AgentID,
		InstallationID: p.Installation.ID,
		Sender:         p.Sender,
		BindingKey:     bindingKey,
		BindingConfig:  config,
		ChatType:       p.Message.Source.ChatType,
	})
}

func (r *sessionBinder) MarkPendingFresh(ctx context.Context, sessionID pgtype.UUID, messageID string) error {
	return r.session.MarkPendingFresh(ctx, sessionID, messageID)
}

func (r *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	_, _, replyThread := telegramSessionRouting(p.Message)
	commandText := p.Message.CommandText
	if commandText == "" {
		commandText = p.Message.Text
	}
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:           p.SessionID,
		Sender:              p.Sender,
		InstallationID:      p.InstallationID,
		Body:                p.Message.Text,
		CommandText:         commandText,
		MessageID:           p.Message.MessageID,
		ThreadID:            replyThread,
		ClaimToken:          p.ClaimToken,
		MediaPendingSeconds: p.MediaPendingSeconds,
		ForceFresh:          p.Message.ForceFresh,
	})
}

func (r *sessionBinder) BindMedia(ctx context.Context, p engine.BindMediaParams) error {
	return r.session.BindMediaRefs(ctx, engine.BindMediaInput{
		MessageID:   p.MessageID,
		SessionID:   p.SessionID,
		WorkspaceID: p.WorkspaceID,
		Sender:      p.Sender,
		MediaRefs:   p.MediaRefs,
	})
}

// ---- audit ----

type auditor struct{ q *db.Queries }

func (r *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	raw, _ := decodeTelegramRaw(msg) // best-effort; a decode miss still audits
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ID:               dbid.NewV7(),
		ChannelType:      string(TypeTelegram),
		EventType:        raw.EventType,
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}

// ---- typing indicator ----

// typingNotifier shows Telegram's native "typing…" chat action when a message
// is ingested. The action self-expires after ~5 seconds, so unlike Slack's
// reaction there is nothing to clear — OnSettled is a no-op.
type typingNotifier struct {
	decrypt Decrypter
	apiBase string
	client  *http.Client
	logger  *slog.Logger
}

// NewTypingNotifier builds the sendChatAction-based typing indicator.
func NewTypingNotifier(decrypt Decrypter, apiBase string, client *http.Client, logger *slog.Logger) engine.TypingNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &typingNotifier{decrypt: decrypt, apiBase: apiBase, client: client, logger: logger}
}

func (n *typingNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return
	}
	creds, err := decodeCredentials(row.Config, n.decrypt)
	if err != nil {
		n.logger.WarnContext(ctx, "telegram typing: decode credentials failed", "error", err)
		return
	}
	chatID, err := strconv.ParseInt(msg.Source.ChatID, 10, 64)
	if err != nil {
		return
	}
	var threadID int64
	if msg.Source.ThreadID != "" {
		threadID, _ = strconv.ParseInt(msg.Source.ThreadID, 10, 64)
	}
	if err := newBotAPI(n.apiBase, creds.BotToken, n.client).SendChatAction(ctx, chatID, threadID); err != nil {
		n.logger.WarnContext(ctx, "telegram typing: sendChatAction failed", "error", err)
	}
}

func (n *typingNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID) {}
