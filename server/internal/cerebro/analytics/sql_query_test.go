package analytics

import (
	"strings"
	"testing"
)

func TestBuildSQLUsesCanonicalProjectionForDimensionsMetricsAndFilters(t *testing.T) {
	query := Query{
		Population: PopulationAgent,
		Metrics:    []Metric{MetricRuns, MetricCostCents, MetricSavedCents, MetricQualityPassRate},
		Dimensions: []Dimension{DimensionPerson, DimensionProject, DimensionProvider},
		Filters: []Filter{
			{Dimension: DimensionModel, Operator: OperatorIn, Values: []string{"gpt-5", "claude"}},
			{Dimension: DimensionStatus, Operator: OperatorNotIn, Values: []string{"failed"}},
		},
		Sort: []Sort{{Field: "cost_cents", Direction: SortDescending}},
		Page: Page{Limit: 25},
	}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"r.person_label AS person", "r.project_label AS project", "r.provider AS provider",
		"COUNT(DISTINCT r.run_id)::bigint AS runs", "SUM(r.cost_cents)::bigint AS cost_cents",
		"SUM(s.saved_cents)::bigint AS saved_cents", "quality_pass_rate",
		"r.model = ANY", "NOT (r.status = ANY", "ORDER BY cost_cents DESC", "LIMIT 26",
	} {
		if !strings.Contains(plan.SQL, fragment) {
			t.Errorf("SQL missing %q:\n%s", fragment, plan.SQL)
		}
	}
	if len(plan.Args) != 4 || plan.Args[0] != "workspace-1" {
		t.Fatalf("Args = %#v", plan.Args)
	}
}

func TestBuildSQLAddsTimezoneAwareGrainAndCursor(t *testing.T) {
	query := Query{Population: PopulationAll, Metrics: []Metric{MetricRuns}, Dimensions: []Dimension{DimensionTime}, Grain: GrainDay, Timezone: "Europe/Copenhagen", Page: Page{Limit: 10, Cursor: "2026-07-10T00:00:00Z"}}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.SQL, "date_trunc('day', r.started_at AT TIME ZONE $2) AS time") || !strings.Contains(plan.SQL, "r.started_at < $3::timestamptz") {
		t.Fatalf("SQL does not apply grain/cursor:\n%s", plan.SQL)
	}
}

func TestBuildSQLSupportsTimeFiltersAndMissingCostMetric(t *testing.T) {
	query := Query{
		Population: PopulationAll,
		Metrics:    []Metric{MetricRuns, MetricMissingCostRuns},
		Dimensions: []Dimension{DimensionCostKind},
		Filters: []Filter{
			{Dimension: DimensionTime, Operator: OperatorGreaterEqual, Values: []string{"2026-07-13T00:00:00Z"}},
			{Dimension: DimensionCostKind, Operator: OperatorIn, Values: []string{"missing"}},
		},
		Page: Page{Limit: 10},
	}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"r.cost_kind AS cost_kind",
		"COUNT(DISTINCT r.run_id) FILTER (WHERE r.cost_kind = 'missing')::bigint AS missing_cost_runs",
		"r.started_at >= ($2::timestamptz[])[1]",
		"r.cost_kind = ANY($3::text[])",
	} {
		if !strings.Contains(plan.SQL, fragment) {
			t.Errorf("SQL missing %q:\n%s", fragment, plan.SQL)
		}
	}
}

func TestBuildSQLRejectsMissingWorkspace(t *testing.T) {
	if _, err := BuildSQL(Query{Population: PopulationAgent}, ""); err == nil {
		t.Fatal("BuildSQL() error = nil")
	}
}

func TestBuildSQLUsesOpaqueOffsetCursorForGroupedResults(t *testing.T) {
	query := Query{Population: PopulationAll, Metrics: []Metric{MetricRuns}, Dimensions: []Dimension{DimensionPerson}, Page: Page{Limit: 12, Cursor: "offset:24"}}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.SQL, "ORDER BY runs DESC") || !strings.Contains(plan.SQL, "LIMIT 13 OFFSET 24") {
		t.Fatalf("SQL does not paginate grouped result:\n%s", plan.SQL)
	}
}

func TestBuildSQLSupportsEveryCatalogMetricAndDimension(t *testing.T) {
	for _, metric := range ContractCatalog().Metrics {
		if _, err := BuildSQL(Query{Population: PopulationAll, Metrics: []Metric{metric}}, "workspace-1"); err != nil {
			t.Errorf("metric %q: %v", metric, err)
		}
	}
	for _, dimension := range ContractCatalog().Dimensions {
		if _, err := BuildSQL(Query{Population: PopulationAll, Dimensions: []Dimension{dimension}}, "workspace-1"); err != nil {
			t.Errorf("dimension %q: %v", dimension, err)
		}
	}
}

func TestBuildSQLExposesRunAndDebugLinkDimensions(t *testing.T) {
	query := Query{Population: PopulationAll, Metrics: []Metric{MetricRuns}, Dimensions: []Dimension{DimensionRun, DimensionSourceID, DimensionReference, DimensionReferenceLabel, DimensionDebugLink, DimensionTrace}, Page: Page{Limit: 20}}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"r.run_id::text AS run", "r.source_id::text AS source_id", "ref.reference_id AS reference", "ref.label AS reference_label", "ref.href AS debug_link", "r.trace_id AS trace"} {
		if !strings.Contains(plan.SQL, fragment) {
			t.Errorf("SQL missing %q:\n%s", fragment, plan.SQL)
		}
	}
}

func TestBuildSQLExposesAndFiltersRuntime(t *testing.T) {
	query := Query{
		Population: PopulationAll,
		Metrics:    []Metric{MetricRuns},
		Dimensions: []Dimension{DimensionRuntime},
		Filters:    []Filter{{Dimension: DimensionRuntime, Operator: OperatorIn, Values: []string{"Codex Local"}}},
		Page:       Page{Limit: 20},
	}
	plan, err := BuildSQL(query, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"r.runtime_label AS runtime", "r.runtime_label = ANY($2::text[])"} {
		if !strings.Contains(plan.SQL, fragment) {
			t.Errorf("SQL missing %q:\n%s", fragment, plan.SQL)
		}
	}
}

func TestBuildSQLJoinsOnlyMetricDependencies(t *testing.T) {
	plan, err := BuildSQL(Query{Population: PopulationAll, Metrics: []Metric{MetricCostCents}, Dimensions: []Dimension{DimensionProvider}}, "workspace-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"cerebro_analytics_run_saving", "cerebro_analytics_quality_measurement", "cerebro_analytics_run_skill", "cerebro_analytics_reference"} {
		if strings.Contains(plan.SQL, unwanted) {
			t.Errorf("SQL unexpectedly joins %s:\n%s", unwanted, plan.SQL)
		}
	}
}
