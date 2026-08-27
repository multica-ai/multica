package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSeatCapacityMetricsExposePendingAndDeadLettersByAction(t *testing.T) {
	metrics := NewSeatCapacityMetrics()
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(metrics.Collectors()...)

	metrics.SetOutbox("release", 3, 1, 901)

	if got := testutil.ToFloat64(metrics.Pending.WithLabelValues("release")); got != 3 {
		t.Fatalf("pending release=%v, want 3", got)
	}
	if got := testutil.ToFloat64(metrics.DeadLettered.WithLabelValues("release")); got != 1 {
		t.Fatalf("dead-lettered release=%v, want 1", got)
	}
	if got := testutil.ToFloat64(metrics.OldestPendingAge.WithLabelValues("release")); got != 901 {
		t.Fatalf("oldest release age=%v, want 901", got)
	}
	if _, err := registry.Gather(); err != nil {
		t.Fatal(err)
	}

	metrics.ResetOutbox()
	if got := testutil.ToFloat64(metrics.DeadLettered.WithLabelValues("release")); got != 0 {
		t.Fatalf("dead-lettered release after reset=%v, want 0", got)
	}
}
