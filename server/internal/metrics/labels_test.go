package metrics

import "testing"

func TestBusinessMetricLabelsRejectHighCardinalityNames(t *testing.T) {
	for metric, labels := range businessMetricLabels {
		for _, label := range labels {
			if _, forbidden := forbiddenMetricLabels[label]; forbidden {
				t.Fatalf("metric %s uses forbidden label %s", metric, label)
			}
		}
	}
}

func TestNormalizeLabelsCollapseUnknownValues(t *testing.T) {
	if got := NormalizeRuntimeProvider("provider-from-user-input"); got != "other" {
		t.Fatalf("NormalizeRuntimeProvider unknown = %q, want other", got)
	}
	if got := NormalizeRuntimeMode("workspace-123"); got != "unknown" {
		t.Fatalf("NormalizeRuntimeMode unknown = %q, want unknown", got)
	}
	if got := NormalizeTaskSource("task-123"); got != "other" {
		t.Fatalf("NormalizeTaskSource unknown = %q, want other", got)
	}
}
