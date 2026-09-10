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
	"github.com/multica-ai/multica/server/internal/util"
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

// TestRuntimeLookupGetManyFailsClosed pins the contract MUL-6788's review added:
// a failed batch read is returned as an error, not swallowed into an empty map
// that a caller could mistake for "none of these runtimes exist". The metric
// also stays honest — one "error" per requested id, matching what N individual
// Get calls would have recorded.
func TestRuntimeLookupGetManyFailsClosed(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection reset")
	m := obsmetrics.NewBusinessMetrics()
	lookup := RuntimeLookup{
		Queries: db.New(scanErrDBTX{err: dbErr}),
		Metrics: m,
		Source:  obsmetrics.RuntimeLookupSourceDaemonAPI,
	}

	ids := []pgtype.UUID{{}, {}, {}}
	got, err := lookup.GetMany(context.Background(), ids)
	if !errors.Is(err, dbErr) {
		t.Fatalf("GetMany error = %v, want %v", err, dbErr)
	}
	if got != nil {
		t.Errorf("GetMany map = %v, want nil on error", got)
	}
	if n := lookupCount(t, m, obsmetrics.RuntimeLookupSourceDaemonAPI, obsmetrics.RuntimeLookupResultError); n != float64(len(ids)) {
		t.Errorf("daemon_api/error = %v, want %d", n, len(ids))
	}
}

// TestRuntimeLookupGetManyNilMetricsIsSafe mirrors the single-read nil-metrics
// guard for the batch path.
func TestRuntimeLookupGetManyNilMetricsIsSafe(t *testing.T) {
	t.Parallel()

	lookup := RuntimeLookup{Queries: db.New(scanErrDBTX{err: errors.New("db down")}), Source: obsmetrics.RuntimeLookupSourceDaemonAPI}
	if _, err := lookup.GetMany(context.Background(), []pgtype.UUID{{}}); err == nil {
		t.Fatal("GetMany error = nil, want the injected read failure")
	}
}

// TestRuntimeLookupGetManySuccessClassifiesEachID is the important success-path
// case: the batch runs one SQL query but the metric must still record one
// logical lookup per REQUESTED id — ok for an id whose row came back,
// not_found for one that did not, and a repeated id counted once per
// occurrence (the same shape N individual Get calls would have produced). The
// returned map is keyed by canonical UUID string so a requested id resolves
// regardless of case.
func TestRuntimeLookupGetManySuccessClassifiesEachID(t *testing.T) {
	t.Parallel()

	found := mustUUID(t, "11111111-1111-1111-1111-111111111111")
	missing := mustUUID(t, "22222222-2222-2222-2222-222222222222")

	m := obsmetrics.NewBusinessMetrics()
	lookup := RuntimeLookup{
		// The query returns only the found row; missing is absent.
		Queries: db.New(runtimeRowsDBTX{rows: []db.AgentRuntime{{ID: found}}}),
		Metrics: m,
		Source:  obsmetrics.RuntimeLookupSourceDaemonAPI,
	}

	// Request found twice (duplicate) plus the missing id.
	ids := []pgtype.UUID{found, missing, found}
	got, err := lookup.GetMany(context.Background(), ids)
	if err != nil {
		t.Fatalf("GetMany error = %v, want nil", err)
	}
	if _, ok := got[util.UUIDToString(found)]; !ok {
		t.Errorf("found id missing from result map %v", got)
	}
	if _, ok := got[util.UUIDToString(missing)]; ok {
		t.Errorf("missing id must not be in result map %v", got)
	}
	if n := lookupCount(t, m, obsmetrics.RuntimeLookupSourceDaemonAPI, obsmetrics.RuntimeLookupResultOK); n != 2 {
		t.Errorf("daemon_api/ok = %v, want 2 (found requested twice)", n)
	}
	if n := lookupCount(t, m, obsmetrics.RuntimeLookupSourceDaemonAPI, obsmetrics.RuntimeLookupResultNotFound); n != 1 {
		t.Errorf("daemon_api/not_found = %v, want 1", n)
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

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

// runtimeRowsDBTX is a db.DBTX whose Query returns a fixed set of AgentRuntime
// rows, so GetMany's success branch (per-id ok/not_found classification) is
// testable without a database. Only the columns GetAgentRuntimes scans are
// served; GetMany only keys on ID, so unset columns stay zero-valued.
type runtimeRowsDBTX struct{ rows []db.AgentRuntime }

func (d runtimeRowsDBTX) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (d runtimeRowsDBTX) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return &runtimeRows{rows: d.rows}, nil
}

func (d runtimeRowsDBTX) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return errRow{err: pgx.ErrNoRows}
}

// runtimeRows is a minimal pgx.Rows over a slice of AgentRuntime. Scan writes
// each row's fields positionally into the destinations GetAgentRuntimes passes.
type runtimeRows struct {
	rows []db.AgentRuntime
	i    int
}

func (r *runtimeRows) Close()                                       {}
func (r *runtimeRows) Err() error                                   { return nil }
func (r *runtimeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *runtimeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *runtimeRows) Values() ([]any, error)                       { return nil, nil }
func (r *runtimeRows) RawValues() [][]byte                          { return nil }
func (r *runtimeRows) Conn() *pgx.Conn                              { return nil }

func (r *runtimeRows) Next() bool {
	if r.i >= len(r.rows) {
		return false
	}
	r.i++
	return true
}

// Scan copies the current row's ID into the first destination — the only field
// GetMany reads — and leaves the rest untouched.
func (r *runtimeRows) Scan(dest ...any) error {
	row := r.rows[r.i-1]
	if len(dest) > 0 {
		if p, ok := dest[0].(*pgtype.UUID); ok {
			*p = row.ID
		}
	}
	return nil
}
