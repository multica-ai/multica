package analytics

import (
	"strings"
	"testing"
)

// FIR-3212: the run projection couples the existing loop judge/rubric verdicts
// and human satisfaction signals into the existing quality measurement model,
// instead of building a parallel pipe. These tests pin the source SQL contract.

func TestQualitySourcesIncludeSkillObservations(t *testing.T) {
	for _, fragment := range []string{
		"'skill_observation'",
		"FROM skill_observation",
		"so.run_id=$1",
	} {
		if !strings.Contains(qualitySourcesSQL, fragment) {
			t.Errorf("quality sources SQL missing %q:\n%s", fragment, qualitySourcesSQL)
		}
	}
}

func TestQualitySourcesCoupleLoopJudgeVerdictsToRuns(t *testing.T) {
	// A reported judge verdict for (issue, gate, round) attributes to the
	// newest build-phase run on the same issue that completed at or before the
	// verdict was reported — the run whose deliverable the judge scored.
	for _, fragment := range []string{
		"'judge_gate'",
		"FROM cerebro_loop_judge_run",
		"jr.ran",
		"WHEN jr.pass THEN 'pass' ELSE 'fail'",
		"->>'loop_phase'",
		"NOT EXISTS",
	} {
		if !strings.Contains(qualitySourcesSQL, fragment) {
			t.Errorf("quality sources SQL missing %q:\n%s", fragment, qualitySourcesSQL)
		}
	}
}

func TestQualitySourcesRecordHumanApprovalAsSatisfaction(t *testing.T) {
	// Human approval/rejection of a gate round is a satisfaction signal
	// (FIR-3212 review decision: reopen-after-done was dropped; approvals and
	// reactions are the satisfaction sources). Only decisions made by a person
	// count — an agent standing in for the assignee is not human satisfaction.
	for _, fragment := range []string{
		"'satisfaction'",
		"FROM cerebro_loop_human_approval",
		"ha.approved_by_type='member'",
		"WHEN ha.approved THEN 'approved' ELSE 'rejected'",
	} {
		if !strings.Contains(qualitySourcesSQL, fragment) {
			t.Errorf("quality sources SQL missing %q:\n%s", fragment, qualitySourcesSQL)
		}
	}
}

func TestQualitySourcesRecordMemberReactionsAsSatisfaction(t *testing.T) {
	// A member reaction on a comment the run's agent posted on the run's issue
	// while the run was active attributes to that run. The raw emoji is the
	// verdict — interpretation happens at aggregation time, not at capture.
	for _, fragment := range []string{
		"JOIN comment_reaction",
		"cr.actor_type='member'",
		"c.author_type='agent'",
		"c.author_id=me.agent_id",
		"cr.emoji",
	} {
		if !strings.Contains(qualitySourcesSQL, fragment) {
			t.Errorf("quality sources SQL missing %q:\n%s", fragment, qualitySourcesSQL)
		}
	}
}

func TestQualitySourcesAreDeterministicallyOrdered(t *testing.T) {
	if !strings.Contains(qualitySourcesSQL, "ORDER BY") {
		t.Errorf("quality sources SQL must be deterministically ordered:\n%s", qualitySourcesSQL)
	}
}

func TestSatisfactionMigrationExtendsMeasurementTypes(t *testing.T) {
	up := readAnalyticsMigration(t, "9142_cerebro_satisfaction_measurement.up.sql")
	for _, fragment := range []string{
		"cerebro_analytics_quality_measurement",
		"'satisfaction'",
		"'judge_gate'",
		"'skill_observation'",
		"'evaluator'",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}

func TestSatisfactionMigrationDownRestoresOriginalCheck(t *testing.T) {
	down := readAnalyticsMigration(t, "9142_cerebro_satisfaction_measurement.down.sql")
	for _, fragment := range []string{
		// Rows the old CHECK would reject must be removed before restoring it.
		"DELETE FROM cerebro_analytics_quality_measurement",
		"measurement_type = 'satisfaction'",
	} {
		if !strings.Contains(down, fragment) {
			t.Errorf("down migration missing %q", fragment)
		}
	}
	if strings.Contains(strings.ReplaceAll(down, "DELETE FROM cerebro_analytics_quality_measurement", ""), "'satisfaction', ") {
		t.Error("down migration must not keep 'satisfaction' in the restored CHECK")
	}
}

func TestQualityPassRateExcludesSatisfactionSignals(t *testing.T) {
	// Satisfaction verdicts are emojis and approval decisions, not pass/fail —
	// counting them in the pass rate would silently read every reaction as a
	// failure. The metric must only aggregate solution-quality measurements.
	plan, err := BuildSQL(Query{
		Population: PopulationAgent,
		Metrics:    []Metric{MetricQualityPassRate},
	}, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.SQL, "q.measurement_type <> 'satisfaction'") {
		t.Errorf("quality_pass_rate must exclude satisfaction measurements:\n%s", plan.SQL)
	}
}
