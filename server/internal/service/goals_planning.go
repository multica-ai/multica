package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/issueposition"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The planning cycle (spec 4.1). At the start of each period a scheduler job opens
// one planning issue for every squad with a charter. The issue carries the
// company's active goals and the squad's charter, and asks the leader to file its
// goals as a proposal of kind goal. One row per squad and period makes a second
// run a no-op.

// PlanningIssue is an issue the planning cycle opened. The caller publishes its
// issue:created event after the commit, which is what wakes the squad's leader.
type PlanningIssue struct {
	WorkspaceID pgtype.UUID
	Issue       db.Issue
}

// PlanningPeriodStart is the first day of the calendar quarter that holds t, in
// UTC. Quarters are the default planning period (spec 4.1).
func PlanningPeriodStart(t time.Time) time.Time {
	t = t.UTC()
	firstMonth := time.Month((int(t.Month())-1)/3*3 + 1)
	return time.Date(t.Year(), firstMonth, 1, 0, 0, 0, 0, time.UTC)
}

// planningPeriodLabel names a quarter for people: "Q4 2026".
func planningPeriodLabel(start time.Time) string {
	return fmt.Sprintf("Q%d %d", (int(start.Month())-1)/3+1, start.Year())
}

// RunPlanningCycle opens this period's planning issues for every squad that has
// a charter and no issue yet. It returns the issues it opened.
func (s *GoalService) RunPlanningCycle(ctx context.Context, now time.Time) ([]PlanningIssue, error) {
	period := PlanningPeriodStart(now)
	wsIDs, err := s.Queries.ListAllWorkspaceIDsForGovernance(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	var opened []PlanningIssue
	var errs []error
	for _, ws := range wsIDs {
		issues, err := s.planWorkspace(ctx, ws, period)
		opened = append(opened, issues...)
		if err != nil {
			errs = append(errs, fmt.Errorf("planning for workspace %s: %w", util.UUIDToString(ws), err))
		}
	}
	return opened, errors.Join(errs...)
}

// planWorkspace opens the period's planning issues of one workspace.
func (s *GoalService) planWorkspace(ctx context.Context, ws pgtype.UUID, period time.Time) ([]PlanningIssue, error) {
	squads, err := s.Queries.ListPlanningSquads(ctx, ws)
	if err != nil {
		return nil, fmt.Errorf("list planning squads: %w", err)
	}
	if len(squads) == 0 {
		return nil, nil
	}
	company, err := s.companyGoalLines(ctx, ws, period)
	if err != nil {
		return nil, err
	}
	var opened []PlanningIssue
	var errs []error
	for _, sq := range squads {
		issue, ok, err := s.openPlanningIssue(ctx, ws, sq, period, company)
		if err != nil {
			errs = append(errs, fmt.Errorf("squad %s: %w", util.UUIDToString(sq.SquadID), err))
			continue
		}
		if ok {
			opened = append(opened, PlanningIssue{WorkspaceID: ws, Issue: issue})
		}
	}
	return opened, errors.Join(errs...)
}

// companyGoalLines describes the active company goals of the period, one block
// per goal, for the planning issue's body.
func (s *GoalService) companyGoalLines(ctx context.Context, ws pgtype.UUID, period time.Time) ([]string, error) {
	goals, err := s.Queries.ListGoalsByStatus(ctx, db.ListGoalsByStatusParams{WorkspaceID: ws, Statuses: []string{GoalStatusActive}})
	if err != nil {
		return nil, fmt.Errorf("list company goals: %w", err)
	}
	rows, err := s.Queries.ListKeyResultLatestReadings(ctx, db.ListKeyResultLatestReadingsParams{
		WorkspaceID: ws, Statuses: []string{GoalStatusActive},
	})
	if err != nil {
		return nil, fmt.Errorf("list company key results: %w", err)
	}
	krsByGoal := map[string][]db.ListKeyResultLatestReadingsRow{}
	for _, r := range rows {
		krsByGoal[util.UUIDToString(r.GoalID)] = append(krsByGoal[util.UUIDToString(r.GoalID)], r)
	}
	quarterEnd := period.AddDate(0, 3, -1)
	var lines []string
	for _, g := range goals {
		if g.ScopeType != ScopeCompany || g.PeriodStart.Time.After(quarterEnd) || g.PeriodEnd.Time.Before(period) {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "- %s", g.Title)
		if g.Why != "" {
			fmt.Fprintf(&b, " (%s)", g.Why)
		}
		b.WriteString("\n")
		for _, kr := range krsByGoal[util.UUIDToString(g.ID)] {
			start, _ := floatFromNumeric(kr.StartValue)
			target, _ := floatFromNumeric(kr.TargetValue)
			fmt.Fprintf(&b, "  - %s: metric %s, from %s to %s\n", kr.Title, kr.MetricKey, formatGoalNumber(start), formatGoalNumber(target))
		}
		lines = append(lines, strings.TrimRight(b.String(), "\n"))
	}
	return lines, nil
}

// openPlanningIssue opens the squad's planning issue for the period, in one
// transaction with the row that records it. A row that already exists means the
// issue was opened, and nothing is written.
func (s *GoalService) openPlanningIssue(ctx context.Context, ws pgtype.UUID, sq db.ListPlanningSquadsRow, period time.Time, companyGoals []string) (db.Issue, bool, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)

	issueID := dbid.NewV7()
	if _, err := qtx.InsertPlanningCycle(ctx, db.InsertPlanningCycleParams{
		ID: dbid.NewV7(), WorkspaceID: ws, PeriodStart: dateOf(period), SquadID: sq.SquadID, IssueID: issueID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Issue{}, false, nil
		}
		return db.Issue{}, false, fmt.Errorf("record planning cycle: %w", err)
	}

	charter, err := qtx.GetSquadCharter(ctx, db.GetSquadCharterParams{SquadID: sq.SquadID, WorkspaceID: ws})
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("read charter: %w", err)
	}
	policy := ResolveIssueCountPolicy(ctx, s.Entitlements, ws)
	number, err := AllocateIssueNumber(ctx, qtx, ws, policy)
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("allocate issue number: %w", err)
	}
	position, err := issueposition.NextTopPosition(ctx, tx, ws, "todo")
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("next position: %w", err)
	}
	issue, err := qtx.CreateIssue(ctx, db.CreateIssueParams{
		ID:           issueID,
		WorkspaceID:  ws,
		Title:        fmt.Sprintf("%s planning: goals for %s", planningPeriodLabel(period), sq.Name),
		Description:  pgtype.Text{String: planningIssueBody(sq.Name, period, companyGoals, charter), Valid: true},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "squad", Valid: true},
		AssigneeID:   sq.SquadID,
		CreatorType:  "agent",
		CreatorID:    sq.LeaderID,
		Position:     position,
		Number:       number,
	})
	if err != nil {
		return db.Issue{}, false, fmt.Errorf("create planning issue: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Issue{}, false, fmt.Errorf("commit planning issue: %w", err)
	}
	return issue, true, nil
}

// planningIssueBody is the planning issue's description: the company goals, the
// squad's charter, and the proposal the leader files with its goals.
func planningIssueBody(squadName string, period time.Time, companyGoals []string, charter db.SquadCharter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Planning for %s. Set the goals %s will work to this period.\n\n", planningPeriodLabel(period), squadName)
	b.WriteString("## Company goals\n\n")
	if len(companyGoals) == 0 {
		b.WriteString("No company goals are active yet. Set your goals from your charter and the boss's direction.\n\n")
	} else {
		b.WriteString(strings.Join(companyGoals, "\n") + "\n\n")
	}
	b.WriteString("## Your charter\n\n")
	if charter.Mission != "" {
		b.WriteString("Mission: " + charter.Mission + "\n")
	}
	var responsibilities []operatingReviewResponsibility
	if json.Unmarshal(charter.Responsibilities, &responsibilities) == nil {
		for _, r := range responsibilities {
			b.WriteString("- " + r.Text + "\n")
		}
	}
	b.WriteString("\n## Your goals\n\n")
	b.WriteString("File your goals as a proposal of kind goal, with the usual proposal fields (title, summary, evidence, recommendation, option, cost-risk, ask). The payload is one goal with its key results, written to a JSON file:\n\n")
	b.WriteString("```json\n")
	b.WriteString(`{"title": "...", "why": "...", "period_start": "` + period.Format("2006-01-02") + `", "period_end": "` + period.AddDate(0, 3, -1).Format("2006-01-02") + `", "key_results": [{"title": "...", "metric_key": "...", "start_value": 0, "target_value": 0, "weight": 1}]}`)
	b.WriteString("\n```\n\n")
	b.WriteString("Then run `multica proposal create --kind goal --payload-file <file.json>` with those fields. Use metric keys the workspace has (`multica metric list`). Approval makes the goal active; your briefing then lists its key results each day.\n")
	return b.String()
}

// formatGoalNumber renders a goal value without a trailing zero fraction.
func formatGoalNumber(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}
