package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeRows struct {
	values [][]any
	index  int
	err    error
}

func (r *fakeRows) Next() bool { return r.index < len(r.values) }
func (r *fakeRows) Values() ([]any, error) {
	value := r.values[r.index]
	r.index++
	return value, nil
}
func (r *fakeRows) Err() error { return r.err }
func (r *fakeRows) Close()     {}

type fakeQueryer struct {
	rows pgx.Rows
	err  error
}

func (q fakeQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return q.rows, q.err
}

func TestCollectResultMapsSelectedFieldsAndReturnsTimeCursor(t *testing.T) {
	startedAt := time.Date(2026, 7, 12, 8, 30, 0, 0, time.UTC)
	rows := &fakeRows{values: [][]any{{startedAt, int64(4), int64(125)}, {startedAt.Add(-time.Hour), int64(2), int64(50)}}}
	query := Query{Dimensions: []Dimension{DimensionTime}, Metrics: []Metric{MetricRuns, MetricCostCents}, Page: Page{Limit: 1}}

	result, err := collectResult(query, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["runs"] != int64(4) || result.Rows[0]["cost_cents"] != int64(125) {
		t.Fatalf("Rows = %#v", result.Rows)
	}
	if result.NextCursor != startedAt.Format(time.RFC3339Nano) {
		t.Fatalf("NextCursor = %q", result.NextCursor)
	}
}

func TestCollectResultRejectsUnexpectedColumnCount(t *testing.T) {
	_, err := collectResult(Query{Metrics: []Metric{MetricRuns}}, &fakeRows{values: [][]any{{int64(1), int64(2)}}})
	if err == nil {
		t.Fatal("collectResult() error = nil")
	}
}

func TestExecutorReturnsDatabaseError(t *testing.T) {
	want := errors.New("database unavailable")
	executor := NewExecutor(fakeQueryer{err: want})
	_, err := executor.Execute(context.Background(), Query{Population: PopulationAll}, "workspace-1")
	if !errors.Is(err, want) {
		t.Fatalf("Execute() error = %v, want %v", err, want)
	}
}

func TestCollectResultUsesNormalizedDefaultMetric(t *testing.T) {
	query := Query{Population: PopulationAll}
	if err := query.Normalize(); err != nil {
		t.Fatal(err)
	}
	result, err := collectResult(query, &fakeRows{values: [][]any{{int64(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Columns) != 1 || result.Columns[0] != "runs" {
		t.Fatalf("Columns = %#v", result.Columns)
	}
}

func TestCollectResultTrimsLookaheadAndReturnsGroupedCursor(t *testing.T) {
	query := Query{Population: PopulationAll, Metrics: []Metric{MetricRuns}, Dimensions: []Dimension{DimensionPerson}, Page: Page{Limit: 2, Cursor: "offset:2"}}
	rows := &fakeRows{values: [][]any{{"A", int64(3)}, {"B", int64(2)}, {"C", int64(1)}}}
	result, err := collectResult(query, rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.NextCursor != "offset:4" {
		t.Fatalf("result = %#v", result)
	}
}
