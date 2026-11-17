package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInboxReadMetrics(t *testing.T) {
	r := NewRegistry(RegistryOptions{})
	m := r.InboxRead
	m.Active(1)
	m.Queue(2)
	m.Reject("list", "queue_full")
	m.Observe("list", time.Second, context.DeadlineExceeded)
	m.Observe("summary", time.Second, &pgconn.PgError{Code: "57014"})
	m.ObserveQuery("summary", 50*time.Millisecond)
	if testutil.ToFloat64(m.active) != 1 || testutil.ToFloat64(m.queued) != 2 {
		t.Fatal("missing admission gauges")
	}
	if testutil.ToFloat64(m.rejected.WithLabelValues("list", "queue_full")) != 1 {
		t.Fatal("missing rejection")
	}
	for _, operation := range []string{"list", "summary"} {
		if testutil.ToFloat64(m.timeouts.WithLabelValues(operation)) != 1 {
			t.Fatal("missing timeout")
		}
	}
	families, err := r.Gatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	foundQuery := false
	for _, family := range families {
		if family.GetName() == "multica_inbox_read_query_duration_seconds" {
			foundQuery = family.Metric[0].Histogram.GetSampleCount() == 1
		}
	}
	if !foundQuery {
		t.Fatal("missing registered SQL duration histogram")
	}
	var disabled *InboxReadMetrics
	disabled.Active(1)
	disabled.Queue(1)
	disabled.Reject("list", "queue_full")
	disabled.Observe("list", time.Second, nil)
	disabled.ObserveQuery("list", time.Second)
}
