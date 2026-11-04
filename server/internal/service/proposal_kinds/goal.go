package proposalkinds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// KeyGoal is the kind a squad files to set its goals for a period (spec 4.1).
const KeyGoal = "goal"

// Goal limits, bounded for the boss who reads them. They match the goal service's.
const (
	goalFilingMaxTitle     = 200
	goalFilingMaxWhy       = 4000
	goalFilingMaxKeyResult = 10
	goalFilingMaxPeriod    = 2 * 366 * 24 * time.Hour
)

// GoalFiling is the payload of a goal proposal. Approval makes the goal active
// with the filing squad as its scope and owner. Dates are YYYY-MM-DD.
type GoalFiling struct {
	Title        string                `json:"title"`
	Why          string                `json:"why,omitempty"`
	PeriodStart  string                `json:"period_start"`
	PeriodEnd    string                `json:"period_end"`
	ParentGoalID string                `json:"parent_goal_id,omitempty"`
	KeyResults   []GoalFilingKeyResult `json:"key_results"`
}

// GoalFilingKeyResult is one key result of a goal filing. Its metric is read at
// the goal's scope, the filing squad.
type GoalFilingKeyResult struct {
	Title       string  `json:"title"`
	MetricKey   string  `json:"metric_key"`
	StartValue  float64 `json:"start_value"`
	TargetValue float64 `json:"target_value"`
	Weight      int     `json:"weight,omitempty"`
}

// Goals is what the goal kind needs from the goal service. The method runs in
// the approval transaction, through q.
type Goals interface {
	// ActivateFromProposal writes the goal a goal proposal asks for, active.
	ActivateFromProposal(ctx context.Context, q *db.Queries, d Decision, g GoalFiling) error
}

type goalKind struct {
	goals Goals
}

// NewGoalKind returns the goal kind wired to the goal service. The initiative
// service's constructor registers it, since the service exists only once the
// proposal service does.
func NewGoalKind(goals Goals) Kind { return goalKind{goals: goals} }

func (goalKind) Key() string         { return KeyGoal }
func (goalKind) FiledByAgents() bool { return true }

func (goalKind) Validate(ctx context.Context, env Env, payload []byte) ([]byte, error) {
	var g GoalFiling
	if err := decodeObject(payload, &g); err != nil {
		return nil, err
	}
	g.Title = strings.TrimSpace(g.Title)
	g.Why = strings.TrimSpace(g.Why)
	switch {
	case g.Title == "":
		return nil, errors.New("goal: title is required: say what the squad will achieve")
	case len([]rune(g.Title)) > goalFilingMaxTitle:
		return nil, fmt.Errorf("goal: title must be at most %d characters", goalFilingMaxTitle)
	case len(g.Why) > goalFilingMaxWhy:
		return nil, fmt.Errorf("goal: why must be at most %d characters", goalFilingMaxWhy)
	}
	start, end, err := goalPeriod(g.PeriodStart, g.PeriodEnd)
	if err != nil {
		return nil, err
	}
	g.PeriodStart, g.PeriodEnd = start.Format("2006-01-02"), end.Format("2006-01-02")
	if len(g.KeyResults) > goalFilingMaxKeyResult {
		return nil, fmt.Errorf("goal: at most %d key results", goalFilingMaxKeyResult)
	}
	for i := range g.KeyResults {
		kr := &g.KeyResults[i]
		kr.Title = strings.TrimSpace(kr.Title)
		kr.MetricKey = strings.TrimSpace(kr.MetricKey)
		switch {
		case kr.Title == "" || len([]rune(kr.Title)) > goalFilingMaxTitle:
			return nil, fmt.Errorf("goal: key result %d needs a title of at most %d characters", i+1, goalFilingMaxTitle)
		case kr.MetricKey == "":
			return nil, fmt.Errorf("goal: key result %d needs a metric_key", i+1)
		case kr.StartValue == kr.TargetValue:
			return nil, fmt.Errorf("goal: key result %d needs a target different from its start", i+1)
		case kr.Weight == 0:
			kr.Weight = 1
		case kr.Weight < 1 || kr.Weight > 10:
			return nil, fmt.Errorf("goal: key result %d weight must be from 1 to 10", i+1)
		}
		if _, err := env.Q.GetMetricInWorkspace(ctx, db.GetMetricInWorkspaceParams{WorkspaceID: env.WorkspaceID, Key: kr.MetricKey}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, fmt.Errorf("goal: key result %d names metric %q, which this workspace does not have", i+1, kr.MetricKey)
			}
			return nil, err
		}
	}
	if g.ParentGoalID != "" {
		id, err := parseUUID(g.ParentGoalID)
		if err != nil {
			return nil, errors.New("goal: parent_goal_id must be a goal id")
		}
		if _, err := env.Q.GetGoalInWorkspace(ctx, db.GetGoalInWorkspaceParams{ID: id, WorkspaceID: env.WorkspaceID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, errors.New("goal: parent goal is not in this workspace")
			}
			return nil, err
		}
	}
	return json.Marshal(g)
}

// goalPeriod parses and checks a goal's period: forward, and at most two years.
func goalPeriod(startText, endText string) (time.Time, time.Time, error) {
	start, err := time.Parse("2006-01-02", startText)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("goal: period_start must be a date, YYYY-MM-DD")
	}
	end, err := time.Parse("2006-01-02", endText)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("goal: period_end must be a date, YYYY-MM-DD")
	}
	if !start.Before(end) {
		return time.Time{}, time.Time{}, errors.New("goal: period_start must be before period_end")
	}
	if end.Sub(start) > goalFilingMaxPeriod {
		return time.Time{}, time.Time{}, errors.New("goal: a period is at most two years")
	}
	return start, end, nil
}

func (goalKind) Summarize(payload []byte) string {
	var g GoalFiling
	if json.Unmarshal(payload, &g) != nil {
		return ""
	}
	return fmt.Sprintf("Goal: %s (%d key results, %s to %s)", g.Title, len(g.KeyResults), g.PeriodStart, g.PeriodEnd)
}

func (k goalKind) Apply(ctx context.Context, q *db.Queries, d Decision) (Effect, error) {
	var g GoalFiling
	if err := json.Unmarshal(d.Proposal.Payload, &g); err != nil {
		return Effect{}, fmt.Errorf("goal payload: %w", err)
	}
	return Effect{}, k.goals.ActivateFromProposal(ctx, q, d, g)
}

// Reject has nothing to undo: a goal is written only on approval.
func (goalKind) Reject(context.Context, *db.Queries, Decision) error { return nil }
