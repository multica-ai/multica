package dingtalk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the DingTalk ResolverSet: the platform-specific seams the
// channel-agnostic engine.Router runs the inbound pipeline through. It is built
// entirely on the generic channel_* queries plus the shared engine.ChatSession,
// mirroring the Feishu and Slack ResolverSets.

// NewDingTalkResolverSet assembles the DingTalk ResolverSet over the generated
// queries + a tx starter (for the shared session service). The replier delivers
// the outbound binding-prompt / status / issue-created notices; pass a nil
// engine.OutboundReplier to disable them. The classic robot send API exposes no
// per-message reaction, so the ack notifier stands in for a typing indicator (a
// "working on it" message on ingest); pass nil to disable it. quick wires the
// engine's QuickCreate seam so an `/issue` command enqueues a chat-originated
// quick-create task instead of the synchronous direct-create path.
// media wires the engine's MediaIngester seam for inbound images; pass nil to
// keep the channel text-only (messages carrying media are then refused with a
// resend prompt).
func NewDingTalkResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier, ack *ackNotifier, quick engine.QuickCreator, media engine.MediaIngester) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &installationResolver{q: q},
		Identity:     &identityResolver{q: q},
		Dedup:        &deduper{q: q},
		Session: &sessionBinder{session: engine.NewChatSession(q, tx, TypeDingTalk, engine.SessionTitles{
			Group:    "DingTalk group",
			Direct:   "DingTalk direct message",
			Fallback: "DingTalk chat",
		})},
		Audit:       &auditor{q: q},
		Replier:     replier,
		QuickCreate: quick,
		Media:       media,
		// DingTalk's replier renders the audio/video/file refusal
		// (unsupportedKindText), so it opts into the engine's capability gate.
		RefuseUnsupportedKinds: true,
		Attachments:            &attachmentCleaner{q: q},
	}
	// Guard against assigning a nil *ackNotifier into the interface field (which
	// would make set.Typing a non-nil typed-nil); mirrors Slack/Feishu.
	if ack != nil {
		set.Typing = ack
	}
	return set
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
)

// dingtalkBindingConfig is the opaque outbound routing persisted on the chat
// binding's config: enough to address a proactive reply back into the
// originating conversation. StaffID is the lone recipient of a 1:1 chat; for a
// group it is empty (the group is addressed by its conversation id).
type dingtalkBindingConfig struct {
	ConversationType string `json:"conversation_type"`
	ConversationID   string `json:"conversation_id"`
	StaffID          string `json:"staff_id,omitempty"`
}

// dingtalkSessionRouting derives the session-isolation key and the outbound
// routing config from one inbound message. DingTalk has no threads, so a
// conversation (1:1 or group) is one continuous session keyed by its
// conversation id; the config carries everything the outbound path needs to send
// back.
func dingtalkSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte) {
	chatID := msg.Source.ChatID
	cfg := dingtalkBindingConfig{
		ConversationType: convTypeGroup,
		ConversationID:   chatID,
	}
	if msg.Source.ChatType == channel.ChatTypeP2P {
		cfg.ConversationType = convTypeP2P
		cfg.StaffID = msg.Source.SenderID
	}
	raw, _ := json.Marshal(cfg)
	return chatID, raw
}

// outboundTarget recovers the send target from a chat binding's config, falling
// back to the channel_chat_id when the config is missing or unparsable.
func outboundTarget(b db.ChannelChatSessionBinding) sendTarget {
	target := sendTarget{ConversationType: convTypeGroup, ConversationID: b.ChannelChatID}
	if len(b.Config) > 0 {
		var cfg dingtalkBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil {
			if cfg.ConversationType != "" {
				target.ConversationType = cfg.ConversationType
			}
			if cfg.ConversationID != "" {
				target.ConversationID = cfg.ConversationID
			}
			target.StaffID = cfg.StaffID
		}
	}
	return target
}

func decodeDingTalkRaw(msg channel.InboundMessage) (dingtalkRawEvent, error) {
	var raw dingtalkRawEvent
	if len(msg.Raw) == 0 {
		return dingtalkRawEvent{}, errors.New("dingtalk: inbound message Raw is empty")
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return dingtalkRawEvent{}, fmt.Errorf("decode dingtalk inbound raw: %w", err)
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
	raw, err := decodeDingTalkRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	// Route by the AppKey the receiving connection stamped into the envelope.
	// Each installation has its own Stream connection, so the stamped AppKey
	// uniquely identifies the installation (the DingTalk callback itself carries
	// no robot code).
	inst, err := r.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeDingTalk),
		AppID:       raw.AppID,
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
	bindingKey, config := dingtalkSessionRouting(p.Message)
	return r.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    p.Installation.WorkspaceID,
		AgentID:        p.Installation.AgentID,
		InstallationID: p.Installation.ID,
		Sender:         p.Sender,
		BindingKey:     bindingKey,
		BindingConfig:  config,
		ChatType:       p.Message.Source.ChatType,
		// Seed the session title from the first message (like a web chat) instead
		// of a fixed label; a /new command starts a fresh session titled by its
		// own body.
		Title: sessionTitleFromMessage(p.Message.Text),
		Fresh: p.Message.ForceFresh,
	})
}

// sessionTitleFromMessage derives a chat title from the first message, matching
// the web new-chat behavior (first 50 characters of the seed). Empty text yields
// an empty title so the engine falls back to the platform default.
func sessionTitleFromMessage(text string) string {
	seed := strings.TrimSpace(text)
	if seed == "" {
		return ""
	}
	runes := []rune(seed)
	if len(runes) > 50 {
		return string(runes[:50])
	}
	return seed
}

func (r *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	return r.session.AppendUserMessage(ctx, appendInput(p))
}

// appendInput builds the AppendInput for one inbound message. The stored Body
// is composed from the ordered segments so image position survives into the
// transcript (an image-carrying turn keeps its "![…](url)" markdown so the UI
// renders it). The /issue command, however, is parsed from the image-STRIPPED
// Message.Text — the exact source the Router uses to decide isIssueTurn /
// MediaChatBind (router.go). Parsing the composed Body instead would let image
// markdown both flip the routing decision (an image-first "/issue …" no longer
// begins with the prefix) and leak into the parsed title, so the two must read
// the same text. Split out so composition and media pass-through are testable
// without a DB.
func appendInput(p engine.AppendParams) engine.AppendInput {
	body := engine.ComposeBody(p.Message, p.Staged)
	return engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           body,
		CommandText:    p.Message.Text,
		MessageID:      p.Message.MessageID,
		ClaimToken:     p.ClaimToken,
		WorkspaceID:    p.WorkspaceID,
		Staged:         p.Staged,
		MediaChatBind:  p.MediaChatBind,
		// DingTalk builds the quick-create prompt from the turn's own content,
		// so a bare "/issue" must ask what to file — never adopt the previous
		// message (which could be an unrelated image).
		SkipPreviousFallback: true,
	}
}

// ---- attachment cleanup ----

// attachmentCleaner removes attachment rows the engine staged for an /issue
// turn that produced no issue (empty prompt or enqueue failure). The staged
// storage objects are discarded separately by the MediaIngester; this drops
// the DB rows so they do not dangle bound to neither a chat nor an issue.
type attachmentCleaner struct{ q *db.Queries }

var _ engine.AttachmentDiscarder = (*attachmentCleaner)(nil)

func (c *attachmentCleaner) DiscardAttachments(ctx context.Context, workspaceID pgtype.UUID, ids []pgtype.UUID) {
	for _, id := range ids {
		if err := c.q.DeleteAttachment(ctx, db.DeleteAttachmentParams{ID: id, WorkspaceID: workspaceID}); err != nil {
			slog.WarnContext(ctx, "dingtalk: discard staged attachment row failed",
				"attachment_id", util.UUIDToString(id), "error", err)
		}
	}
}

// ---- audit ----

type auditor struct{ q *db.Queries }

func (r *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      string(TypeDingTalk),
		EventType:        "message",
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}
