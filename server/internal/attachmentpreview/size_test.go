package attachmentpreview

import "testing"

func TestConfiguredMaxSizeBytes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"", 2 << 20}, {"8MiB", 8 << 20}, {"16MiB", 16 << 20},
		{"invalid", 2 << 20}, {"17MiB", 16 << 20}, {"32MiB", 16 << 20},
		{"9223372036854775807", 16 << 20}, {"9223372036854775808", 2 << 20},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := ConfiguredMaxSizeBytes(tc.raw); got != tc.want {
				t.Fatalf("ConfiguredMaxSizeBytes(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestParseMaxSizeBytes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"", 2 << 20}, {"  ", 2 << 20}, {"1", 1}, {"4096B", 4096},
		{"8KB", 8192}, {"8KiB", 8192}, {"8MB", 8 << 20},
		{" 8 MiB ", 8 << 20}, {"16mib", 16 << 20}, {"16777216", 16 << 20},
		{"17MiB", 16 << 20}, {"32MiB", 16 << 20}, {"16777217", 16 << 20},
		{"9223372036854775807", 16 << 20}, {"9223372036854775807MiB", 16 << 20},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := parseMaxSizeBytes(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("parseMaxSizeBytes(%q) = %d, %v; want %d", tc.raw, got, err, tc.want)
			}
		})
	}
	for _, raw := range []string{"0", "-1", "+1", "1.5MiB", "MiB", "16watts", "1GiB", "9223372036854775808"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseMaxSizeBytes(raw); err == nil {
				t.Fatalf("parseMaxSizeBytes(%q) accepted invalid value", raw)
			}
		})
	}
}

func TestEffectiveMaxSizeBytes(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want int64
	}{
		{0, 2 << 20}, {-1, 2 << 20}, {1, 1}, {8 << 20, 8 << 20},
		{16 << 20, 16 << 20}, {(16 << 20) + 1, 16 << 20},
		{32 << 20, 16 << 20}, {1<<63 - 1, 16 << 20},
	} {
		if got := EffectiveMaxSizeBytes(tc.size); got != tc.want {
			t.Fatalf("EffectiveMaxSizeBytes(%d) = %d, want %d", tc.size, got, tc.want)
		}
	}
}
