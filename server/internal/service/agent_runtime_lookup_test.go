package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	dto "github.com/prometheus/client_model/go"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestRuntimeLookupClassifiesResult pins the distinction the metric exists to
// preserve: a deleted runtime row (pgx.ErrNoRows, which the daemon reads as
// "drop your local registration") must not be counted as a database failure,
// and neither may be counted as a success. Collapsing them would make a real
// outage indistinguishable from a user deleting a machine.
func TestRuntimeLookupClassifiesResult(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection reset")
	for _, tc := range []struct {
		name       string
		scanErr    error
		wantResult string
		wantErr    error
	}{
		{"found", nil, obsmetrics.RuntimeLookupResultOK, nil},
		{"deleted", pgx.ErrNoRows, obsmetrics.RuntimeLookupResultNotFound, pgx.ErrNoRows},
		{"db down", dbErr, obsmetrics.RuntimeLookupResultError, dbErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := obsmetrics.NewBusinessMetrics()
			lookup := RuntimeLookup{
				Queries: db.New(scanErrDBTX{err: tc.scanErr}),
				Metrics: m,
				Source:  obsmetrics.RuntimeLookupSourceHeartbeatWS,
			}

			_, err := lookup.Get(context.Background(), pgtype.UUID{})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Get error = %v, want %v", err, tc.wantErr)
			}
			if got := lookupCount(t, m, obsmetrics.RuntimeLookupSourceHeartbeatWS, tc.wantResult); got != 1 {
				t.Errorf("heartbeat_ws/%s = %v, want 1", tc.wantResult, got)
			}
		})
	}
}

// TestRuntimeLookupNilMetricsIsSafe covers self-hosted deployments and tests
// that run without the metrics listener: the read must still happen.
func TestRuntimeLookupNilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	lookup := RuntimeLookup{Queries: db.New(scanErrDBTX{err: pgx.ErrNoRows}), Source: obsmetrics.RuntimeLookupSourceTask}
	if _, err := lookup.Get(context.Background(), pgtype.UUID{}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("Get error = %v, want pgx.ErrNoRows", err)
	}
}

// ---- helpers --------------------------------------------------------------

func lookupCount(t *testing.T, m *obsmetrics.BusinessMetrics, source, result string) float64 {
	t.Helper()

	fam := obsmetrics.GatherForTest(t, m)["multica_agent_runtime_lookup_total"]
	if fam == nil {
		t.Fatalf("multica_agent_runtime_lookup_total not registered")
	}
	for _, mtr := range fam.GetMetric() {
		if labelValue(mtr, "source") == source && labelValue(mtr, "result") == result {
			return mtr.GetCounter().GetValue()
		}
	}
	t.Fatalf("no sample for %s/%s", source, result)
	return 0
}

func labelValue(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

// scanErrDBTX is a db.DBTX whose single-row reads fail with a fixed error, so
// the wrapper's error classification is testable without a database.
type scanErrDBTX struct{ err error }

func (d scanErrDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, d.err
}

func (d scanErrDBTX) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, d.err
}

func (d scanErrDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row { return errRow{d.err} }

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }
