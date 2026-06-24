package wecom

import (
	"context"
	"log/slog"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type OutcomeReplier interface {
	Reply(ctx context.Context, inst db.WecomInstallation, msg InboundMessage, res DispatchResult)
}

type noopReplier struct {
	log *slog.Logger
}

func (n *noopReplier) Reply(ctx context.Context, inst db.WecomInstallation, msg InboundMessage, res DispatchResult) {
	switch res.Outcome {
	case OutcomeNeedsBinding, OutcomeAgentOffline, OutcomeAgentArchived:
		n.log.Warn("wecom outcome replier: reply skipped (noop)", "outcome", res.Outcome)
	}
}

func NewNoopOutcomeReplier(log *slog.Logger) OutcomeReplier {
	if log == nil {
		log = slog.Default()
	}
	return &noopReplier{log: log}
}

type WecomOutcomeReplier struct {
	bindingSvc  *BindingTokenService
	publicURL   string
	bindingPath string
	log         *slog.Logger
}

type OutcomeReplierConfig struct {
	BindingSvc  *BindingTokenService
	PublicURL   string
	BindingPath string
	Logger      *slog.Logger
}

func NewWecomOutcomeReplier(cfg OutcomeReplierConfig) OutcomeReplier {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.BindingSvc == nil {
		return NewNoopOutcomeReplier(log)
	}
	path := strings.TrimSpace(cfg.BindingPath)
	if path == "" {
		path = "/wecom/bind"
	}
	return &WecomOutcomeReplier{
		bindingSvc:  cfg.BindingSvc,
		publicURL:   strings.TrimRight(strings.TrimSpace(cfg.PublicURL), "/"),
		bindingPath: path,
		log:         log,
	}
}

func (r *WecomOutcomeReplier) Reply(ctx context.Context, inst db.WecomInstallation, msg InboundMessage, res DispatchResult) {
	// BindingLink is minted synchronously in Hub.enrichBindingLink and
	// sent by the WS adapter; nothing else to do here for MVP.
	if res.Outcome == OutcomeNeedsBinding && res.BindingLink != "" {
		r.log.Debug("wecom outcome replier: binding link already sent", "installation_id", inst.ID)
	}
}
