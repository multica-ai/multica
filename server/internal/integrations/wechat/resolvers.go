package wechat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the WeChat ResolverSet: the platform-specific seams the
// channel-agnostic engine.Router runs the inbound pipeline through. It mirrors
// the Slack ResolverSet but is built entirely on the generic channel_* queries
// (no new query, no schema change) plus the shared engine.ChatSession — so
// "adding WeChat" stays "implement Channel + register a ResolverSet".

// NewWechatResolverSet assembles the WeChat ResolverSet over the generated
// queries + a tx starter (for the shared session service). The replier delivers
// the outbound binding-prompt / status / issue-created notices; pass a nil
// engine.OutboundReplier to disable them (the inbound pipeline — route,
// identity, dedup, session, /issue, run trigger — is fully functional without
// it). typing is nil for the MVP (WeChat iLink typing needs a separate
// getconfig ticket dance, deferred); the nil-guard still mirrors Slack/Feishu
// to keep the door open.
func NewWechatResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier, typing *TypingIndicatorManager) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &installationResolver{q: q},
		Identity:     &identityResolver{q: q},
		Dedup:        &deduper{q: q},
		Session: &sessionBinder{
			session: engine.NewChatSession(q, tx, TypeWechat, engine.SessionTitles{
				Group:    "WeChat group",
				Direct:   "WeChat direct message",
				Fallback: "WeChat chat",
			}),
			q: q,
		},
		Audit:      &auditor{q: q},
		Replier:    replier,
		OriginType: originWechatChat,
	}
	// Guard against assigning a nil *TypingIndicatorManager into the interface
	// field (which would make set.Typing a non-nil typed-nil); mirrors Feishu and
	// Slack.
	if typing != nil {
		set.Typing = &wechatTypingNotifier{mgr: typing}
	}
	return set
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
	_ engine.TypingNotifier       = (*wechatTypingNotifier)(nil)
)

func nullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// ---- installation routing ----

type installationResolver struct{ q *db.Queries }

func (r *installationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	raw := decodeWechatRaw(msg)
	if raw.IlinkBotID == "" {
		return engine.ResolvedInstallation{}, fmt.Errorf("wechat: inbound message carries no bot id")
	}
	// Route by the message's to_user_id (the bot id): each WeChat installation
	// stores its iLink bot id in the routing-key slot (config->>'app_id'), and a
	// message addressed to that bot uniquely identifies the installation.
	inst, err := r.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeWechat),
		AppID:       raw.IlinkBotID,
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

// identityQueries is the slice of generated queries the identityResolver needs.
// It is an interface (not *db.Queries) so the cross-installation reuse path is
// unit-tested with fakes, mirroring Slack. *db.Queries satisfies it.
type identityQueries interface {
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	FindReusableChannelUserBinding(ctx context.Context, arg db.FindReusableChannelUserBindingParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	CreateChannelUserBinding(ctx context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error)
}

type identityResolver struct{ q identityQueries }

func (r *identityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	senderID := msg.Source.SenderID
	binding, err := r.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  senderID,
	})
	reused := false
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, err
		}
		// Not linked to THIS installation. WeChat has no team concept (unlike
		// Slack), so reuse looks up a binding for the same user id across other
		// WeChat installations in the same workspace — one link per WeChat user
		// per workspace, not per bot.
		cand, ok, ferr := r.reusableBinding(ctx, inst, senderID)
		if ferr != nil {
			return engine.ResolvedIdentity{}, ferr
		}
		if !ok {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		binding, reused = cand, true
	}
	// Binding existence no longer proves membership (no FK); re-check.
	if _, err := r.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.MulticaUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if reused {
				return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
			}
			return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
		}
		return engine.ResolvedIdentity{}, err
	}
	if reused {
		// Materialize the reused link as a binding on THIS installation.
		if _, err := r.q.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
			WorkspaceID:    inst.WorkspaceID,
			MulticaUserID:  binding.MulticaUserID,
			InstallationID: inst.ID,
			ChannelType:    string(TypeWechat),
			ChannelUserID:  senderID,
			Config:         []byte(`{}`),
		}); err != nil {
			return engine.ResolvedIdentity{}, fmt.Errorf("materialize reused wechat binding: %w", err)
		}
	}
	return engine.ResolvedIdentity{UserID: binding.MulticaUserID}, nil
}

// reusableBinding looks for a link the same WeChat user already made to ANOTHER
// installation in the SAME workspace, so a second bot need not re-prompt. ok=false
// (nil error) means "no reuse — prompt to link".
func (r *identityResolver) reusableBinding(ctx context.Context, inst engine.ResolvedInstallation, senderID string) (db.ChannelUserBinding, bool, error) {
	// WeChat has no team id; the reuse key is (workspace, channel_type=wechat,
	// channel_user_id). An empty TeamID is what FindReusableChannelUserBinding
	// expects for team-less channels.
	cand, err := r.q.FindReusableChannelUserBinding(ctx, db.FindReusableChannelUserBindingParams{
		WorkspaceID:   inst.WorkspaceID,
		ChannelType:   string(TypeWechat),
		ChannelUserID: senderID,
		TeamID:        "",
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelUserBinding{}, false, nil
		}
		return db.ChannelUserBinding{}, false, err
	}
	return cand, true, nil
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

// sessionBinder wraps the shared engine.ChatSession. It ALSO refreshes the
// context_token on the binding after each append (the core iLink quirk: the
// outbound reply must echo back the inbound message's context_token). The
// shared ChatSession only sets the binding config once at creation, so the
// per-message refresh is done here via the generated query.
type sessionBinder struct {
	session *engine.ChatSession
	q       bindingConfigUpdater
	logger  *slog.Logger
}

func (r *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	bindingKey, config := wechatSessionRouting(p.Message)
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
	res, err := r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,
		// WeChat text is not enriched, so the command source is the body itself.
		CommandText: p.Message.Text,
		MessageID:   p.Message.MessageID,
		ClaimToken:  p.ClaimToken,
	})
	if err != nil {
		return res, err
	}
	// Refresh the context_token on the binding so the next outbound reply carries
	// the freshest token. Best-effort: a failure here is logged but does not
	// unwind the append (the message is already durably stored); the outbound
	// path degrades to the prior token and ultimately to a resend fallback.
	if r.q != nil {
		if ferr := updateContextToken(ctx, r.q, p.SessionID, p.Message); ferr != nil {
			lg := r.logger
			if lg == nil {
				lg = slog.Default()
			}
			lg.WarnContext(ctx, "wechat: refresh context_token failed; outbound may degrade",
				"chat_session_id", p.SessionID, "error", ferr)
		}
	}
	return res, nil
}

// ---- audit ----

type auditor struct{ q *db.Queries }

func (r *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	raw := decodeWechatRaw(msg) // msg_type is best-effort; a decode miss still audits the drop
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      string(TypeWechat),
		EventType:        raw.MsgType,
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}

// ---- typing indicator (stub for MVP; left for a later phase) ----

type wechatTypingNotifier struct{ mgr *TypingIndicatorManager }

func (n *wechatTypingNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	ci, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return
	}
	n.mgr.Add(ctx, ci, sessionID, msg.Source.ChatID, msg.MessageID)
}

func (n *wechatTypingNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID) {
	n.mgr.Clear(ctx, sessionID)
}
