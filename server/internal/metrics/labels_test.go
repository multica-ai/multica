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

func TestNormalizeRuntimeProviderRecognizesKnownProviders(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "QWEN", want: "qwen"},
		{input: "Qoder", want: "qoder"},
		{input: "QODERCLICN", want: "qoderclicn"},
		{input: "TraeCLI", want: "traecli"},
		{input: "Reasonix", want: "reasonix"},
	}
	for _, tt := range tests {
		if got := NormalizeRuntimeProvider(tt.input); got != tt.want {
			t.Errorf("NormalizeRuntimeProvider(%q) = %q, want %q", tt.input, got, tt.want)
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

// TestForbiddenLabelsCoverChannelIdentifiers: the channel adapters (slack,
// lark, dingtalk, wecom) all carry an installation id at every metric call
// site, so it is the natural label to reach for. One series per installation
// grows with tenants, not with the deployment — the same reason workspace_id
// and session_id are on this list.
func TestForbiddenLabelsCoverChannelIdentifiers(t *testing.T) {
	for _, label := range []string{"installation_id", "workspace_id", "session_id"} {
		if _, forbidden := forbiddenMetricLabels[label]; !forbidden {
			t.Errorf("%s is not forbidden — a per-tenant identifier will eventually be used as a metric label and multiply the series count by the tenant count", label)
		}
	}
}
