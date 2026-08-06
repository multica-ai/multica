package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	sourceKindChatDone   = "chat_done"
	sourceKindTaskFailed = "task_failed"
	maxOutboundAttempts  = 8
)

type outboundQueries interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetChatMessageByTaskAssistant(ctx context.Context, taskID pgtype.UUID) (db.ChatMessage, error)
	EnqueueChannelOutbound(ctx context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error)
}

// Outbound enqueues terminal task replies onto channel_outbound_queue (spec §5.3).
type Outbound struct {
	q       outboundQueries
	wake    *OutboundWakeRegistry
	metrics WecomMetrics
	logger  *slog.Logger
}

// OutboundConfig wires the fast path subscriber.
type OutboundConfig struct {
	Queries outboundQueries
	Wake    *OutboundWakeRegistry
	Metrics WecomMetrics
	Logger  *slog.Logger
}

// NewOutbound builds the events.Bus subscriber.
func NewOutbound(cfg OutboundConfig) *Outbound {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NoopMetrics()
	}
	return &Outbound{q: cfg.Queries, wake: cfg.Wake, metrics: metrics, logger: logger}
}

// Register subscribes to chat completion and failure events.
func (o *Outbound) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, o.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, o.handleEvent)
}

func (o *Outbound) handleEvent(e events.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := o.processEvent(ctx, e); err != nil {
		o.logger.WarnContext(ctx, "wecom outbound: enqueue failed",
			"event_type", e.Type,
			"task_id", e.TaskID,
			"chat_session_id", e.ChatSessionID,
			"error", err,
		)
	}
}

func (o *Outbound) processEvent(ctx context.Context, e events.Event) error {
	if o.q == nil {
		return nil
	}
	taskID, ok := taskIDFromEvent(e)
	if !ok {
		return nil
	}
	task, err := o.q.GetAgentTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load agent task: %w", err)
	}
	// The chat session comes from the task row, not the event envelope:
	// chat:done carries it on the envelope but task:failed carries it nowhere.
	if !task.ChatSessionID.Valid {
		return nil
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, o.q, task)
	if err != nil {
		return fmt.Errorf("classify task origin: %w", err)
	}
	if !deliver {
		return nil
	}
	binding, err := o.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: task.ChatSessionID,
		ChannelType:   string(TypeWecom),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("lookup wecom chat binding: %w", err)
	}
	inst, err := o.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeWecom),
	})
	if err != nil {
		return fmt.Errorf("load installation: %w", err)
	}
	if inst.Status != "active" {
		return nil
	}
	targetID, targetType, err := targetFromBinding(binding)
	if err != nil {
		return err
	}

	switch e.Type {
	case protocol.EventChatDone:
		content := chatDoneContent(e.Payload)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		payload, err := json.Marshal(map[string]string{"content": content})
		if err != nil {
			return fmt.Errorf("marshal chat_done payload: %w", err)
		}
		return o.enqueue(ctx, inst, binding, taskID, sourceKindChatDone, targetID, targetType, payload)
	case protocol.EventTaskFailed:
		agentName := ""
		if agent, err := o.q.GetAgent(ctx, inst.AgentID); err == nil {
			agentName = agent.Name
		}
		reason := ""
		if task.FailureReason.Valid {
			reason = task.FailureReason.String
		}
		payload, err := json.Marshal(map[string]string{
			"template":       "task_failed",
			"failure_reason": reason,
			"agent_name":     agentName,
		})
		if err != nil {
			return fmt.Errorf("marshal task_failed payload: %w", err)
		}
		return o.enqueue(ctx, inst, binding, taskID, sourceKindTaskFailed, targetID, targetType, payload)
	default:
		return nil
	}
}

func (o *Outbound) enqueue(ctx context.Context, inst db.ChannelInstallation, binding db.ChannelChatSessionBinding, taskID pgtype.UUID, sourceKind, targetID string, targetType int16, payload []byte) error {
	_, err := o.q.EnqueueChannelOutbound(ctx, db.EnqueueChannelOutboundParams{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		ChannelType:    string(TypeWecom),
		ChatSessionID:  binding.ChatSessionID,
		SourceKind:     sourceKind,
		SourceID:       util.UUIDToString(taskID),
		TargetChatID:   targetID,
		TargetChatType: targetType,
		MsgType:        "markdown",
		Payload:        payload,
	})
	switch {
	case err == nil:
		// Only fresh inserts are counted, so the fast/reconcile split stays a
		// usable ratio: a business-key conflict means someone else already
		// enqueued this reply and counting it here would double-report.
		o.metrics.RecordOutboundEnqueued(enqueuePathFast, sourceKind)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("enqueue outbound: %w", err)
	}
	if o.wake != nil {
		o.wake.Wake(util.UUIDToString(inst.ID))
	}
	return nil
}

func targetFromBinding(binding db.ChannelChatSessionBinding) (string, int16, error) {
	if len(binding.Config) > 0 {
		var cfg wecomBindingConfig
		if err := json.Unmarshal(binding.Config, &cfg); err == nil && cfg.TargetChatID != "" {
			return cfg.TargetChatID, cfg.TargetChatType, nil
		}
	}
	return "", 0, errors.New("wecom outbound: binding config missing target")
}

// taskIDFromEvent resolves the task the event refers to. Envelope TaskID is
// preferred, but both publishers can leave it empty: broadcastChatDone only
// sets ChatSessionID and carries the task id inside a typed
// protocol.ChatDonePayload, while task:failed carries a map payload.
func taskIDFromEvent(e events.Event) (pgtype.UUID, bool) {
	raw := e.TaskID
	if raw == "" {
		switch p := e.Payload.(type) {
		case protocol.ChatDonePayload:
			raw = p.TaskID
		case map[string]any:
			raw, _ = p["task_id"].(string)
		}
	}
	if raw == "" {
		return pgtype.UUID{}, false
	}
	id, err := util.ParseUUID(raw)
	return id, err == nil && id.Valid
}

func chatDoneContent(payload any) string {
	switch p := payload.(type) {
	case protocol.ChatDonePayload:
		return p.Content
	case map[string]any:
		if s, ok := p["content"].(string); ok {
			return s
		}
	}
	return ""
}

func outboundBackoff(attempt int32) time.Duration {
	const (
		base = 2 * time.Second
		cap  = 5 * time.Minute
	)
	exp := int(attempt)
	if exp > 10 {
		exp = 10
	}
	max := base * time.Duration(1<<exp)
	if max > cap {
		max = cap
	}
	if max <= 0 {
		return base
	}
	return time.Duration(rand.Int63n(int64(max)))
}

func sanitizeLastError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	return msg
}
