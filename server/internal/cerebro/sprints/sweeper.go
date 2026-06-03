package sprints

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

// Sweeper runs the daily auto-create / mark-done / clone-recurring loop.
// One per process. Owned by main.go via NewSweeper(...) and started with
// Run(ctx, interval). The interval is set in main.go (24h in prod, shorter
// in tests).
type Sweeper struct {
	Pool     *pgxpool.Pool
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries

	// nowFunc is overridden in tests so we can run the sweeper at a known
	// instant without mocking time.Time globally.
	nowFunc func() time.Time
}

// ProjectSweepResult summarises what one project's sweep did. Returned by
// SweepProject so callers (the daily Tick + the manual ops endpoint) can
// log and surface "what changed".
type ProjectSweepResult struct {
	// FlagEnabled is true when the workspace had cerebro_sprints turned
	// on. When false the rest of the fields are zero and no work was done.
	FlagEnabled bool
	// SprintsMarkedDone is the number of previously-active sprints whose
	// end_date had passed and were therefore moved to status=done.
	SprintsMarkedDone int
	// PlannedActivated is the id of the planned sprint promoted to active
	// during this sweep, or zero-UUID when none was promoted.
	PlannedActivated pgtype.UUID
	// NextSprintCreated is the id of the newly-created planned sprint, or
	// zero-UUID when no new sprint was created (active not in lead-days
	// window, or a later planned sprint already exists).
	NextSprintCreated pgtype.UUID
	// IncompleteIssuesMoved is the number of unfinished issues moved from
	// the previous active sprint to the new sprint (only set when
	// move_incomplete_enabled is true and a new sprint was created).
	IncompleteIssuesMoved int
	// RecurringTasksCloned is the number of recurring task templates
	// cloned into the new sprint (only set when a new sprint was created).
	RecurringTasksCloned int
}

// NewSweeper builds a Sweeper. The cerebro queries are required; the
// upstream queries are needed for IncrementIssueCounter + CreateIssue
// (recurring-task clone).
func NewSweeper(pool *pgxpool.Pool, cerebro *cerebrodb.Queries, upstream *db.Queries) *Sweeper {
	return &Sweeper{Pool: pool, Cerebro: cerebro, Upstream: upstream, nowFunc: time.Now}
}

// Run blocks on ctx and ticks the sweep at the requested interval. Returns
// when ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	// Tick once up front so a fresh process catches up immediately, then
	// settle into the requested cadence.
	if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("cerebro sprint sweeper: initial tick failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("cerebro sprint sweeper: tick failed", "error", err)
			}
		}
	}
}

// Tick runs one pass: for every project with auto-create enabled, advance
// active → done where overdue, activate planned where due, and create the
// next sprint when within the lead-days window. Workspaces where the
// cerebro_sprints feature flag is OFF are skipped — the flag defaults OFF,
// so a fresh workspace whose owner has never visited Feature Flags gets no
// sprint activity.
func (s *Sweeper) Tick(ctx context.Context) error {
	settings, err := s.Cerebro.ListCerebroSprintSettingsForAutoCreate(ctx)
	if err != nil {
		return fmt.Errorf("list auto-create settings: %w", err)
	}
	flagCache := make(map[[16]byte]bool, 4)
	for _, row := range settings {
		full := cerebrodb.CerebroSprintSetting{
			ProjectID:             row.ProjectID,
			WorkspaceID:           row.WorkspaceID,
			Enabled:               row.Enabled,
			DurationUnit:          row.DurationUnit,
			DurationCount:         row.DurationCount,
			StartWeekday:          row.StartWeekday,
			NameTemplate:          row.NameTemplate,
			AutoCreateEnabled:     row.AutoCreateEnabled,
			AutoCreateLeadDays:    row.AutoCreateLeadDays,
			MoveIncompleteEnabled: row.MoveIncompleteEnabled,
			Timezone:              row.Timezone,
		}
		enabled, err := s.isSprintsFlagEnabled(ctx, full.WorkspaceID, flagCache)
		if err != nil {
			slog.Warn("cerebro sprint sweeper: flag check failed",
				"workspace_id", util.UUIDToString(full.WorkspaceID),
				"error", err,
			)
			continue
		}
		if !enabled {
			continue
		}
		if _, err := s.handleProject(ctx, full); err != nil {
			slog.Warn("cerebro sprint sweeper: project failed",
				"project_id", util.UUIDToString(full.ProjectID),
				"error", err,
			)
		}
	}
	return nil
}

// SweepProject runs the sweep logic for a single project synchronously and
// returns a summary of what changed. Used by the ops/QA endpoint to verify
// behaviour without waiting for the daily tick.
//
// Honors the workspace cerebro_sprints flag — when OFF, returns a result
// with FlagEnabled=false and does nothing else, mirroring Tick semantics.
// Returns pgx.ErrNoRows if the project has no sprint_settings row.
func (s *Sweeper) SweepProject(ctx context.Context, projectID pgtype.UUID) (ProjectSweepResult, error) {
	settings, err := s.Cerebro.GetCerebroSprintSettings(ctx, projectID)
	if err != nil {
		return ProjectSweepResult{}, err
	}
	enabled, err := s.isSprintsFlagEnabled(ctx, settings.WorkspaceID, map[[16]byte]bool{})
	if err != nil {
		return ProjectSweepResult{}, fmt.Errorf("flag check: %w", err)
	}
	if !enabled {
		return ProjectSweepResult{FlagEnabled: false}, nil
	}
	res, err := s.handleProject(ctx, settings)
	if err != nil {
		return res, err
	}
	res.FlagEnabled = true
	return res, nil
}

// isSprintsFlagEnabled reports whether the cerebro_sprints workspace-level
// flag is ON. The lookup is cached per Tick so a workspace with several
// projects pays one DB round-trip, not N.
func (s *Sweeper) isSprintsFlagEnabled(ctx context.Context, workspaceID pgtype.UUID, cache map[[16]byte]bool) (bool, error) {
	if !workspaceID.Valid {
		return false, nil
	}
	if v, ok := cache[workspaceID.Bytes]; ok {
		return v, nil
	}
	enabled, err := s.Cerebro.GetCerebroSprintsFlagForWorkspace(ctx, workspaceID)
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

func (s *Sweeper) handleProject(ctx context.Context, settings cerebrodb.CerebroSprintSetting) (ProjectSweepResult, error) {
	loc := LoadTimezone(settings.Timezone)
	today := TodayIn(loc, s.nowFunc())

	var res ProjectSweepResult

	marked, err := s.advanceExpiredActive(ctx, settings, today)
	if err != nil {
		return res, fmt.Errorf("advance expired active: %w", err)
	}
	res.SprintsMarkedDone = marked

	activated, err := s.activatePlannedDue(ctx, settings, today)
	if err != nil {
		return res, fmt.Errorf("activate planned: %w", err)
	}
	res.PlannedActivated = activated

	created, moved, cloned, err := s.maybeCreateNext(ctx, settings, today)
	if err != nil {
		return res, err
	}
	res.NextSprintCreated = created
	res.IncompleteIssuesMoved = moved
	res.RecurringTasksCloned = cloned
	return res, nil
}

// advanceExpiredActive marks any active sprint that has passed its end as
// done. Returns the number of sprints transitioned.
func (s *Sweeper) advanceExpiredActive(ctx context.Context, settings cerebrodb.CerebroSprintSetting, today time.Time) (int, error) {
	expired, err := s.Cerebro.ListExpiredActiveCerebroSprints(ctx, pgtype.Date{Time: today.AddDate(0, 0, -1), Valid: true})
	if err != nil {
		return 0, err
	}
	var marked int
	for _, sprint := range expired {
		if !uuidEqual(sprint.ProjectID, settings.ProjectID) {
			// The query is workspace-wide so we filter to the project we
			// are currently sweeping.
			continue
		}
		if err := s.Cerebro.SetCerebroSprintStatus(ctx, cerebrodb.SetCerebroSprintStatusParams{
			ID:     sprint.ID,
			Status: StatusDone,
		}); err != nil {
			return marked, err
		}
		marked++
	}
	return marked, nil
}

// activatePlannedDue promotes the earliest planned sprint to active when
// (a) there is no other active sprint and (b) its start_date has arrived.
// Returns the id of the activated sprint (zero-valued pgtype.UUID when none).
func (s *Sweeper) activatePlannedDue(ctx context.Context, settings cerebrodb.CerebroSprintSetting, today time.Time) (pgtype.UUID, error) {
	_, err := s.Cerebro.GetActiveCerebroSprintByProject(ctx, settings.ProjectID)
	if err == nil {
		return pgtype.UUID{}, nil // active sprint already present
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, err
	}
	sprints, err := s.Cerebro.ListCerebroSprintsByProject(ctx, settings.ProjectID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	// ListCerebroSprintsByProject returns DESC by sequence — walk reversed
	// so we pick the *earliest* planned sprint whose start has arrived.
	for i := len(sprints) - 1; i >= 0; i-- {
		sp := sprints[i]
		if sp.Status != StatusPlanned {
			continue
		}
		if !sp.StartDate.Valid || sp.StartDate.Time.After(today) {
			continue
		}
		if err := s.Cerebro.SetCerebroSprintStatus(ctx, cerebrodb.SetCerebroSprintStatusParams{
			ID:     sp.ID,
			Status: StatusActive,
		}); err != nil {
			return pgtype.UUID{}, err
		}
		return sp.ID, nil
	}
	return pgtype.UUID{}, nil
}

// maybeCreateNext creates the next planned sprint when the active sprint
// is within lead_days of its end and no later sprint exists yet. Returns
// the new sprint id (zero-value when nothing was created), the count of
// incomplete issues moved, and the count of recurring tasks cloned.
func (s *Sweeper) maybeCreateNext(ctx context.Context, settings cerebrodb.CerebroSprintSetting, today time.Time) (pgtype.UUID, int, int, error) {
	active, err := s.Cerebro.GetActiveCerebroSprintByProject(ctx, settings.ProjectID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, 0, 0, nil
		}
		return pgtype.UUID{}, 0, 0, err
	}
	if !active.EndDate.Valid {
		return pgtype.UUID{}, 0, 0, nil
	}
	leadDays := int(settings.AutoCreateLeadDays)
	if leadDays < 0 {
		leadDays = 0
	}
	threshold := today.AddDate(0, 0, leadDays)
	// Window check: end_date is on or before (today + lead_days). The "X
	// days before" value is the setting — never a constant — so changing
	// AutoCreateLeadDays in the DB changes when the sweeper fires.
	if active.EndDate.Time.After(threshold) {
		return pgtype.UUID{}, 0, 0, nil
	}

	latest, err := s.Cerebro.GetLatestCerebroSprintByProject(ctx, settings.ProjectID)
	if err != nil {
		return pgtype.UUID{}, 0, 0, err
	}
	if !uuidEqual(latest.ID, active.ID) {
		// A later (planned) sprint already exists; nothing to create.
		return pgtype.UUID{}, 0, 0, nil
	}

	nextStart := ComputeNextStart(active.EndDate.Time, settings.DurationUnit, int(settings.StartWeekday))
	nextEnd := ComputeEnd(nextStart, settings.DurationUnit, int(settings.DurationCount))
	nextSeq := active.SequenceNo + 1
	nextName := ApplyNameTemplate(settings.NameTemplate, nextSeq)

	var (
		newSprintID pgtype.UUID
		moved       int
		cloned      int
	)
	err = s.runInTx(ctx, func(tx pgx.Tx) error {
		ctx := ctx
		cqtx := s.Cerebro.WithTx(tx)
		dqtx := s.Upstream.WithTx(tx)

		newSprint, err := cqtx.CreateCerebroSprint(ctx, cerebrodb.CreateCerebroSprintParams{
			WorkspaceID: settings.WorkspaceID,
			ProjectID:   settings.ProjectID,
			Name:        nextName,
			SequenceNo:  nextSeq,
			Status:      StatusPlanned,
			StartDate:   pgtype.Date{Time: nextStart, Valid: true},
			EndDate:     pgtype.Date{Time: nextEnd, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("create next sprint: %w", err)
		}
		newSprintID = newSprint.ID

		if settings.MoveIncompleteEnabled {
			ids, err := cqtx.ListIncompleteIssuesInCerebroSprint(ctx, active.ID)
			if err != nil {
				return fmt.Errorf("list incomplete issues: %w", err)
			}
			if len(ids) > 0 {
				if err := cqtx.MoveCerebroSprintIssuesBatch(ctx, cerebrodb.MoveCerebroSprintIssuesBatchParams{
					SprintID: newSprint.ID,
					Column2:  ids,
				}); err != nil {
					return fmt.Errorf("move incomplete issues: %w", err)
				}
				moved = len(ids)
			}
		}

		c, err := s.cloneRecurringTasks(ctx, tx, cqtx, dqtx, settings, newSprint)
		if err != nil {
			return fmt.Errorf("clone recurring tasks: %w", err)
		}
		cloned = c
		return nil
	})
	if err != nil {
		return pgtype.UUID{}, 0, 0, err
	}
	return newSprintID, moved, cloned, nil
}

func (s *Sweeper) cloneRecurringTasks(ctx context.Context, tx pgx.Tx, cqtx *cerebrodb.Queries, dqtx *db.Queries, settings cerebrodb.CerebroSprintSetting, sprint cerebrodb.CerebroSprint) (int, error) {
	templates, err := cqtx.ListCerebroSprintRecurringTasksForCadence(ctx, cerebrodb.ListCerebroSprintRecurringTasksForCadenceParams{
		ProjectID:    settings.ProjectID,
		CadenceUnit:  settings.DurationUnit,
		CadenceCount: settings.DurationCount,
	})
	if err != nil {
		return 0, err
	}
	if len(templates) == 0 {
		return 0, nil
	}
	// The sweeper has no user session, so we attribute the cloned issues
	// to the workspace owner (or oldest admin). issue.creator_id is NOT
	// NULL; an empty UUID hits the constraint and aborts the whole tick.
	creatorID, err := cqtx.GetCerebroSprintRecurringIssueCreator(ctx, settings.WorkspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("workspace %s has no owner/admin to attribute recurring issue to", util.UUIDToString(settings.WorkspaceID))
		}
		return 0, fmt.Errorf("resolve recurring issue creator: %w", err)
	}
	var cloned int
	for _, t := range templates {
		number, err := dqtx.IncrementIssueCounter(ctx, settings.WorkspaceID)
		if err != nil {
			return cloned, fmt.Errorf("issue counter: %w", err)
		}
		position, err := issueposition.NextTopPosition(ctx, tx, settings.WorkspaceID, "backlog")
		if err != nil {
			return cloned, fmt.Errorf("issue position: %w", err)
		}
		priority := "none"
		if t.Priority.Valid && t.Priority.String != "" {
			priority = t.Priority.String
		}
		issue, err := dqtx.CreateIssue(ctx, db.CreateIssueParams{
			WorkspaceID:  settings.WorkspaceID,
			Title:        t.Title,
			Description:  t.Description,
			Status:       "backlog",
			Priority:     priority,
			AssigneeType: t.AssigneeType,
			AssigneeID:   t.AssigneeID,
			CreatorType:  "member",
			CreatorID:    creatorID,
			Position:     position,
			Number:       number,
			ProjectID:    settings.ProjectID,
		})
		if err != nil {
			return cloned, fmt.Errorf("create recurring issue: %w", err)
		}
		if err := cqtx.AssignIssueToCerebroSprint(ctx, cerebrodb.AssignIssueToCerebroSprintParams{
			IssueID:  issue.ID,
			SprintID: sprint.ID,
		}); err != nil {
			return cloned, fmt.Errorf("assign recurring issue: %w", err)
		}
		cloned++
	}
	return cloned, nil
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

