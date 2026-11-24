package daemon

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/awakeclock"
)

// hostClock simulates a host that can suspend. Bubble time stands in for
// wall time. awake() is what awakeclock reports: it freezes while the host
// sleeps and then resumes without the slept interval. wall() is the
// pre-#9024 reading, used as a control to prove that each scenario really
// lands on the bug.
type hostClock struct {
	mu     sync.Mutex
	slept  time.Duration
	asleep bool
	frozen time.Duration
}

func (c *hostClock) awake() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.asleep {
		return c.frozen
	}
	return awakeclock.Now() - c.slept
}

func (c *hostClock) wall() time.Duration { return awakeclock.Now() }

// hostSleep suspends the host for d. Ticks that fire meanwhile model Windows,
// where the overdue tick runs as soon as the host wakes. They see the frozen
// awake reading.
func (c *hostClock) hostSleep(d time.Duration) {
	c.mu.Lock()
	c.frozen = awakeclock.Now() - c.slept
	c.asleep = true
	c.mu.Unlock()
	time.Sleep(d)
	c.mu.Lock()
	c.slept += d
	c.asleep = false
	c.mu.Unlock()
}

type suspendScenario struct {
	name   string
	window time.Duration
	tools  int32
	// run drives the scenario and returns at the checkpoint where awake
	// silence is still under window. next returns once awake silence has
	// passed window plus one tick.
	run, next func(c *hostClock, stamp func())
}

// GitHub #9024 and the PR review: a sleep must never count as silence, both
// when it falls inside one tick interval and when it begins right after a
// tick on a host whose overdue tick fires as soon as it wakes. A run that
// stays silent for a full window of awake time must still be force-stopped.
func TestIdleWatchdogDoesNotCountHostSuspendAsSilence(t *testing.T) {
	scenarios := []suspendScenario{
		{
			// 10m window (the OpenCode budget) and 5m ticks. Silent for 3m at
			// the 5m tick, then a 4m sleep that stays inside one tick
			// interval. The tick-gap heuristic credited none of it, so 9m of
			// awake silence read as 13m.
			name:   "sleep inside one tick interval",
			window: 10 * time.Minute,
			run: func(c *hostClock, stamp func()) {
				time.Sleep(2 * time.Minute)
				stamp()
				time.Sleep(3*time.Minute + time.Second)
				c.hostSleep(4 * time.Minute)
				time.Sleep(6 * time.Minute) // awake silence: 9m1s
			},
			next: func(*hostClock, func()) { time.Sleep(6 * time.Minute) }, // 15m1s
		},
		{
			// 2h window. Silent for 1h57m, then a 2h5m sleep straight after
			// a tick, with the overdue tick firing on wake. The heuristic
			// credited only gap−tick (2h), so 1h58m read as 2h3m.
			name:   "long sleep right after a tick, overdue tick on wake",
			window: 2 * time.Hour,
			tools:  1,
			run: func(c *hostClock, stamp func()) {
				time.Sleep(time.Hour + 57*time.Minute)
				c.hostSleep(2*time.Hour + 5*time.Minute)
				time.Sleep(time.Minute) // awake silence: 1h58m
			},
			next: func(*hostClock, func()) { time.Sleep(7 * time.Minute) }, // 2h5m
		},
		{
			name:   "sleep far longer than the window",
			window: time.Hour,
			run: func(c *hostClock, _ func()) {
				time.Sleep(10 * time.Minute)
				c.hostSleep(3 * time.Hour)
				time.Sleep(21 * time.Minute) // awake silence: 31m
			},
			next: func(*hostClock, func()) { time.Sleep(35 * time.Minute) }, // 66m
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			t.Run("awake clock", func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := &hostClock{}
					fired, threshold := runSuspendScenario(t, sc, c, c.awake)
					sc.run(c, fired.stamp)
					synctest.Wait()
					if fired.Load() {
						t.Fatal("host suspend was counted as agent silence and force-stopped the run")
					}
					sc.next(c, fired.stamp)
					synctest.Wait()
					if !fired.Load() {
						t.Fatal("watchdog no longer stops a run silent for a full awake window")
					}
					if got := time.Duration(threshold.Load()); got != sc.window {
						t.Fatalf("threshold = %s, want %s", got, sc.window)
					}
				})
			})
			// Control: the same timeline on the wall clock fires early. If it
			// did not, the scenario would not exercise the defect.
			t.Run("wall clock control", func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := &hostClock{}
					fired, _ := runSuspendScenario(t, sc, c, c.wall)
					sc.run(c, fired.stamp)
					synctest.Wait()
					if !fired.Load() {
						t.Fatal("scenario no longer reproduces the wall-clock false positive")
					}
				})
			})
		})
	}
}

type firedProbe struct {
	atomic.Bool
	stamp func()
}

func runSuspendScenario(t *testing.T, sc suspendScenario, c *hostClock, now func() time.Duration) (*firedProbe, *atomic.Int64) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var last, threshold atomic.Int64
	probe := &firedProbe{}
	last.Store(int64(now()))
	probe.stamp = func() { last.Store(int64(now())) }
	// Mirrors the Cursor native-tool callback, which republishes its own
	// pre-sleep stamp on every read. The CAS only moves the stamp forward.
	staleToolStamp := last.Load()
	tools := func() int32 {
		for {
			previous := last.Load()
			if staleToolStamp <= previous || last.CompareAndSwap(previous, staleToolStamp) {
				break
			}
		}
		return sc.tools
	}
	go new(Daemon).runIdleWatchdogWithClock(ctx, sc.window, sc.window, &last, tools, &probe.Bool, &threshold,
		cancel, make(chan agent.Message), nil, nil, slog.Default(), now)
	synctest.Wait()
	return probe, &threshold
}
