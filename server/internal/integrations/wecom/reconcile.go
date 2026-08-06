package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	reconcileOverlapWindow   = 5 * time.Minute
	reconcileInitialLookback = 24 * time.Hour
	reconcilePageSize        = 100
	reconcilePollInterval    = 2 * time.Second
	reconcilePurgeInterval   = time.Hour
	sentQueueRetention       = 24 * time.Hour
	failedQueueRetention     = 7 * 24 * time.Hour
	sendAttemptRetention     = 24 * time.Hour
)

type reconcileStore interface {
	ClaimChannelOutboundReconcileState(ctx context.Context, arg db.ClaimChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error)
	ListWecomOutboundReconcileCandidates(ctx context.Context, arg db.ListWecomOutboundReconcileCandidatesParams) ([]db.ListWecomOutboundReconcileCandidatesRow, error)
	AdvanceChannelOutboundReconcileState(ctx context.Context, arg db.AdvanceChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error)
	ReleaseChannelOutboundReconcileState(ctx context.Context, arg db.ReleaseChannelOutboundReconcileStateParams) error
	FailUndeliverableChannelOutbound(ctx context.Context) error
	PurgeChannelOutboundSendAttemptsBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	PurgeSentChannelOutboundQueueBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	PurgeFailedChannelOutboundQueueBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
	GetChatMessageByTaskAssistant(ctx context.Context, taskID pgtype.UUID) (db.ChatMessage, error)
	EnqueueChannelOutbound(ctx context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error)
}

// OutboundReconciler compensates for missed events.Bus enqueues (spec §5.3).
type OutboundReconciler struct {
	q          reconcileStore
	wake       *OutboundWakeRegistry
	enqueueOK  func() bool
	metrics    WecomMetrics
	logger     *slog.Logger
	pollEvery  time.Duration
	purgeEvery time.Duration
	done       chan struct{}
	now        func() time.Time
}

// OutboundReconcilerConfig wires the global reconciler worker.
type OutboundReconcilerConfig struct {
	Queries    reconcileStore
	Wake       *OutboundWakeRegistry
	EnqueueOK  func() bool
	Metrics    WecomMetrics
	Logger     *slog.Logger
	PollEvery  time.Duration
	PurgeEvery time.Duration
	Now        func() time.Time
}

// NewOutboundReconciler builds the worker. EnqueueOK gates compensating INSERTs
// when the integration runs in maintenance mode (spec §7.1.1).
func NewOutboundReconciler(cfg OutboundReconcilerConfig) *OutboundReconciler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	poll := cfg.PollEvery
	if poll == 0 {
		poll = reconcilePollInterval
	}
	purge := cfg.PurgeEvery
	if purge == 0 {
		purge = reconcilePurgeInterval
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	enqueueOK := cfg.EnqueueOK
	if enqueueOK == nil {
		enqueueOK = func() bool { return true }
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NoopMetrics()
	}
	return &OutboundReconciler{
		q:          cfg.Queries,
		wake:       cfg.Wake,
		enqueueOK:  enqueueOK,
		metrics:    metrics,
		logger:     logger,
		pollEvery:  poll,
		purgeEvery: purge,
		done:       make(chan struct{}),
		now:        now,
	}
}

// Run is the worker main loop (spec §7.1.1).
func (r *OutboundReconciler) Run(ctx context.Context) {
	if r == nil {
		return
	}
	defer close(r.done)
	if r.q == nil {
		return
	}
	poll := time.NewTicker(r.pollEvery)
	defer poll.Stop()
	purge := time.NewTicker(r.purgeEvery)
	defer purge.Stop()

	r.sweep(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			r.sweep(ctx)
		case <-purge.C:
			if err := r.purge(ctx); err != nil && !errors.Is(err, context.Canceled) {
				r.logger.Warn("wecom reconciler: purge", "error", err)
			}
		}
	}
}

// WaitWithTimeout blocks until Run returns or timeout elapses.
func (r *OutboundReconciler) WaitWithTimeout(timeout time.Duration) bool {
	if r == nil {
		return true
	}
	if timeout <= 0 {
		<-r.done
		return true
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-r.done:
		return true
	case <-t.C:
		return false
	}
}

func (r *OutboundReconciler) sweep(ctx context.Context) {
	if err := r.q.FailUndeliverableChannelOutbound(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Warn("wecom reconciler: fail undeliverable", "error", err)
	}

	now := r.now()
	initialCursor := now.Add(-reconcileInitialLookback)
	state, err := r.q.ClaimChannelOutboundReconcileState(ctx, db.ClaimChannelOutboundReconcileStateParams{
		ChannelType: string(TypeWecom),
		CursorAt:    pgtype.Timestamptz{Time: initialCursor, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		r.logger.Warn("wecom reconciler: claim state", "error", err)
		return
	}
	if !state.LeaseToken.Valid {
		return
	}
	lease := state.LeaseToken.String

	windowEnd := now.Add(-30 * time.Second)
	windowStart := state.CursorAt.Time.Add(-reconcileOverlapWindow)
	if !state.CursorAt.Valid {
		windowStart = initialCursor.Add(-reconcileOverlapWindow)
	}

	if !r.enqueueOK() {
		if _, err := r.q.AdvanceChannelOutboundReconcileState(ctx, db.AdvanceChannelOutboundReconcileStateParams{
			ChannelType: string(TypeWecom),
			CursorAt:    pgtype.Timestamptz{Time: windowEnd, Valid: true},
			LeaseToken:  pgtype.Text{String: lease, Valid: true},
		}); err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Warn("wecom reconciler: advance disabled", "error", err)
		}
		return
	}

	if err := r.scanWindow(ctx, windowStart, windowEnd); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Warn("wecom reconciler: scan window", "error", err)
		_ = r.q.ReleaseChannelOutboundReconcileState(ctx, db.ReleaseChannelOutboundReconcileStateParams{
			ChannelType: string(TypeWecom),
			LeaseToken:  pgtype.Text{String: lease, Valid: true},
		})
		return
	}

	if _, err := r.q.AdvanceChannelOutboundReconcileState(ctx, db.AdvanceChannelOutboundReconcileStateParams{
		ChannelType: string(TypeWecom),
		CursorAt:    pgtype.Timestamptz{Time: windowEnd, Valid: true},
		LeaseToken:  pgtype.Text{String: lease, Valid: true},
	}); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Warn("wecom reconciler: advance cursor", "error", err)
	}
}

func (r *OutboundReconciler) scanWindow(ctx context.Context, windowStart, windowEnd time.Time) error {
	var afterCompleted pgtype.Timestamptz
	var afterTaskID pgtype.UUID
	for {
		rows, err := r.q.ListWecomOutboundReconcileCandidates(ctx, db.ListWecomOutboundReconcileCandidatesParams{
			WindowStart:      pgtype.Timestamptz{Time: windowStart, Valid: true},
			WindowEnd:        pgtype.Timestamptz{Time: windowEnd, Valid: true},
			AfterCompletedAt: afterCompleted,
			AfterTaskID:      afterTaskID,
			Limit:            reconcilePageSize,
		})
		if err != nil {
			return fmt.Errorf("list candidates: %w", err)
		}
		for _, row := range rows {
			if err := r.reconcileCandidate(ctx, row); err != nil {
				return err
			}
		}
		if len(rows) < reconcilePageSize {
			return nil
		}
		last := rows[len(rows)-1]
		afterCompleted = last.CompletedAt
		afterTaskID = last.TaskID
	}
}

func (r *OutboundReconciler) reconcileCandidate(ctx context.Context, row db.ListWecomOutboundReconcileCandidatesRow) error {
	task, err := r.q.GetAgentTask(ctx, row.TaskID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}
	deliver, err := engine.TaskInputIsChannelIngested(ctx, r.q, task)
	if err != nil {
		return fmt.Errorf("provenance: %w", err)
	}
	if !deliver {
		return nil
	}
	binding, err := r.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: row.ChatSessionID,
		ChannelType:   string(TypeWecom),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("binding: %w", err)
	}
	inst, err := r.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeWecom),
	})
	if err != nil || inst.Status != "active" {
		return nil
	}
	targetID, targetType, err := targetFromBinding(binding)
	if err != nil {
		return nil
	}

	var payload []byte
	sourceKind := sourceKindTaskFailed
	switch row.TaskStatus {
	case "completed":
		sourceKind = sourceKindChatDone
		msg, err := r.q.GetChatMessageByTaskAssistant(ctx, row.TaskID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("assistant message: %w", err)
		}
		if strings.TrimSpace(msg.Content) == "" {
			return nil
		}
		payload, err = json.Marshal(map[string]string{"content": msg.Content})
		if err != nil {
			return fmt.Errorf("marshal chat_done: %w", err)
		}
	case "failed":
		agentName := ""
		if agent, err := r.q.GetAgent(ctx, inst.AgentID); err == nil {
			agentName = agent.Name
		}
		reason := ""
		if row.FailureReason.Valid {
			reason = row.FailureReason.String
		}
		payload, err = json.Marshal(map[string]string{
			"template":       "task_failed",
			"failure_reason": reason,
			"agent_name":     agentName,
		})
		if err != nil {
			return fmt.Errorf("marshal task_failed: %w", err)
		}
	default:
		return nil
	}

	_, err = r.q.EnqueueChannelOutbound(ctx, db.EnqueueChannelOutboundParams{
		InstallationID: inst.ID,
		WorkspaceID:    inst.WorkspaceID,
		ChannelType:    string(TypeWecom),
		ChatSessionID:  binding.ChatSessionID,
		SourceKind:     sourceKind,
		SourceID:       util.UUIDToString(row.TaskID),
		TargetChatID:   targetID,
		TargetChatType: targetType,
		MsgType:        "markdown",
		Payload:        payload,
	})
	switch {
	case err == nil:
		// A fresh insert here means the realtime path never enqueued this
		// reply: the candidate query already excludes tasks that have a queue
		// row. This counter is the alerting signal for a broken fast path,
		// because the reconciler's window lags on purpose and the user is
		// therefore waiting tens of seconds longer than they should.
		r.metrics.RecordOutboundEnqueued(enqueuePathReconcile, sourceKind)
		r.logger.WarnContext(ctx, "wecom reconciler: rescued a reply the realtime path missed",
			"task_id", util.UUIDToString(row.TaskID),
			"source_kind", sourceKind,
		)
	case errors.Is(err, pgx.ErrNoRows):
		// Business-key conflict: the fast path won the race between the
		// candidate scan and this insert. Expected, not a problem.
		r.metrics.RecordReconcileRaceLost()
	default:
		return fmt.Errorf("enqueue: %w", err)
	}
	if r.wake != nil {
		r.wake.Wake(util.UUIDToString(inst.ID))
	}
	return nil
}

func (r *OutboundReconciler) purge(ctx context.Context) error {
	now := r.now()
	if err := r.q.PurgeChannelOutboundSendAttemptsBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-sendAttemptRetention), Valid: true,
	}); err != nil {
		return err
	}
	if err := r.q.PurgeSentChannelOutboundQueueBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-sentQueueRetention), Valid: true,
	}); err != nil {
		return err
	}
	return r.q.PurgeFailedChannelOutboundQueueBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-failedQueueRetention), Valid: true,
	})
}
