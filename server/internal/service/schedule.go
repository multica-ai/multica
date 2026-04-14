package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ScheduleService runs the scheduled-task loop: on a fixed tick, claim any
// scheduled_task rows whose next_run_at has passed, create a corresponding
// issue (assigned to the agent or member configured on the schedule), and
// advance next_run_at to the next cron slot.
type ScheduleService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	Tasks     *TaskService
	Logger    *slog.Logger

	// Tick controls how often the scheduler polls for due rows. Defaults to
	// 60s — minute granularity is the contract we expose via the 5-field
	// cron parser, so there's no reason to poll more frequently.
	Tick time.Duration
}

// TxStarter is the minimal transaction interface the scheduler needs. It
// mirrors the handler.txStarter type so we don't have to import the handler
// package from the service package.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// NewScheduleService builds a ScheduleService with sensible defaults.
func NewScheduleService(q *db.Queries, txStarter TxStarter, tasks *TaskService, logger *slog.Logger) *ScheduleService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScheduleService{
		Queries:   q,
		TxStarter: txStarter,
		Tasks:     tasks,
		Logger:    logger,
		Tick:      60 * time.Second,
	}
}

// Run starts the scheduler loop. It blocks until ctx is cancelled. The first
// tick runs immediately so that a freshly started server picks up due
// schedules without waiting a full minute.
func (s *ScheduleService) Run(ctx context.Context) {
	s.Logger.Info("scheduler: starting", "tick", s.Tick.String())
	// First tick immediately, then on s.Tick cadence.
	if err := s.runOnce(ctx); err != nil {
		s.Logger.Error("scheduler: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(s.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Logger.Info("scheduler: shutting down")
			return
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				s.Logger.Error("scheduler: tick failed", "error", err)
			}
		}
	}
}

// runOnce performs a single scheduler tick: claim due rows, fire each one.
//
// Claiming, issue creation, and next_run_at bookkeeping all happen inside a
// single transaction per schedule row so a row is either fully processed
// (issue created + next_run_at advanced) or left untouched. Task-queue
// enqueueing happens after commit because the daemon polls via a separate
// connection — we want the issue to be visible when the task is queued.
func (s *ScheduleService) runOnce(ctx context.Context) error {
	// Pull the set of due rows in a short-lived transaction. FOR UPDATE SKIP
	// LOCKED ensures multiple server instances don't double-fire.
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.Queries.WithTx(tx)
	due, err := qtx.ClaimDueScheduledTasks(ctx)
	if err != nil {
		return fmt.Errorf("claim due: %w", err)
	}
	if len(due) == 0 {
		return nil
	}

	s.Logger.Info("scheduler: firing", "count", len(due))

	type fireResult struct {
		schedule db.ScheduledTask
		issue    db.Issue
		ok       bool
	}
	results := make([]fireResult, 0, len(due))

	for _, row := range due {
		issue, err := s.fireSchedule(ctx, qtx, row)
		res := fireResult{schedule: row, issue: issue, ok: err == nil}

		// Compute the next fire time regardless of success/failure. We never
		// want a broken schedule to get wedged at a past timestamp — that
		// would cause the scheduler to hammer the same row every tick.
		nextRun, nextErr := NextFireTime(row.CronExpression, row.Timezone, time.Now())
		if nextErr != nil {
			// Invalid cron/tz on a row that was live. Push the next run far
			// into the future so the scheduler stops trying; the API will
			// surface last_run_error for the user to fix the row.
			nextRun = time.Now().Add(100 * 365 * 24 * time.Hour)
			if err == nil {
				err = nextErr
			}
		}

		var lastErr pgtype.Text
		if err != nil {
			lastErr = pgtype.Text{String: err.Error(), Valid: true}
			s.Logger.Warn("scheduler: fire failed",
				"schedule_id", util.UUIDToString(row.ID),
				"name", row.Name,
				"error", err,
			)
		}

		var lastRunIssueID pgtype.UUID
		if res.ok {
			lastRunIssueID = issue.ID
		}

		if uerr := qtx.UpdateScheduledTaskRun(ctx, db.UpdateScheduledTaskRunParams{
			ID:             row.ID,
			LastRunAt:      pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
			LastRunIssueID: lastRunIssueID,
			LastRunError:   lastErr,
			NextRunAt:      pgtype.Timestamptz{Time: nextRun, Valid: true},
		}); uerr != nil {
			s.Logger.Error("scheduler: update run failed",
				"schedule_id", util.UUIDToString(row.ID),
				"error", uerr,
			)
			return fmt.Errorf("update run: %w", uerr)
		}

		results = append(results, res)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit claim tx: %w", err)
	}

	// Now that every issue row is durable, enqueue task-queue entries for
	// the successful agent-assigned issues. Failures here are logged but do
	// not roll the schedule back — the issue still exists and can be
	// re-dispatched by the user through normal means.
	for _, r := range results {
		if !r.ok || r.schedule.AssigneeType != "agent" {
			continue
		}
		if _, err := s.Tasks.EnqueueTaskForIssue(ctx, r.issue); err != nil {
			s.Logger.Warn("scheduler: enqueue task failed",
				"schedule_id", util.UUIDToString(r.schedule.ID),
				"issue_id", util.UUIDToString(r.issue.ID),
				"error", err,
			)
		}
	}
	return nil
}

// fireSchedule creates a single issue for the given schedule inside the
// supplied transaction. It returns the created issue on success.
func (s *ScheduleService) fireSchedule(ctx context.Context, qtx *db.Queries, row db.ScheduledTask) (db.Issue, error) {
	issueNumber, err := qtx.IncrementIssueCounter(ctx, row.WorkspaceID)
	if err != nil {
		return db.Issue{}, fmt.Errorf("increment counter: %w", err)
	}

	title := expandTemplate(row.TitleTemplate, row)
	description := expandTemplate(row.Description, row)

	// Scheduled issues are always created by the schedule's owner. The
	// creator_type is therefore "member" — this mirrors what a human
	// clicking "Run now" or filling the form would produce.
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		WorkspaceID:   row.WorkspaceID,
		Title:         title,
		Description:   pgtype.Text{String: description, Valid: description != ""},
		Status:        "todo",
		Priority:      row.Priority,
		AssigneeType:  pgtype.Text{String: row.AssigneeType, Valid: true},
		AssigneeID:    row.AssigneeID,
		CreatorType:   "member",
		CreatorID:     row.CreatedBy,
		ParentIssueID: pgtype.UUID{},
		Position:      0,
		DueDate:       pgtype.Timestamptz{},
		Number:        issueNumber,
		ProjectID:     pgtype.UUID{},
	})
	if err != nil {
		return db.Issue{}, fmt.Errorf("create issue: %w", err)
	}
	return issue, nil
}

// expandTemplate substitutes the supported variables in a schedule title or
// description. We deliberately keep this tiny and string-based rather than
// reaching for text/template — users type these in a form and plain
// {{date}} placeholders are easier to explain than Go template syntax.
func expandTemplate(input string, row db.ScheduledTask) string {
	if input == "" {
		return input
	}
	now := time.Now().UTC()
	replacements := map[string]string{
		"{{date}}":          now.Format("2006-01-02"),
		"{{datetime}}":      now.Format("2006-01-02 15:04:05 UTC"),
		"{{schedule_name}}": row.Name,
	}
	out := input
	for k, v := range replacements {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}
