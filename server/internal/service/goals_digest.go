package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The goals the Secretary reads (spec 4.1). The brief's Goals section lists the
// goals whose state changed since its last run and every goal that is at risk or
// off track, with the key result that holds it back and what to do about it.

// DigestGoal is one goal in the digest.
type DigestGoal struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	ScopeType string  `json:"scope_type"`
	ScopeID   *string `json:"scope_id"`
	Status    string  `json:"status"`
	// Confidence is the effective value: the owner's override when one is set.
	Confidence string   `json:"confidence"`
	Overridden bool     `json:"overridden"`
	Progress   *float64 `json:"progress"`
	// Cause names the key result that holds the goal back, in words. Empty when
	// the goal has no reading to name.
	Cause string `json:"cause"`
	// Recommendation is what the boss should do next; empty when none applies.
	Recommendation string `json:"recommendation"`
	// ChangedSince is true when the goal's state changed after the last brief.
	ChangedSince bool `json:"changed_since"`
}

// computeGoals returns the goals to report: active goals in the scope whose state
// changed since the last brief, and every active goal in the scope at risk or off
// track. It also returns how many active goals the scope has.
func (s *DigestService) computeGoals(ctx context.Context, ws pgtype.UUID, since time.Time, now time.Time, sc digestScope) ([]DigestGoal, int, error) {
	goals, err := s.Queries.ListGoalsByStatus(ctx, db.ListGoalsByStatusParams{WorkspaceID: ws, Statuses: []string{GoalStatusActive}})
	if err != nil {
		return nil, 0, fmt.Errorf("digest goals: %w", err)
	}
	if len(goals) == 0 {
		return []DigestGoal{}, 0, nil
	}
	rows, err := s.Queries.ListKeyResultLatestReadings(ctx, db.ListKeyResultLatestReadingsParams{
		WorkspaceID: ws, Statuses: []string{GoalStatusActive},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("digest key results: %w", err)
	}
	out := []DigestGoal{}
	count := 0
	for _, v := range buildGoalViews(goals, rows, now) {
		if !sc.keeps(v.Goal.ScopeType, v.Goal.ScopeID) {
			continue
		}
		count++
		changed := v.Goal.ConfidenceChangedAt.Time.After(since) || v.Goal.UpdatedAt.Time.After(since)
		if !changed && v.Effective != ConfidenceAtRisk && v.Effective != ConfidenceOffTrack {
			continue
		}
		out = append(out, digestGoalOf(v, changed))
	}
	return out, count, nil
}

// digestGoalOf turns one goal view into the digest's shape.
func digestGoalOf(v GoalView, changed bool) DigestGoal {
	g := DigestGoal{
		ID:           util.UUIDToString(v.Goal.ID),
		Title:        v.Goal.Title,
		ScopeType:    v.Goal.ScopeType,
		ScopeID:      util.UUIDToPtr(v.Goal.ScopeID),
		Status:       v.Goal.Status,
		Confidence:   v.Effective,
		Overridden:   v.Overridden,
		Progress:     v.Progress,
		Cause:        goalCause(v),
		ChangedSince: changed,
	}
	if v.Overridden && v.Goal.ConfidenceNote != "" {
		g.Cause = "Owner override: " + v.Goal.ConfidenceNote
	}
	switch v.Effective {
	case ConfidenceAtRisk:
		g.Recommendation = "Ask the owner what closes the gap before the next review, or re-plan the target."
	case ConfidenceOffTrack:
		g.Recommendation = "Decide whether to re-plan, narrow the goal or drop it."
	}
	return g
}

// goalCause names the key result that holds the goal back: the one furthest
// behind its expected progress, or the first with no reading yet. It returns
// empty when a goal has no key results.
func goalCause(v GoalView) string {
	if len(v.KeyResults) == 0 {
		return ""
	}
	var worst *KeyResultView
	for i := range v.KeyResults {
		kr := &v.KeyResults[i]
		if kr.Progress == nil {
			return fmt.Sprintf("No reading of %s yet this period.", kr.MetricKey)
		}
		if worst == nil || *kr.Progress < *worst.Progress {
			worst = kr
		}
	}
	return fmt.Sprintf("%s is at %s of a target of %s (%s), %d%% of the way.",
		worst.Title, formatGoalNumber(*worst.Current), formatGoalNumber(worst.TargetValue), worst.MetricKey,
		int(*worst.Progress*100+0.5))
}

// SquadGoalsBriefing renders a squad leader's "Your goals this period" block:
// each active key result of the squad's goals in the period now, with its latest
// reading. It returns "" when the squad has none, so a squad without goals gets
// no block.
func SquadGoalsBriefing(ctx context.Context, q *db.Queries, ws, squadID pgtype.UUID, now time.Time) (string, error) {
	rows, err := q.ListActiveKeyResultsForSquad(ctx, db.ListActiveKeyResultsForSquadParams{
		WorkspaceID: ws, SquadID: squadID, Today: dateOf(now.UTC()),
	})
	if err != nil {
		return "", fmt.Errorf("list squad goals: %w", err)
	}
	if len(rows) == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("## Your goals this period\n\n")
	sb.WriteString("These are the key results of the goals the boss activated for your squad. Your operating review reports progress against them.\n\n")
	for _, r := range rows {
		start, _ := floatFromNumeric(r.StartValue)
		target, _ := floatFromNumeric(r.TargetValue)
		line := fmt.Sprintf("- %s: %s (metric %s, from %s to %s)", r.GoalTitle, r.Title, r.MetricKey,
			formatGoalNumber(start), formatGoalNumber(target))
		if cur, ok := floatFromNumeric(r.CurrentValue); ok {
			progress := KeyResultProgress(start, target, cur)
			line += fmt.Sprintf(", latest reading %s on %s (%d%%)", formatGoalNumber(cur),
				r.ObservedAt.Time.UTC().Format("2006-01-02"), int(progress*100+0.5))
		} else {
			line += ", no reading yet this period"
		}
		sb.WriteString(line + "\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}
