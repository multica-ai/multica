package wecom

// replier.go — the WeCom OutboundReplier. Handles the engine's needs_binding
// / agent_offline / agent_archived / issue_created outcomes by sending a
// text message back over the same aibot WebSocket the inbound loop owns
// (aibot has no REST outbound; every write is on the socket, looked up via
// the sendersRegistry).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	agentOfflineText  = "⚠️ 智能体当前不在线，你的消息已收到，等它上线后会处理。"
	agentArchivedText = "⚠️ 该智能体已归档，无法回复。请联系工作区管理员。"
)

// OutboundReplier implements engine.OutboundReplier for WeCom.
type OutboundReplier struct {
	binding     binder
	senders     *sendersRegistry
	appURL      string
	bindingPath string
	logger      *slog.Logger
}

// binder is the slice of BindingTokenService sendBindingPrompt needs, declared
// as an interface so the group-vs-private routing can be exercised with a fake
// (no DB-backed token mint). *BindingTokenService is the production value.
type binder interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wecomUserID string) (BindingToken, error)
}

// OutboundReplierConfig configures the replier. Binding + AppURL are
// required for the needs_binding prompt to work; without them the prompt
// is skipped (the offline/archived/issue notices still fire).
type OutboundReplierConfig struct {
	Binding *BindingTokenService

	// Senders is the same sendersRegistry the wecom ChannelDeps was built
	// with. The replier looks up the live wsSender by installation id.
	Senders *sendersRegistry

	// AppURL is the Multica web app host the user clicks into to redeem
	// the binding token (e.g. https://multica.example). It comes from
	// MULTICA_APP_URL (falling back to FRONTEND_ORIGIN) and is
	// intentionally separate from MULTICA_PUBLIC_URL, which is the
	// backend/API URL — the bind page (/wecom/bind) is served by the web
	// app, so the link must point at the app host.
	AppURL      string
	BindingPath string // default "/wecom/bind"
	Logger      *slog.Logger
}

var _ engine.OutboundReplier = (*OutboundReplier)(nil)

// NewOutboundReplier builds the replier.
func NewOutboundReplier(cfg OutboundReplierConfig) *OutboundReplier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/wecom/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	r := &OutboundReplier{
		senders:     cfg.Senders,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
	// Assign through the interface only when non-nil: a nil *BindingTokenService
	// stored in the binder interface would be a non-nil interface value holding
	// a typed nil, defeating the `r.binding == nil` guard in sendBindingPrompt
	// and panicking on Mint.
	if cfg.Binding != nil {
		r.binding = cfg.Binding
	}
	return r
}

// Reply routes each outcome to its user-visible message. Errors are
// logged, not propagated: the replier runs detached from the inbound ACK
// path (the engine.Router owns that goroutine).
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "wecom replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, inst, msg, agentOfflineText); err != nil {
			r.logger.WarnContext(ctx, "wecom replier: offline notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, inst, msg, agentArchivedText); err != nil {
			r.logger.WarnContext(ctx, "wecom replier: archived notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIngested:
		// Only a /issue-created message warrants a confirmation; a plain
		// chat message stays silent (the agent's own reply lands via
		// EventChatDone / Channel.Send).
		if res.IssueID.Valid {
			if err := r.post(ctx, inst, msg, issueCreatedText(res)); err != nil {
				r.logger.WarnContext(ctx, "wecom replier: issue-created confirmation failed",
					"installation_id", util.UUIDToString(inst.ID), "error", err)
			}
		}
	}
}

func (r *OutboundReplier) sendBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) error {
	sender := res.Sender
	if sender == "" {
		sender = msg.Source.SenderID
	}
	if sender == "" {
		return errors.New("wecom: missing sender id")
	}
	if r.binding == nil {
		return errors.New("wecom: binding service not configured")
	}
	if r.appURL == "" {
		return errors.New("wecom: app url not configured")
	}
	token, err := r.binding.Mint(ctx, inst.WorkspaceID, inst.ID, sender)
	if err != nil {
		return fmt.Errorf("wecom: mint binding token: %w", err)
	}
	// The throttle suppressed the mint: a live link is already with this user.
	// Only its hash was ever stored, so there is no URL to rebuild — point
	// them at the message they already have. The throttle window is far
	// shorter than the TTL, so that link still has most of its life left.
	//
	// This text is delivered by postPrivate below, which always lands in the
	// 1:1 — the same conversation the earlier link is sitting in, whichever
	// room triggered this. So it points up the current thread rather than
	// telling the reader to go to a chat they are already reading. Only the
	// group ack further down runs in the room, and it is the one that names
	// the 1:1.
	text := "👋 绑定链接刚才已经发给你了，就在上方，请直接点击完成绑定。"
	if !token.Reused {
		bindURL := r.appURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
		text = "👋 请先绑定你的 Multica 账号，才能与我对话：\n" + bindURL + "\n（链接 15 分钟内有效）"
	}
	// A binding token is a bearer credential: binding.Redeem only checks that
	// the redeemer belongs to the token's workspace, and the bind page redeems
	// on load as whoever is signed in. Sending it to msg.Source.ChatID — which
	// in a group IS the group — would let any member click first and bind the
	// sender's WeCom userid to their own Multica account, after which the
	// sender's messages (/issue included) resolve to the hijacker. So deliver
	// the link privately to the sender's own userid with chat_type=1 (the same
	// address outbound.go uses for inbox pushes), never to the room. Lark's
	// SendBindingPromptCard targets the sender's OpenID for the same reason.
	if err := r.postPrivate(ctx, inst, sender, text); err != nil {
		return err
	}
	// A group trigger still needs an answer — silence reads as a broken bot —
	// but a token-less one that names nobody. Posted only after the private
	// send is accepted, so the room is never pointed at a message the wire
	// refused. A 1:1 trigger already received the prompt in its only room.
	if aibotChatTypeFromChannel(msg.Source.ChatType) == chatTypeGroupInt {
		return r.post(ctx, inst, msg, "👋 已把绑定链接私发给你，请在与我的单聊里点击完成绑定。")
	}
	return nil
}

// postPrivate delivers text to a single user's 1:1 chat (chat_type=1),
// regardless of which room triggered the message. Used for bearer-credential
// content (the binding link) that must never land in a group.
func (r *OutboundReplier) postPrivate(ctx context.Context, inst engine.ResolvedInstallation, userID, text string) error {
	if r.senders == nil {
		return errors.New("wecom: sender registry not configured")
	}
	if !inst.ID.Valid {
		return errors.New("wecom: installation id is zero")
	}
	if userID == "" {
		return errors.New("wecom: missing user id")
	}
	sender := r.senders.get(inst.ID)
	if sender == nil {
		return errors.New("wecom: connection not ready")
	}
	return sender.sendTextCtx(ctx, userID, chatTypeSingleInt, text)
}

// post looks up the installation's live wsSender in the registry and
// pushes aibot_send_msg with the given text. Returns "connection not
// ready" when the Supervisor has no active connection (mid-reconnect
// after lease flip, or right after Revoke).
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	if r.senders == nil {
		return errors.New("wecom: sender registry not configured")
	}
	if !inst.ID.Valid {
		return errors.New("wecom: installation id is zero")
	}
	sender := r.senders.get(inst.ID)
	if sender == nil {
		return errors.New("wecom: connection not ready")
	}
	chatID := msg.Source.ChatID
	if chatID == "" {
		return errors.New("wecom: missing chat_id")
	}
	chatType := aibotChatTypeFromChannel(msg.Source.ChatType)
	return sender.sendTextCtx(ctx, chatID, chatType, text)
}

func issueCreatedText(res engine.Result) string {
	id := res.IssueIdentifier
	if id == "" {
		id = fmt.Sprintf("#%d", res.IssueNumber)
	}
	title := strings.TrimSpace(res.IssueTitle)
	if title == "" {
		return "✅ 已创建 " + id
	}
	return "✅ 已创建 " + id + " — " + title
}
