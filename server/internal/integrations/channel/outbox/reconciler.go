package outbox

import (
	"context"
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
	// reconcileOverlapWindow re-scans a little of the previous window on every
	// pass, so a task that committed just before the cursor advanced is not
	// missed at the seam.
	reconcileOverlapWindow = 5 * time.Minute

	// reconcileInitialLookback is the ceiling on how far back any scan reaches.
	// Older terminal tasks are deliberately not resurrected: delivering a
	// day-old reply into a live chat is worse than not delivering it. It is not
	// a seed for a fresh cursor — see the created_at floor in sweep, and the
	// column comment on channel_outbound_reconcile_state.
	reconcileInitialLookback = 24 * time.Hour

	// reconcileSettleDelay keeps the scan window's trailing edge behind now().
	// The realtime producer needs a moment to insert its row; without this lag
	// the reconciler would race it on every freshly finished task and turn the
	// idempotency conflict into the common case.
	reconcileSettleDelay = 30 * time.Second

	reconcilePageSize      = 100
	reconcilePollInterval  = 2 * time.Second
	reconcilePurgeInterval = time.Hour

	// Retention for terminal rows. Sent rows only need to outlast the
	// reconciler's lookback, since their remaining job is to suppress a
	// re-enqueue of the same business key. Dead letters are kept longer
	// because they are the record of what failed and why.
	sentRetention   = 24 * time.Hour
	failedRetention = 7 * 24 * time.Hour

	// sendAttemptRetention bounds the rate gate's ledger. It only has to
	// outlast the widest window any gate looks back over; the rows are a
	// sliding-window count, not history.
	sendAttemptRetention = 24 * time.Hour
)

// Candidate is one terminal task the reconciler found without a queue row.
type Candidate struct {
	TaskID         pgtype.UUID
	ChatSessionID  pgtype.UUID
	TaskStatus     string
	CompletedAt    pgtype.Timestamptz
	FailureReason  pgtype.Text
	InstallationID pgtype.UUID
	WorkspaceID    pgtype.UUID
	ChannelType    string
}

// PayloadBuilder turns a reconcile candidate into an enqueueable row. It is
// the channel's half of the reconciler: resolving the delivery target from its
// own binding config, and rendering the payload its Sender will later read.
type PayloadBuilder interface {
	// SourceKinds are the source_kind values this channel enqueues for
	// terminal tasks. The candidate scan uses them to skip tasks that already
	// have a row, so this must list every kind the realtime producer writes
	// for a terminal task — a missing kind makes the reconciler re-enqueue
	// replies that were already delivered under a different name.
	SourceKinds() []string

	// Build resolves one candidate. ok=false skips it, which is ordinary:
	// an empty reply, an unbound or revoked target, or a task status this
	// channel does not notify on. A non-nil error aborts the whole scan
	// without advancing the cursor, so the same window is retried.
	Build(ctx context.Context, cand Candidate) (req Request, ok bool, err error)
}

// ReconcilerStore is the generated-query surface the Reconciler needs.
type ReconcilerStore interface {
	ClaimChannelOutboundReconcileState(ctx context.Context, arg db.ClaimChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error)
	ListChannelOutboundReconcileCandidates(ctx context.Context, arg db.ListChannelOutboundReconcileCandidatesParams) ([]db.ListChannelOutboundReconcileCandidatesRow, error)
	AdvanceChannelOutboundReconcileState(ctx context.Context, arg db.AdvanceChannelOutboundReconcileStateParams) (db.ChannelOutboundReconcileState, error)
	ReleaseChannelOutboundReconcileState(ctx context.Context, arg db.ReleaseChannelOutboundReconcileStateParams) error
	FailUndeliverableChannelOutbound(ctx context.Context) error
	PurgeSentChannelOutboundQueueBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	PurgeFailedChannelOutboundQueueBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	PurgeChannelOutboundSendAttemptsBefore(ctx context.Context, cutoff pgtype.Timestamptz) error
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
}

// Reconciler compensates for replies the realtime producer never enqueued —
// the producing replica died between finishing the task and inserting the row.
// One instance runs per channel per deployment; the cursor's lease makes the
// scan single-writer across replicas.
type Reconciler struct {
	channelType string
	q           ReconcilerStore
	producer    *Producer
	builder     PayloadBuilder
	enqueueOK   func() bool
	metrics     Metrics
	logger      *slog.Logger
	pollEvery   time.Duration
	purgeEvery  time.Duration
	done        chan struct{}
	now         func() time.Time
}

// ReconcilerConfig wires the per-channel reconciler worker.
type ReconcilerConfig struct {
	ChannelType string
	Queries     ReconcilerStore
	Producer    *Producer
	Builder     PayloadBuilder

	// EnqueueOK gates the compensating inserts. When it returns false the
	// cursor still advances, so disabling the integration does not build a
	// backlog that floods users with stale replies when it is re-enabled.
	EnqueueOK func() bool

	Metrics    Metrics
	Logger     *slog.Logger
	PollEvery  time.Duration
	PurgeEvery time.Duration
	Now        func() time.Time
}

// NewReconciler builds the worker.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if strings.TrimSpace(cfg.ChannelType) == "" {
		return nil, errors.New("outbox: channel type is required")
	}
	if cfg.Queries == nil || cfg.Producer == nil || cfg.Builder == nil {
		return nil, errors.New("outbox: queries, producer and builder are required")
	}
	if len(cfg.Builder.SourceKinds()) == 0 {
		return nil, errors.New("outbox: builder must declare at least one source kind")
	}
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
	return &Reconciler{
		channelType: cfg.ChannelType,
		q:           cfg.Queries,
		producer:    cfg.Producer,
		builder:     cfg.Builder,
		enqueueOK:   enqueueOK,
		metrics:     metrics,
		logger:      logger,
		pollEvery:   poll,
		purgeEvery:  purge,
		done:        make(chan struct{}),
		now:         now,
	}, nil
}

// Run is the worker main loop.
func (r *Reconciler) Run(ctx context.Context) {
	if r == nil {
		return
	}
	defer close(r.done)

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
				r.logger.Warn("channel outbox reconciler: purge", "channel_type", r.channelType, "error", err)
			}
		}
	}
}

// WaitWithTimeout blocks until Run returns or timeout elapses, reporting
// whether Run finished. A zero timeout waits indefinitely.
func (r *Reconciler) WaitWithTimeout(timeout time.Duration) bool {
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

func (r *Reconciler) sweep(ctx context.Context) {
	// Runs regardless of the cursor lease: rows stranded 'queued' behind a
	// revoked installation or an archived session are never claimable, so
	// nothing else would ever move them to a terminal state.
	if err := r.q.FailUndeliverableChannelOutbound(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Warn("channel outbox reconciler: fail undeliverable", "channel_type", r.channelType, "error", err)
	}

	now := r.now()
	state, err := r.q.ClaimChannelOutboundReconcileState(ctx, db.ClaimChannelOutboundReconcileStateParams{
		ChannelType: r.channelType,
		// A cursor born at now(), not one backdated by the lookback. On a
		// deployment that predates the queue, channel_outbound_queue starts
		// empty while the old direct-socket path has already delivered
		// everything in that window — and absence of a queue row is exactly
		// what this scan reads as "never delivered". A backdated seed therefore
		// re-sends a day of replies into live chats, which aibot cannot unsend.
		CursorAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		// No row means another replica holds the lease. Not a problem: that is
		// exactly what the lease is for.
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		r.logger.Warn("channel outbox reconciler: claim cursor", "channel_type", r.channelType, "error", err)
		return
	}
	if !state.LeaseToken.Valid {
		return
	}
	lease := state.LeaseToken.String

	windowEnd := now.Add(-reconcileSettleDelay)
	windowStart := now
	if state.CursorAt.Valid {
		windowStart = state.CursorAt.Time
	}
	windowStart = windowStart.Add(-reconcileOverlapWindow)
	// Clamp to the lookback ceiling. A cursor stalled for days — a replica down,
	// or enqueues gated off — would otherwise resurrect replies whose 'sent'
	// tombstones the purge has already removed, so nothing is left to suppress
	// the re-enqueue: sentRetention is shorter than the stall.
	if floor := now.Add(-reconcileInitialLookback); windowStart.Before(floor) {
		windowStart = floor
	}
	// Never scan behind the queue's own epoch for this channel. Nothing that
	// finished before the cursor row existed can be missing from a table that
	// did not exist either, so a window reaching back past created_at is reading
	// absence of evidence as evidence of absence. This has to be checked every
	// sweep, not just the first: the cursor advances to windowEnd immediately,
	// which puts cursor_at behind created_at.
	if state.CreatedAt.Valid && windowStart.Before(state.CreatedAt.Time) {
		windowStart = state.CreatedAt.Time
	}

	if !r.enqueueOK() {
		r.advance(ctx, windowEnd, lease, "advance disabled")
		return
	}

	if err := r.scanWindow(ctx, windowStart, windowEnd); err != nil {
		if !errors.Is(err, context.Canceled) {
			r.logger.Warn("channel outbox reconciler: scan window", "channel_type", r.channelType, "error", err)
		}
		// Release without advancing so the same window is retried rather than
		// skipped past whatever the scan failed on.
		if relErr := r.q.ReleaseChannelOutboundReconcileState(ctx, db.ReleaseChannelOutboundReconcileStateParams{
			ChannelType: r.channelType,
			LeaseToken:  pgtype.Text{String: lease, Valid: true},
		}); relErr != nil && !errors.Is(relErr, context.Canceled) {
			r.logger.Warn("channel outbox reconciler: release cursor", "channel_type", r.channelType, "error", relErr)
		}
		return
	}

	r.advance(ctx, windowEnd, lease, "advance cursor")
}

func (r *Reconciler) advance(ctx context.Context, windowEnd time.Time, lease, what string) {
	if _, err := r.q.AdvanceChannelOutboundReconcileState(ctx, db.AdvanceChannelOutboundReconcileStateParams{
		ChannelType: r.channelType,
		CursorAt:    pgtype.Timestamptz{Time: windowEnd, Valid: true},
		LeaseToken:  pgtype.Text{String: lease, Valid: true},
	}); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.Warn("channel outbox reconciler: "+what, "channel_type", r.channelType, "error", err)
	}
}

func (r *Reconciler) scanWindow(ctx context.Context, windowStart, windowEnd time.Time) error {
	sourceKinds := r.builder.SourceKinds()
	var afterCompleted pgtype.Timestamptz
	var afterTaskID pgtype.UUID
	for {
		rows, err := r.q.ListChannelOutboundReconcileCandidates(ctx, db.ListChannelOutboundReconcileCandidatesParams{
			ChannelType:      r.channelType,
			SourceKinds:      sourceKinds,
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

func (r *Reconciler) reconcileCandidate(ctx context.Context, row db.ListChannelOutboundReconcileCandidatesRow) error {
	task, err := r.q.GetAgentTask(ctx, row.TaskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load task: %w", err)
	}
	// A channel-bound session can also carry web/mobile tasks. Their replies
	// stay in Multica, so only channel-ingested input earns a delivery.
	deliver, err := engine.TaskInputIsChannelIngested(ctx, r.q, task)
	if err != nil {
		return fmt.Errorf("provenance: %w", err)
	}
	if !deliver {
		return nil
	}

	req, ok, err := r.builder.Build(ctx, Candidate{
		TaskID:         row.TaskID,
		ChatSessionID:  row.ChatSessionID,
		TaskStatus:     row.TaskStatus,
		CompletedAt:    row.CompletedAt,
		FailureReason:  row.FailureReason,
		InstallationID: row.InstallationID,
		WorkspaceID:    row.WorkspaceID,
		ChannelType:    row.ChannelType,
	})
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	if !ok {
		return nil
	}

	inserted, err := r.producer.Enqueue(ctx, req, EnqueuePathReconcile)
	if err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	if !inserted {
		// The realtime path won the race between the candidate scan and this
		// insert. Expected, not a problem.
		r.metrics.RecordReconcileRaceLost(r.channelType)
		return nil
	}
	// A fresh insert means the realtime path never enqueued this reply: the
	// candidate query already excluded tasks that have a queue row. This is
	// the alerting signal for a broken realtime path, because the reconciler's
	// window lags on purpose and the user has therefore been waiting tens of
	// seconds longer than they should.
	r.logger.WarnContext(ctx, "channel outbox reconciler: rescued a reply the realtime path missed",
		"channel_type", r.channelType,
		"task_id", util.UUIDToString(row.TaskID),
		"source_kind", req.SourceKind,
	)
	return nil
}

func (r *Reconciler) purge(ctx context.Context) error {
	now := r.now()
	if err := r.q.PurgeChannelOutboundSendAttemptsBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-sendAttemptRetention), Valid: true,
	}); err != nil {
		return err
	}
	if err := r.q.PurgeSentChannelOutboundQueueBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-sentRetention), Valid: true,
	}); err != nil {
		return err
	}
	return r.q.PurgeFailedChannelOutboundQueueBefore(ctx, pgtype.Timestamptz{
		Time: now.Add(-failedRetention), Valid: true,
	})
}
