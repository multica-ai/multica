package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
)

// This file is the DingTalk OutboundReplier — the engine seam that delivers
// a verdict-driven reply back to the user. Unlike the Slack/Lark repliers it
// posts through the inbound message's sessionWebhook (carried in Raw): the
// webhook needs no access token and no API permission, and a verdict reply
// always follows an inbound message, so the webhook is guaranteed fresh
// (~90-minute validity, minted per message).
//
// Outcomes handled:
//   - NeedsBinding: mint a single-use binding token and reply with a
//     "link your account" prompt pointing at the in-product redeem page.
//   - AgentOffline / AgentArchived: a status notice.
//   - Ingested with an /issue created: a confirmation of the new issue.

const (
	agentOfflineText  = "⚠️ 智能体当前离线。你的消息已收到，上线后会继续处理。"
	agentArchivedText = "⚠️ 该智能体已归档，无法响应。请联系工作区管理员。"
	agentBusyText     = "⏳ 智能体正在处理其他任务，你的消息已排队，空出来后会立即处理。"
	unboundText       = "✅ 已解除你的钉钉账号与 Multica 的绑定。之后再发消息会重新识别身份。"
	unboundMissText   = "当前钉钉账号没有绑定记录，无需解绑。"
)

// bindingMinter is the binding-token surface the replier needs.
// *BindingTokenService satisfies it.
type bindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, dingTalkUserID string) (BindingToken, error)
}

// OutboundReplier implements engine.OutboundReplier for DingTalk.
type OutboundReplier struct {
	binding     bindingMinter
	httpClient  *http.Client
	appURL      string
	bindingPath string
	logger      *slog.Logger
}

// OutboundReplierConfig configures the replier. Binding + AppURL are
// required for the NeedsBinding prompt to work; without them the prompt is
// skipped (the offline/archived/issue notices still fire).
type OutboundReplierConfig struct {
	Binding bindingMinter
	// AppURL is the Multica web app host the user clicks into to redeem the
	// binding token (MULTICA_APP_URL ?? FRONTEND_ORIGIN — the bind page
	// /dingtalk/bind is served by the web app, not the API host).
	AppURL      string
	BindingPath string // default "/dingtalk/bind"
	HTTPClient  *http.Client
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
		bindingPath = "/dingtalk/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &OutboundReplier{
		binding:     cfg.Binding,
		httpClient:  httpClient,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
}

// Reply routes each outcome to its user-visible message. Errors are logged,
// not propagated: the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, msg, res); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: binding prompt failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentOffline:
		if err := r.post(ctx, msg, agentOfflineText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: offline notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentArchived:
		if err := r.post(ctx, msg, agentArchivedText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: archived notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeAgentBusy:
		if err := r.post(ctx, msg, agentBusyText); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: busy notice failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeUnbound:
		text := unboundText
		if !res.UnbindExisted {
			text = unboundMissText
		}
		if err := r.post(ctx, msg, text); err != nil {
			r.logger.WarnContext(ctx, "dingtalk replier: unbind confirmation failed",
				"installation_id", util.UUIDToString(inst.ID), "error", err)
		}
	case engine.OutcomeIngested:
		// Only a /issue-created message warrants a confirmation; a plain chat
		// message stays silent (the agent's own reply lands via EventChatDone).
		if res.IssueID.Valid {
			if err := r.post(ctx, msg, issueCreatedText(res)); err != nil {
				r.logger.WarnContext(ctx, "dingtalk replier: issue-created confirmation failed",
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
	text := "👋 要开始与我对话，请先将你的钉钉账号绑定到 Multica：[点击绑定](" +
		bindURL + ")\n\n（链接 15 分钟内有效）"
	return r.post(ctx, msg, text)
}

// post delivers text through the inbound message's session webhook.
func (r *OutboundReplier) post(ctx context.Context, msg channel.InboundMessage, text string) error {
	raw, err := decodeDingTalkRaw(msg)
	if err != nil {
		return fmt.Errorf("decode raw: %w", err)
	}
	if raw.SessionWebhook == "" {
		return errors.New("inbound message carries no session webhook")
	}
	return postSessionWebhook(ctx, r.httpClient, raw.SessionWebhook, text)
}

// postSessionWebhook posts a markdown message to a bot-message session
// webhook. The webhook accepts the custom-robot message format and needs
// no authentication beyond the URL itself.
func postSessionWebhook(ctx context.Context, httpClient *http.Client, webhook, text string) error {
	body, err := json.Marshal(map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": markdownTitle(text),
			"text":  text,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal webhook message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("new webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Status: resp.StatusCode, Code: "session_webhook_failed", Message: strings.TrimSpace(truncate(string(payload), 256))}
	}
	// The webhook returns 200 with an errcode envelope; a non-zero code is
	// a delivery failure even under HTTP 200.
	var env registrationEnvelope
	if err := json.Unmarshal(payload, &env); err == nil {
		if envErr := env.err(); envErr != nil {
			return &APIError{Code: envErr.Code, Message: envErr.Description}
		}
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
