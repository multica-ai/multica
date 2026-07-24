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
	binding     *BindingTokenService
	senders     *sendersRegistry
	appURL      string
	bindingPath string
	logger      *slog.Logger
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
	return &OutboundReplier{
		binding:     cfg.Binding,
		senders:     cfg.Senders,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
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
	bindURL := r.appURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	text := "👋 请先绑定你的 Multica 账号，才能与我对话：\n" + bindURL + "\n（链接 15 分钟内有效）"
	return r.post(ctx, inst, msg, text)
}

// post looks up the installation's live wsSender in the registry and
// pushes aibot_send_msg with the given text. Returns "connection not
// ready" when the Supervisor has no active connection (mid-reconnect
// after lease flip, or right after Revoke).
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	_ = ctx
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
	return sender.sendText(chatID, chatType, text)
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
