package aiimpact

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildDerivedEvidenceReportsFourMeasuredMetricsPerLoop(t *testing.T) {
	workspaceID := uuid.New()
	projectID := uuid.New()
	agentID := uuid.New()
	end := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	start := end.Add(-DerivedWindow)

	evidence := BuildDerivedEvidence(workspaceID, []derivedGroup{{
		ProjectID:        projectID,
		ProjectName:      "Firtal Management",
		AgentID:          agentID,
		AgentName:        "Sara",
		Runs:             42,
		CostCents:        sql.NullInt64{Int64: 250, Valid: true},
		SkillInvocations: 3,
		QualityPassed:    4,
		QualityScored:    5,
	}}, start, end)

	if len(evidence) != 4 {
		t.Fatalf("evidence count = %d, want one per derived metric", len(evidence))
	}

	byName := make(map[string]EvidenceReadModel, len(evidence))
	for _, item := range evidence {
		if item.Function.ID != projectID {
			t.Fatalf("Function ID = %s, want the Project ID %s", item.Function.ID, projectID)
		}
		if item.Function.Name != "Firtal Management" {
			t.Fatalf("Function name = %q, want the Project name", item.Function.Name)
		}
		if item.OperatingLoop.Name != "Sara" {
			t.Fatalf("Operating Loop name = %q, want the agent name", item.OperatingLoop.Name)
		}
		if item.OperatingLoop.FunctionID != projectID {
			t.Fatalf("Operating Loop FunctionID = %s, want %s", item.OperatingLoop.FunctionID, projectID)
		}
		if item.Observation.Source != DerivedSource || item.Observation.Method != DerivedMethod {
			t.Fatalf("observation provenance = %q/%q, want the analytics projection",
				item.Observation.Source, item.Observation.Method)
		}
		if !item.Observation.PeriodStart.Equal(start) || !item.Observation.PeriodEnd.Equal(end) {
			t.Fatalf("observation period = %s..%s, want %s..%s",
				item.Observation.PeriodStart, item.Observation.PeriodEnd, start, end)
		}
		byName[item.Metric.Name] = item
	}

	for name, want := range map[string]struct {
		family MetricFamily
		value  float64
	}{
		"Runs":              {FamilyAdoption, 42},
		"AI cost":           {FamilyEconomics, 250},
		"Skill invocations": {FamilyOutput, 3},
		"Quality pass rate": {FamilyQuality, 0.8},
	} {
		item, ok := byName[name]
		if !ok {
			t.Fatalf("metric %q is missing from derived evidence", name)
		}
		if item.Metric.Family != want.family {
			t.Fatalf("metric %q family = %q, want %q", name, item.Metric.Family, want.family)
		}
		if item.Observation.Value != want.value {
			t.Fatalf("metric %q value = %v, want %v", name, item.Observation.Value, want.value)
		}
		if item.Observation.EvidenceStatus != EvidenceMeasured {
			t.Fatalf("metric %q status = %q, want Measured", name, item.Observation.EvidenceStatus)
		}
	}

	// Derived identifiers must be stable so the UI does not see a new Function per request.
	again := BuildDerivedEvidence(workspaceID, []derivedGroup{{
		ProjectID: projectID, ProjectName: "Firtal Management",
		AgentID: agentID, AgentName: "Sara", Runs: 42,
	}}, start, end)
	if again[0].Metric.ID != byName["Runs"].Metric.ID {
		t.Fatalf("derived metric ID is not stable across calls")
	}
}

func TestBuildDerivedEvidenceMarksMissingCostAndThinQualityAsMissing(t *testing.T) {
	end := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	evidence := BuildDerivedEvidence(uuid.New(), []derivedGroup{{
		AgentID:       uuid.New(),
		Runs:          2,
		CostCents:     sql.NullInt64{},
		QualityPassed: 1,
		QualityScored: 2,
	}}, end.Add(-DerivedWindow), end)

	byName := make(map[string]Observation, len(evidence))
	for _, item := range evidence {
		byName[item.Metric.Name] = item.Observation
	}
	for _, name := range []string{"AI cost", "Quality pass rate"} {
		if byName[name].EvidenceStatus != EvidenceMissing {
			t.Fatalf("metric %q status = %q, want Missing when the signal is absent or below the sample floor",
				name, byName[name].EvidenceStatus)
		}
		if byName[name].Confidence != 0 {
			t.Fatalf("metric %q confidence = %v, want 0 for Missing evidence", name, byName[name].Confidence)
		}
	}
	if evidence[0].Function.Name != "No project" {
		t.Fatalf("Function name = %q, want the explicit No project bucket", evidence[0].Function.Name)
	}
}

// A workspace that never registered a Function still has to show its runs, which
// is exactly the break that left Overview, Functions and Quality & Risk empty.
func TestServiceReadModelsSurfaceDerivedEvidenceWithoutRegistryRows(t *testing.T) {
	workspaceID := uuid.New()
	end := time.Now().UTC()
	store := &recordingObservationStore{
		derivedEvidence: BuildDerivedEvidence(workspaceID, []derivedGroup{{
			ProjectID:        uuid.New(),
			ProjectName:      "Firtal Management",
			AgentID:          uuid.New(),
			AgentName:        "Sara",
			Runs:             11,
			CostCents:        sql.NullInt64{Int64: 900, Valid: true},
			SkillInvocations: 5,
			QualityPassed:    9,
			QualityScored:    10,
		}}, end.Add(-DerivedWindow), end),
	}
	service := NewService(store)
	ctx := context.Background()

	families, err := service.ListOverviewSummary(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list overview summary: %v", err)
	}
	populated := make(map[MetricFamily]int)
	for _, family := range families {
		if len(family.Evidence) > 0 {
			populated[family.Family] = len(family.Evidence)
		}
	}
	for _, family := range []MetricFamily{FamilyAdoption, FamilyOutput, FamilyQuality, FamilyEconomics} {
		if populated[family] == 0 {
			t.Fatalf("overview family %q is empty, want derived evidence", family)
		}
	}

	decisions, err := service.ListQualityRiskDecisions(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list quality risk decisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want one per derived Operating Loop", len(decisions))
	}
	if decisions[0].OperatingLoop.Name != "Sara" {
		t.Fatalf("decision loop = %q, want the derived agent loop", decisions[0].OperatingLoop.Name)
	}

	summaries, err := service.ListFunctionSummaries(ctx, workspaceID)
	if err != nil {
		t.Fatalf("list function summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("function summary count = %d, want the derived Function", len(summaries))
	}
	if summaries[0].Function.Name != "Firtal Management" {
		t.Fatalf("function summary = %q, want the derived Project Function", summaries[0].Function.Name)
	}
	if len(summaries[0].OperatingLoops) != 1 {
		t.Fatalf("function summary loops = %d, want the derived loop", len(summaries[0].OperatingLoops))
	}
	if store.listDerivedWorkspaceID != workspaceID {
		t.Fatalf("derived evidence workspace = %s, want %s", store.listDerivedWorkspaceID, workspaceID)
	}
}

func TestStoreListDerivedEvidenceAggregatesProjectedRunsPerAgent(t *testing.T) {
	if storeTestPool == nil {
		t.Skip("no test database")
	}

	ctx := context.Background()
	store := NewStore(storeTestPool)

	var workspaceID uuid.UUID
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, issue_prefix)
		VALUES ('AI Impact Derived', 'ai-impact-derived-' || gen_random_uuid(), 'AID')
		RETURNING id`).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = storeTestPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	now := time.Now().UTC()
	if derived, err := store.ListDerivedEvidence(ctx, workspaceID, now); err != nil {
		t.Fatalf("list derived evidence on an empty workspace: %v", err)
	} else if len(derived) != 0 {
		t.Fatalf("derived evidence on an empty workspace = %d items, want none", len(derived))
	}

	var runtimeID, agentID, userID, issueID, taskID, analyticsRunID uuid.UUID
	projectID := uuid.New()
	for _, step := range []struct {
		label  string
		sql    string
		args   []any
		target *uuid.UUID
	}{
		{"runtime", `INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider)
			VALUES ($1, 'Derived RT', 'local', 'claude') RETURNING id`, []any{workspaceID}, &runtimeID},
		{"agent", `INSERT INTO agent (workspace_id, name, runtime_mode)
			VALUES ($1, 'Sara', 'local') RETURNING id`, []any{workspaceID}, &agentID},
		{"user", `INSERT INTO "user" (email, name)
			VALUES ('ai-impact-derived-' || gen_random_uuid() || '@example.com', 'Probe') RETURNING id`, nil, &userID},
	} {
		if err := storeTestPool.QueryRow(ctx, step.sql, step.args...).Scan(step.target); err != nil {
			t.Fatalf("create %s: %v", step.label, err)
		}
	}
	t.Cleanup(func() {
		_, _ = storeTestPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'Derived probe', 'member', $2) RETURNING id`,
		workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id)
		VALUES ($1, $2, $3) RETURNING id`,
		agentID, issueID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := storeTestPool.QueryRow(ctx, `
		INSERT INTO cerebro_analytics_run (
			workspace_id, run_id, population, source_type, status, started_at,
			agent_id, agent_label, project_id, project_label, cost_cents)
		VALUES ($1, $2, 'agent', 'issue', 'completed', $3, $4, 'Sara', $5, 'Firtal Management', 250)
		RETURNING id`,
		workspaceID, taskID, now.Add(-24*time.Hour), agentID, projectID).Scan(&analyticsRunID); err != nil {
		t.Fatalf("create analytics run: %v", err)
	}
	if _, err := storeTestPool.Exec(ctx, `
		INSERT INTO cerebro_analytics_run_skill (
			analytics_run_id, workspace_id, skill_name, invocation_count, first_used_at, last_used_at)
		VALUES ($1, $2, 'probe-skill', 3, $3, $3)`,
		analyticsRunID, workspaceID, now); err != nil {
		t.Fatalf("create skill usage: %v", err)
	}
	for i, verdict := range []string{"pass", "pass", "pass", "pass", "fail"} {
		if _, err := storeTestPool.Exec(ctx, `
			INSERT INTO cerebro_analytics_quality_measurement (
				analytics_run_id, workspace_id, measurement_type, category, verdict, evaluator_version)
			VALUES ($1, $2, 'judge_gate', $3, $4, 'v1')`,
			analyticsRunID, workspaceID, uuid.New().String(), verdict); err != nil {
			t.Fatalf("create quality measurement %d: %v", i, err)
		}
	}

	derived, err := store.ListDerivedEvidence(ctx, workspaceID, now)
	if err != nil {
		t.Fatalf("list derived evidence: %v", err)
	}
	if len(derived) != 4 {
		t.Fatalf("derived evidence = %d items, want one per derived metric", len(derived))
	}
	values := make(map[string]float64, len(derived))
	for _, item := range derived {
		if item.Function.ID != projectID {
			t.Fatalf("Function ID = %s, want the Project ID %s", item.Function.ID, projectID)
		}
		if item.OperatingLoop.Name != "Sara" {
			t.Fatalf("Operating Loop = %q, want the agent label", item.OperatingLoop.Name)
		}
		values[item.Metric.Name] = item.Observation.Value
	}
	for name, want := range map[string]float64{
		"Runs":              1,
		"AI cost":           250,
		"Skill invocations": 3,
		"Quality pass rate": 0.8,
	} {
		if values[name] != want {
			t.Fatalf("derived %q = %v, want %v", name, values[name], want)
		}
	}
}
