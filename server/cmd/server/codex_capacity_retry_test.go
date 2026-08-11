package main

import "testing"

func TestCodexCapacityRetryCountFromEnv(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int32
	}{
		{"empty uses default", "", 6},
		{"zero disables", "0", 0},
		{"positive count", "3", 3},
		{"surrounding whitespace", " 3 ", 3},
		{"negative uses default", "-1", 6},
		{"malformed uses default", "six", 6},
		{"maximum accepted", "2147483646", 2147483646},
		{"overflowing total uses default", "2147483647", 6},
		{"int32 overflow uses default", "2147483648", 6},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_CODEX_CAPACITY_RETRY_COUNT", tc.raw)
			if got := codexCapacityRetryCountFromEnv(); got != tc.want {
				t.Fatalf("codexCapacityRetryCountFromEnv() = %d, want %d", got, tc.want)
			}
		})
	}
}
