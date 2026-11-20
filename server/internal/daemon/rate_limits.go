package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Runtime rate limits (MUL-9427). The agent backend hands every rate-limit
// change the CLI reports to ExecOptions.OnRateLimits; this reporter forwards
// the newest snapshot to the server, which keeps it as the runtime's latest
// state. Only the newest snapshot matters, so a lost post is simply replaced
// by the next one.

// rateLimitFlushEvery bounds how often a running task posts rate limits. The
// CLI reports a change whenever a window's rounded percentage moves, which
// can be every model call.
const rateLimitFlushEvery = 30 * time.Second

const rateLimitReportTimeout = 5 * time.Second

type rateLimitReporter struct {
	post   func(context.Context, agent.RateLimitSnapshot) error
	log    *slog.Logger
	latest atomic.Pointer[agent.RateLimitSnapshot]

	mu       sync.Mutex
	reported *agent.RateLimitSnapshot
	// disabled is set once the server answers that it has no rate-limit
	// endpoint (an older self-hosted server), so the task stops trying.
	disabled bool
}

// observe records a snapshot. It runs on the backend's protocol reader
// goroutine, so it only stores.
func (r *rateLimitReporter) observe(snapshot agent.RateLimitSnapshot) {
	r.latest.Store(&snapshot)
}

// flush posts the newest snapshot if it differs from the last one posted.
func (r *rateLimitReporter) flush(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	latest := r.latest.Load()
	if latest == nil || (r.reported != nil && rateLimitSnapshotsEqual(*latest, *r.reported)) {
		return
	}
	postCtx, cancel := context.WithTimeout(ctx, rateLimitReportTimeout)
	defer cancel()
	err := r.post(postCtx, *latest)
	if err == nil {
		r.reported = latest
		return
	}
	var reqErr *requestError
	if errors.As(err, &reqErr) && (reqErr.StatusCode == http.StatusNotFound || reqErr.StatusCode == http.StatusMethodNotAllowed) {
		r.disabled = true
		r.log.Debug("server does not accept rate limits; skipping", "error", err)
		return
	}
	// Not fatal: the next change, or the final flush, posts again.
	r.log.Warn("report rate limits failed", "error", err)
}

// rateLimitSnapshotsEqual ignores ObservedAt, which moves on every event even
// when nothing the reader shows has changed.
func rateLimitSnapshotsEqual(a, b agent.RateLimitSnapshot) bool {
	a.ObservedAt, b.ObservedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(a, b)
}

// startRateLimitFlush wires a reporter into the task's exec options and
// returns a stop func that posts whatever is still unposted. The stop func
// must run before the task's terminal report so the snapshot from a run that
// hit its limit is stored.
func (d *Daemon) startRateLimitFlush(ctx context.Context, taskID string, taskLog *slog.Logger) (observe func(agent.RateLimitSnapshot), stop func()) {
	reporter := &rateLimitReporter{
		post: func(ctx context.Context, s agent.RateLimitSnapshot) error {
			return d.client.ReportTaskRateLimits(ctx, taskID, s)
		},
		log: taskLog,
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(rateLimitFlushEvery)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				reporter.flush(context.WithoutCancel(ctx))
			}
		}
	}()
	return reporter.observe, sync.OnceFunc(func() {
		close(done)
		<-stopped
		reporter.flush(context.WithoutCancel(ctx))
	})
}
