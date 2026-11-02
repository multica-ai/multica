package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Routine states, as the squad panel, the digest and the Secretary read them.
// "held" (a phase 4 policy hold) is not derived yet.
const (
	RoutineStateOnTime   = "on_time"
	RoutineStateNoOutput = "no_output"
	RoutineStateLate     = "late"
	RoutineStateFailed   = "failed"
	RoutineStatePaused   = "paused"
	RoutineStateNeverRan = "never_ran"
)

// RoutineCompliance is one routine with the state derived for it on read.
type RoutineCompliance struct {
	AutopilotID    string
	SquadID        string
	Title          string
	Purpose        string
	ExpectedOutput string
	Required       bool
	State          string
}

// routineFacts is what the database says about one routine. It is the input to
// deriveRoutineState, which holds the rules and needs no database to test.
type routineFacts struct {
	autopilotStatus string
	expectedOutput  string
	graceHours      int32

	hasSchedule      bool
	cronExpression   string
	timezone         string
	scheduleBaseline time.Time

	hasDecider    bool
	deciderStatus string
	hasIssue      bool
	hasProposal   bool
	hasKBDocument bool
	hasReport     bool
	hasNoAction   bool
}

// RoutineComplianceStates derives the state of every routine in the workspace, or
// of one squad's routines when squadID is valid. Nothing is cached: each call
// reads the current runs and records.
func RoutineComplianceStates(ctx context.Context, q *db.Queries, workspaceID, squadID pgtype.UUID, now time.Time) ([]RoutineCompliance, error) {
	rows, err := q.ListRoutineComplianceFacts(ctx, db.ListRoutineComplianceFactsParams{
		WorkspaceID: workspaceID,
		SquadID:     squadID,
	})
	if err != nil {
		return nil, fmt.Errorf("list routine compliance facts: %w", err)
	}
	out := make([]RoutineCompliance, 0, len(rows))
	for _, row := range rows {
		facts := routineFacts{
			autopilotStatus:  row.AutopilotStatus,
			expectedOutput:   row.ExpectedOutput,
			graceHours:       row.GraceHours,
			hasSchedule:      row.HasSchedule,
			cronExpression:   row.ScheduleCronExpression,
			timezone:         row.ScheduleTimezone,
			scheduleBaseline: row.ScheduleBaseline.Time,
			hasDecider:       row.HasDecider,
			deciderStatus:    row.DeciderStatus,
			hasIssue:         row.DeciderHasIssue,
			hasProposal:      row.HasProposal,
			hasKBDocument:    row.HasKbDocument,
			hasReport:        row.HasReport,
			hasNoAction:      row.HasNoAction,
		}
		out = append(out, RoutineCompliance{
			AutopilotID:    util.UUIDToString(row.AutopilotID),
			SquadID:        util.UUIDToString(row.SquadID),
			Title:          row.AutopilotTitle,
			Purpose:        row.Purpose,
			ExpectedOutput: row.ExpectedOutput,
			Required:       row.Required,
			State:          deriveRoutineState(facts, now),
		})
	}
	return out, nil
}

// deriveRoutineState applies the rules in the spec's routine compliance table. The
// order matters: a paused routine is not judged, a routine whose next fire is
// overdue is late whatever its last run did, and a finished run decides the rest.
func deriveRoutineState(f routineFacts, now time.Time) string {
	switch {
	case f.autopilotStatus != "active":
		return RoutineStatePaused
	case f.late(now):
		return RoutineStateLate
	case !f.hasDecider:
		return RoutineStateNeverRan
	case f.deciderStatus != "completed":
		// failed, or skipped for a reason that is not a policy hold.
		return RoutineStateFailed
	case f.expectedOutput == "none" || f.produced() || f.hasNoAction:
		return RoutineStateOnTime
	default:
		return RoutineStateNoOutput
	}
}

// late reports whether a scheduled fire is overdue by more than the grace. The
// scheduler dispatches the latest fire after the last planned one, so a fire
// after the baseline that is older than the grace has been missed.
func (f routineFacts) late(now time.Time) bool {
	if !f.hasSchedule {
		return false
	}
	// A stored cron was validated when the schedule was written, so a parse
	// error cannot happen here; treating it as not late keeps the state safe.
	overdue, err := NextOccurrencesUTC(f.cronExpression, f.timezone, f.scheduleBaseline,
		now.Add(-time.Duration(f.graceHours)*time.Hour))
	return err == nil && len(overdue) > 0
}

// produced reports whether the decider's run made the output the routine
// expects. A "none" routine never reaches this check.
func (f routineFacts) produced() bool {
	switch f.expectedOutput {
	case "issue":
		return f.hasIssue
	case "proposal":
		return f.hasProposal
	case "kb_document":
		return f.hasKBDocument
	case "report":
		return f.hasReport
	default:
		return false
	}
}
