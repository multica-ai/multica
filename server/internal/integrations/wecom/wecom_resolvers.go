package wecom

// wecom_resolvers.go — the ResolverSet the engine.Router routes through when
// the inbound channel_type is "wecom". Each interface method translates
// between the engine's normalized channel.InboundMessage and the wecom store
// / services. Platform-specific fields the normalized envelope does not carry
// (BotID, sender userid) come out of the wecom InboundMessage stashed in
// channel.InboundMessage.Raw.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// originWecomChat is the issue.origin_type label written for issues created
// via the wecom /issue command. Matches the "lark_chat" pattern (platform +
// "_chat") so analytics keeps the origin family consistent.
const originWecomChat = "wecom_chat"

// wecomMsgFromRaw decodes the wecom-side InboundMessage from
// channel.InboundMessage.Raw. Every resolver ends up doing this at least
// once; we centralize the JSON tag so a Raw shape change is a single-file
// edit.
func wecomMsgFromRaw(msg channel.InboundMessage) (InboundMessage, error) {
	if len(msg.Raw) == 0 {
		return InboundMessage{}, errors.New("wecom: inbound message Raw is empty")
	}
	var wm InboundMessage
	if err := json.Unmarshal(msg.Raw, &wm); err != nil {
		return InboundMessage{}, fmt.Errorf("wecom: decode inbound raw: %w", err)
	}
	return wm, nil
}

// NewResolverSet assembles the wecom ResolverSet from the store, the shared
// chat-session service, and an outbound replier. wecom has no typing-
// indicator affordance, so Typing is left nil — the Router treats a nil
// Typing as a no-op.
//
// The replier is optional: pass nil to disable outbound binding prompts.
func NewResolverSet(
	store *Store,
	session engineSessionBinder,
	replier engine.OutboundReplier,
) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &installationResolver{store: store},
		Identity:     &identityResolver{store: store},
		Dedup:        &deduper{store: store},
		Session:      &sessionBinder{session: session},
		Audit:        &auditor{store: store},
		OriginType:   originWecomChat,
	}
	if replier != nil {
		set.Replier = replier
	}
	return set
}

// engineSessionBinder is the slice of engine.ChatSession the wecom binder
// drives. Declared as an interface so the platform-specific param mapping
// can be exercised with a fake in unit tests; *engine.ChatSession is the
// production value.
type engineSessionBinder interface {
	EnsureSession(ctx context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error)
	AppendUserMessage(ctx context.Context, in engine.AppendInput) (engine.AppendResult, error)
}

// ---- installation routing ----

type installationResolver struct{ store *Store }

// ResolveInstallation looks up the wecom installation by the BotID carried
// on the inbound event. Every aibot_msg_callback frame identifies the bot
// via the WebSocket connection it arrived on (one bot per connection); the
// connector stamps BotID into InboundMessage.Raw so this resolver stays a
// pure DB lookup rather than needing socket-side plumbing.
func (r *installationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	wm, err := wecomMsgFromRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	if wm.BotID == "" {
		return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
	}
	inst, err := r.store.GetInstallationByBotID(ctx, wm.BotID)
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
		Active:          inst.Status == InstallationActive,
		Platform:        inst,
	}, nil
}

// ---- identity ----

type identityResolver struct{ store *Store }

// ResolveSender maps the WeChat userid (from Source.SenderID) to a Multica
// user via implicit email-prefix binding: WeChat userids in a Tencent-style
// deployment usually match the local-part of the corporate email
// (leroychen@tencent.com → wecom userid "leroychen"). A proper explicit
// binding table is on the roadmap but not needed for the first iteration.
//
// ErrSenderUnbound / ErrSenderNotMember let the Router surface the correct
// product outcome (a needs-binding reply vs. a silent drop).
func (r *identityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	prefix := strings.TrimSpace(msg.Source.SenderID)
	if prefix == "" {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	userID, err := r.findWorkspaceMemberByEmailPrefix(ctx, inst.WorkspaceID, prefix)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		return engine.ResolvedIdentity{}, err
	}
	isMember, err := r.store.IsWorkspaceMember(ctx, inst.WorkspaceID, userID)
	if err != nil {
		return engine.ResolvedIdentity{}, err
	}
	if !isMember {
		return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
	}
	return engine.ResolvedIdentity{UserID: userID}, nil
}

// findWorkspaceMemberByEmailPrefix walks the workspace's member list looking
// for a user whose email local-part (case-insensitive) matches prefix.
// Bounded by workspace size; on a full deployment (thousands of members) we
// would introduce a functional index and a dedicated sqlc query, but the
// smart-bot scenario does not warrant that yet. Returns pgx.ErrNoRows when
// nothing matches.
func (r *identityResolver) findWorkspaceMemberByEmailPrefix(ctx context.Context, workspaceID pgtype.UUID, prefix string) (pgtype.UUID, error) {
	members, err := r.store.Queries.ListMembersWithUser(ctx, workspaceID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	lp := strings.ToLower(prefix)
	for _, m := range members {
		local := m.UserEmail
		if at := strings.IndexByte(local, '@'); at >= 0 {
			local = local[:at]
		}
		if strings.ToLower(local) == lp {
			return m.UserID, nil
		}
	}
	return pgtype.UUID{}, pgx.ErrNoRows
}

// ---- dedup ----

// deduper is the wecom Deduper. It uses the shared channel_inbound_message_dedup
// sqlc queries — the same table Feishu / Slack use — so the two-phase
// idempotency invariant is enforced uniformly across channels.
type deduper struct{ store *Store }

func (d *deduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	row, err := d.store.Queries.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrDuplicate
		}
		return pgtype.UUID{}, err
	}
	return row.ClaimToken, nil
}

func (d *deduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.store.Queries.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

func (d *deduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.store.Queries.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

// ---- session binding ----

type sessionBinder struct{ session engineSessionBinder }

// EnsureSession picks the wecom session-isolation key. For single (p2p)
// chats the wecom ChatID already IS the userid, one session per user;
// for group chats we key on the chatid so all group traffic lands in one
// session — the aibot API does not have a first-class thread concept.
func (r *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	return r.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    p.Installation.WorkspaceID,
		AgentID:        p.Installation.AgentID,
		InstallationID: p.Installation.ID,
		Sender:         p.Sender,
		BindingKey:     p.Message.Source.ChatID,
		ChatType:       p.Message.Source.ChatType,
	})
}

func (r *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,
		CommandText:    p.Message.Text, // wecom has no enrichment; command == body
		MessageID:      p.Message.MessageID,
		ClaimToken:     p.ClaimToken,
	})
}

// ---- audit ----

type auditor struct{ store *Store }

func (a *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	var eventType string
	if wm, err := wecomMsgFromRaw(msg); err == nil {
		eventType = wm.MsgType
	}
	var instIDArg pgtype.UUID
	if instID.Valid {
		instIDArg = instID
	}
	return a.store.Queries.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		InstallationID:   instIDArg,
		ChannelType:      channelTypeWecom,
		ChannelChatID:    textOrNull(msg.Source.ChatID),
		EventType:        eventType,
		ChannelEventID:   textOrNull(msg.EventID),
		ChannelMessageID: textOrNull(msg.MessageID),
		DropReason:       string(reason),
	})
}

// textOrNull maps an empty string to a NULL pgtype.Text — the shared
// RecordChannelInboundDrop query uses sqlc.narg on the id columns so we
// need to pass NULL rather than "".
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
