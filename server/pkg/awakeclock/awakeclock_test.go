package awakeclock

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestNowTracksAwakeTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := Now()
		time.Sleep(90 * time.Minute)
		if got := Now() - start; got != 90*time.Minute {
			t.Fatalf("elapsed = %s, want 90m", got)
		}
	})
}

func TestSleptFromReadings(t *testing.T) {
	// 5h since boot including sleep; 3h awake → 2h slept.
	if got := sleptFromReadings(uint64((5 * time.Hour).Milliseconds()), uint64(3*time.Hour/100)); got != 2*time.Hour {
		t.Fatalf("slept = %s, want 2h", got)
	}
	// Tick count lags by sub-ms resolution: clamped, never negative.
	if got := sleptFromReadings(1000, uint64(1000*time.Millisecond/100)+5000); got != 0 {
		t.Fatalf("slept = %s, want 0", got)
	}
}

func TestSleptDelta(t *testing.T) {
	if got := sleptDelta(time.Hour, 3*time.Hour); got != 2*time.Hour {
		t.Fatalf("delta = %s, want 2h", got)
	}
	if got := sleptDelta(time.Hour, time.Hour-time.Millisecond); got != 0 {
		t.Fatalf("delta = %s, want 0", got)
	}
}
