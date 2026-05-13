package runtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/cerebro/account"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MaybeAutoPauseOnFailure inspects a recently-failed task and pauses the
// task's runtime when the error text signals "the agent cannot work right
// now": rate limits, monthly quota caps, expired auth tokens, OpenAI
// insufficient_quota. Returns true when a pause was issued.
//
// Wired into upstream's TaskService.FailTask via the AutoPauseInvoker seam
// in task.go and the router-side assignment in router.go. The TaskService
// holds a nil-safe interface reference so a misconfigured boot or a future
// upstream sync that drops the seam degrades to a no-op rather than a
// crash.
//
// Idempotent against bursts: when a runtime hits its monthly cap the next
// dozen queued tasks all fail with the same error in quick succession. The
// first call pauses; subsequent calls re-pause (refreshing unpause_at +
// reason) which is a cheap no-op SQL update.
//
// Fail-safe loop: ParseRateLimitReset returns a 5-minute fallback when it
// detects a pause-worthy signal but cannot parse a reset time (e.g. a
// monthly cap or an expired token). After 5 min the sweeper unpauses, the
// next queued task attempts and fails the same way, and we re-pause for
// another 5 min — bouncing until the upstream condition actually clears.
// The sweeper tick is 30s so worst-case is one wasted dispatch per 5-min
// window, which is well within acceptable cost.
func (s *Service) MaybeAutoPauseOnFailure(ctx context.Context, task db.AgentTaskQueue) bool {
	unpauseAt, ok := shouldAutoPause(task, time.Now())
	if !ok {
		return false
	}
	if _, err := s.PauseRuntime(ctx, task.RuntimeID, handler.RuntimePauseOptions{
		UnpauseAt: unpauseAt,
		Reason:    "auto",
	}); err != nil {
		slog.Warn("auto-pause on failure: pause failed",
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return false
	}
	slog.Info("auto-paused runtime on task failure",
		"runtime_id", util.UUIDToString(task.RuntimeID),
		"task_id", util.UUIDToString(task.ID),
		"unpause_at", unpauseAt.Format(time.RFC3339),
	)
	return true
}

// shouldAutoPause is the pure decision half of MaybeAutoPauseOnFailure.
// Returns (unpauseAt, true) when the task's error text warrants a runtime
// pause. Split out for unit-testability — the side-effectful PauseRuntime
// call sits behind a real DB so cannot be exercised in a plain unit test.
func shouldAutoPause(task db.AgentTaskQueue, now time.Time) (time.Time, bool) {
	if !task.RuntimeID.Valid {
		return time.Time{}, false
	}
	if !task.Error.Valid || task.Error.String == "" {
		return time.Time{}, false
	}
	return account.ParseRateLimitReset(task.Error.String, now)
}
