package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Company controls (spec 4.5). The boss sets a state; the quiet window applies
// on top of it. Paused holds routine and delegated runs. Quiet defers routine
// runs during the daily window in the workspace timezone. Human and exempt runs
// are never held by either.

// Company states. The stored setting is one of these; the state in force is
// computed from the setting and the quiet window.
const (
	CompanyStateRunning = "running"
	CompanyStatePaused  = "paused"
	CompanyStateQuiet   = "quiet"
)

// Verdict reasons. They are the dispatch reason codes of the same names.
const (
	VerdictReasonCompanyPaused = "company_paused"
	VerdictReasonQuietHours    = "quiet_hours"
)

var (
	// ErrCompanyStateInvalid: the state is not running, paused or quiet.
	ErrCompanyStateInvalid = errors.New("state must be running, paused or quiet")
	// ErrQuietHoursInvalid: the window is malformed or names no timezone.
	ErrQuietHoursInvalid = errors.New("quiet hours need start and end as HH:MM, a different start and end, and an IANA timezone")
)

// Verdict is the answer for one run. Reason is set when Admit is false.
type Verdict struct {
	Admit  bool
	Reason string
}

// QuietHours is the daily window. Start and End are "HH:MM" in Timezone. A
// window whose start is after its end runs over midnight.
type QuietHours struct {
	Enabled  bool
	Start    string
	End      string
	Timezone string
}

// CompanyStateView is the company's state as the API reports it.
type CompanyStateView struct {
	// State is the state in force now.
	State string
	// Setting is the state the boss set. An open quiet window does not change
	// it: a company set to running is quiet until the window closes.
	Setting    string
	QuietHours QuietHours
	UpdatedAt  time.Time
}

// GovernanceActor is who changes the company's controls.
type GovernanceActor struct {
	// Type is member, agent or system.
	Type string
	ID   pgtype.UUID
}

// effectiveState is the state in force at now.
func effectiveState(setting string, qh QuietHours, now time.Time) string {
	switch {
	case setting == CompanyStatePaused:
		return CompanyStatePaused
	case setting == CompanyStateQuiet || quietWindowOpen(qh, now):
		return CompanyStateQuiet
	}
	return CompanyStateRunning
}

// quietWindowOpen reports whether now falls in the window, in its timezone. The
// window is [start, end). A start equal to the end is no window.
func quietWindowOpen(qh QuietHours, now time.Time) bool {
	if !qh.Enabled {
		return false
	}
	start, err1 := parseClockMinutes(qh.Start)
	end, err2 := parseClockMinutes(qh.End)
	if err1 != nil || err2 != nil || start == end {
		return false
	}
	loc, err := time.LoadLocation(qh.Timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	minute := local.Hour()*60 + local.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}

// companyVerdict decides one run of class at now, given the stored setting and
// the window. It is the whole admission rule for the company controls.
func companyVerdict(setting string, qh QuietHours, class RunClass, now time.Time) Verdict {
	switch effectiveState(setting, qh, now) {
	case CompanyStatePaused:
		if class == RunClassRoutine || class == RunClassDelegated {
			return Verdict{Reason: VerdictReasonCompanyPaused}
		}
	case CompanyStateQuiet:
		if class == RunClassRoutine {
			return Verdict{Reason: VerdictReasonQuietHours}
		}
	}
	return Verdict{Admit: true}
}

// CompanyVerdict decides one run of class in workspace ws at now, reading the
// company state through q. Callers inside a transaction pass their own handle.
func CompanyVerdict(ctx context.Context, q *db.Queries, ws pgtype.UUID, class RunClass, now time.Time) (Verdict, error) {
	row, err := q.GetWorkspaceCompanyState(ctx, ws)
	if err != nil {
		return Verdict{}, fmt.Errorf("load company state: %w", err)
	}
	return companyVerdict(row.CompanyState, quietHoursOf(row.QuietEnabled, row.QuietStart, row.QuietEnd, row.QuietTimezone), class, now), nil
}

// CompanyIsPaused reports whether the company is paused. Quiet hours do not
// count: they defer routine runs and nothing else.
func CompanyIsPaused(ctx context.Context, q *db.Queries, ws pgtype.UUID) (bool, error) {
	row, err := q.GetWorkspaceCompanyState(ctx, ws)
	if err != nil {
		return false, fmt.Errorf("load company state: %w", err)
	}
	return row.CompanyState == CompanyStatePaused, nil
}

// CompanyStateService reads and changes the company's controls.
type CompanyStateService struct {
	Queries   *db.Queries
	TxStarter TxStarter
	// Tasks cancels running routine and delegated tasks in PauseAndStop.
	Tasks *TaskService
}

// Get returns the company's state now.
func (s *CompanyStateService) Get(ctx context.Context, ws pgtype.UUID) (CompanyStateView, error) {
	row, err := s.Queries.GetWorkspaceCompanyState(ctx, ws)
	if err != nil {
		return CompanyStateView{}, fmt.Errorf("load company state: %w", err)
	}
	return companyStateView(row.CompanyState, row.CompanyStateUpdatedAt, quietHoursOf(row.QuietEnabled, row.QuietStart, row.QuietEnd, row.QuietTimezone), time.Now()), nil
}

// Verdict decides one run of class in workspace ws at now. The claim gate and
// the autopilot dispatch use it.
func (s *CompanyStateService) Verdict(ctx context.Context, ws pgtype.UUID, class RunClass, now time.Time) (Verdict, error) {
	return CompanyVerdict(ctx, s.Queries, ws, class, now)
}

// Set changes the stored state. Entering paused records company_paused and
// leaving it records company_resumed, in the same transaction as the change.
func (s *CompanyStateService) Set(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, state string) (CompanyStateView, error) {
	if !validCompanyState(state) {
		return CompanyStateView{}, ErrCompanyStateInvalid
	}
	var out CompanyStateView
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.setInTx(ctx, q, ws, actor, state, nil)
		return err
	})
	return out, err
}

// SetQuietHours saves the daily window.
func (s *CompanyStateService) SetQuietHours(ctx context.Context, ws pgtype.UUID, qh QuietHours) (CompanyStateView, error) {
	if err := validateQuietHours(qh); err != nil {
		return CompanyStateView{}, err
	}
	row, err := s.Queries.SetWorkspaceQuietHours(ctx, db.SetWorkspaceQuietHoursParams{
		WorkspaceID:   ws,
		QuietEnabled:  qh.Enabled,
		QuietStart:    clockTime(qh.Start),
		QuietEnd:      clockTime(qh.End),
		QuietTimezone: qh.Timezone,
	})
	if err != nil {
		return CompanyStateView{}, fmt.Errorf("save quiet hours: %w", err)
	}
	return companyStateView(row.CompanyState, row.CompanyStateUpdatedAt, quietHoursOf(row.QuietEnabled, row.QuietStart, row.QuietEnd, row.QuietTimezone), time.Now()), nil
}

// PauseAndStop pauses the company and cancels its running routine and delegated
// tasks through the task cancel path. Queued tasks stay queued: the claim gate
// holds them while the company is paused. The pause commits first, so no run
// can be claimed after the cancellation starts. It returns how many runs were
// cancelled. A run that fails to cancel is reported and the rest still stop.
func (s *CompanyStateService) PauseAndStop(ctx context.Context, ws pgtype.UUID, actor GovernanceActor) (CompanyStateView, int, error) {
	var out CompanyStateView
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = s.setInTx(ctx, q, ws, actor, CompanyStatePaused, map[string]any{"stop_running": true})
		return err
	})
	if err != nil {
		return CompanyStateView{}, 0, err
	}
	tasks, err := s.Queries.ListRunningTasksInWorkspace(ctx, ws)
	if err != nil {
		return out, 0, fmt.Errorf("list running tasks: %w", err)
	}
	cancelled := 0
	var errs []error
	for _, task := range tasks {
		class, err := RunClassOfTask(ctx, s.Queries, task)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if class != RunClassRoutine && class != RunClassDelegated {
			continue
		}
		if _, err := s.Tasks.CancelTaskWithReason(ctx, task.ID,
			"The company was paused, so this run was stopped.", VerdictReasonCompanyPaused); err != nil {
			errs = append(errs, fmt.Errorf("cancel task %s: %w", util.UUIDToString(task.ID), err))
			continue
		}
		cancelled++
	}
	slog.Info("company paused and stopped running work", "workspace_id", util.UUIDToString(ws), "cancelled", cancelled, "failed", len(errs))
	return out, cancelled, errors.Join(errs...)
}

// setInTx applies a state change and its governance entry through q. A change
// to the state already stored does nothing.
func (s *CompanyStateService) setInTx(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, state string, details map[string]any) (CompanyStateView, error) {
	prev, err := q.LockWorkspaceCompanyState(ctx, ws)
	if err != nil {
		return CompanyStateView{}, fmt.Errorf("lock company state: %w", err)
	}
	qh := quietHoursOf(prev.QuietEnabled, prev.QuietStart, prev.QuietEnd, prev.QuietTimezone)
	if prev.CompanyState == state {
		return companyStateView(prev.CompanyState, prev.CompanyStateUpdatedAt, qh, time.Now()), nil
	}
	row, err := q.SetWorkspaceCompanyState(ctx, db.SetWorkspaceCompanyStateParams{WorkspaceID: ws, CompanyState: state})
	if err != nil {
		return CompanyStateView{}, fmt.Errorf("save company state: %w", err)
	}
	var kind, summary string
	switch {
	case state == CompanyStatePaused:
		kind, summary = GovernanceKindCompanyPaused, "The boss paused the company."
	case prev.CompanyState == CompanyStatePaused:
		kind, summary = GovernanceKindCompanyResumed, "The boss resumed the company."
	}
	if kind != "" {
		if details == nil {
			details = map[string]any{}
		}
		details["from"], details["to"] = prev.CompanyState, state
		if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
			WorkspaceID: ws,
			Kind:        kind,
			ActorType:   actor.Type,
			ActorID:     actorIDOf(actor),
			SubjectType: "workspace",
			SubjectID:   ws,
			Summary:     summary,
			Details:     details,
		}); err != nil {
			return CompanyStateView{}, err
		}
	}
	return companyStateView(row.CompanyState, row.CompanyStateUpdatedAt, quietHoursOf(row.QuietEnabled, row.QuietStart, row.QuietEnd, row.QuietTimezone), time.Now()), nil
}

// inTx runs fn in one transaction and commits when it returns nil.
func (s *CompanyStateService) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func actorIDOf(a GovernanceActor) pgtype.UUID {
	if a.Type == GovernanceActorSystem {
		return pgtype.UUID{}
	}
	return a.ID
}

func validCompanyState(state string) bool {
	switch state {
	case CompanyStateRunning, CompanyStatePaused, CompanyStateQuiet:
		return true
	}
	return false
}

func companyStateView(setting string, updatedAt pgtype.Timestamptz, qh QuietHours, now time.Time) CompanyStateView {
	return CompanyStateView{
		State:      effectiveState(setting, qh, now),
		Setting:    setting,
		QuietHours: qh,
		UpdatedAt:  updatedAt.Time,
	}
}

// quietHoursOf reads the workspace's window. A stored timezone that no longer
// loads is reported as stored; quietWindowOpen then treats it as UTC.
func quietHoursOf(enabled bool, start, end pgtype.Time, tz string) QuietHours {
	return QuietHours{Enabled: enabled, Start: formatClock(start), End: formatClock(end), Timezone: tz}
}

func validateQuietHours(qh QuietHours) error {
	start, err := parseClockMinutes(qh.Start)
	if err != nil {
		return ErrQuietHoursInvalid
	}
	end, err := parseClockMinutes(qh.End)
	if err != nil {
		return ErrQuietHoursInvalid
	}
	if qh.Enabled && start == end {
		return ErrQuietHoursInvalid
	}
	if _, err := time.LoadLocation(qh.Timezone); err != nil || qh.Timezone == "" {
		return ErrQuietHoursInvalid
	}
	return nil
}

// parseClockMinutes reads "HH:MM" as minutes after midnight.
func parseClockMinutes(s string) (int, error) {
	hh, mm, ok := strings.Cut(s, ":")
	if !ok || len(hh) != 2 || len(mm) != 2 {
		return 0, ErrQuietHoursInvalid
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, ErrQuietHoursInvalid
	}
	return h*60 + m, nil
}

func formatClock(t pgtype.Time) string {
	minutes := int(t.Microseconds / int64(time.Minute/time.Microsecond))
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func clockTime(s string) pgtype.Time {
	minutes, err := parseClockMinutes(s)
	if err != nil {
		return pgtype.Time{}
	}
	return pgtype.Time{Microseconds: int64(minutes) * int64(time.Minute/time.Microsecond), Valid: true}
}

// companyHold reports whether the company's controls or a budget keep an
// autopilot run from starting now, with the reason code. A quiet_hours code
// defers the run (see recordDeferredRun); company_paused and budget_hold skip
// it. The returned bool is false when the run may start.
func (s *AutopilotService) companyHold(ctx context.Context, ap db.Autopilot, source string) (string, dispatch.ReasonCode, bool) {
	now := time.Now()
	row, err := s.Queries.GetWorkspaceCompanyState(ctx, ap.WorkspaceID)
	if err != nil {
		// As the admission check does: a transient failure never swallows a run.
		slog.Warn("autopilot admission: company state unreadable; dispatching",
			"autopilot_id", util.UUIDToString(ap.ID), "error", err)
		return "", "", false
	}
	qh := quietHoursOf(row.QuietEnabled, row.QuietStart, row.QuietEnd, row.QuietTimezone)
	ws, err := s.Queries.GetWorkspace(ctx, ap.WorkspaceID)
	if err != nil {
		slog.Warn("autopilot admission: workspace unreadable; dispatching",
			"autopilot_id", util.UUIDToString(ap.ID), "error", err)
		return "", "", false
	}
	class := ResolveRunClass(RunClassInput{
		AutopilotSource: source,
		SecretaryBrief:  isSecretaryBriefAutopilot(ap.Title, ap.AssigneeType, ap.AssigneeID, ws.SecretaryAgentID),
	})
	v := companyVerdict(row.CompanyState, qh, class, now)
	if !v.Admit {
		if v.Reason == VerdictReasonCompanyPaused {
			return "the company is paused; routine runs are held", dispatch.ReasonCompanyPaused, true
		}
		return "quiet hours; routine runs are deferred", dispatch.ReasonQuietHours, true
	}
	if class == RunClassRoutine || class == RunClassDelegated {
		if _, held, err := s.autopilotBudgetHold(ctx, ap, class); err != nil {
			slog.Warn("autopilot admission: budgets unreadable; dispatching",
				"autopilot_id", util.UUIDToString(ap.ID), "error", err)
		} else if held {
			return "the budget is at its limit; routine runs are held", dispatch.ReasonBudgetHold, true
		}
	}
	return "", "", false
}

// wakeupRunClass is the class of an issue wakeup's run: a person's "wake now" is
// human, every other firing is routine.
func wakeupRunClass(manual bool) RunClass {
	if manual {
		return RunClassHuman
	}
	return RunClassRoutine
}
