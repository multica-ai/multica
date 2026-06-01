package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/account"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Auto-pause circuit-breaker tuning (FIR-2476). The pre-FIR-2476 behaviour
// re-paused a rate-limited runtime for a fixed 5 minutes, bounced on unpause,
// failed the same way, and re-paused — ~288 cycles a day per runtime, burning
// credits with no chance of success. Two mechanisms tame that:
//
//   - Growing backoff: when the provider error carries no parseable reset time,
//     the fallback pause doubles each consecutive auto-pause (5m, 10m, 20m, 40m,
//     80m …) up to autoPauseBackoffCap, instead of a flat 5 minutes. A parseable
//     reset time always wins over the fallback (unchanged behaviour).
//   - Circuit breaker: after autoPauseCircuitLimit consecutive auto-pauses with
//     no intervening success, stop scheduling auto-resume (pause with a NULL
//     unpause_at so the sweeper never picks it up) and post one comment so a
//     human can intervene. CompleteTask resets the counter on the next success.
const (
	autoPauseBackoffCap   = 2 * time.Hour
	autoPauseCircuitLimit = 6
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
// dozen queued tasks all fail with the same error in quick succession. Each
// in-flight task is suspended (not failed) when the first pause lands, so in
// the common case only the single triggering task re-enters here and bumps
// the consecutive-pause counter once per cycle.
func (s *Service) MaybeAutoPauseOnFailure(ctx context.Context, task db.AgentTaskQueue) bool {
	now := time.Now()
	resetAt, hasReset, pauseWorthy := classifyAutoPause(task, now)
	if !pauseWorthy {
		return false
	}

	// Bump the consecutive auto-pause counter first; its value decides the
	// backoff length and whether the circuit breaker trips. On a counter
	// read/write error fall back to count=1 (flat default backoff) — never
	// skip the pause itself, otherwise the storm continues unthrottled.
	count, err := s.Cerebro.IncrementAutoPauseCount(ctx, task.RuntimeID)
	if err != nil {
		slog.Warn("auto-pause on failure: increment counter failed",
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		count = 1
	}

	circuitOpen := count >= autoPauseCircuitLimit
	unpauseAt := nextUnpauseAt(count, resetAt, hasReset, now)

	opts := handler.RuntimePauseOptions{Reason: "rate_limit"}
	if !circuitOpen {
		opts.UnpauseAt = unpauseAt
	}
	// circuitOpen leaves UnpauseAt zero → PauseRuntime stores NULL unpause_at,
	// so the unpause sweeper never auto-resumes. Only a manual unpause (or a
	// later success that resets the counter) revives it.

	if _, err := s.PauseRuntime(ctx, task.RuntimeID, opts); err != nil {
		slog.Warn("auto-pause on failure: pause failed",
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
		return false
	}
	// Re-classify the triggering task so the unpause sweeper can resume it.
	// The daemon sends an empty failure_reason for generic errors, which
	// defaults to 'agent_error'. That value is excluded from
	// ListResumableTasksForRuntime's Category-2 resume set. Reclassifying
	// to 'rate_limit' puts it back in the window. The SQL WHERE guard
	// makes this idempotent on re-pause.
	if err := s.Cerebro.ReclassifyAsRateLimit(ctx, task.ID); err != nil {
		slog.Warn("auto-pause on failure: reclassify task failed",
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
	}

	// Post a human-facing issue notice for each scheduled auto-resume pause,
	// and exactly once when the circuit opens. Past the circuit limit the
	// runtime is already paused/manual-only, so repeating the same notice
	// would just bury the useful failure detail.
	if !circuitOpen || count == autoPauseCircuitLimit {
		s.notifyAutoPause(ctx, task, count, unpauseAt, circuitOpen)
	}

	slog.Info("auto-paused runtime on task failure",
		"runtime_id", util.UUIDToString(task.RuntimeID),
		"task_id", util.UUIDToString(task.ID),
		"consecutive_pauses", count,
		"circuit_open", circuitOpen,
		"unpause_at", unpauseAtLog(unpauseAt, circuitOpen),
	)
	return true
}

// ResetAutoPauseCount clears the consecutive auto-pause counter for a runtime.
// Called from CompleteTask (via the AutoPauseInvoker seam) when a task finishes
// successfully — a single success means the runtime is working again, so the
// next rate-limit pause starts a fresh chain. Best-effort: a failure here only
// risks an early circuit trip on the next pause storm, not a crash.
func (s *Service) ResetAutoPauseCount(ctx context.Context, runtimeID pgtype.UUID) {
	if !runtimeID.Valid {
		return
	}
	if err := s.Cerebro.ResetAutoPauseCount(ctx, runtimeID); err != nil {
		slog.Warn("reset auto-pause counter failed",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
	}
}

// classifyAutoPause is the pure decision half of MaybeAutoPauseOnFailure.
// Returns (resetAt, hasReset, pauseWorthy):
//   - pauseWorthy=false → the error does not warrant a runtime pause.
//   - hasReset=true     → resetAt is a concrete provider reset time to pause until.
//   - hasReset=false    → pause-worthy but no parseable reset; caller applies
//     the growing backoff.
//
// Split out for unit-testability — the side-effectful PauseRuntime / counter
// calls sit behind a real DB so cannot be exercised in a plain unit test.
func classifyAutoPause(task db.AgentTaskQueue, now time.Time) (time.Time, bool, bool) {
	if !task.RuntimeID.Valid {
		return time.Time{}, false, false
	}
	if !task.Error.Valid || task.Error.String == "" {
		return time.Time{}, false, false
	}
	return account.ClassifyRateLimitReset(task.Error.String, now)
}

// nextUnpauseAt computes the scheduled unpause time for a non-circuit-open
// pause. A parseable provider reset time always wins; otherwise the fallback
// grows with the consecutive-pause count.
func nextUnpauseAt(count int32, resetAt time.Time, hasReset bool, now time.Time) time.Time {
	if hasReset {
		return resetAt
	}
	return now.Add(growingBackoff(count))
}

// growingBackoff returns the fallback pause duration for the count-th
// consecutive auto-pause: DefaultBackoff * 2^(count-1), capped at
// autoPauseBackoffCap. count is 1-based (the first pause is count==1).
func growingBackoff(count int32) time.Duration {
	if count < 1 {
		count = 1
	}
	d := account.DefaultRateLimitBackoff
	for i := int32(1); i < count; i++ {
		d *= 2
		if d >= autoPauseBackoffCap {
			return autoPauseBackoffCap
		}
	}
	return d
}

// notifyAutoPause posts the human-facing issue notice when an agent failure
// pauses its runtime. Best-effort and deliberately mention-free: an @mention
// here would re-trigger the same agent loop that just hit the external limit.
// Skipped for tasks with no issue (e.g. chat tasks) — there is nowhere to post.
func (s *Service) notifyAutoPause(ctx context.Context, task db.AgentTaskQueue, count int32, unpauseAt time.Time, circuitOpen bool) {
	if !task.IssueID.Valid || !task.AgentID.Valid {
		return
	}
	body := autoPauseCommentBody(task, count, unpauseAt, circuitOpen)
	comment, err := s.Cerebro.CreateAutoPauseAlertComment(ctx, cerebrodb.CreateAutoPauseAlertCommentParams{
		AuthorID: task.AgentID,
		Content:  body,
		IssueID:  task.IssueID,
	})
	if err != nil {
		slog.Warn("auto-pause: post notice failed",
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"issue_id", util.UUIDToString(task.IssueID),
			"error", err,
		)
		return
	}
	s.publishCommentCreated(comment)
}

func autoPauseCommentBody(task db.AgentTaskQueue, count int32, unpauseAt time.Time, circuitOpen bool) string {
	taskID := util.UUIDToString(task.ID)
	errText := ""
	if task.Error.Valid {
		errText = strings.TrimSpace(redact.Text(task.Error.String))
	}
	if errText == "" {
		errText = "Runtimen ramte en rate-limit eller usage-limit."
	}
	if len([]rune(errText)) > 900 {
		rs := []rune(errText)
		errText = string(rs[:900]) + "..."
	}

	if circuitOpen {
		return fmt.Sprintf(
			"⚠️ Auto-genoptagelse er stoppet for denne runtime.\n\n"+
				"Runtimen er blevet automatisk sat på pause %d gange i træk uden en succesfuld kørsel "+
				"(typisk \"usage limit\" eller \"runtime paused\"). For ikke at brænde flere kreditter på "+
				"forsøg der alligevel fejler, genoptager systemet ikke længere automatisk.\n\n"+
				"Fejlen var:\n\n> %s\n\n"+
				"Run: `%s`\n\n"+
				"Et menneske skal gribe ind: vent til runtimens loft er nulstillet og genoptag den manuelt, "+
				"eller skift den fejlende konto/nøgle ud. Tælleren nulstilles automatisk ved første succesfulde kørsel.",
			count,
			errText,
			taskID,
		)
	}

	return fmt.Sprintf(
		"⏸️ Runtimen er sat på pause, fordi agent-runnet fejlede på en rate-limit eller usage-limit.\n\n"+
			"Fejlen var:\n\n> %s\n\n"+
			"Run: `%s`\n\n"+
			"Systemet prøver automatisk igen omkring `%s`.",
		errText,
		taskID,
		unpauseAt.UTC().Format(time.RFC3339),
	)
}

// unpauseAtLog renders the unpause_at value for structured logging — "circuit
// open (manual)" when the breaker tripped, otherwise the RFC3339 timestamp.
func unpauseAtLog(unpauseAt time.Time, circuitOpen bool) string {
	if circuitOpen {
		return "circuit open (manual)"
	}
	return unpauseAt.Format(time.RFC3339)
}

// publishCommentCreated mirrors the upstream broadcastTaskEvent for
// comment:created so the circuit-breaker notice surfaces in real time
// (inbox / issue view) the same way an agent comment does.
func (s *Service) publishCommentCreated(comment cerebrodb.Comment) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: util.UUIDToString(comment.WorkspaceID),
		ActorType:   "agent",
		ActorID:     util.UUIDToString(comment.AuthorID),
		Payload: map[string]any{
			"comment": map[string]any{
				"id":          util.UUIDToString(comment.ID),
				"issue_id":    util.UUIDToString(comment.IssueID),
				"author_type": comment.AuthorType,
				"author_id":   util.UUIDToString(comment.AuthorID),
				"content":     comment.Content,
				"type":        comment.Type,
				"created_at":  comment.CreatedAt.Time.Format(time.RFC3339),
			},
		},
	})
}
