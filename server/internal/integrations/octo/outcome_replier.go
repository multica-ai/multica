package octo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OutcomeReplier reacts to the Dispatcher's verdict by sending the appropriate
// reply back to Octo. It is the outbound half of the inbound pipeline that the
// Patcher does not own: the Patcher relays the agent's eventual chat reply
// (chat:done / task:failed), while the replier handles the synchronous,
// pre-agent outcomes — NeedsBinding (DM the unbound sender a one-shot binding
// link), AgentOffline / AgentArchived (tell the user the agent can't run).
// OutcomeIngested and OutcomeDropped produce no reply here.
//
// Reply is best-effort by design: a transient Octo outage MUST NOT fail the
// inbound pipeline. For NeedsBinding the message was not stored, and for the
// agent-unavailable outcomes the chat_message is already durable, so there is
// nothing to roll back. Errors are logged and swallowed; the next inbound
// message from the same user retries the reply on its own.
type OutcomeReplier interface {
	Reply(ctx context.Context, inst db.OctoInstallation, msg InboundMessage, res DispatchResult)
}

// BindingMinter mints a single-use binding token. Satisfied by
// *BindingTokenService; an interface so the replier is unit-testable without a
// DB.
type BindingMinter interface {
	Mint(ctx context.Context, workspaceID, installationID pgtype.UUID, uid UID) (BindingToken, error)
}

// noopReplier is the safe default when the integration is wired without the
// dependencies the production replier needs (no binding service, no public
// URL). It logs each outcome that would have produced a reply so the gap is
// visible in production logs.
type noopReplier struct {
	log *slog.Logger
}

func (n *noopReplier) Reply(_ context.Context, inst db.OctoInstallation, msg InboundMessage, res DispatchResult) {
	switch res.Outcome {
	case OutcomeNeedsBinding, OutcomeAgentOffline, OutcomeAgentArchived:
		n.log.Warn("octo outcome replier: outbound reply skipped (replier not wired)",
			"outcome", string(res.Outcome),
			"installation_id", uuidString(inst.ID),
			"channel_id", string(msg.ChannelID),
			"sender_uid", string(res.SenderUID),
		)
	}
}

// NewNoopOutcomeReplier returns the no-op replier, used as the fallback when
// production wiring is incomplete.
func NewNoopOutcomeReplier(log *slog.Logger) OutcomeReplier {
	if log == nil {
		log = slog.Default()
	}
	return &noopReplier{log: log}
}

// octoOutcomeReplier is the production OutcomeReplier. It reuses the same
// MessageSender + TokenDecryptor the Patcher uses for outbound delivery, so a
// binding prompt is just a plain-text DM to the sender's uid (Octo renders
// markdown natively — no interactive cards like Lark).
type octoOutcomeReplier struct {
	minter      BindingMinter
	decryptor   TokenDecryptor
	sender      MessageSender
	publicURL   string // e.g. https://multica.example, trailing slash trimmed
	bindingPath string // path component of the binding URL, default "/octo/bind"
	log         *slog.Logger
}

// OutcomeReplierConfig wires the production replier. PublicURL is the Multica
// HTTP host the user clicks into to redeem the binding token; empty means the
// binding flow can only log the uid, not produce a clickable link.
type OutcomeReplierConfig struct {
	Minter      BindingMinter
	Decryptor   TokenDecryptor
	Sender      MessageSender
	PublicURL   string
	BindingPath string
	Logger      *slog.Logger
}

// NewOutcomeReplier validates the configuration and returns the production
// replier. Missing dependencies fall back to noop so the boot path stays robust
// on partially-configured deployments.
func NewOutcomeReplier(cfg OutcomeReplierConfig) OutcomeReplier {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.Minter == nil || cfg.Decryptor == nil || cfg.Sender == nil {
		return NewNoopOutcomeReplier(log)
	}
	if cfg.PublicURL == "" {
		log.Warn("octo outcome replier: MULTICA_PUBLIC_URL not set; binding link will not work")
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/octo/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	return &octoOutcomeReplier{
		minter:      cfg.Minter,
		decryptor:   cfg.Decryptor,
		sender:      cfg.Sender,
		publicURL:   strings.TrimRight(cfg.PublicURL, "/"),
		bindingPath: bindingPath,
		log:         log,
	}
}

// Reply implements OutcomeReplier. The switch is the SOURCE OF TRUTH for which
// outcomes generate a reply; a missing branch silently drops the user-visible
// side effect.
func (r *octoOutcomeReplier) Reply(ctx context.Context, inst db.OctoInstallation, msg InboundMessage, res DispatchResult) {
	switch res.Outcome {
	case OutcomeNeedsBinding:
		if err := r.sendBindingPrompt(ctx, inst, res); err != nil {
			r.log.Warn("octo outcome replier: binding prompt failed",
				"installation_id", uuidString(inst.ID),
				"sender_uid", string(res.SenderUID),
				"err", err.Error(),
			)
		}
	case OutcomeAgentOffline:
		if err := r.sendDM(ctx, inst, msg.ChannelID, msg.ChannelType, agentOfflineCopy); err != nil {
			r.log.Warn("octo outcome replier: offline notice failed",
				"installation_id", uuidString(inst.ID),
				"channel_id", string(msg.ChannelID),
				"err", err.Error(),
			)
		}
	case OutcomeAgentArchived:
		if err := r.sendDM(ctx, inst, msg.ChannelID, msg.ChannelType, agentArchivedCopy); err != nil {
			r.log.Warn("octo outcome replier: archived notice failed",
				"installation_id", uuidString(inst.ID),
				"channel_id", string(msg.ChannelID),
				"err", err.Error(),
			)
		}
	case OutcomeIngested, OutcomeDropped:
		// Ingested replies flow through the Patcher (agent output); Dropped is
		// silent.
	}
}

// sendBindingPrompt mints a one-shot token and DMs the unbound sender a link to
// redeem it. The DM goes to the sender's own uid as a 1:1 channel — even when
// the triggering message arrived in a group, the binding prompt is private so a
// group is never spammed with binding links.
func (r *octoOutcomeReplier) sendBindingPrompt(ctx context.Context, inst db.OctoInstallation, res DispatchResult) error {
	if res.SenderUID == "" {
		return errors.New("missing sender uid")
	}
	if r.publicURL == "" {
		return errors.New("public_url not configured")
	}
	token, err := r.minter.Mint(ctx, inst.WorkspaceID, inst.ID, res.SenderUID)
	if err != nil {
		return fmt.Errorf("mint binding token: %w", err)
	}
	bindURL := r.publicURL + r.bindingPath + "?token=" + url.QueryEscape(token.Raw)
	return r.sendDM(ctx, inst, ChannelID(res.SenderUID), ChannelDM, bindingPromptText(bindURL))
}

// sendDM decrypts the installation's bot token and sends content to the given
// channel. Used for both the binding prompt (sender's DM channel) and the
// agent-unavailable notices (the originating channel).
func (r *octoOutcomeReplier) sendDM(ctx context.Context, inst db.OctoInstallation, channelID ChannelID, channelType ChannelType, content string) error {
	token, err := r.decryptor.DecryptBotToken(inst)
	if err != nil {
		return fmt.Errorf("decrypt bot token: %w", err)
	}
	if _, err := r.sender.Send(ctx, inst.ApiUrl, token, string(channelID), transport.ChannelType(channelType), content); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return nil
}

// bindingPromptText is the user-facing copy DMed to an unbound sender. The link
// is on its own line so Octo's auto-linker turns it into a tappable URL.
func bindingPromptText(bindURL string) string {
	return "你还没有绑定 Multica 账号，无法处理你的消息。\n点击下面的链接完成绑定（15 分钟内有效）：\n" + bindURL
}

// agentOfflineCopy and agentArchivedCopy are the user-visible strings for the
// two agent-unavailability outcomes. An offline agent will run when its daemon
// reconnects; an archived agent needs operator action.
const (
	agentOfflineCopy  = "Agent 当前离线，消息已记录。下次 daemon 上线后会自动继续处理。"
	agentArchivedCopy = "这个 Agent 已被归档，无法继续处理消息。请联系工作区管理员恢复或重新绑定。"
)
