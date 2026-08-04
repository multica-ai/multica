package aiimpact

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// DerivedWindow is the trailing period every derived observation covers.
const DerivedWindow = 30 * 24 * time.Hour

// DerivedSource names the authoritative table every derived observation is
// aggregated from, so a reader can trace a number back to its runs.
const DerivedSource = "cerebro_analytics_run"

// DerivedMethod documents how a derived value was produced.
const DerivedMethod = "30-day projection aggregate"

// derivedQualitySampleFloor mirrors the People privacy floor: a pass rate is
// only reported once enough scored measurements exist to be meaningful.
const derivedQualitySampleFloor = 5

// derivedGroupLimit caps how many Project x Agent loops the dashboard derives,
// ordered by run volume, so a large workspace stays readable.
const derivedGroupLimit = 150

// derivedNamespace keeps derived identifiers stable across requests without
// writing anything to the registry tables.
var derivedNamespace = uuid.MustParse("6f1d0a2e-7f2b-4a1e-9c3d-8b5a4e7c1d20")

func derivedID(parent uuid.UUID, key string) uuid.UUID {
	return uuid.NewSHA1(derivedNamespace, append(parent[:], []byte(key)...))
}

// derivedGroup is one Project x Agent slice of the analytics projection.
type derivedGroup struct {
	ProjectID        uuid.UUID
	ProjectName      string
	AgentID          uuid.UUID
	AgentName        string
	Runs             int64
	CostCents        sql.NullInt64
	SkillInvocations int64
	QualityPassed    int64
	QualityScored    int64
}

// ListDerivedEvidence turns the analytics projection into evidence that already
// sits in the Function -> Operating Loop -> Metric -> Observation taxonomy.
// Nothing is written: every value is aggregated from real runs at read time, so
// a workspace that never registered a Function still sees measured numbers.
func (s *Store) ListDerivedEvidence(
	ctx context.Context,
	workspaceID uuid.UUID,
	now time.Time,
) ([]EvidenceReadModel, error) {
	start := now.Add(-DerivedWindow)
	rows, err := s.pool.Query(ctx, `
		WITH windowed AS (
			SELECT r.id, r.run_id, r.project_id, r.project_label,
				r.agent_id, r.agent_label, r.cost_cents
			FROM cerebro_analytics_run r
			WHERE r.workspace_id = $1 AND r.started_at >= $2 AND r.agent_id IS NOT NULL
		),
		skills AS (
			SELECT sk.analytics_run_id, SUM(sk.invocation_count)::bigint AS invocations
			FROM cerebro_analytics_run_skill sk
			WHERE sk.workspace_id = $1
			GROUP BY sk.analytics_run_id
		),
		quality AS (
			SELECT q.analytics_run_id,
				COUNT(*) FILTER (
					WHERE q.verdict IN ('pass', 'approved', '👍', '❤️', '🎉')
				)::bigint AS passed,
				COUNT(*) FILTER (
					WHERE q.verdict IN ('pass', 'fail', 'approved', 'rejected', '👍', '👎', '❤️', '🎉')
				)::bigint AS scored
			FROM cerebro_analytics_quality_measurement q
			WHERE q.workspace_id = $1
			GROUP BY q.analytics_run_id
		)
		SELECT w.project_id, COALESCE(w.project_label, ''),
			w.agent_id, COALESCE(w.agent_label, ''),
			COUNT(DISTINCT w.run_id)::bigint,
			CASE
				WHEN COUNT(*) FILTER (WHERE w.cost_cents IS NOT NULL) = 0 THEN NULL
				ELSE SUM(w.cost_cents) FILTER (WHERE w.cost_cents IS NOT NULL)::bigint
			END,
			COALESCE(SUM(s.invocations), 0)::bigint,
			COALESCE(SUM(q.passed), 0)::bigint,
			COALESCE(SUM(q.scored), 0)::bigint
		FROM windowed w
		LEFT JOIN skills s ON s.analytics_run_id = w.id
		LEFT JOIN quality q ON q.analytics_run_id = w.id
		GROUP BY w.project_id, w.project_label, w.agent_id, w.agent_label
		ORDER BY 5 DESC, 2, 4, 3
		LIMIT $3`, workspaceID, start, derivedGroupLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]derivedGroup, 0)
	for rows.Next() {
		var group derivedGroup
		var projectID, agentID *uuid.UUID
		if err := rows.Scan(
			&projectID, &group.ProjectName, &agentID, &group.AgentName,
			&group.Runs, &group.CostCents, &group.SkillInvocations,
			&group.QualityPassed, &group.QualityScored,
		); err != nil {
			return nil, err
		}
		if projectID != nil {
			group.ProjectID = *projectID
		}
		if agentID != nil {
			group.AgentID = *agentID
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return BuildDerivedEvidence(workspaceID, groups, start, now), nil
}

// BuildDerivedEvidence maps aggregated run groups onto the AI Impact taxonomy.
// It is pure so the mapping can be tested without a database.
func BuildDerivedEvidence(
	workspaceID uuid.UUID,
	groups []derivedGroup,
	periodStart, periodEnd time.Time,
) []EvidenceReadModel {
	evidence := make([]EvidenceReadModel, 0, len(groups)*4)
	for _, group := range groups {
		functionID := group.ProjectID
		functionName := group.ProjectName
		if functionID == uuid.Nil {
			functionID = derivedID(workspaceID, "function:no-project")
			functionName = "No project"
		}
		if functionName == "" {
			functionName = "Untitled project"
		}
		function := Function{
			ID:          functionID,
			WorkspaceID: workspaceID,
			Name:        functionName,
			Description: "Derived from runs projected for this Project.",
			OwnerType:   "agent",
			OwnerID:     group.AgentID,
			Active:      true,
		}

		loopName := group.AgentName
		if loopName == "" {
			loopName = "Unnamed agent"
		}
		operatingLoop := OperatingLoop{
			ID:          derivedID(functionID, "loop:"+group.AgentID.String()),
			WorkspaceID: workspaceID,
			FunctionID:  functionID,
			Name:        loopName,
			Description: "Derived from this agent's runs in the Project.",
			Active:      true,
		}

		add := func(name string, family MetricFamily, unit string, direction MetricDirection, guardrail bool, value float64, status EvidenceStatus, confidence float64) {
			metricID := derivedID(operatingLoop.ID, "metric:"+name)
			evidence = append(evidence, EvidenceReadModel{
				Function:      function,
				OperatingLoop: operatingLoop,
				Metric: Metric{
					ID:              metricID,
					WorkspaceID:     workspaceID,
					OperatingLoopID: operatingLoop.ID,
					Name:            name,
					Family:          family,
					Unit:            unit,
					Direction:       direction,
					BaselineStart:   periodStart,
					BaselineEnd:     periodEnd,
					Source:          DerivedSource,
					Guardrail:       guardrail,
					Active:          true,
				},
				Observation: Observation{
					ID:             derivedID(metricID, "observation:"+periodStart.UTC().Format(time.RFC3339)),
					MetricID:       metricID,
					PeriodStart:    periodStart,
					PeriodEnd:      periodEnd,
					Value:          value,
					EvidenceStatus: status,
					Confidence:     confidence,
					Source:         DerivedSource,
					Method:         DerivedMethod,
					CreatedAt:      periodEnd,
				},
			})
		}

		add("Runs", FamilyAdoption, "runs", DirectionIncrease, false,
			float64(group.Runs), EvidenceMeasured, 1)

		costStatus, costConfidence, costValue := EvidenceMissing, 0.0, 0.0
		if group.CostCents.Valid {
			costStatus, costConfidence, costValue = EvidenceMeasured, 1, float64(group.CostCents.Int64)
		}
		add("AI cost", FamilyEconomics, "cents", DirectionDecrease, false,
			costValue, costStatus, costConfidence)

		add("Skill invocations", FamilyOutput, "invocations", DirectionIncrease, false,
			float64(group.SkillInvocations), EvidenceMeasured, 1)

		qualityStatus, qualityConfidence, qualityValue := EvidenceMissing, 0.0, 0.0
		if group.QualityScored >= derivedQualitySampleFloor {
			qualityStatus = EvidenceMeasured
			qualityConfidence = 1
			qualityValue = float64(group.QualityPassed) / float64(group.QualityScored)
		}
		add("Quality pass rate", FamilyQuality, "ratio", DirectionIncrease, true,
			qualityValue, qualityStatus, qualityConfidence)
	}
	return evidence
}
