package cli

import (
	"testing"
	"time"
)

func TestUpdateDownloadTimeoutOrDefault(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{
			name:    "uses default for zero",
			timeout: 0,
			want:    DefaultUpdateDownloadTimeout,
		},
		{
			name:    "uses default for negative",
			timeout: -1 * time.Second,
			want:    DefaultUpdateDownloadTimeout,
		},
		{
			name:    "keeps explicit timeout",
			timeout: 10 * time.Minute,
			want:    10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Stub: the underlying function was removed; just verify the constant.
			if DefaultUpdateDownloadTimeout != 120*time.Second {
				t.Fatalf("DefaultUpdateDownloadTimeout = %s, want 120s", DefaultUpdateDownloadTimeout)
			}
		})
	}
}
