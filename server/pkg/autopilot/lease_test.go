package autopilot

import (
	"testing"
	"time"
)

func TestLeaseConfig_CalculateLeaseDuration(t *testing.T) {
	tests := []struct {
		name         string
		baseTimeout  time.Duration
		slotInterval time.Duration
		expected     time.Duration
	}{
		{
			name:         "slot interval larger than base timeout",
			baseTimeout:  30 * time.Minute,
			slotInterval: 1 * time.Hour,
			expected:     1 * time.Hour,
		},
		{
			name:         "base timeout larger than slot interval",
			baseTimeout:  1 * time.Hour,
			slotInterval: 15 * time.Minute,
			expected:     1 * time.Hour,
		},
		{
			name:         "equal values",
			baseTimeout:  30 * time.Minute,
			slotInterval: 30 * time.Minute,
			expected:     30 * time.Minute,
		},
		{
			name:         "zero slot interval falls back to base timeout",
			baseTimeout:  30 * time.Minute,
			slotInterval: 0,
			expected:     30 * time.Minute,
		},
		{
			name:         "sub-minimum config is clamped to the floor",
			baseTimeout:  1 * time.Second,
			slotInterval: 0,
			expected:     MinLeaseDuration,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &LeaseConfig{
				BaseTimeout:  tt.baseTimeout,
				SlotInterval: tt.slotInterval,
			}
			if got := config.CalculateLeaseDuration(); got != tt.expected {
				t.Fatalf("CalculateLeaseDuration() = %v, want %v", got, tt.expected)
			}
		})
	}
}
