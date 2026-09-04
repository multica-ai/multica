package metrics

import "testing"

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
		{input: "DSH", want: "dsh"},
		{input: "Dim", want: "dim"},
		{input: "ZeroClaw", want: "zeroclaw"},
		{input: "CodeArts", want: "codearts"},
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
