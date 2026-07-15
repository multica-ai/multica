package handler

// CEREBRO-PATCH(agent-quality): FIR-3212 — tests for the honest per-version
// quality/satisfaction response mapping. The hard requirement under test:
// missing observations must read as missing (nil), never as zero.

import (
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
