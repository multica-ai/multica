package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// Authentication failures require an operator to fix the installation;
// connection failures usually recover. Keep them as independently actionable
// Prometheus series rather than allowing both methods to increment one counter.
func TestAuthAndConnectFailuresAreSeparateSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, collector := range m.Collectors() {
		if err := reg.Register(collector); err != nil {
			t.Fatalf("register WeCom collector: %v", err)
		}
	}

	m.RecordAuthFailure()
	m.RecordAuthFailure()
	m.RecordConnectFailure()

	values := gatherWecomCounterValues(t, reg)
	if got := values["multica_wecom_auth_failures_total"]; got != 2 {
		t.Errorf("auth failures = %v, want 2", got)
	}
	if got := values["multica_wecom_connect_failures_total"]; got != 1 {
		t.Errorf("connect failures = %v, want 1", got)
	}
}

func gatherWecomCounterValues(t *testing.T, reg prometheus.Gatherer) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather WeCom metrics: %v", err)
	}
	values := make(map[string]float64, len(families))
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			if counter := metric.GetCounter(); counter != nil {
				values[family.GetName()] += counter.GetValue()
			}
		}
	}
	return values
}

// The Help string is the whole of what an operator is told about a counter: it
// ships on /metrics and it is what a dashboard prints beside the series. This
// one used to say none of the skips was a delivery failure, and listed the
// three that existed then. Both stopped being true when no_delivery_row was
// added — a turn the channel ingested with no row saying which chat, so a reply
// may well be owed and nothing left can name the room, which is the one skip
// the adapter logs at WARN.
//
// A label an operator sees in the breakdown and cannot find in the Help, or a
// Help that tells them every skip is harmless while one of them is the alert,
// both get resolved the wrong way at 3am. So the reason list is asserted here
// rather than left to whoever edits the prose next. It duplicates the closed
// set in wecom/outbound_outcome.go deliberately: the constants are unexported
// and in another package, and the point of the test is that the two stay in
// step.
//
// REVERSE VERIFICATION: drop no_delivery_row from the Help, or put the old
// "none of these is a delivery failure" sentence back, and this fails naming
// what the operator would have been left with.
func TestSkippedHelpNamesEveryReasonAndTheOneWorthAnAlert(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := NewWecomMetrics()
	for _, collector := range m.Collectors() {
		if err := reg.Register(collector); err != nil {
			t.Fatalf("register WeCom collector: %v", err)
		}
	}
	// A CounterVec with no children is gathered as no family at all, so the
	// Help only becomes readable once a label value exists.
	m.RecordOutboundSkipped("no_delivery_row")

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather WeCom metrics: %v", err)
	}
	help := ""
	for _, family := range families {
		if family.GetName() == "multica_wecom_outbound_skipped_total" {
			help = family.GetHelp()
		}
	}
	if help == "" {
		t.Fatal("multica_wecom_outbound_skipped_total carries no help text")
	}

	for _, reason := range []string{
		"origin_not_channel", "installation_inactive", "nothing_to_say",
		"not_wecom_turn", "no_delivery_row",
	} {
		if !strings.Contains(help, reason) {
			t.Errorf("help does not explain the %q label, which an operator will meet in the breakdown with nothing to read:\n%s", reason, help)
		}
	}
	if strings.Contains(help, "none of these is a delivery failure") {
		t.Errorf("help still tells the operator every skip is harmless, while no_delivery_row is a reply that may be owed and unplaceable:\n%s", help)
	}
}
