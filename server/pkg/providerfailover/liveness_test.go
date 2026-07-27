package providerfailover

import (
	"testing"
	"time"
)

func TestLivenessDeadline(t *testing.T) {
	t.Parallel()
	cases := map[string]time.Duration{
		"codex":   60 * time.Minute,
		"claude":  180 * time.Second,
		"grok":    defaultLivenessDeadline, // unmodeled provider → conservative default
		"":        defaultLivenessDeadline,
		"unknown": defaultLivenessDeadline,
	}
	for provider, want := range cases {
		if got := LivenessDeadline(provider); got != want {
			t.Errorf("LivenessDeadline(%q) = %s, want %s", provider, got, want)
		}
	}
}

func TestIsSilentHang(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		provider       string
		runningFor     time.Duration
		heartbeatAlive bool
		want           bool
	}{
		{"codex hung past deadline, dead heartbeat", "codex", 61 * time.Minute, false, true},
		{"codex past deadline but heartbeat alive (busy)", "codex", 61 * time.Minute, true, false},
		{"codex under deadline, dead heartbeat", "codex", 10 * time.Minute, false, false},
		{"claude hung past 180s, dead heartbeat", "claude", 200 * time.Second, false, true},
		{"claude under 180s, dead heartbeat", "claude", 120 * time.Second, false, false},
		{"claude exactly at deadline, dead heartbeat", "claude", 180 * time.Second, false, true},
		{"unknown provider uses default deadline", "grok", 61 * time.Minute, false, true},
		{"live long run never a hang", "codex", 6 * time.Hour, true, false},
	}
	for _, tc := range tests {
		if got := IsSilentHang(tc.provider, tc.runningFor, tc.heartbeatAlive); got != tc.want {
			t.Errorf("%s: IsSilentHang(%q, %s, alive=%v) = %v, want %v",
				tc.name, tc.provider, tc.runningFor, tc.heartbeatAlive, got, tc.want)
		}
	}
}
