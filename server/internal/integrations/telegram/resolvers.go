package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the Telegram ResolverSet: the platform-specific seams the
// channel-agnostic engine.Router runs the inbound pipeline through. It mirrors
// the Slack ResolverSet but is built entirely on the generic channel_* queries
// (no new query, no schema change) plus the shared engine.ChatSession — so
// "adding Telegram" stays "implement Channel + register a ResolverSet".
//
// Unlike Slack, Telegram has no team concept: one bot (BotID) IS the
// installation, so installation routing has no team check, and identity has
// no cross-installation reuse (there is only ever one installation per bot).

// originTelegramChat is the issue.origin_type label for issues created via the
// Telegram /issue command.
const originTelegramChat = "telegram_chat"

// NewTelegramResolverSet assembles the Telegram ResolverSet over the generated
// queries + a tx starter (for the shared session service). The replier delivers
// the outbound binding-prompt / status / issue-created notices; pass a nil
// engine.OutboundReplier to disable them (the inbound pipeline — route,
// identity, dedup, session, /issue, run trigger — is fully functional without
// it). Telegram has no typing indicator in v1, so Typing is left unset.
func NewTelegramResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier) engine.ResolverSet {
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
		OriginType: originTelegramChat,
	}
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
)

// telegramBindingConfig is the opaque outbound routing persisted on the chat
// binding's config. v1 keeps only the chat id (the binding key IS the chat
// id, so this is mostly for parity with other platforms' binding rows).
type telegramBindingConfig struct {
	ChatID string `json:"chat_id"`
}

// telegramSessionRouting derives, from one inbound Telegram message, the
// three things the session layer needs kept distinct: bindingKey (session
// isolation key, stored as channel_chat_id), config (opaque outbound
// routing), and replyThread (the thread to reply into).
//
// v1 is deliberately simple: one session per Telegram chat (bindingKey =
// ChatID), no thread-root composite like Slack's per-thread isolation. The
// reply threads into msg.Source.ThreadID (a Telegram forum topic thread) when
// present.
func telegramSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte, replyThread string) {
	chatID := msg.Source.ChatID
	cfg, _ := json.Marshal(telegramBindingConfig{ChatID: chatID})
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
		// Route by the bot id: each Telegram installation is one bot, and the
		// per-installation webhook only ever delivers updates for its own bot,
		// so bot_id uniquely identifies the installation. Unlike Slack there is
		// no team to additionally check.
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

// identityQueries is the slice of generated queries the identityResolver
// needs. It is an interface (not *db.Queries) so it is unit-tested with
// fakes, mirroring Slack's identityQueries. *db.Queries satisfies it.
type identityQueries interface {
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

type identityResolver struct{ q identityQueries }

func (r *identityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	senderID := msg.Source.SenderID
	binding, err := r.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  senderID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Telegram has no team concept and only one installation per bot, so
			// unlike Slack there is no cross-installation reuse to try — an
			// unbound sender always needs a fresh /link.
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

type sessionBinder struct{ session *engine.ChatSession }

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

func (r *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	_, _, replyThread := telegramSessionRouting(p.Message)
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,
		// Telegram text is not enriched, so the command source is the body itself.
		CommandText:         p.Message.Text,
		MessageID:           p.Message.MessageID,
		ThreadID:            replyThread,
		ClaimToken:          p.ClaimToken,
		MediaPendingSeconds: p.MediaPendingSeconds,
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
	raw, _ := decodeTelegramRaw(msg) // event_type is best-effort; a decode miss still audits the drop
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      string(TypeTelegram),
		EventType:        raw.EventType,
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}
