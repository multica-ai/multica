package weixin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type engineSessionBinder interface {
	EnsureSession(context.Context, engine.EnsureSessionInput) (pgtype.UUID, error)
	AppendUserMessage(context.Context, engine.AppendInput) (engine.AppendResult, error)
}

func NewResolverSet(store *Store, session engineSessionBinder, replier engine.OutboundReplier) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &installationResolver{store}, Identity: &identityResolver{store}, Dedup: &deduper{store},
		Session: &sessionBinder{session}, Audit: &auditor{store}, OriginType: "weixin_chat",
	}
	if replier != nil {
		set.Replier = replier
	}
	return set
}

func envelopeFrom(msg channel.InboundMessage) (inboundEnvelope, error) {
	var env inboundEnvelope
	err := json.Unmarshal(msg.Raw, &env)
	return env, err
}

type installationResolver struct{ store *Store }

func (r *installationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	env, err := envelopeFrom(msg)
	if err != nil || env.BotID == "" {
		return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
	}
	inst, err := r.store.GetInstallationByBotID(ctx, env.BotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
	}
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	return engine.ResolvedInstallation{ID: inst.ID, WorkspaceID: inst.WorkspaceID, AgentID: inst.AgentID, InstallerUserID: inst.InstallerUserID, Active: inst.Status == InstallationActive, Platform: inst}, nil
}

type identityResolver struct{ store *Store }

func (r *identityResolver) ResolveSender(ctx context.Context, resolved engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	inst, ok := resolved.Platform.(Installation)
	if !ok || strings.TrimSpace(msg.Source.SenderID) == "" || msg.Source.SenderID != inst.WeixinUserID {
		return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
	}
	member, err := r.store.IsWorkspaceMember(ctx, inst.WorkspaceID, inst.InstallerUserID)
	if err != nil {
		return engine.ResolvedIdentity{}, err
	}
	if !member {
		return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
	}
	return engine.ResolvedIdentity{UserID: inst.InstallerUserID}, nil
}

type deduper struct{ store *Store }

func (d *deduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	row, err := d.store.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{InstallationID: installationID, MessageID: messageID})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, engine.ErrDuplicate
	}
	return row.ClaimToken, err
}

func (d *deduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.store.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{InstallationID: installationID, MessageID: messageID, ClaimToken: claimToken})
	return err
}

func (d *deduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := d.store.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{InstallationID: installationID, MessageID: messageID, ClaimToken: claimToken})
	return err
}

type sessionBinder struct{ session engineSessionBinder }

func (b *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	return b.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID: p.Installation.WorkspaceID, AgentID: p.Installation.AgentID, InstallationID: p.Installation.ID,
		Sender: p.Sender, BindingKey: p.Message.Source.ChatID, ChatType: channel.ChatTypeP2P,
	})
}

func (b *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	return b.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID: p.SessionID, Sender: p.Sender, InstallationID: p.InstallationID,
		Body: p.Message.Text, CommandText: p.Message.CommandText, MessageID: p.Message.MessageID, ClaimToken: p.ClaimToken,
	})
}

func (*sessionBinder) BindMedia(context.Context, engine.BindMediaParams) error { return nil }

type auditor struct{ store *Store }

func (a *auditor) RecordDrop(ctx context.Context, installationID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	return a.store.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		InstallationID: installationID, ChannelType: channelTypeWeixin,
		ChannelChatID: optionalText(msg.Source.ChatID), EventType: "message",
		ChannelEventID: optionalText(msg.EventID), ChannelMessageID: optionalText(msg.MessageID), DropReason: string(reason),
	})
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
