package handler

import "testing"

func TestParseMaxPreviewSize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "default", raw: "", want: DefaultMaxPreviewSizeBytes},
		{name: "bytes", raw: "4096", want: 4096},
		{name: "megabytes", raw: "8MB", want: 8 << 20},
		{name: "mebibytes with whitespace", raw: " 8 MiB ", want: 8 << 20},
		{name: "maximum", raw: "16MiB", want: MaxConfigurablePreviewSizeBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMaxPreviewSize(tt.raw)
			if err != nil {
				t.Fatalf("ParseMaxPreviewSize(%q): %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMaxPreviewSize(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseMaxPreviewSizeRejectsUnsafeValues(t *testing.T) {
	for _, raw := range []string{
		"0",
		"-1",
		"10.5MiB",
		"100watts",
		"16777217",
		"17MiB",
		"1GiB",
		"9223372036854775807",
		"9223372036854775808",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseMaxPreviewSize(raw); err == nil {
				t.Fatalf("ParseMaxPreviewSize(%q) unexpectedly succeeded", raw)
			}
		})
	}
}

func TestMaxPreviewSizeBytesNormalizesUnsafeConfig(t *testing.T) {
	tests := []struct {
		name string
		size int64
		want int64
	}{
		{name: "zero selects default", size: 0, want: DefaultMaxPreviewSizeBytes},
		{name: "negative selects default", size: -1, want: DefaultMaxPreviewSizeBytes},
		{name: "custom", size: 8 << 20, want: 8 << 20},
		{name: "maximum", size: MaxConfigurablePreviewSizeBytes, want: MaxConfigurablePreviewSizeBytes},
		{name: "over maximum selects default", size: MaxConfigurablePreviewSizeBytes + 1, want: DefaultMaxPreviewSizeBytes},
		{name: "max int64 selects default", size: int64(1<<63 - 1), want: DefaultMaxPreviewSizeBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{cfg: Config{MaxPreviewSizeBytes: tt.size}}
			if got := h.maxPreviewSizeBytes(); got != tt.want {
				t.Fatalf("maxPreviewSizeBytes() = %d, want %d", got, tt.want)
			}
		})
	}
}
