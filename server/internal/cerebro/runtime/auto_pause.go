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
	decision := classifyAutoPause(task, now)
	if !decision.pauseWorthy {
		return false
	}

	// Re-classify the triggering task on EVERY pause-worthy failure (FIR-2611),
	// before — and independent of — the pause attempt below. The daemon sends an
	// empty failure_reason for generic errors, which defaults to 'agent_error';
	// that value is excluded from ListResumableTasksForRuntime's Category-2
	// resume set and shows up as a generic failed run rather than a rate-limit.
	// Reclassifying every auth/cap failure (not only the ones whose pause call
	// happens to succeed) is what stops ~83 monthly-cap failures from being
	// stranded as agent_error. The SQL WHERE guard keeps it idempotent.
	if err := s.Cerebro.ReclassifyAsRateLimit(ctx, task.ID); err != nil {
		slog.Warn("auto-pause on failure: reclassify task failed",
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
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
	if decision.manualOnly {
		circuitOpen = true
	}
	unpauseAt := nextUnpauseAt(count, decision.resetAt, decision.hasReset, now)

	opts := handler.RuntimePauseOptions{Reason: decision.pauseReason}
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
	// Re-classify the triggering task so the run log and unpause sweeper see
	// the real pause cause. Rate-limit-like failures remain resumable; auth
	// failures stay manual-only and require a human to fix credentials before
	// retrying.
	// The daemon sends an empty failure_reason for generic errors, which
	// defaults to 'agent_error'. That value is excluded from
	// ListResumableTasksForRuntime's Category-2 resume set.
	if err := s.Cerebro.ReclassifyAutoPauseFailure(ctx, task.ID, decision.failureReason); err != nil {
		slog.Warn("auto-pause on failure: reclassify task failed",
			"task_id", util.UUIDToString(task.ID),
			"error", err,
		)
	}

	// Collapse the runtime's auth/quota bounce-loop into ONE aggregated inbox
	// card per (runtime, day) for the runtime owner (FIR-2611), instead of the
	// long list of failed-run rows the user used to see. Bumped on every pause
	// cycle so the card carries an accurate failed-run count and the latest
	// reset time. Best-effort: a card failure never blocks the pause.
	s.upsertRuntimePauseCard(ctx, task.RuntimeID, count, unpauseAt, circuitOpen)

	s.notifyAutoPauseFailure(ctx, task, decision, unpauseAt, circuitOpen, count)

	slog.Info("auto-paused runtime on task failure",
		"runtime_id", util.UUIDToString(task.RuntimeID),
		"task_id", util.UUIDToString(task.ID),
		"consecutive_pauses", count,
		"circuit_open", circuitOpen,
		"pause_reason", decision.pauseReason,
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

type autoPauseDecision struct {
	pauseWorthy   bool
	hasReset      bool
	manualOnly    bool
	resetAt       time.Time
	pauseReason   string
	failureReason string
	title         string
	detail        string
}

// classifyAutoPause is the pure decision half of MaybeAutoPauseOnFailure.
// It separates pause-worthy provider blockers into user-facing categories so
// the task row, runtime banner, and issue comment all explain the same cause.
//
// Split out for unit-testability — the side-effectful PauseRuntime / counter
// calls sit behind a real DB so cannot be exercised in a plain unit test.
func classifyAutoPause(task db.AgentTaskQueue, now time.Time) autoPauseDecision {
	if !task.RuntimeID.Valid {
		return autoPauseDecision{}
	}
	if !task.Error.Valid || task.Error.String == "" {
		return autoPauseDecision{}
	}
	if isProviderAuthError(task.Error.String) {
		return autoPauseDecision{
			pauseWorthy:   true,
			manualOnly:    true,
			pauseReason:   "auth_error",
			failureReason: "auth_error",
			title:         "Provider authentication failed",
			detail:        "Runtimen kan ikke starte nye kørsler, før konto eller API-nøgle er fornyet.",
		}
	}
	resetAt, hasReset, pauseWorthy := account.ClassifyRateLimitReset(task.Error.String, now)
	if !pauseWorthy {
		return autoPauseDecision{}
	}
	return autoPauseDecision{
		pauseWorthy:   true,
		hasReset:      hasReset,
		resetAt:       resetAt,
		pauseReason:   "rate_limit",
		failureReason: "rate_limit",
		title:         "Usage or rate limit reached",
		detail:        "Runtimen er midlertidigt sat på pause, så den ikke brænder flere forsøg mens udbyderen afviser kørsler.",
	}
}

func isProviderAuthError(errText string) bool {
	lower := strings.ToLower(errText)
	return strings.Contains(lower, "401 invalid authentication credentials") ||
		strings.Contains(lower, "failed to authenticate")
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

// notifyAutoPauseFailure posts the human-facing explanation for the failed run
// that paused the runtime. Best-effort and deliberately mention-free: an
// @mention here would re-trigger the agent loop the pause just stopped.
// Skipped for tasks with no issue (e.g. chat tasks) — there is nowhere to post.
func (s *Service) notifyAutoPauseFailure(ctx context.Context, task db.AgentTaskQueue, decision autoPauseDecision, unpauseAt time.Time, manualOnly bool, count int32) {
	if !task.IssueID.Valid || !task.AgentID.Valid {
		return
	}
	next := "Den genoptager automatisk " + unpauseAt.Format(time.RFC3339) + "."
	if manualOnly {
		next = "Den genoptager ikke automatisk. Ret årsagen og genoptag runtimen manuelt."
		if count >= autoPauseCircuitLimit && !decision.manualOnly {
			next = fmt.Sprintf(
				"Den genoptager ikke automatisk, fordi samme runtime er blevet pauset %d gange i træk uden en succesfuld kørsel.",
				count,
			)
		}
	}
	body := fmt.Sprintf(
		"Runtimen er sat på pause: %s.\n\n%s\n\n%s",
		decision.title,
		decision.detail,
		next,
	)
	comment, err := s.Cerebro.CreateAutoPauseAlertComment(ctx, cerebrodb.CreateAutoPauseAlertCommentParams{
		AuthorID: task.AgentID,
		Content:  body,
		IssueID:  task.IssueID,
	})
	if err != nil {
		slog.Warn("auto-pause failure: post notice failed",
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
