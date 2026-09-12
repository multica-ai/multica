package selfhosttelemetry

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dailyJitterWindow = 6 * time.Hour
	minRetryBackoff   = time.Minute
	maxRetryBackoff   = 6 * time.Hour
	leaderRetryDelay  = time.Minute
	collectionRetry   = time.Hour
	logRateLimit      = time.Hour
)

type Worker struct {
	store         leaderStore
	collector     eventCollector
	sender        eventSender
	serverVersion string
	logger        *slog.Logger
	now           func() time.Time

	logMu     sync.Mutex
	nextLogAt time.Time
}

type workerFactories struct {
	store     func() leaderStore
	collector func() eventCollector
	sender    func() eventSender
}

// New performs no database, DNS or HTTP work. When DO_NOT_TRACK disabled the
// feature, it also returns before constructing any collector or HTTP client.
func New(pool *pgxpool.Pool, serverVersion string, config Config, logger *slog.Logger) *Worker {
	return newWithFactories(config, serverVersion, logger, workerFactories{
		store:     func() leaderStore { return newPostgresStore(pool) },
		collector: func() eventCollector { return SQLCollector{} },
		sender:    func() eventSender { return newHTTPClient() },
	})
}

func newWithFactories(config Config, serverVersion string, logger *slog.Logger, factories workerFactories) *Worker {
	if !config.Enabled {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		store:         factories.store(),
		collector:     factories.collector(),
		sender:        factories.sender(),
		serverVersion: serverVersion,
		logger:        logger,
		now:           time.Now,
	}
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil {
		return
	}
	defer w.sender.Close()
	for {
		now := w.now().UTC()
		next, err := w.runOnce(ctx, now)
		if err != nil && ctx.Err() == nil {
			w.logFailure(now)
		}
		if ctx.Err() != nil {
			return
		}
		delay := next.Sub(w.now().UTC())
		if delay < 0 {
			delay = 0
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

// runOnce performs at most one collection/delivery attempt while holding the
// deployment-wide leader lock. It returns the earliest useful next wake time.
func (w *Worker) runOnce(ctx context.Context, now time.Time) (next time.Time, err error) {
	session, leader, err := w.store.TryLeader(ctx)
	if err != nil {
		return now.Add(leaderRetryDelay), err
	}
	if !leader {
		return now.Add(leaderRetryDelay), nil
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	state, err := session.LoadOrCreate(ctx)
	if err != nil {
		return now.Add(leaderRetryDelay), err
	}
	today := utcDay(now)
	tomorrow := today.AddDate(0, 0, 1)

	if state.LastSuccessfulDay != nil && sameDay(*state.LastSuccessfulDay, today) {
		if state.PendingDay != nil {
			if err := session.DropPending(ctx); err != nil {
				return now.Add(leaderRetryDelay), err
			}
		}
		return scheduledAt(state.InstanceID, tomorrow), nil
	}

	if state.PendingDay != nil && (!sameDay(*state.PendingDay, today) || now.Sub(utcDay(*state.PendingDay)) >= 72*time.Hour) {
		if err := session.DropPending(ctx); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		state.PendingDay = nil
		state.PendingBody = nil
		state.NextAttemptAt = nil
		state.AttemptCount = 0
	}

	if state.PendingDay == nil {
		firstAttempt := scheduledAt(state.InstanceID, today)
		if now.Before(firstAttempt) {
			return firstAttempt, nil
		}
		counts, collectErr := w.collector.Collect(ctx, session.Connection(), now)
		if collectErr != nil {
			return now.Add(collectionRetry), collectErr
		}
		body, marshalErr := marshalEvent(newEvent(state.InstanceID, now, w.serverVersion, counts))
		if marshalErr != nil {
			return now.Add(collectionRetry), marshalErr
		}
		if err := session.SavePending(ctx, today, body, now); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		state.PendingDay = &today
		state.PendingBody = body
		state.NextAttemptAt = nil
		state.AttemptCount = 0
	}

	if len(state.PendingBody) == 0 || len(state.PendingBody) > maxRequestSize {
		if err := session.DropPending(ctx); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		return now.Add(collectionRetry), errors.New("stored telemetry event has invalid size")
	}
	if state.NextAttemptAt != nil && now.Before(*state.NextAttemptAt) {
		return *state.NextAttemptAt, nil
	}

	switch w.sender.Send(ctx, state.PendingBody) {
	case deliverySuccess:
		if err := session.MarkSuccessful(ctx, today); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		return scheduledAt(state.InstanceID, tomorrow), nil
	case deliveryRetry:
		attempt := state.AttemptCount + 1
		next = now.Add(retryBackoff(state.InstanceID, today, attempt))
		if err := session.ScheduleRetry(ctx, next, attempt); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		return next, nil
	default:
		next = scheduledAt(state.InstanceID, tomorrow)
		if err := session.ScheduleRetry(ctx, next, state.AttemptCount+1); err != nil {
			return now.Add(leaderRetryDelay), err
		}
		return next, nil
	}
}

func (w *Worker) logFailure(now time.Time) {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	if now.Before(w.nextLogAt) {
		return
	}
	w.nextLogAt = now.Add(logRateLimit)
	// Intentionally omit errors, request bodies, response bodies and instance
	// identity. Operators only need to know this optional path degraded.
	w.logger.Warn("self-host telemetry attempt failed; normal server operation continues")
}

func scheduledAt(instanceID uuid.UUID, day time.Time) time.Time {
	sum := sha256.Sum256(instanceID[:])
	offset := time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(dailyJitterWindow))
	return utcDay(day).Add(offset)
}

func retryBackoff(instanceID uuid.UUID, day time.Time, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := minRetryBackoff
	for i := 1; i < attempt && base < maxRetryBackoff; i++ {
		base *= 2
		if base >= maxRetryBackoff {
			return maxRetryBackoff
		}
	}
	seed := make([]byte, 0, 16+8+8)
	seed = append(seed, instanceID[:]...)
	var encoded [16]byte
	binary.BigEndian.PutUint64(encoded[:8], uint64(utcDay(day).Unix()))
	binary.BigEndian.PutUint64(encoded[8:], uint64(attempt))
	seed = append(seed, encoded[:]...)
	sum := sha256.Sum256(seed)
	// [1.0, 1.25) jitter keeps the documented one-minute lower bound.
	jitter := time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(base/4))
	if base+jitter > maxRetryBackoff {
		return maxRetryBackoff
	}
	return base + jitter
}

func sameDay(a, b time.Time) bool {
	return utcDay(a).Equal(utcDay(b))
}
