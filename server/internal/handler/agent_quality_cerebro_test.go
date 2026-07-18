package handler

// CEREBRO-PATCH(agent-quality): FIR-3212 — tests for the honest per-version
// quality/satisfaction response mapping. The hard requirement under test:
// missing observations must read as missing (nil), never as zero.

import (
	"context"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

func TestAgentQualityVersionMapsAveragesOnlyWhenScored(t *testing.T) {
	row := cerebrodb.AggregateAgentQualityByVersionRow{
		AgentContextVersion:  "1.4.0",
		Runs:                 12,
		SolutionMeasurements: 9,
		SolutionPasses:       7,
		SolutionScored:       2,
		SolutionScoreSum:     1.5,
		SolutionConfident:    0,
	}
	v := agentQualityVersion(row)
	if v.Solution.AvgScore == nil || *v.Solution.AvgScore != 0.75 {
		t.Fatalf("AvgScore = %v, want 0.75", v.Solution.AvgScore)
	}
	if v.Solution.AvgConfidence != nil {
		t.Fatalf("AvgConfidence = %v, want nil for zero confident measurements", *v.Solution.AvgConfidence)
	}
}

func TestAgentQualityVersionReportsMissingObservationsAsNilNotZero(t *testing.T) {
	v := agentQualityVersion(cerebrodb.AggregateAgentQualityByVersionRow{
		AgentContextVersion: "1.0.0",
		Runs:                3,
	})
	if v.Solution.AvgScore != nil || v.Solution.AvgConfidence != nil {
		t.Fatal("averages must be nil when nothing was scored")
	}
	if v.Solution.MeasuredRuns != 0 || v.Satisfaction.MeasuredRuns != 0 {
		t.Fatal("sample sizes must be explicit zeros")
	}
	if v.Runs != 3 {
		t.Fatalf("Runs = %d, want 3", v.Runs)
	}
}

func TestAgentQualityVersionOmitsInvalidVersionID(t *testing.T) {
	v := agentQualityVersion(cerebrodb.AggregateAgentQualityByVersionRow{
		AgentContextVersion: "1.0.0",
	})
	if v.AgentContextVersionID != nil {
		t.Fatalf("AgentContextVersionID = %v, want nil for pruned version row", *v.AgentContextVersionID)
	}
	id := pgtype.UUID{Valid: true}
	id.Bytes[15] = 7
	v = agentQualityVersion(cerebrodb.AggregateAgentQualityByVersionRow{
		AgentContextVersion:   "1.0.0",
		AgentContextVersionID: id,
	})
	if v.AgentContextVersionID == nil || *v.AgentContextVersionID != "00000000-0000-0000-0000-000000000007" {
		t.Fatalf("AgentContextVersionID = %v, want resolved uuid", v.AgentContextVersionID)
	}
}

func TestAgentQualityVersionCarriesSatisfactionCounts(t *testing.T) {
	v := agentQualityVersion(cerebrodb.AggregateAgentQualityByVersionRow{
		AgentContextVersion:      "2.0.0",
		Runs:                     5,
		SatisfactionMeasuredRuns: 2,
		Reactions:                4,
		Approvals:                1,
		Rejections:               1,
	})
	s := v.Satisfaction
	if s.MeasuredRuns != 2 || s.Reactions != 4 || s.Approvals != 1 || s.Rejections != 1 {
		t.Fatalf("satisfaction = %+v", s)
	}
}

func TestAggregateAgentQualityByVersionUsesRealDatabaseAndKeepsVersionsIsolated(t *testing.T) {
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "FIR-3212 quality aggregation", nil)

	insertVersion := func(version string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_context_version (
				agent_id, version, snapshot, description, created_by
			) VALUES ($1, $2, '{}'::jsonb, 'FIR-3212 quality test', $3)
			RETURNING id
		`, agentID, version, testUserID).Scan(&id); err != nil {
			t.Fatalf("insert context version %s: %v", version, err)
		}
		return id
	}

	previousVersionID := insertVersion("1.0.0")
	currentVersionID := insertVersion("1.1.0")

	type runFixture struct {
		taskID         string
		analyticsRunID string
	}
	insertRun := func(version, versionID, createdAt string) runFixture {
		t.Helper()
		taskID := createHandlerTestTaskForAgent(t, agentID)
		if _, err := testPool.Exec(ctx, `
			UPDATE agent_task_queue
			SET status = 'completed', completed_at = $2::timestamptz
			WHERE id = $1
		`, taskID, createdAt); err != nil {
			t.Fatalf("complete task %s: %v", taskID, err)
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO cerebro_run_prompt_snapshot (
				workspace_id, task_id, agent_id,
				agent_context_version, agent_context_version_id,
				provider, model, runtime_version, system_prompt_mode, layers,
				sha256_original, sha256_redacted, total_bytes, redacted, created_at
			) VALUES (
				$1, $2, $3, $4, $5,
				'claude', 'claude-opus-4-8', '1.2.3', 'native', '[]'::jsonb,
				'original-hash', 'redacted-hash', 128, false, $6::timestamptz
			)
		`, testWorkspaceID, taskID, agentID, version, versionID, createdAt); err != nil {
			t.Fatalf("insert prompt snapshot for %s: %v", version, err)
		}

		var analyticsRunID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO cerebro_analytics_run (
				workspace_id, run_id, population, source_type,
				agent_id, status, started_at, completed_at
			) VALUES (
				$1, $2, 'agent', 'manual', $3, 'completed',
				$4::timestamptz - interval '1 minute', $4::timestamptz
			)
			RETURNING id
		`, testWorkspaceID, taskID, agentID, createdAt).Scan(&analyticsRunID); err != nil {
			t.Fatalf("insert analytics run for %s: %v", version, err)
		}
		return runFixture{taskID: taskID, analyticsRunID: analyticsRunID}
	}

	previous := insertRun("1.0.0", previousVersionID, "2026-07-17T06:00:00Z")
	currentA := insertRun("1.1.0", currentVersionID, "2026-07-17T07:00:00Z")
	currentB := insertRun("1.1.0", currentVersionID, "2026-07-17T07:05:00Z")
	_ = previous

	insertMeasurement := func(runID, measurementType, category, verdict string, score, confidence any, evaluatorVersion string) {
		t.Helper()
		if _, err := testPool.Exec(ctx, `
			INSERT INTO cerebro_analytics_quality_measurement (
				analytics_run_id, workspace_id, measurement_type, category,
				verdict, score, confidence, evaluator_version
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, runID, testWorkspaceID, measurementType, category, verdict, score, confidence, evaluatorVersion); err != nil {
			t.Fatalf("insert %s measurement %s: %v", measurementType, category, err)
		}
	}

	insertMeasurement(currentA.analyticsRunID, "judge_gate", "solution", "pass", 0.8, 0.9, "judge-v1")
	insertMeasurement(currentA.analyticsRunID, "satisfaction", "reaction:thumbs-up:member", "thumbs-up", nil, nil, "human-v1")
	insertMeasurement(currentA.analyticsRunID, "satisfaction", "human_approval:release:quality", "approved", nil, nil, "human-v1")
	insertMeasurement(currentB.analyticsRunID, "evaluator", "solution", "fail", 0.4, nil, "evaluator-v1")
	insertMeasurement(currentB.analyticsRunID, "satisfaction", "human_approval:release:quality", "rejected", nil, nil, "human-v1")

	rows, err := testHandler.CerebroQueries.AggregateAgentQualityByVersion(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("aggregate quality: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("versions = %d, want 2", len(rows))
	}

	current := rows[0]
	if current.AgentContextVersion != "1.1.0" || uuidToString(current.AgentContextVersionID) != currentVersionID {
		t.Fatalf("current version = %q (%s), want 1.1.0 (%s)", current.AgentContextVersion, uuidToString(current.AgentContextVersionID), currentVersionID)
	}
	if current.Runs != 2 {
		t.Fatalf("current runs = %d, want 2", current.Runs)
	}
	if current.SolutionMeasuredRuns != 2 || current.SolutionMeasurements != 2 || current.SolutionPasses != 1 || current.SolutionScored != 2 {
		t.Fatalf("current solution counts = %+v", current)
	}
	if math.Abs(current.SolutionScoreSum-1.2) > 0.000001 {
		t.Fatalf("current score sum = %f, want 1.2", current.SolutionScoreSum)
	}
	if current.SolutionConfident != 1 || math.Abs(current.SolutionConfidenceSum-0.9) > 0.000001 {
		t.Fatalf("current confidence = %d / %f, want 1 / 0.9", current.SolutionConfident, current.SolutionConfidenceSum)
	}
	if current.SatisfactionMeasuredRuns != 2 || current.Reactions != 1 || current.Approvals != 1 || current.Rejections != 1 {
		t.Fatalf("current satisfaction counts = %+v", current)
	}

	previousRow := rows[1]
	if previousRow.AgentContextVersion != "1.0.0" || uuidToString(previousRow.AgentContextVersionID) != previousVersionID {
		t.Fatalf("previous version = %q (%s), want 1.0.0 (%s)", previousRow.AgentContextVersion, uuidToString(previousRow.AgentContextVersionID), previousVersionID)
	}
	previousResponse := agentQualityVersion(previousRow)
	if previousRow.Runs != 1 || previousResponse.Solution.AvgScore != nil || previousResponse.Solution.AvgConfidence != nil {
		t.Fatalf("unmeasured previous version became data: row=%+v response=%+v", previousRow, previousResponse)
	}
}
