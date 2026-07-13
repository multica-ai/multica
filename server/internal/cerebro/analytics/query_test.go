package analytics

import (
	"reflect"
	"testing"
)

func TestQueryNormalizeAppliesStableDefaults(t *testing.T) {
	q := Query{Population: PopulationAgent}
	if err := q.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}

	wantMetrics := []Metric{MetricRuns}
	if !reflect.DeepEqual(q.Metrics, wantMetrics) {
		t.Fatalf("Metrics = %#v, want %#v", q.Metrics, wantMetrics)
	}
	if q.Grain != GrainNone || q.Page.Limit != DefaultPageLimit || q.Timezone != "UTC" {
		t.Fatalf("defaults = grain %q, limit %d, timezone %q", q.Grain, q.Page.Limit, q.Timezone)
	}
}

func TestQueryNormalizeRejectsInvalidContractValues(t *testing.T) {
	tests := []struct {
		name  string
		query Query
	}{
		{name: "population required", query: Query{}},
		{name: "metric", query: Query{Population: PopulationAgent, Metrics: []Metric{"revenue"}}},
		{name: "dimension", query: Query{Population: PopulationAgent, Dimensions: []Dimension{"team"}}},
		{name: "grain", query: Query{Population: PopulationAgent, Grain: "minute"}},
		{name: "operator", query: Query{Population: PopulationAgent, Filters: []Filter{{Dimension: DimensionModel, Operator: "contains", Values: []string{"x"}}}}},
		{name: "filter values", query: Query{Population: PopulationAgent, Filters: []Filter{{Dimension: DimensionModel, Operator: OperatorIn}}}},
		{name: "page limit", query: Query{Population: PopulationAgent, Page: Page{Limit: MaxPageLimit + 1}}},
		{name: "timezone", query: Query{Population: PopulationAgent, Timezone: "Mars/Olympus"}},
		{name: "sort dimension absent", query: Query{Population: PopulationAgent, Sort: []Sort{{Field: "project", Direction: SortAscending}}}},
		{name: "sort direction", query: Query{Population: PopulationAgent, Metrics: []Metric{MetricRuns}, Sort: []Sort{{Field: "runs", Direction: "sideways"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.query.Normalize(); err == nil {
				t.Fatal("Normalize() error = nil, want validation error")
			}
		})
	}
}

func TestQueryNormalizeCanonicalizesFilters(t *testing.T) {
	q := Query{
		Population: PopulationAll,
		Metrics:    []Metric{MetricCostCents, MetricRuns, MetricCostCents},
		Dimensions: []Dimension{DimensionProvider, DimensionPerson, DimensionProvider},
		Filters: []Filter{
			{Dimension: DimensionModel, Operator: OperatorIn, Values: []string{"gpt-5", "claude", "gpt-5"}},
			{Dimension: DimensionPerson, Operator: OperatorEqual, Values: []string{"person-2"}},
		},
		Sort: []Sort{{Field: "cost_cents", Direction: SortDescending}},
	}

	if err := q.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got, want := q.Metrics, []Metric{MetricCostCents, MetricRuns}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Metrics = %#v, want %#v", got, want)
	}
	if got, want := q.Dimensions, []Dimension{DimensionProvider, DimensionPerson}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Dimensions = %#v, want %#v", got, want)
	}
	if got, want := q.Filters[0].Values, []string{"claude", "gpt-5"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filter values = %#v, want %#v", got, want)
	}
}

func TestCatalogExposesEverySupportedValue(t *testing.T) {
	catalog := ContractCatalog()
	if len(catalog.Populations) != 3 || len(catalog.Metrics) != 9 || len(catalog.Dimensions) != 20 {
		t.Fatalf("unexpected catalog sizes: populations=%d metrics=%d dimensions=%d", len(catalog.Populations), len(catalog.Metrics), len(catalog.Dimensions))
	}
}
