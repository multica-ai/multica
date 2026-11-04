package main

import "testing"

func TestConfiguredMaxPreviewSize(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"", 2 << 20}, {"8MiB", 8 << 20}, {"16MiB", 16 << 20},
		{"invalid", 2 << 20}, {"17MiB", 2 << 20}, {"9223372036854775807", 2 << 20},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := configuredMaxPreviewSize(tc.raw); got != tc.want {
				t.Fatalf("configuredMaxPreviewSize(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseMaxPreviewSize(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"", 2 << 20}, {"  ", 2 << 20}, {"1", 1}, {"4096B", 4096},
		{"8KB", 8192}, {"8KiB", 8192}, {"8MB", 8 << 20},
		{" 8 MiB ", 8 << 20}, {"16mib", 16 << 20}, {"16777216", 16 << 20},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := parseMaxPreviewSize(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("parseMaxPreviewSize(%q) = %d, %v; want %d", tc.raw, got, err, tc.want)
			}
		})
	}
	for _, raw := range []string{"0", "-1", "+1", "1.5MiB", "MiB", "16watts", "17MiB", "16777217", "1GiB", "9223372036854775807", "9223372036854775808", "9223372036854775807MiB"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseMaxPreviewSize(raw); err == nil {
				t.Fatalf("parseMaxPreviewSize(%q) accepted unsafe value", raw)
			}
		})
	}
}
