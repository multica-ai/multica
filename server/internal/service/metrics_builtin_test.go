package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// closeTo reports whether a reading is within a rounding error of want.
func closeTo(got, want float64) bool { return math.Abs(got-want) < 1e-6 }

// TestBuiltinMetricsMatchTheirFixtures seeds one squad's yesterday and today, and
// checks each built-in reading the nightly run writes against values worked out by
// hand: runs, the rates, proposal decision time, issues done, the pipeline, and
// brief usefulness. A metric with no data writes no reading.
func TestBuiltinMetricsMatchTheirFixtures(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	yesterday := f.now.AddDate(0, 0, -1)
	at := func(h int) string { return yesterday.Add(time.Duration(h) * time.Hour).Format(time.RFC3339) }

	// Four finished runs yesterday for the squad: three completed, one failed.
	// Only the first carries a provider cost, one dollar.
	for i, status := range []string{"completed", "completed", "completed", "failed"} {
		task := f.Task(t, f.agent, testutil.Cols{"status": status, "squad_id": f.squad, "completed_at": at(10 + i)})
		if i == 0 {
			f.Insert(t, "task_usage", testutil.Cols{
				"task_id": task, "provider": "anthropic", "model": "fixture-model",
				"cost_usd_ticks": int64(1e10), "updated_at": testutil.Raw("now()"),
			})
		}
	}

	// Two proposals decided yesterday for the squad: approved after two hours,
	// rejected after four. Acceptance is 1 in 2; decision time averages three hours.
	for _, p := range []struct {
		state    string
		filedAt  time.Time
		decidedA time.Time
	}{
		{"approved", yesterday.Add(9 * time.Hour), yesterday.Add(11 * time.Hour)},
		{"rejected", yesterday.Add(9 * time.Hour), yesterday.Add(13 * time.Hour)},
	} {
		issue := f.Issue(t, "proposal "+p.state)
		f.Insert(t, "proposal", testutil.Cols{
			"workspace_id": f.wsText, "squad_id": f.squad, "issue_id": issue, "state": p.state,
			"created_by_type": "agent", "created_by_id": f.agent, "kind": "decision",
			"created_at": p.filedAt, "decided_at": p.decidedA, "expires_at": p.decidedA.AddDate(0, 0, 14),
		})
	}

	// One squad issue moved to done yesterday, and one still open.
	f.Issue(t, "shipped", testutil.Cols{"assignee_type": "squad", "assignee_id": f.squad, "status": "done", "updated_at": at(15)})
	f.Issue(t, "open", testutil.Cols{"assignee_type": "squad", "assignee_id": f.squad, "status": "todo", "updated_at": at(15)})

	// An initiative of the squad, in its only stage since 40 days before now, with a
	// 30 day SLA: stalled, and 40 days in its stage.
	pipeline := f.Insert(t, "pipeline", testutil.Cols{
		"workspace_id": f.wsText, "name": "Fixture pipeline", "workflow_id": newUUIDText(),
		"project_id": newUUIDText(), "stages": string(pipelineStagesJSON(t)), "is_default": true,
	})
	f.Insert(t, "initiative", testutil.Cols{
		"workspace_id": f.wsText, "pipeline_id": pipeline, "seq": 1, "title": "Fixture initiative",
		"thesis": "fixture", "stage_key": "define", "owner_squad_id": f.squad, "status": "active",
		"issue_id": newUUIDText(), "stage_entered_at": f.now.AddDate(0, 0, -40),
	})

	// Brief ratings yesterday: three useful, one not.
	for i, rating := range []string{"useful", "useful", "useful", "not_useful"} {
		f.Insert(t, "brief_feedback", testutil.Cols{
			"workspace_id": f.wsText, "brief_id": newUUIDText(), "member_id": f.owner,
			"rating": rating, "updated_at": at(9 + i),
		})
	}

	svc := f.metricsService()
	if _, err := svc.RollupBuiltinMetrics(ctx, f.ws, f.now); err != nil {
		t.Fatalf("rollup: %v", err)
	}

	squad := f.readingsOf(t, ScopeSquad, f.squad)
	want := map[string]float64{
		"runs.count":                  4,
		"runs.success_rate":           75,
		"runs.cost_usd":               1,
		"proposals.acceptance_rate":   50,
		"proposals.decision_hours":    3,
		"delivery.issues_done":        1,
		"pipeline.initiatives_active": 1,
		"pipeline.stalled":            1,
		"pipeline.stage_days":         40,
	}
	for key, v := range want {
		got, ok := squad[key]
		if !ok {
			t.Errorf("squad: %s has no reading; want %v", key, v)
			continue
		}
		if !closeTo(got, v) {
			t.Errorf("squad: %s = %v, want %v", key, got, v)
		}
	}
	// A rate with no denominator writes no reading: no routines are charted here,
	// and no knowledge document was published.
	for _, key := range []string{"routines.compliance_rate", "knowledge.fresh_ratio"} {
		if _, ok := squad[key]; ok {
			t.Errorf("squad: %s written with no data behind it", key)
		}
	}

	company := f.readingsOf(t, ScopeCompany, "")
	for key, v := range map[string]float64{
		"runs.count":                  4,
		"runs.success_rate":           75,
		"proposals.acceptance_rate":   50,
		"brief.usefulness":            75,
		"knowledge.docs_published":    0,
		"pipeline.initiatives_active": 1,
	} {
		if got, ok := company[key]; !ok || !closeTo(got, v) {
			t.Errorf("company: %s = %v (present %v), want %v", key, got, ok, v)
		}
	}
	if _, ok := company["knowledge.retrieval_hit_rate"]; ok {
		t.Errorf("company: a retrieval rate was written with no eval run")
	}

	// An initiative scope reads its own pipeline only.
	var iniID string
	f.QueryRow(t, `SELECT id::text FROM initiative WHERE workspace_id = $1 AND title = 'Fixture initiative'`, f.wsText).Scan(&iniID)
	ini := f.readingsOf(t, ScopeInitiative, iniID)
	if !closeTo(ini["pipeline.stalled"], 1) || !closeTo(ini["pipeline.stage_days"], 40) {
		t.Errorf("initiative: stalled=%v stage_days=%v, want 1 and 40", ini["pipeline.stalled"], ini["pipeline.stage_days"])
	}
	if _, ok := ini["runs.count"]; ok {
		t.Errorf("initiative: a run count was written; runs belong to squads")
	}

	// The run is idempotent: a second run the same day replaces each reading.
	before := f.countSystemReadings(t)
	if _, err := svc.RollupBuiltinMetrics(ctx, f.ws, f.now); err != nil {
		t.Fatalf("second rollup: %v", err)
	}
	if after := f.countSystemReadings(t); after != before {
		t.Errorf("a second run the same day changed the readings: %d before, %d after", before, after)
	}
}

// countSystemReadings counts this workspace's system readings.
func (f govFixture) countSystemReadings(t *testing.T) int {
	t.Helper()
	var n int
	f.QueryRow(t, `SELECT count(*) FROM metric_reading WHERE workspace_id = $1 AND actor_type = 'system'`, f.wsText).Scan(&n)
	return n
}

// TestBuiltinWindowIsTheDayBeforeTheRun makes sure a run finds yesterday's work
// only: a finished run from before yesterday is not counted.
func TestBuiltinWindowIsTheDayBeforeTheRun(t *testing.T) {
	f := newGovFixture(t)
	old := f.now.AddDate(0, 0, -3).Format(time.RFC3339)
	f.Task(t, f.agent, testutil.Cols{"status": "completed", "squad_id": f.squad, "completed_at": old})
	if _, err := f.metricsService().RollupBuiltinMetrics(context.Background(), f.ws, f.now); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	if got, ok := f.readingsOf(t, ScopeSquad, f.squad)["runs.count"]; ok && got != 0 {
		t.Fatalf("a run three days old counted as yesterday's: %v", got)
	}
}
