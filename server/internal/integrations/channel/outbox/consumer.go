package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// MaxAttempts bounds retries before a row is dead-lettered. With the
	// backoff below that is roughly 20 minutes of trying, which comfortably
	// outlasts a reconnect or a platform blip while still surfacing a
	// genuinely broken target instead of retrying it forever.
	MaxAttempts = 8

	// ClaimLease is how long a claimed row stays fenced against other
	// workers. It must exceed the slowest realistic Send; a worker that dies
	// mid-send leaves the row reclaimable after this window.
	ClaimLease = 30 * time.Second

	// PollInterval is the fallback drain cadence when no wake arrives. A wake
	// covers the common case, so this only bounds the latency of rows enqueued
	// on another replica.
	PollInterval = 2 * time.Second

	// maxLastErrorBytes caps what is persisted in last_error. Platform errors
	// can embed a whole response body, and this column exists for triage, not
	// for archival.
	maxLastErrorBytes = 1024
)

// Disposition tells the Consumer how to settle a row after a send attempt.
type Disposition int

const (
	// DispositionSent settles the row as delivered.
	DispositionSent Disposition = iota
	// DispositionRetry schedules another attempt with backoff, up to
	// MaxAttempts, after which the row is dead-lettered.
	DispositionRetry
	// DispositionFailed dead-letters the row immediately.
	DispositionFailed
)

// Sender delivers claimed rows to one platform. It owns everything
// platform-specific: rendering payload into a wire body, holding the
// connection, and classifying the platform's error codes.
type Sender interface {
	// Send attempts delivery of one claimed row.
	//
	// The returned Disposition decides how the Consumer settles the row, and
	// err is recorded as last_error. An unrenderable payload — an unknown
	// template, or a payload_version this build predates — is
	// DispositionFailed: retrying cannot make it renderable.
	//
	// (DispositionSent, non-nil err) is the deliberate "ambiguous, do not
	// retry" case: a send whose outcome is unknown but whose content must
	// never arrive twice, such as a one-shot credential. The Consumer settles
	// the row as sent and logs err.
	Send(ctx context.Context, row db.ChannelOutboundQueue) (Disposition, error)
}

// RateGate is an optional per-target admission check run immediately before
// Send.
type RateGate interface {
	// Reserve reports whether row may be sent now. ok=false defers the row
	// until deferUntil without spending an attempt, because a target that is
	// merely over quota has not failed and must not be dead-lettered for
	// waiting.
	Reserve(ctx context.Context, row db.ChannelOutboundQueue) (deferUntil time.Time, ok bool, err error)
}

// ConsumerStore is the generated-query surface the Consumer needs.
type ConsumerStore interface {
	ClaimChannelOutbound(ctx context.Context, arg db.ClaimChannelOutboundParams) (db.ChannelOutboundQueue, error)
	DeferClaimedChannelOutbound(ctx context.Context, arg db.DeferClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	RetryClaimedChannelOutbound(ctx context.Context, arg db.RetryClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	CompleteClaimedChannelOutbound(ctx context.Context, arg db.CompleteClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	FailClaimedChannelOutbound(ctx context.Context, arg db.FailClaimedChannelOutboundParams) (db.ChannelOutboundQueue, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
}

// Consumer drains channel_outbound_queue for one installation. It runs on the
// replica holding that installation's connection lease, for the lifetime of
// the connection.
type Consumer struct {
	installationID string
	instUUID       pgtype.UUID
	channelType    string
	q              ConsumerStore
	sender         Sender
	rate           RateGate
	wake           <-chan struct{}
	logger         *slog.Logger
	metrics        Metrics
	now            func() time.Time
}

// ConsumerConfig wires one installation's consumer.
type ConsumerConfig struct {
	InstallationID string
	ChannelType    string
	Queries        ConsumerStore
	Sender         Sender

	// Rate is optional. A nil RateGate admits every row.
	Rate RateGate

	// Wake is the receive side of a WakeRegistry channel. A nil Wake falls
	// back to polling only.
	Wake <-chan struct{}

	Logger  *slog.Logger
	Metrics Metrics
	Now     func() time.Time
}

// NewConsumer builds a consumer for one installation.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	instUUID, err := util.ParseUUID(cfg.InstallationID)
	if err != nil || !instUUID.Valid {
		return nil, errors.New("outbox: invalid installation id")
	}
	if strings.TrimSpace(cfg.ChannelType) == "" {
		return nil, errors.New("outbox: channel type is required")
	}
	if cfg.Queries == nil || cfg.Sender == nil {
		return nil, errors.New("outbox: queries and sender are required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NoopMetrics()
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Consumer{
		installationID: cfg.InstallationID,
		instUUID:       instUUID,
		channelType:    cfg.ChannelType,
		q:              cfg.Queries,
		sender:         cfg.Sender,
		rate:           cfg.Rate,
		wake:           cfg.Wake,
		logger:         logger,
		metrics:        metrics,
		now:            now,
	}, nil
}

// Run processes queued rows until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) {
	poll := time.NewTicker(PollInterval)
	defer poll.Stop()
	for {
		worked, err := c.processOne(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			c.logger.WarnContext(ctx, "channel outbox: process failed",
				"channel_type", c.channelType,
				"installation_id", c.installationID,
				"error", err,
			)
		}
		// Keep draining while rows remain: a burst enqueued elsewhere should
		// not be metered out one row per poll tick.
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-poll.C:
		}
	}
}

// processOne claims and settles at most one row. It reports whether a row was
// claimed, which is what tells Run to keep draining.
func (c *Consumer) processOne(ctx context.Context) (bool, error) {
	row, err := c.q.ClaimChannelOutbound(ctx, db.ClaimChannelOutboundParams{
		InstallationID: c.instUUID,
		LeaseExpiresAt: pgtype.Timestamptz{Time: c.now().Add(ClaimLease), Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if !row.LeaseToken.Valid {
		return true, errors.New("outbox: claimed row missing lease token")
	}

	// A deliver failure is logged, not returned: the row has already been
	// settled (or left claimed for the lease to expire), and returning would
	// make Run treat a single bad row as a reason to stop draining the rest.
	if err := c.deliverClaimed(ctx, row, row.LeaseToken.String); err != nil && !errors.Is(err, context.Canceled) {
		c.logger.WarnContext(ctx, "channel outbox: deliver failed",
			"channel_type", c.channelType,
			"queue_id", util.UUIDToString(row.ID),
			"source_kind", row.SourceKind,
			"error", err,
		)
	}
	return true, nil
}

func (c *Consumer) deliverClaimed(ctx context.Context, row db.ChannelOutboundQueue, lease string) error {
	inst, err := c.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          row.InstallationID,
		ChannelType: row.ChannelType,
	})
	// A failed read says nothing about whether the installation is still
	// active, so only a genuinely missing row is terminal; anything else is
	// retried rather than permanently dropping a user-visible reply.
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return c.retryOrFail(ctx, row, lease, fmt.Errorf("load installation: %w", err))
		}
		return c.terminate(ctx, row, lease, OutcomeFenced, "installation not found")
	}
	if inst.Status != "active" {
		return c.terminate(ctx, row, lease, OutcomeFenced, "installation inactive")
	}

	if row.ChatSessionID.Valid {
		reason, err := c.checkChatSessionDeliverable(ctx, row)
		if err != nil {
			return c.retryOrFail(ctx, row, lease, err)
		}
		if reason != "" {
			return c.terminate(ctx, row, lease, OutcomeFenced, reason)
		}
	}

	if c.rate != nil {
		deferAt, allowed, err := c.rate.Reserve(ctx, row)
		if err != nil {
			// Leave the row claimed and let the lease expire: a gate that
			// cannot answer must not be treated as either a grant or a
			// failure, and the reclaim costs one lease window.
			return fmt.Errorf("rate reserve: %w", err)
		}
		if !allowed {
			_, err := c.q.DeferClaimedChannelOutbound(ctx, db.DeferClaimedChannelOutboundParams{
				ID:            row.ID,
				LeaseToken:    pgtype.Text{String: lease, Valid: true},
				NextAttemptAt: pgtype.Timestamptz{Time: deferAt, Valid: true},
			})
			if err == nil {
				c.metrics.RecordDelivery(row.ChannelType, OutcomeDeferred)
			}
			return err
		}
	}

	disposition, sendErr := c.sender.Send(ctx, row)
	switch disposition {
	case DispositionSent:
		if sendErr != nil {
			// Ambiguous send the adapter refuses to repeat. Settle as sent so
			// the content cannot be delivered twice, but say so in the log —
			// this is the one path where "sent" does not mean "confirmed".
			c.logger.WarnContext(ctx, "channel outbox: settling ambiguous send as delivered",
				"channel_type", row.ChannelType,
				"queue_id", util.UUIDToString(row.ID),
				"source_kind", row.SourceKind,
				"error", sendErr,
			)
		}
		return c.complete(ctx, row, lease)
	case DispositionRetry:
		if sendErr == nil {
			sendErr = errors.New("send failed")
		}
		return c.retryOrFail(ctx, row, lease, sendErr)
	default:
		reason := "send failed"
		if sendErr != nil {
			reason = sendErr.Error()
		}
		return c.terminate(ctx, row, lease, OutcomeFailed, reason)
	}
}

// checkChatSessionDeliverable re-verifies that the claimed row's chat session
// is still bound to this installation and still active, immediately before
// send.
//
// ClaimChannelOutbound already fences on both, but the claim and the send are
// not one transaction: an unbind/rebind or an archive in between must fence
// the send rather than deliver into a session the target no longer owns.
//
// A non-empty reason fences the row terminally; a non-nil error means the
// check itself could not be completed and the row must be retried instead.
func (c *Consumer) checkChatSessionDeliverable(ctx context.Context, row db.ChannelOutboundQueue) (string, error) {
	binding, err := c.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: row.ChatSessionID,
		ChannelType:   row.ChannelType,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "chat session binding not found", nil
		}
		return "", fmt.Errorf("load chat session binding: %w", err)
	}
	if binding.InstallationID != row.InstallationID {
		return "chat session bound to a different installation", nil
	}
	session, err := c.q.GetChatSession(ctx, row.ChatSessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "chat session not found", nil
		}
		return "", fmt.Errorf("load chat session: %w", err)
	}
	if session.Status != "active" {
		return "chat session inactive", nil
	}
	return "", nil
}

// terminate moves a claimed row to the dead letter with a fixed reason and
// records the outcome once. OutcomeFenced means the target stopped being
// settleTimeout bounds a settle that shutdown is deliberately not allowed to
// cancel. Without a bound, a wedged database would hold shutdown open.
const settleTimeout = 10 * time.Second

// settleContext detaches the settling UPDATE from the consumer's own lifecycle.
//
// By the time a row is settled the send has already happened. If a shutdown
// cancels the UPDATE, the row stays queued with a lease that expires, the next
// holder claims it, and the user gets the same message a second time — with
// nothing anywhere reporting a failure to explain it. The settle is the record
// of something already done to the outside world, so it does not belong on a
// context that means "stop doing things".
func settleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
}

// deliverable after enqueue; OutcomeFailed means delivery was attempted and
// did not succeed.
func (c *Consumer) terminate(ctx context.Context, row db.ChannelOutboundQueue, lease, outcome, reason string) error {
	ctx, cancel := settleContext(ctx)
	defer cancel()
	_, err := c.q.FailClaimedChannelOutbound(ctx, db.FailClaimedChannelOutboundParams{
		ID:         row.ID,
		LeaseToken: pgtype.Text{String: lease, Valid: true},
		LastError:  pgtype.Text{String: sanitizeLastError(reason), Valid: true},
	})
	if err == nil {
		c.metrics.RecordDelivery(row.ChannelType, outcome)
	}
	return err
}

func (c *Consumer) complete(ctx context.Context, row db.ChannelOutboundQueue, lease string) error {
	ctx, cancel := settleContext(ctx)
	defer cancel()
	_, err := c.q.CompleteClaimedChannelOutbound(ctx, db.CompleteClaimedChannelOutboundParams{
		ID:         row.ID,
		LeaseToken: pgtype.Text{String: lease, Valid: true},
	})
	if err == nil {
		c.metrics.RecordDelivery(row.ChannelType, OutcomeSent)
	}
	return err
}

func (c *Consumer) retryOrFail(ctx context.Context, row db.ChannelOutboundQueue, lease string, cause error) error {
	if row.Attempts+1 >= MaxAttempts {
		return c.terminate(ctx, row, lease, OutcomeFailed, cause.Error())
	}
	next := c.now().Add(Backoff(row.Attempts + 1))
	ctx, cancel := settleContext(ctx)
	defer cancel()
	_, err := c.q.RetryClaimedChannelOutbound(ctx, db.RetryClaimedChannelOutboundParams{
		ID:            row.ID,
		LeaseToken:    pgtype.Text{String: lease, Valid: true},
		NextAttemptAt: pgtype.Timestamptz{Time: next, Valid: true},
		LastError:     pgtype.Text{String: sanitizeLastError(cause.Error()), Valid: true},
	})
	if err == nil {
		c.metrics.RecordDelivery(row.ChannelType, OutcomeRetried)
	}
	return err
}

// Backoff returns the delay before the given attempt number, as full jitter
// over an exponentially growing window capped at five minutes.
//
// Full jitter rather than exponential-plus-noise because the failures this
// backs off from are usually correlated — one dropped connection strands every
// row for an installation at once — and equal delays would reconverge them
// into a thundering retry on each wake.
func Backoff(attempt int32) time.Duration {
	const (
		base  = 2 * time.Second
		limit = 5 * time.Minute
	)
	exp := int(attempt)
	if exp > 10 {
		exp = 10
	}
	window := base * time.Duration(1<<exp)
	if window > limit {
		window = limit
	}
	if window <= 0 {
		return base
	}
	return time.Duration(rand.Int64N(int64(window)))
}

func sanitizeLastError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > maxLastErrorBytes {
		msg = msg[:maxLastErrorBytes]
	}
	return msg
}
