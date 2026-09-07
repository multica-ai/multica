package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type taskReservationKey struct{}

// A parked task keeps its identity and cancellation watcher, but returns its
// execution token. Only this owner may return/reacquire that token; concurrent
// path wakeups still contend on the same bounded semaphore as fresh claims.
type taskExecutionReservation struct {
	mu     sync.Mutex
	slots  chan int
	slot   int
	held   bool
	wakeup chan struct{}
}

func (r *taskExecutionReservation) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.held {
		return
	}
	r.slots <- r.slot
	r.held = false
	signalPollerWakeup(r.wakeup)
}

func (r *taskExecutionReservation) acquire(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.held {
		return nil
	}
	select {
	case slot := <-r.slots:
		r.slot, r.held = slot, true
		if err := ctx.Err(); err != nil {
			r.slots <- slot
			r.held = false
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseTaskExecutionSlot(ctx context.Context) {
	if r, ok := ctx.Value(taskReservationKey{}).(*taskExecutionReservation); ok {
		r.release()
	}
}

func reacquireTaskExecutionSlot(ctx context.Context) error {
	if r, ok := ctx.Value(taskReservationKey{}).(*taskExecutionReservation); ok {
		return r.acquire(ctx)
	}
	return ctx.Err()
}

func (d *Daemon) startTaskWithAdmission(ctx context.Context, taskID string) error {
	for {
		if err := reacquireTaskExecutionSlot(ctx); err != nil {
			return err
		}
		err := d.client.StartTask(ctx, taskID)
		var response *requestError
		if !errors.As(err, &response) || response.StatusCode != http.StatusTooManyRequests || !strings.Contains(response.Body, "execution_capacity_unavailable") {
			return err
		}
		// The server still owns a durable queued row. A deterministic capacity
		// refusal is not a provider failure or a new execution attempt.
		_ = d.client.MarkTaskWaitingLocalDirectory(ctx, taskID, "specialist execution capacity; resumes automatically")
		releaseTaskExecutionSlot(ctx)
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}
