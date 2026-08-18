package handler

import "testing"

func TestParseMaxUploadSize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "default", raw: "", want: DefaultMaxUploadSizeBytes},
		{name: "bytes", raw: "4096", want: 4096},
		{name: "megabytes", raw: "250MB", want: 250 << 20},
		{name: "mebibytes with whitespace", raw: " 64 MiB ", want: 64 << 20},
		{name: "maximum", raw: "1GiB", want: MaxConfigurableUploadSizeBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMaxUploadSize(tt.raw)
			if err != nil {
				t.Fatalf("ParseMaxUploadSize(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMaxUploadSize(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseMaxUploadSizeRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{"0", "-1", "10.5MB", "100watts", "2GiB"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseMaxUploadSize(raw); err == nil {
				t.Fatalf("ParseMaxUploadSize(%q) unexpectedly succeeded", raw)
			}
		})
	}
}
