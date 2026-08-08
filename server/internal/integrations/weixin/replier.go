package weixin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type OutboundReplier struct {
	senders *SendersRegistry
	logger  *slog.Logger
}

func NewOutboundReplier(senders *SendersRegistry, logger *slog.Logger) *OutboundReplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboundReplier{senders: senders, logger: logger}
}

func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, result engine.Result) {
	var text string
	switch result.Outcome {
	case engine.OutcomeNeedsBinding:
		text = "这个微信账号无权使用该私人智能体。请让绑定者重新扫码连接。"
	case engine.OutcomeAgentOffline:
		text = "智能体当前离线，消息已收到。"
	case engine.OutcomeAgentArchived:
		text = "该智能体已归档，暂时无法回复。"
	case engine.OutcomeIngested:
		if result.IssueID.Valid {
			text = "已创建 " + result.IssueIdentifier
			if result.IssueTitle != "" {
				text += " — " + result.IssueTitle
			}
		}
	}
	if text == "" || r.senders == nil {
		return
	}
	sender := r.senders.get(inst.ID)
	if sender == nil {
		return
	}
	if _, err := sender.send(ctx, msg.Source.ChatID, text); err != nil {
		r.logger.WarnContext(ctx, "weixin: outcome reply failed", "error", err, "installation_id", fmt.Sprint(inst.ID))
	}
}
