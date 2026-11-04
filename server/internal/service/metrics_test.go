package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestRecordReadingRefusesWhatItMustAndCreatesManualMetrics covers the rules of a
// report: the key format, a finite value, a built-in metric that is computed,
// scopes that name nothing in the workspace, and dating. The first valid report
// creates a manual metric; the next one reuses it.
func TestRecordReadingRefusesWhatItMustAndCreatesManualMetrics(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	svc := f.metricsService()
	if err := svc.EnsureBuiltinMetrics(ctx, f.q, f.ws); err != nil {
		t.Fatalf("seed built-ins: %v", err)
	}
	base := ReadingInput{WorkspaceID: f.ws, Key: "mrr", ScopeType: ScopeCompany, Value: 1200, ActorType: ReadingActorMember, ActorID: util.MustParseUUID(f.owner)}

	cases := []struct {
		name   string
		mutate func(in *ReadingInput)
		want   error
	}{
		{"key with a capital letter", func(in *ReadingInput) { in.Key = "MRR" }, ErrMetricKeyInvalid},
		{"empty key", func(in *ReadingInput) { in.Key = "" }, ErrMetricKeyInvalid},
		{"NaN value", func(in *ReadingInput) { in.Value = math.NaN() }, ErrMetricValueInvalid},
		{"infinite value", func(in *ReadingInput) { in.Value = math.Inf(1) }, ErrMetricValueInvalid},
		{"built-in metric is computed", func(in *ReadingInput) { in.Key = "runs.count" }, ErrMetricBuiltinReadOnly},
		{"company scope with an id", func(in *ReadingInput) { in.ScopeID = f.squadPG(t) }, ErrMetricScopeInvalid},
		{"squad scope that is not in the workspace", func(in *ReadingInput) {
			in.ScopeType = ScopeSquad
			in.ScopeID = util.MustParseUUID("11111111-2222-4333-8444-555555555555")
		}, ErrMetricScopeInvalid},
		{"unknown scope type", func(in *ReadingInput) { in.ScopeType = "team" }, ErrMetricScopeInvalid},
		{"dated in the far future", func(in *ReadingInput) { in.ObservedAt = f.now.Add(48 * time.Hour) }, ErrMetricObservedInvalid},
		{"dated more than a year back", func(in *ReadingInput) { in.ObservedAt = f.now.AddDate(-2, 0, 0) }, ErrMetricObservedInvalid},
		{"note over the limit", func(in *ReadingInput) {
			b := make([]byte, metricReadingNote+1)
			for i := range b {
				b[i] = 'x'
			}
			in.Note = string(b)
		}, ErrMetricNoteTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := base
			c.mutate(&in)
			if _, err := svc.RecordReading(ctx, in, f.now); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}

	row, err := svc.RecordReading(ctx, base, f.now)
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	if row.MetricKey != "mrr" || row.ActorType != ReadingActorMember {
		t.Fatalf("reading = %+v", row)
	}
	var source string
	f.QueryRow(t, `SELECT source FROM metric WHERE workspace_id = $1 AND key = 'mrr'`, f.wsText).Scan(&source)
	if source != MetricSourceManual {
		t.Fatalf("first report created a %q metric, want manual", source)
	}
	if _, err := svc.RecordReading(ctx, ReadingInput{WorkspaceID: f.ws, Key: "mrr", Source: MetricSourceWebhook, ScopeType: ScopeCompany, Value: 1300, ActorType: ReadingActorWebhook}, f.now); err != nil {
		t.Fatalf("second report: %v", err)
	}
	var metrics int
	f.QueryRow(t, `SELECT count(*) FROM metric WHERE workspace_id = $1 AND key = 'mrr'`, f.wsText).Scan(&metrics)
	if metrics != 1 {
		t.Fatalf("a second report made %d metric rows, want 1", metrics)
	}
	series, err := svc.ListReadings(ctx, MetricSeriesParams{WorkspaceID: f.ws, Key: "mrr", ScopeType: ScopeCompany, Until: f.now.Add(time.Hour), Limit: 10})
	if err != nil || len(series) != 2 || !closeTo(mustFloat(t, series[0].Value), 1300) {
		t.Fatalf("series = %d readings (err %v), newest should be 1300", len(series), err)
	}
}

// squadPG gives a company-scope test a scope id that the company scope refuses.
func (f govFixture) squadPG(t *testing.T) pgtype.UUID { return util.MustParseUUID(f.squad) }

func mustFloat(t *testing.T, n pgtype.Numeric) float64 {
	t.Helper()
	v, ok := floatFromNumeric(n)
	if !ok {
		t.Fatalf("numeric is NULL")
	}
	return v
}

// TestDownsampleKeepsOneReadingPerSeriesPerDay: readings older than the retention
// window keep only the newest reading of each series on each day. Recent readings
// are untouched, and so are other series.
func TestDownsampleKeepsOneReadingPerSeriesPerDay(t *testing.T) {
	f := newGovFixture(t)
	ctx := context.Background()
	svc := f.metricsService()
	old := f.now.AddDate(0, 0, -120)
	day := time.Date(old.Year(), old.Month(), old.Day(), 0, 0, 0, 0, time.UTC)
	// Three readings on one day, one on the next, one of another series, and one
	// recent reading that the window must keep.
	for _, at := range []time.Time{day.Add(1 * time.Hour), day.Add(5 * time.Hour), day.Add(20 * time.Hour), day.AddDate(0, 0, 1).Add(10 * time.Hour)} {
		f.insertReading(t, "dup.key", ScopeCompany, "", 1, at)
	}
	f.insertReading(t, "dup.other", ScopeCompany, "", 7, day.Add(3*time.Hour))
	f.insertReading(t, "dup.key", ScopeCompany, "", 9, f.now.Add(-time.Hour))

	if _, err := svc.Downsample(ctx, f.now); err != nil {
		t.Fatalf("downsample: %v", err)
	}
	series, err := svc.ListReadings(ctx, MetricSeriesParams{WorkspaceID: f.ws, Key: "dup.key", ScopeType: ScopeCompany, Since: day.AddDate(0, 0, -1), Until: f.now.Add(time.Hour), Limit: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(series) != 3 {
		t.Fatalf("after downsampling the series has %d readings, want 3 (one per day, plus the recent one)", len(series))
	}
	if !series[0].ObservedAt.Time.Equal(f.now.Add(-time.Hour)) {
		t.Errorf("the recent reading was touched")
	}
	if got := mustFloat(t, series[1].Value); got != 1 || !series[1].ObservedAt.Time.Equal(day.AddDate(0, 0, 1).Add(10*time.Hour)) {
		t.Errorf("the day after kept %v at %v", got, series[1].ObservedAt.Time)
	}
	if !series[2].ObservedAt.Time.Equal(day.Add(20 * time.Hour)) {
		t.Errorf("the day kept %v, want its newest reading at 20:00", series[2].ObservedAt.Time)
	}
	other, _ := svc.ListReadings(ctx, MetricSeriesParams{WorkspaceID: f.ws, Key: "dup.other", ScopeType: ScopeCompany, Until: f.now.Add(time.Hour), Limit: 10})
	if len(other) != 1 {
		t.Errorf("another series lost readings: %d left", len(other))
	}
}

// insertReading writes one reading directly, to set up history the retention job
// will reduce.
func (f govFixture) insertReading(t *testing.T, key, scopeType, scopeID string, value float64, at time.Time) {
	t.Helper()
	var scope any
	if scopeID != "" {
		scope = scopeID
	}
	f.Exec(t, `INSERT INTO metric_reading (workspace_id, metric_key, scope_type, scope_id, value, observed_at, actor_type)
		VALUES ($1, $2, $3, $4, $5, $6, 'member')`, f.wsText, key, scopeType, scope, value, at)
}
