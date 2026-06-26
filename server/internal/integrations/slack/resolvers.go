package slack

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

// This file is the Slack ResolverSet: the platform-specific seams the
// channel-agnostic engine.Router runs the inbound pipeline through. It mirrors
// the Feishu ResolverSet but is built entirely on the generic channel_* queries
// (no new query, no schema change) plus the shared engine.ChatSession — so
// "adding Slack" stays "implement Channel + register a ResolverSet".

// originSlackChat is the issue.origin_type label for issues created via the
// Slack /issue command.
const originSlackChat = "slack_chat"

// NewSlackResolverSet assembles the Slack ResolverSet over the generated
// queries + a tx starter (for the shared session service). Replier/Typing are
// left nil for now: the outbound binding-prompt / notice path is a later step
// (the inbound pipeline — route, identity, dedup, session, /issue, run trigger
// — is fully functional without them).
func NewSlackResolverSet(q *db.Queries, tx engine.TxStarter) engine.ResolverSet {
	return engine.ResolverSet{
		Installation: &installationResolver{q: q},
		Identity:     &identityResolver{q: q},
		Dedup:        &deduper{q: q},
		Session: &sessionBinder{session: engine.NewChatSession(q, tx, TypeSlack, engine.SessionTitles{
			Group:    "Slack channel",
			Direct:   "Slack direct message",
			Fallback: "Slack chat",
		})},
		Audit:      &auditor{q: q},
		OriginType: originSlackChat,
	}
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
)

// slackBindingConfig is the opaque outbound routing persisted on the chat
// binding's config. When the binding key is a composite (Slack channel thread),
// the real channel id lives here so the outbound path can post back.
type slackBindingConfig struct {
	ChannelID string `json:"channel_id"`
}

// slackSessionRouting derives, from one inbound Slack message, the three things
// the session layer needs kept distinct (Elon's round-2 must-fix):
//
//   - bindingKey: the session-isolation key (stored as channel_chat_id). A DM is
//     one continuous session per channel, so the key is the channel id. A
//     channel/group message is isolated by THREAD ROOT — key = "channel:root" —
//     so two @bot threads in one channel are two sessions, matching Hermes. The
//     thread root is the inbound thread_ts when replying in a thread, else the
//     message ts (a top-level @mention starts a new root).
//   - config: the real channel id, so outbound works even when the key is
//     composite.
//   - replyThread: the thread_ts to reply into (the thread root for groups; the
//     inbound thread for DMs, which may be empty for a top-level send).
//
// It is a pure function so the isolation contract is unit-tested without a DB.
func slackSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte, replyThread string) {
	chatID := msg.Source.ChatID
	cfg, _ := json.Marshal(slackBindingConfig{ChannelID: chatID})
	if msg.Source.ChatType == channel.ChatTypeP2P {
		return chatID, cfg, msg.Source.ThreadID
	}
	threadRoot := msg.Source.ThreadID
	if threadRoot == "" {
		threadRoot = msg.MessageID
	}
	return chatID + ":" + threadRoot, cfg, threadRoot
}

func decodeSlackRaw(msg channel.InboundMessage) (slackRawEvent, error) {
	var raw slackRawEvent
	if len(msg.Raw) == 0 {
		return slackRawEvent{}, errors.New("slack: inbound message Raw is empty")
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return slackRawEvent{}, fmt.Errorf("decode slack inbound raw: %w", err)
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
	raw, err := decodeSlackRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	inst, err := r.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       raw.TeamID, // Slack team_id is stored in the routing-key slot
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

type sessionBinder struct{ session *engine.ChatSession }

func (r *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	bindingKey, config, _ := slackSessionRouting(p.Message)
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
	_, _, replyThread := slackSessionRouting(p.Message)
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,
		// Slack text is not enriched, so the command source is the body itself.
		CommandText: p.Message.Text,
		MessageID:   p.Message.MessageID,
		ThreadID:    replyThread,
		ClaimToken:  p.ClaimToken,
	})
}

// ---- audit ----

type auditor struct{ q *db.Queries }

func (r *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	raw, _ := decodeSlackRaw(msg) // event_type is best-effort; a decode miss still audits the drop
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      string(TypeSlack),
		EventType:        raw.EventType,
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}
