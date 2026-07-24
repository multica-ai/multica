package wechat

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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This file is the WeChat OutboundReplier — the engine seam that delivers a
// verdict-driven reply back to the user. It posts through the iLink sendmessage
// path, reusing the inbound message's context_token (the reply-association key).
//
// Outcomes handled:
//   - NeedsBinding: the sender is unbound. Mint a single-use binding token and
//     reply with a "link your account" prompt pointing at the in-product redeem
//     page. After they bind, their next message reaches the agent.
//   - AgentOffline / AgentArchived: a status notice so the user is not left
//     wondering why nothing happened.
//   - Ingested with an /issue created: a confirmation of the new issue.

const (
	agentOfflineText  = "⚠️ 机器人暂时离线，已收到你的消息，上线后会立即处理。"
	agentArchivedText = "⚠️ 该机器人已归档，无法回复，请联系工作区管理员。"
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, wechatUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for WeChat.
type OutboundReplier struct {
	binding     bindingMinter
	decrypt     Decrypter
	client      *iLinkClient
	appURL      string
	bindingPath string
	baseURL     string
	logger      *slog.Logger
}

// OutboundReplierConfig configures the replier. Binding + AppURL are required
// for the NeedsBinding prompt to work; without them the prompt is skipped (the
// offline/archived/issue notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	Decrypt Decrypter
	// AppURL is the Multica web app host the user clicks into to redeem the
	// binding token (e.g. https://multica.example). The bind page (/wechat/bind)
	// is served by the web app.
	AppURL      string
	BindingPath string // default "/wechat/bind"
	// BaseURL seeds the iLink client for the QR-login flow; per-account base_url
	// from the installation config overrides it for sendmessage.
	BaseURL string
	Logger  *slog.Logger
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
		bindingPath = "/wechat/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		decrypt:     cfg.Decrypt,
		client:      newILinkClient(cfg.BaseURL, logger),
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		baseURL:     cfg.BaseURL,
		logger:      logger,
	}
}

// Reply routes each outcome to its user-visible message. Errors are logged, not
// propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	r.logger.InfoContext(ctx, "wechat replier: dispatching",
		"outcome", res.Outcome, "installation_id", util.UUIDToString(inst.ID),
		"sender", res.Sender, "has_binding", r.binding != nil, "app_url_set", r.appURL != "")
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "wechat replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, inst, msg, agentOfflineText); err != nil {
			r.logger.WarnContext(ctx, "wechat replier: offline notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, inst, msg, agentArchivedText); err != nil {
			r.logger.WarnContext(ctx, "wechat replier: archived notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIngested:
		// Only a /issue-created message warrants a confirmation; a plain chat
		// message stays silent (the agent's own reply lands via EventChatDone).
		if res.IssueID.Valid {
			if err := r.post(ctx, inst, msg, issueCreatedText(res)); err != nil {
				r.logger.WarnContext(ctx, "wechat replier: issue-created confirmation failed",
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
		return errors.New("missing sender id")
	}
	if r.binding == nil {
		return errors.New("binding service not configured")
	}
	if r.appURL == "" {
		return errors.New("app url not configured")
	}
	token, err := r.binding.Mint(ctx, inst.WorkspaceID, inst.ID, sender)
	if err != nil {
		return fmt.Errorf("mint binding token: %w", err)
	}
	bindURL := r.appURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	// WeChat has no Slack-style <url|label> mrkdwn; send the bare URL on its own
	// line so the WeChat client linkifies it. Chinese copy matches the WeChat
	// audience.
	text := "👋 为了让我能回复你，请先绑定你的 Multica 账号：\n" + bindURL + "\n（此链接 15 分钟内有效）"
	if err := r.post(ctx, inst, msg, text); err != nil {
		return err
	}
	r.logger.InfoContext(ctx, "wechat replier: binding prompt sent", "sender", sender)
	return nil
}

// post resolves the installation's bot token + base_url from the carried
// platform row and sends text back to the inbound sender, echoing the inbound
// message's context_token (the iLink reply-association key).
func (r *OutboundReplier) post(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, text string) error {
	row, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return errors.New("installation platform row unavailable")
	}
	creds, err := decodeCredentials(row.Config, r.decrypt)
	if err != nil {
		return fmt.Errorf("decode credentials: %w", err)
	}
	// The replier runs in the inbound message's own context, so the
	// context_token is still on the message's Raw. Echo it back so the reply is
	// associated with the conversation.
	raw := decodeWechatRaw(msg)
	if raw.ContextToken == "" {
		return errors.New("wechat replier: inbound message has no context_token")
	}
	// Reply target: for a p2p chat, the sender; for a group, the chat (group) id.
	peer := msg.Source.SenderID
	if msg.Source.ChatType == channel.ChatTypeGroup {
		peer = msg.Source.ChatID
	}
	if _, err := r.client.sendMessage(ctx, creds.BotToken, creds.BaseURL, raw.ContextToken, peer, text); err != nil {
		return fmt.Errorf("post wechat reply: %w", err)
	}
	return nil
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
