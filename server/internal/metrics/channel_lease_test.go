package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestChannelLeaseMetricsRegisterAndRecord(t *testing.T) {
	m := NewChannelLeaseMetrics()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(m.Collectors()...)
	m.RecordLeaseOperation("renew", "error")
	m.SetActiveLeaseOwners(2)
	m.SetOwnersWithRenewalErrors(1)
	m.SetLastSuccessfulRenewal(time.Unix(123, 0))
	m.ObserveTakeoverLatency(30 * time.Second)

	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"multica_channel_lease_operations_total":                          false,
		"multica_channel_lease_active_owners":                             false,
		"multica_channel_lease_owners_with_renewal_errors":                false,
		"multica_channel_lease_last_successful_renewal_timestamp_seconds": false,
		"multica_channel_lease_takeover_latency_seconds":                  false,
	}
	for _, family := range families {
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("metric %s was not gathered", name)
		}
	}
}
