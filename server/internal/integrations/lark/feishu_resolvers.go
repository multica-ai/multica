package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// This file is the Feishu ResolverSet: the platform-specific implementations
// the channel-agnostic engine.Router runs the inbound pipeline through. Each
// resolver translates between the engine's normalized channel.InboundMessage /
// engine types and the Feishu store / services. Platform-specific fields the
// normalized envelope does not carry (app_id, event_type, the un-enriched
// command body, create time) are read from the original InboundMessage the
// feishuChannel stashes in channel.InboundMessage.Raw — the documented adapter
// boundary (the core never reads Raw).

// originFeishuChat is the issue.origin_type label written for issues created
// via the Feishu /issue command. Kept as "lark_chat" (unchanged from the
// pre-cutover dispatcher) so analytics classification does not shift.
const originFeishuChat = "lark_chat"

// larkMsgFromRaw decodes the original Feishu InboundMessage the feishuChannel
// stashed in channel.InboundMessage.Raw.
func larkMsgFromRaw(msg channel.InboundMessage) (InboundMessage, error) {
	var lm InboundMessage
	if len(msg.Raw) == 0 {
		return InboundMessage{}, errors.New("lark: inbound message Raw is empty")
	}
	if err := json.Unmarshal(msg.Raw, &lm); err != nil {
		return InboundMessage{}, fmt.Errorf("decode feishu inbound raw: %w", err)
	}
	return lm, nil
}

// NewFeishuResolverSet assembles the Feishu ResolverSet from the store, the
// shared session service, audit logger, and (optional) outbound replier +
// typing indicator. Feishu is just another consumer of the channel-agnostic
// engine.ChatSession — there is no Feishu-specific session implementation.
func NewFeishuResolverSet(store *ChannelStore, session *engine.ChatSession, audit AuditLogger, replier OutcomeReplier, typing *TypingIndicatorManager) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &feishuInstallationResolver{store: store},
		Identity:     &feishuIdentityResolver{store: store},
		Dedup:        &feishuDeduper{store: store},
		Session:      &feishuSessionBinder{session: session},
		Audit:        &feishuAuditor{audit: audit},
		OriginType:   originFeishuChat,
	}
	if replier != nil {
		set.Replier = &feishuOutboundReplier{replier: replier}
	}
	if typing != nil {
		set.Typing = &feishuTypingNotifier{mgr: typing}
	}
	return set
}

// ---- installation routing ----

type feishuInstallationResolver struct{ store *ChannelStore }

func (r *feishuInstallationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	lm, err := larkMsgFromRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	inst, err := r.store.GetLarkInstallationByAppID(ctx, lm.AppID)
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
		Active:          InstallationStatus(inst.Status) == InstallationActive,
		Platform:        inst,
	}, nil
}

// ---- identity ----

type feishuIdentityResolver struct{ store *ChannelStore }

func (r *feishuIdentityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	binding, err := r.store.GetLarkUserBindingByOpenID(ctx, GetUserBindingByOpenIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  msg.Source.SenderID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		return engine.ResolvedIdentity{}, err
	}
	isMember, err := r.store.IsWorkspaceMember(ctx, inst.WorkspaceID, binding.MulticaUserID)
	if err != nil {
		return engine.ResolvedIdentity{}, err
	}
	if !isMember {
		return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
	}
	return engine.ResolvedIdentity{UserID: binding.MulticaUserID}, nil
}

// ---- dedup ----

type feishuDeduper struct{ store *ChannelStore }

func (r *feishuDeduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	claim, err := r.store.ClaimLarkInboundDedup(ctx, ClaimInboundDedupParams{
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

func (r *feishuDeduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.store.MarkLarkInboundDedupProcessed(ctx, MarkInboundDedupProcessedParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

func (r *feishuDeduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.store.ReleaseLarkInboundDedup(ctx, ReleaseInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

// ---- session bind / append ----

// chatSession is the slice of engine.ChatSession the Feishu binder drives.
// Declared as an interface so the (platform-specific) param mapping can be
// unit-tested with a fake; *engine.ChatSession is the production value.
type chatSession interface {
	EnsureSession(ctx context.Context, in engine.EnsureSessionInput) (pgtype.UUID, error)
	AppendUserMessage(ctx context.Context, in engine.AppendInput) (engine.AppendResult, error)
}

type feishuSessionBinder struct{ session chatSession }

// larkBindingConfig is the opaque outbound routing persisted on the chat
// binding's config when the binding key is a composite (Lark topic): the real
// chat id lives here so the outbound path can post back.
type larkBindingConfig struct {
	ChatID string `json:"chat_id"`
}

// larkSessionRouting derives the session-isolation key (stored as
// channel_chat_id) and the outbound config from one inbound Feishu message.
// A p2p or plain group chat is one continuous session per chat, so the key is
// the chat id and the key alone routes outbound (no config). A message inside
// a Lark topic (话题, thread_id present) is isolated by topic — key =
// "chat:thread" — so two @bot topics in one group are two sessions (the same
// model as Slack's channel:threadRoot; see engine.EnsureSessionInput). Pure
// function so the isolation contract is unit-tested without a DB.
func larkSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte) {
	chatID := msg.Source.ChatID
	if msg.Source.ChatType != channel.ChatTypeGroup || msg.Source.ThreadID == "" {
		return chatID, nil
	}
	cfg, _ := json.Marshal(larkBindingConfig{ChatID: chatID})
	return chatID + ":" + msg.Source.ThreadID, cfg
}

func (r *feishuSessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	bindingKey, config := larkSessionRouting(p.Message)
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

func (r *feishuSessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	// CommandText is the user's OWN typed text: the Feishu enricher inlines
	// quoted/forwarded context into Body, so /issue parsing must use the
	// un-enriched command body stashed in Raw, not Body.
	lm, err := larkMsgFromRaw(p.Message)
	if err != nil {
		return engine.AppendResult{}, err
	}
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,
		CommandText:    lm.CommandBody,
		MessageID:      p.Message.MessageID,
		ThreadID:       p.Message.Source.ThreadID,
		ClaimToken:     p.ClaimToken,
	})
}

// ---- audit ----

type feishuAuditor struct{ audit AuditLogger }

func (r *feishuAuditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	// event_type is platform-specific (read from Raw); a decode failure is
	// non-fatal — the drop is still worth auditing without it.
	lm, _ := larkMsgFromRaw(msg)
	return r.audit.RecordDrop(ctx, AuditDropParams{
		InstallationID: instID,
		ChatID:         ChatID(msg.Source.ChatID),
		EventType:      lm.EventType,
		LarkEventID:    msg.EventID,
		LarkMessageID:  msg.MessageID,
		Reason:         DropReason(string(reason)),
	})
}

// ---- outbound replier ----

type feishuOutboundReplier struct{ replier OutcomeReplier }

func (r *feishuOutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	larkInst, ok := inst.Platform.(Installation)
	if !ok {
		return
	}
	lm, _ := larkMsgFromRaw(msg)
	r.replier.Reply(ctx, larkInst, lm, dispatchResultFromEngine(res))
}

// dispatchResultFromEngine maps the engine verdict to the Feishu DispatchResult
// the OutcomeReplier consumes. The Outcome/DropReason string values match 1:1.
func dispatchResultFromEngine(res engine.Result) DispatchResult {
	return DispatchResult{
		Outcome:         Outcome(string(res.Outcome)),
		DropReason:      DropReason(string(res.DropReason)),
		InstallationID:  res.InstallationID,
		ChatSessionID:   res.ChatSessionID,
		SenderOpenID:    OpenID(res.Sender),
		IssueID:         res.IssueID,
		IssueNumber:     res.IssueNumber,
		IssueIdentifier: res.IssueIdentifier,
		IssueTitle:      res.IssueTitle,
	}
}

// ---- typing indicator ----

type feishuTypingNotifier struct{ mgr *TypingIndicatorManager }

func (r *feishuTypingNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	larkInst, ok := inst.Platform.(Installation)
	if !ok {
		return
	}
	lm, _ := larkMsgFromRaw(msg)
	r.mgr.Add(ctx, larkInst, sessionID, msg.MessageID, lm.CreateTime)
}

// OnSettled clears the reaction when the run trigger enqueued no task (agent
// offline / archived, or an enqueue failure) — the Patcher's bus-driven clear on
// chat-done / task-failed never fires for those, so without this the Typing
// reaction sticks.
func (r *feishuTypingNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID) {
	r.mgr.Clear(ctx, sessionID)
}
