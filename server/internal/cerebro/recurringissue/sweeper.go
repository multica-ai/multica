package recurringissue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultInterval is how often the sweeper polls when main passes <= 0. Short
// so a closed issue spawns its successor promptly ("oprettes nyt med ny due
// date"), unlike the daily sprint sweeper.
const DefaultInterval = time.Minute

// Sweeper polls enabled recurrence rules and, when a rule's source issue has
// edge-transitioned into its trigger status, spawns the next occurrence. One
// per process. Owned by main.go via NewSweeper(...) and started with
// Run(ctx, interval). Mirrors server/internal/cerebro/sprints/sweeper.go.
type Sweeper struct {
	Pool     *pgxpool.Pool
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries

	// nowFunc is overridden in tests so we can run the sweeper at a known
	// instant without mocking time.Time globally.
	nowFunc func() time.Time
}

// NewSweeper builds a Sweeper. The cerebro queries are required; the upstream
// queries are needed for IncrementIssueCounter + CreateIssue + GetIssue.
func NewSweeper(pool *pgxpool.Pool, cerebro *cerebrodb.Queries, upstream *db.Queries) *Sweeper {
	return &Sweeper{Pool: pool, Cerebro: cerebro, Upstream: upstream, nowFunc: time.Now}
}

// Run blocks on ctx and ticks the sweep at the requested interval.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("cerebro recurring-issue sweeper: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro recurring-issue sweeper: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one pass over every enabled recurrence. Workspaces where the
// cerebro_recurring_issues flag is OFF are skipped — the flag defaults OFF,
// so the feature does nothing until an admin enables it.
func (s *Sweeper) Tick(ctx context.Context) error {
	rules, err := s.Cerebro.ListEnabledCerebroIssueRecurrences(ctx)
	if err != nil {
		return fmt.Errorf("list enabled recurrences: %w", err)
	}
	flagCache := make(map[[16]byte]bool, 4)
	for _, r := range rules {
		enabled, err := s.isFlagEnabled(ctx, r.WorkspaceID, flagCache)
		if err != nil {
			slog.Warn("cerebro recurring-issue sweeper: flag check failed",
				"workspace_id", util.UUIDToString(r.WorkspaceID), "error", err)
			continue
		}
		if !enabled {
			continue
		}
		if _, err := s.processRule(ctx, r); err != nil {
			slog.Warn("cerebro recurring-issue sweeper: rule failed",
				"recurrence_id", util.UUIDToString(r.ID), "error", err)
		}
	}
	return nil
}

// ProcessRuleByID runs a single rule synchronously (manual ops/QA trigger).
// Honors the workspace flag. Returns true when an occurrence was spawned.
func (s *Sweeper) ProcessRuleByID(ctx context.Context, id pgtype.UUID) (bool, error) {
	r, err := s.Cerebro.GetCerebroIssueRecurrence(ctx, id)
	if err != nil {
		return false, err
	}
	enabled, err := s.isFlagEnabled(ctx, r.WorkspaceID, map[[16]byte]bool{})
	if err != nil {
		return false, fmt.Errorf("flag check: %w", err)
	}
	if !enabled || !r.Enabled {
		return false, nil
	}
	return s.processRule(ctx, r)
}

func (s *Sweeper) isFlagEnabled(ctx context.Context, workspaceID pgtype.UUID, cache map[[16]byte]bool) (bool, error) {
	if !workspaceID.Valid {
		return false, nil
	}
	if v, ok := cache[workspaceID.Bytes]; ok {
		return v, nil
	}
	enabled, err := s.Cerebro.GetCerebroRecurringIssuesFlagForWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			cache[workspaceID.Bytes] = false
			return false, nil
		}
		return false, err
	}
	cache[workspaceID.Bytes] = enabled
	return enabled, nil
}

// processRule implements the edge-trigger: arm the rule while the source
// issue is not in trigger status, and fire exactly once when it transitions
// into trigger status. Returns true when an occurrence was spawned.
func (s *Sweeper) processRule(ctx context.Context, r cerebrodb.CerebroIssueRecurrence) (bool, error) {
	issue, err := s.Upstream.GetIssue(ctx, r.SourceIssueID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Source issue gone; the rule was ON DELETE CASCADE'd already in
			// most cases. Nothing to do.
			return false, nil
		}
		return false, fmt.Errorf("load source issue: %w", err)
	}
	isTrigger := issue.Status == r.TriggerStatus

	if !r.Armed {
		if !isTrigger {
			// Issue re-opened (or was never closed) — re-arm for next close.
			if err := s.Cerebro.SetCerebroIssueRecurrenceArmed(ctx, cerebrodb.SetCerebroIssueRecurrenceArmedParams{
				ID: r.ID, Armed: true,
			}); err != nil {
				return false, fmt.Errorf("re-arm: %w", err)
			}
		}
		return false, nil
	}
	if !isTrigger {
		return false, nil // armed but issue still open — wait.
	}

	// armed && isTrigger → fire.
	return s.fire(ctx, r, issue)
}

func (s *Sweeper) fire(ctx context.Context, r cerebrodb.CerebroIssueRecurrence, issue db.Issue) (bool, error) {
	// Base date: previous due date when syncing to due date, else completion
	// day (now).
	base := s.nowFunc()
	if r.Anchor == AnchorDueDate && issue.DueDate.Valid {
		base = issue.DueDate.Time
	}
	nextDue := NextDueDate(base, r.Frequency, int(r.IntervalCount), int16sToInts(r.Weekdays), int(r.DaysAfter))
	newCount := r.OccurrenceCount + 1

	// Stop conditions (only when not recurring forever).
	if !r.RecurForever {
		if r.MaxOccurrences.Valid && r.OccurrenceCount >= r.MaxOccurrences.Int32 {
			return false, s.disable(ctx, r)
		}
		if r.EndDate.Valid && nextDue.After(r.EndDate.Time) {
			return false, s.disable(ctx, r)
		}
	}

	armedAfter := r.NewStatus != r.TriggerStatus
	enabledAfter := true
	if !r.RecurForever && r.MaxOccurrences.Valid && newCount >= r.MaxOccurrences.Int32 {
		enabledAfter = false
	}

	err := s.runInTx(ctx, func(tx pgx.Tx) error {
		cqtx := s.Cerebro.WithTx(tx)
		dqtx := s.Upstream.WithTx(tx)

		newSource := r.SourceIssueID
		var spawnedID pgtype.UUID

		if r.CreateNewIssue {
			number, err := dqtx.IncrementIssueCounter(ctx, r.WorkspaceID)
			if err != nil {
				return fmt.Errorf("issue counter: %w", err)
			}
			position, err := issueposition.NextTopPosition(ctx, tx, r.WorkspaceID, r.NewStatus)
			if err != nil {
				return fmt.Errorf("issue position: %w", err)
			}
			created, err := dqtx.CreateIssue(ctx, db.CreateIssueParams{
				WorkspaceID:   r.WorkspaceID,
				Title:         issue.Title,
				Description:   issue.Description,
				Status:        r.NewStatus,
				Priority:      issue.Priority,
				AssigneeType:  issue.AssigneeType,
				AssigneeID:    issue.AssigneeID,
				CreatorType:   issue.CreatorType,
				CreatorID:     issue.CreatorID,
				ParentIssueID: issue.ParentIssueID,
				Position:      position,
				DueDate:       pgtype.Date{Time: nextDue, Valid: true},
				Number:        number,
				ProjectID:     issue.ProjectID,
				Kind:          pgtype.Text{String: issue.Kind, Valid: issue.Kind != ""},
			})
			if err != nil {
				return fmt.Errorf("create next issue: %w", err)
			}
			if err := cqtx.CopyCerebroIssueLabels(ctx, cerebrodb.CopyCerebroIssueLabelsParams{
				IssueID: created.ID, IssueID_2: issue.ID,
			}); err != nil {
				return fmt.Errorf("copy labels: %w", err)
			}
			if err := cqtx.CopyCerebroIssueAttachments(ctx, cerebrodb.CopyCerebroIssueAttachmentsParams{
				IssueID: created.ID, IssueID_2: issue.ID,
			}); err != nil {
				return fmt.Errorf("copy attachments: %w", err)
			}
			newSource = created.ID
			spawnedID = created.ID
		} else {
			// Reopen the same issue into new_status with a bumped due date.
			if err := cqtx.ReopenCerebroRecurringIssue(ctx, cerebrodb.ReopenCerebroRecurringIssueParams{
				ID:      issue.ID,
				Status:  r.NewStatus,
				DueDate: pgtype.Date{Time: nextDue, Valid: true},
			}); err != nil {
				return fmt.Errorf("reopen issue: %w", err)
			}
		}

		if err := cqtx.InsertCerebroIssueRecurrenceLog(ctx, cerebrodb.InsertCerebroIssueRecurrenceLogParams{
			RecurrenceID:     r.ID,
			TriggerIssueID:   issue.ID,
			SpawnedIssueID:   spawnedID,
			OccurrenceNumber: newCount,
		}); err != nil {
			return fmt.Errorf("insert log: %w", err)
		}

		if err := cqtx.AdvanceCerebroIssueRecurrenceAfterFire(ctx, cerebrodb.AdvanceCerebroIssueRecurrenceAfterFireParams{
			ID:              r.ID,
			SourceIssueID:   newSource,
			OccurrenceCount: newCount,
			Armed:           armedAfter,
			Enabled:         enabledAfter,
		}); err != nil {
			return fmt.Errorf("advance recurrence: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// disable marks the rule done without spawning (stop condition reached).
func (s *Sweeper) disable(ctx context.Context, r cerebrodb.CerebroIssueRecurrence) error {
	return s.Cerebro.AdvanceCerebroIssueRecurrenceAfterFire(ctx, cerebrodb.AdvanceCerebroIssueRecurrenceAfterFireParams{
		ID:              r.ID,
		SourceIssueID:   r.SourceIssueID,
		OccurrenceCount: r.OccurrenceCount,
		Armed:           false,
		Enabled:         false,
	})
}

func (s *Sweeper) runInTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
