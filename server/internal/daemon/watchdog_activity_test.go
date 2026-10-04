package daemon

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Use Go's virtual clock for synctest on every OS, including Windows where the
// production awake clock is a syscall outside the bubble.
func newTestWatchdogActivity() *watchdogActivity {
	start := time.Now()
	return newWatchdogActivity(func() time.Duration { return time.Since(start) })
}

func TestIdleWatchdogExcludesSuspend(t *testing.T) {
	for _, toolInFlight := range []bool{false, true} {
		name := "idle"
		if toolInFlight {
			name = "tool"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var awake, threshold atomic.Int64
				activity := newWatchdogActivity(func() time.Duration { return time.Duration(awake.Load()) })
				var fired atomic.Bool
				tools := func() int32 {
					if toolInFlight {
						return 1
					}
					return 0
				}
				idle := time.Minute
				if toolInFlight {
					idle = 10 * time.Minute
				}
				go new(Daemon).runIdleWatchdog(ctx, idle, time.Minute, activity, tools,
					&fired, &threshold, cancel, make(chan agent.Message), nil, nil, slog.Default())
				synctest.Wait()
				awake.Store(int64(5 * time.Second))
				activity.record()
				// Timers and civil time advance by hours while the host's awake
				// clock does not. Even ticking throughout must not consume budget.
				time.Sleep(3 * time.Hour)
				synctest.Wait()
				if fired.Load() || ctx.Err() != nil {
					t.Fatal("host suspension consumed the inactivity budget")
				}
				awake.Add(int64(59 * time.Second))
				time.Sleep(30 * time.Second)
				synctest.Wait()
				if fired.Load() {
					t.Fatal("watchdog fired before one minute of awake silence")
				}
				awake.Add(int64(time.Second))
				time.Sleep(30 * time.Second)
				synctest.Wait()
				if !fired.Load() || ctx.Err() == nil || time.Duration(threshold.Load()) != time.Minute {
					t.Fatalf("real silence must still expire: fired=%v threshold=%v err=%v", fired.Load(), time.Duration(threshold.Load()), ctx.Err())
				}
			})
		})
	}
}

func TestWatchdogActivityNativeMarkers(t *testing.T) {
	awake := time.Hour
	activity := newWatchdogActivity(func() time.Duration { return awake })
	at := time.Unix(1_700_000_000, 0)
	tools := activity.trackTools(func() (int32, time.Time) { return 1, at })
	awake += time.Second
	tools()
	if activity.last.Load() != int64(time.Hour) {
		t.Fatal("unchanged startup marker counted as progress")
	}
	for _, jump := range []time.Duration{3 * time.Hour, -6 * time.Hour} {
		awake += time.Second
		at = at.Add(jump)
		if tools() != 1 || activity.last.Load() != int64(awake) {
			t.Fatal("native transition must use awake time, not the backend's civil timestamp")
		}
		last := activity.last.Load()
		awake += time.Minute
		tools()
		if activity.last.Load() != last {
			t.Fatal("repeated polling renewed a silent tool's budget")
		}
	}
	at = time.Time{}
	last := activity.last.Load()
	tools()
	if activity.last.Load() != last {
		t.Fatal("missing native timestamp counted as activity")
	}
}

func TestWatchdogActivityConcurrentRecordsDoNotRegress(t *testing.T) {
	var calls atomic.Int32
	olderReady, releaseOlder := make(chan struct{}), make(chan struct{})
	activity := newWatchdogActivity(func() time.Duration {
		switch calls.Add(1) {
		case 1:
			return 0
		case 2:
			close(olderReady)
			<-releaseOlder
			return time.Second
		default:
			return 2 * time.Second
		}
	})
	done := make(chan struct{})
	go func() { activity.record(); close(done) }()
	<-olderReady
	activity.record()
	close(releaseOlder)
	<-done
	if activity.last.Load() != int64(2*time.Second) {
		t.Fatal("delayed older writer overwrote newer activity")
	}
}

func TestWatchdogClockAdvances(t *testing.T) {
	// Exercises the real platform implementation, including loading the Windows
	// API. Sleep/hibernate exclusion itself needs an OS suspend smoke test.
	now := newWatchdogClock()
	before := now()
	time.Sleep(20 * time.Millisecond)
	if after := now(); after <= before {
		t.Fatalf("awake clock did not advance: before=%v after=%v", before, after)
	}
}
