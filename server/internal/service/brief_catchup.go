package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The brief catch-up (spec 2.9, plan 9.13). A boss's machine may be asleep at
// the brief's 08:00 schedule, so the day's brief never starts. Once an hour, for
// each workspace whose Secretary has an active daily brief, catch-up fires that
// autopilot once, with source 'catchup', when today's brief is still missing and
// the Secretary can run now.

// BriefCatchupSource is the autopilot run source a catch-up run records. It is
// the audit trail's reason for a run that no schedule started.
const BriefCatchupSource = "catchup"

const (
	// WorkspaceSettingBriefCatchupEnabled turns catch-up off when false.
	WorkspaceSettingBriefCatchupEnabled = "brief_catchup_enabled"
	// WorkspaceSettingBriefCatchupHour is the local hour, 0 to 23, from which a
	// missed brief is caught up.
	WorkspaceSettingBriefCatchupHour = "brief_catchup_hour"
	// BriefCatchupDefaultHour is 10:00 local when the workspace does not say.
	BriefCatchupDefaultHour = 10
)

// BriefCatchupSettings reads catch-up's two settings from a workspace's settings
// JSON. A missing or unreadable value takes its default: catch-up on, from 10:00.
// An hour outside 0 to 23 takes the default too.
func BriefCatchupSettings(settings []byte) (enabled bool, hour int) {
	enabled, hour = true, BriefCatchupDefaultHour
	var values map[string]json.RawMessage
	if json.Unmarshal(settings, &values) != nil {
		return enabled, hour
	}
	var on bool
	if raw, ok := values[WorkspaceSettingBriefCatchupEnabled]; ok && json.Unmarshal(raw, &on) == nil {
		enabled = on
	}
	var h int
	if raw, ok := values[WorkspaceSettingBriefCatchupHour]; ok && json.Unmarshal(raw, &h) == nil && h >= 0 && h <= 23 {
		hour = h
	}
	return enabled, hour
}

// briefCatchupDay is the local day that now falls in, and the local hour.
// The window is [start, end). The local day is the timezone of the daily brief's
// schedule, the clock that starts the brief, since a workspace has no timezone of
// its own. A missing or unreadable timezone is UTC.
type briefCatchupDay struct {
	hour       int
	start, end time.Time
}

func briefCatchupLocalDay(now time.Time, timezone string) briefCatchupDay {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	local := now.In(loc)
	y, m, d := local.Date()
	return briefCatchupDay{
		hour:  local.Hour(),
		start: time.Date(y, m, d, 0, 0, 0, 0, loc),
		end:   time.Date(y, m, d+1, 0, 0, 0, 0, loc),
	}
}

// BriefCatchupDispatcher starts an autopilot run. The autopilot service is the
// production implementation; the catch-up goes through it, so admission,
// attribution and run recording are the same as any other trigger.
type BriefCatchupDispatcher interface {
	DispatchAutopilot(ctx context.Context, autopilot db.Autopilot, triggerID pgtype.UUID, source string, payload []byte) (*db.AutopilotRun, error)
}

// BriefCatchupService fires the missed daily briefs.
type BriefCatchupService struct {
	q          *db.Queries
	dispatcher BriefCatchupDispatcher
	now        func() time.Time
}

func NewBriefCatchupService(q *db.Queries, dispatcher BriefCatchupDispatcher) *BriefCatchupService {
	return &BriefCatchupService{q: q, dispatcher: dispatcher, now: time.Now}
}

// CatchUpBriefs runs one pass and returns how many daily briefs it fired.
//
// A workspace is checked and fired on its own, so one that fails is logged and
// left for the next hourly pass while the others go on. The pass is idempotent:
// a run it fired is either still in flight or counted as today's catch-up, and
// a published brief ends the question for the day, so a second pass in the same
// hour or later the same day fires nothing. The scheduler runs one pass per hour
// under a lease, so two passes do not race.
func (s *BriefCatchupService) CatchUpBriefs(ctx context.Context) (int, error) {
	candidates, err := s.q.ListBriefCatchupCandidates(ctx, DailyBriefTitle)
	if err != nil {
		return 0, fmt.Errorf("list brief catch-up candidates: %w", err)
	}
	now := s.now()
	fired := 0
	var errs []error
	for _, c := range candidates {
		ok, err := s.catchUpOne(ctx, c, now)
		if err != nil {
			slog.Warn("brief catch-up failed; retried on the next pass",
				"autopilot_id", util.UUIDToString(c.AutopilotID),
				"workspace_id", util.UUIDToString(c.WorkspaceID),
				"error", err)
			errs = append(errs, err)
			continue
		}
		if ok {
			fired++
		}
	}
	return fired, errors.Join(errs...)
}

// catchUpOne fires one workspace's daily brief if every condition holds. The
// conditions are checked cheapest first and each refusal returns without a write.
func (s *BriefCatchupService) catchUpOne(ctx context.Context, c db.ListBriefCatchupCandidatesRow, now time.Time) (bool, error) {
	enabled, hour := BriefCatchupSettings(c.WorkspaceSettings)
	if !enabled {
		return false, nil
	}
	timezone := ""
	if c.TriggerTimezone.Valid {
		timezone = c.TriggerTimezone.String
	}
	day := briefCatchupLocalDay(now, timezone)
	if day.hour < hour {
		return false, nil
	}

	briefs, err := s.q.CountDailyBriefsBetween(ctx, db.CountDailyBriefsBetweenParams{
		WorkspaceID: c.WorkspaceID,
		WindowStart: pgtype.Timestamptz{Time: day.start, Valid: true},
		WindowEnd:   pgtype.Timestamptz{Time: day.end, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("count daily briefs: %w", err)
	}
	if briefs > 0 {
		return false, nil
	}

	caught, err := s.q.CountCatchupRunsBetween(ctx, db.CountCatchupRunsBetweenParams{
		AutopilotID: c.AutopilotID,
		WindowStart: pgtype.Timestamptz{Time: day.start, Valid: true},
		WindowEnd:   pgtype.Timestamptz{Time: day.end, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("count catch-up runs: %w", err)
	}
	if caught > 0 {
		return false, nil
	}

	inFlight, err := s.q.CountInFlightAutopilotRuns(ctx, c.AutopilotID)
	if err != nil {
		return false, fmt.Errorf("count in-flight brief runs: %w", err)
	}
	if inFlight > 0 {
		return false, nil
	}

	secretary, err := s.q.GetAgent(ctx, c.SecretaryAgentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("load secretary: %w", err)
	}
	// The runtime must be online and usable by the Secretary, as the admission
	// gate for any run requires. An offline runtime is not an error: the next
	// hourly pass asks again.
	verdict, err := AgentReadiness(ctx, RuntimeLookup{Queries: s.q, Source: obsmetrics.RuntimeLookupSourceAutopilot}, secretary)
	if err != nil {
		return false, fmt.Errorf("check secretary runtime: %w", err)
	}
	if !verdict.Ready() {
		return false, nil
	}

	busy, err := s.q.CountActiveTasksForAgent(ctx, secretary.ID)
	if err != nil {
		return false, fmt.Errorf("count secretary tasks: %w", err)
	}
	if busy > 0 {
		return false, nil
	}

	autopilot, err := s.q.GetAutopilot(ctx, c.AutopilotID)
	if err != nil {
		return false, fmt.Errorf("load daily brief autopilot: %w", err)
	}
	run, err := s.dispatcher.DispatchAutopilot(ctx, autopilot, c.TriggerID, BriefCatchupSource, nil)
	if err != nil {
		return false, fmt.Errorf("dispatch catch-up: %w", err)
	}
	runStatus := ""
	if run != nil {
		runStatus = run.Status
	}
	slog.Info("brief catch-up fired",
		"autopilot_id", util.UUIDToString(c.AutopilotID),
		"workspace_id", util.UUIDToString(c.WorkspaceID),
		"run_status", runStatus,
		"local_hour", day.hour)
	return true, nil
}
