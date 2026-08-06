package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Outbound intent templates stored in queue payload (spec §5.3). Raw binding
// tokens are never persisted — the consumer mints them at write time (Task 10).
const (
	templateBindingPrompt      = "binding_prompt"
	templateBindingPromptGroup = "binding_prompt_group"
	templateAgentOffline       = "agent_offline"
	templateAgentArchived      = "agent_archived"
	templateIssueCreated       = "issue_created"
)

type channelOutboundEnqueuer interface {
	EnqueueChannelOutbound(ctx context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error)
}

// OutboundReplier enqueues credential-free intents onto channel_outbound_queue
// and wakes the local WS consumer (spec §5.3 unified outbound).
type OutboundReplier struct {
	q    channelOutboundEnqueuer
	wake *OutboundWakeRegistry
	log  *slog.Logger
}

// OutboundReplierConfig wires the replier. Wake may be nil (cross-replica poll
// still delivers, just slower).
type OutboundReplierConfig struct {
	Queries channelOutboundEnqueuer
	Wake    *OutboundWakeRegistry
	Logger  *slog.Logger
}

var _ engine.OutboundReplier = (*OutboundReplier)(nil)

// NewOutboundReplier builds the replier.
func NewOutboundReplier(cfg OutboundReplierConfig) *OutboundReplier {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboundReplier{q: cfg.Queries, wake: cfg.Wake, log: logger}
}

// Reply routes each outcome to a queued intent. Errors are logged, not
// propagated — the replier runs detached from the inbound ACK path.
func (r *OutboundReplier) Reply(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, res engine.Result) {
	if r.q == nil {
		return
	}
	var err error
	switch res.Outcome {
	case engine.OutcomeNeedsBinding:
		err = r.enqueueBindingPrompt(ctx, inst, msg)
	case engine.OutcomeAgentOffline:
		err = r.enqueueIntent(ctx, inst, msg, "agent_offline", map[string]string{"template": templateAgentOffline})
	case engine.OutcomeAgentArchived:
		err = r.enqueueIntent(ctx, inst, msg, "agent_archived", map[string]string{"template": templateAgentArchived})
	case engine.OutcomeIngested:
		if res.IssueID.Valid {
			err = r.enqueueIntent(ctx, inst, msg, "issue_created", map[string]any{
				"template":         templateIssueCreated,
				"issue_id":         util.UUIDToString(res.IssueID),
				"issue_identifier": res.IssueIdentifier,
				"issue_number":     res.IssueNumber,
				"issue_title":      res.IssueTitle,
			})
		}
	}
	if err != nil {
		r.log.WarnContext(ctx, "wecom replier: enqueue failed",
			"installation_id", util.UUIDToString(inst.ID),
			"outcome", string(res.Outcome),
			"error", err,
		)
	}
}

func (r *OutboundReplier) enqueueBindingPrompt(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) error {
	if msg.Source.ChatType == channel.ChatTypeGroup {
		// Group unbound: no token — guide user to DM (spec §5.3 / §6.2).
		return r.enqueueIntent(ctx, inst, msg, "binding_prompt", map[string]string{
			"template": templateBindingPromptGroup,
		})
	}
	// DM unbound: credential-free intent; consumer mints token at send time.
	return r.enqueueIntent(ctx, inst, msg, "binding_prompt", map[string]string{
		"template": templateBindingPrompt,
	})
}

func (r *OutboundReplier) enqueueIntent(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sourceKind string, payload any) error {
	targetID, targetType, err := OutboundTarget(msg)
	if err != nil {
		return err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	_, err = r.q.EnqueueChannelOutbound(ctx, db.EnqueueChannelOutboundParams{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		ChannelType:    string(TypeWecom),
		SourceKind:     sourceKind,
		SourceID:       msg.MessageID,
		TargetChatID:   targetID,
		TargetChatType: targetType,
		MsgType:        "markdown",
		Payload:        rawPayload,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("enqueue outbound: %w", err)
	}
	if r.wake != nil {
		r.wake.Wake(util.UUIDToString(inst.ID))
	}
	return nil
}
