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

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Scorecards (spec 4.6). The nightly job computes, for every live agent, every
// live squad and the company, a scorecard for the last completed ISO week and
// the last completed calendar month, and stores it. Reviews and probation read
// them through ScorecardService.Latest.

// ScorecardScope is the subject of one scorecard and the filters its queries
// take. Agent scopes set AgentID, squad scopes set SquadID, and the company
// scope sets neither.
type ScorecardScope struct {
	SubjectType string
	SubjectID   pgtype.UUID
	AgentID     pgtype.UUID
	SquadID     pgtype.UUID
	// Secretary is true for the workspace's Secretary agent, whose scorecard
	// carries the brief usefulness.
	Secretary bool
}

// ScorecardService computes, stores and reads scorecards.
type ScorecardService struct {
	Queries *db.Queries
	Now     func() time.Time
}

// NewScorecardService returns the scorecard service over q.
func NewScorecardService(q *db.Queries) *ScorecardService {
	return &ScorecardService{Queries: q, Now: time.Now}
}

// ErrScorecardNotFound: no scorecard exists for the subject and period.
var ErrScorecardNotFound = errors.New("scorecard not found")

// ScorecardPeriodBounds returns the half-open UTC window of the last completed
// period of the kind: the ISO week (Monday to Monday) or the calendar month
// before now. The end is the start of the current period.
func ScorecardPeriodBounds(period string, now time.Time) (start, end time.Time, err error) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch period {
	case ScorecardPeriodWeek:
		offset := (int(today.Weekday()) + 6) % 7 // days since Monday
		end = today.AddDate(0, 0, -offset)
		return end.AddDate(0, 0, -7), end, nil
	case ScorecardPeriodMonth:
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return end.AddDate(0, -1, 0), end, nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("period must be week or month, not %q", period)
}

// ScorecardInputsFor reads the inputs of one scope over a window through q.
func ScorecardInputsFor(ctx context.Context, q *db.Queries, ws pgtype.UUID, scope ScorecardScope, start, end time.Time, now time.Time) (ScorecardInputs, error) {
	ps := pgtype.Timestamptz{Time: start, Valid: true}
	pe := pgtype.Timestamptz{Time: end, Valid: true}
	var in ScorecardInputs

	runs, err := q.ScorecardRunCounts(ctx, db.ScorecardRunCountsParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard run counts: %w", err)
	}
	in.Runs = ScorecardRunInputs{Completed: runs.Completed, ChargedFailed: runs.ChargedFailed, UnchargedFailed: runs.UnchargedFailed, FailuresByCause: map[string]int64{}}
	causes, err := q.ScorecardFailureCauses(ctx, db.ScorecardFailureCausesParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard failure causes: %w", err)
	}
	for _, c := range causes {
		in.Runs.FailuresByCause[c.Cause] = c.Runs
	}

	doneRows, err := q.ScorecardDoneIssues(ctx, db.ScorecardDoneIssuesParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard done issues: %w", err)
	}
	for _, r := range doneRows {
		in.Done = append(in.Done, ScorecardIssueRow{
			CreatedAt: r.CreatedAt.Time, DoneAt: r.DoneAt.Time,
			ReopenCount: int64(r.ReopenCount), CompletedRuns: r.CompletedRuns, HumanAfter: r.HumanAfter,
		})
	}

	cost, err := q.ScorecardRunCost(ctx, db.ScorecardRunCostParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard run cost: %w", err)
	}
	in.Cost = ScorecardCostInputs{PricedTicks: cost.PricedTicks, UnpricedTokens: cost.UnpricedTokens}

	props, err := q.ScorecardProposalCounts(ctx, db.ScorecardProposalCountsParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard proposal counts: %w", err)
	}
	in.Proposals = ScorecardProposalInputs{Approved: props.Approved, Decided: props.Decided}

	know, err := q.ScorecardKnowledgeCounts(ctx, db.ScorecardKnowledgeCountsParams{
		WorkspaceID: ws, PeriodStart: ps, PeriodEnd: pe, AgentID: scope.AgentID, SquadID: scope.SquadID,
	})
	if err != nil {
		return in, fmt.Errorf("scorecard knowledge counts: %w", err)
	}
	in.Knowledge = ScorecardKnowledgeInputs{Published: know.Published, Reviewed: know.Reviewed}

	if scope.SubjectType != ScorecardSubjectAgent {
		// Routine compliance is the state of the required routines now: a
		// routine has no state for a past period.
		states, err := RoutineComplianceStates(ctx, q, ws, scope.SquadID, now)
		if err != nil {
			return in, err
		}
		var routines ScorecardRoutineInputs
		for _, r := range states {
			if !r.Required || r.State == RoutineStatePaused {
				continue
			}
			routines.Total++
			if r.State == RoutineStateOnTime {
				routines.OnTime++
			}
		}
		in.Routines = &routines
	}
	if scope.Secretary {
		fb, err := q.ScorecardBriefFeedback(ctx, db.ScorecardBriefFeedbackParams{
			WorkspaceID: ws, AgentID: scope.AgentID, PeriodStart: ps, PeriodEnd: pe,
		})
		if err != nil {
			return in, fmt.Errorf("scorecard brief feedback: %w", err)
		}
		in.Brief = &ScorecardBriefInputs{Useful: fb.Useful, Total: fb.Total}
	}
	return in, nil
}

// Compute reads, computes and stores the scorecard of one scope for one period
// through q. A rerun for the same period replaces the stored row.
func (s *ScorecardService) Compute(ctx context.Context, q *db.Queries, ws pgtype.UUID, scope ScorecardScope, period string) (db.Scorecard, error) {
	now := s.now()
	start, end, err := ScorecardPeriodBounds(period, now)
	if err != nil {
		return db.Scorecard{}, err
	}
	in, err := ScorecardInputsFor(ctx, q, ws, scope, start, end, now)
	if err != nil {
		return db.Scorecard{}, err
	}
	res := ComputeScorecard(in)
	stored, err := json.Marshal(ScorecardDocument{Metrics: res.Metrics, FailuresByCause: res.FailuresByCause})
	if err != nil {
		return db.Scorecard{}, fmt.Errorf("encode scorecard: %w", err)
	}
	return q.UpsertScorecard(ctx, db.UpsertScorecardParams{
		ID:          dbid.NewV7(),
		WorkspaceID: ws,
		SubjectType: scope.SubjectType,
		SubjectID:   scope.SubjectID,
		Period:      period,
		PeriodStart: pgtype.Date{Time: start, Valid: true},
		PeriodEnd:   pgtype.Date{Time: end, Valid: true},
		Metrics:     stored,
		SampleSize:  int32(res.SampleSize),
		Status:      res.Status,
	})
}

// ScorecardDocument is the stored metrics object: the numeric metrics by key,
// and the failed runs by cause.
type ScorecardDocument struct {
	Metrics         map[string]ScorecardMetric `json:"metrics"`
	FailuresByCause map[string]int64           `json:"failures_by_cause"`
}

// DecodeScorecardDocument reads the stored metrics object.
func DecodeScorecardDocument(raw []byte) (ScorecardDocument, error) {
	var doc ScorecardDocument
	if len(raw) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return doc, fmt.Errorf("decode scorecard metrics: %w", err)
	}
	return doc, nil
}

// Latest returns the newest stored scorecard of a subject for a period kind. It
// is the reader other services use; a subject with no row is ErrScorecardNotFound.
func (s *ScorecardService) Latest(ctx context.Context, q *db.Queries, ws pgtype.UUID, subjectType string, subjectID pgtype.UUID, period string) (db.Scorecard, error) {
	rows, err := q.ListScorecards(ctx, db.ListScorecardsParams{
		WorkspaceID: ws,
		SubjectType: pgtype.Text{String: subjectType, Valid: true},
		SubjectID:   subjectID,
		Period:      pgtype.Text{String: period, Valid: true},
		RowLimit:    1,
	})
	if err != nil {
		return db.Scorecard{}, fmt.Errorf("list scorecards: %w", err)
	}
	if len(rows) == 0 {
		return db.Scorecard{}, ErrScorecardNotFound
	}
	return rows[0], nil
}

// LatestOKAsOf returns the newest ok scorecard of a subject for a period that
// ended by asOf. Probation reads its before and after figures this way.
func (s *ScorecardService) LatestOKAsOf(ctx context.Context, q *db.Queries, ws pgtype.UUID, subjectType string, subjectID pgtype.UUID, period string, asOf time.Time) (db.Scorecard, error) {
	row, err := q.GetLatestOKScorecard(ctx, db.GetLatestOKScorecardParams{
		WorkspaceID: ws, SubjectType: subjectType, SubjectID: subjectID, Period: period,
		AsOf: pgtype.Date{Time: asOf, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Scorecard{}, ErrScorecardNotFound
	}
	return row, err
}

func (s *ScorecardService) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// RunNightly computes the last completed week and month for every live agent,
// squad and the company of every workspace. It returns how many scorecards it
// wrote. A failure for one subject is logged and does not stop the rest.
func (s *ScorecardService) RunNightly(ctx context.Context) (int, error) {
	workspaces, err := s.Queries.ListAllWorkspaceIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	written := 0
	for _, ws := range workspaces {
		n, err := s.RunWorkspace(ctx, ws)
		written += n
		if err != nil {
			slog.Warn("scorecards: workspace failed", "workspace_id", util.UUIDToString(ws), "error", err)
		}
	}
	return written, nil
}

// RunWorkspace computes the scorecards of one workspace.
func (s *ScorecardService) RunWorkspace(ctx context.Context, ws pgtype.UUID) (int, error) {
	wsRow, err := s.Queries.GetWorkspace(ctx, ws)
	if err != nil {
		return 0, fmt.Errorf("load workspace: %w", err)
	}
	agents, err := s.Queries.ListAllAgents(ctx, ws)
	if err != nil {
		return 0, fmt.Errorf("list agents: %w", err)
	}
	squads, err := s.Queries.ListSquads(ctx, ws)
	if err != nil {
		return 0, fmt.Errorf("list squads: %w", err)
	}
	var scopes []ScorecardScope
	for _, a := range agents {
		if a.ArchivedAt.Valid {
			continue
		}
		scopes = append(scopes, ScorecardScope{
			SubjectType: ScorecardSubjectAgent, SubjectID: a.ID, AgentID: a.ID,
			Secretary: wsRow.SecretaryAgentID.Valid && wsRow.SecretaryAgentID == a.ID,
		})
	}
	for _, sq := range squads {
		scopes = append(scopes, ScorecardScope{SubjectType: ScorecardSubjectSquad, SubjectID: sq.ID, SquadID: sq.ID})
	}
	scopes = append(scopes, ScorecardScope{SubjectType: ScorecardSubjectCompany, SubjectID: ws})

	written := 0
	var firstErr error
	for _, scope := range scopes {
		for _, period := range []string{ScorecardPeriodWeek, ScorecardPeriodMonth} {
			if _, err := s.Compute(ctx, s.Queries, ws, scope, period); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				slog.Warn("scorecards: subject failed", "subject_type", scope.SubjectType,
					"subject_id", util.UUIDToString(scope.SubjectID), "period", period, "error", err)
				continue
			}
			written++
		}
	}
	return written, firstErr
}
