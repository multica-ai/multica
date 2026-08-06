package wecom

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func newManualClock(t time.Time) *manualClock {
	return &manualClock{now: t}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestRetryAuthStreakProgression(t *testing.T) {
	r := NewRetryState()
	got := make([]time.Duration, 0, 5)
	for i := 0; i < 5; i++ {
		got = append(got, r.NoteAuthFail())
	}
	want := []time.Duration{
		5 * time.Minute,
		15 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
		1 * time.Hour, // tail repeats
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("auth streak = %v, want %v", got, want)
	}
}

func TestRetryAuthResetOnSuccess(t *testing.T) {
	r := NewRetryState()
	_ = r.NoteAuthFail()
	_ = r.NoteAuthFail()
	if r.AuthStreak() == 0 {
		t.Fatalf("expected non-zero streak after failures")
	}
	r.NoteAuthSuccess()
	if r.AuthStreak() != 0 {
		t.Fatalf("expected streak reset after success")
	}
	if d := r.NoteAuthFail(); d != 5*time.Minute {
		t.Fatalf("after success, next NoteAuthFail = %v, want 5m", d)
	}
}

func TestRetryKickStreakProgression(t *testing.T) {
	clk := newManualClock(time.Unix(1000, 0))
	r := NewRetryStateWithClock(clk.Now)
	// Simulate: subscribe success, then instant kicks (no stable window).
	r.NoteAuthSuccess()
	got := make([]time.Duration, 0, 6)
	for i := 0; i < 6; i++ {
		got = append(got, r.NoteKick())
	}
	want := []time.Duration{
		60 * time.Second,
		2 * time.Minute,
		4 * time.Minute,
		8 * time.Minute,
		15 * time.Minute,
		15 * time.Minute, // tail repeats
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kick streak = %v, want %v", got, want)
	}
}

func TestRetryKickResetsAfterStableWindow(t *testing.T) {
	clk := newManualClock(time.Unix(1000, 0))
	r := NewRetryStateWithClock(clk.Now)

	// First connection: subscribe OK, instant kick.
	r.NoteAuthSuccess()
	if d := r.NoteKick(); d != 60*time.Second {
		t.Fatalf("first kick delay = %v, want 60s", d)
	}
	if d := r.NoteKick(); d != 2*time.Minute {
		t.Fatalf("second kick delay = %v, want 2m", d)
	}

	// Reconnect: subscribe OK, stays up 6 minutes, then kicked.
	r.NoteAuthSuccess()
	clk.advance(6 * time.Minute)
	if d := r.NoteKick(); d != 60*time.Second {
		t.Fatalf("kick after stable window = %v, want reset to 60s", d)
	}
	// Next kick without stable window advances again.
	r.NoteAuthSuccess()
	clk.advance(30 * time.Second)
	if d := r.NoteKick(); d != 2*time.Minute {
		t.Fatalf("kick before stable window = %v, want 2m", d)
	}
}

func TestRetryKickBeforeAnySubscribe(t *testing.T) {
	r := NewRetryState()
	// A kick before we ever subscribed still uses the base delay
	// (connectedAt is zero → skip stable-reset branch).
	if d := r.NoteKick(); d != 60*time.Second {
		t.Fatalf("cold kick delay = %v, want 60s", d)
	}
}

func TestRetryConcurrentSafe(t *testing.T) {
	r := NewRetryState()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.NoteAuthFail()
				_ = r.NoteKick()
				r.NoteAuthSuccess()
			}
		}()
	}
	wg.Wait()
	// Just ensure no data race and state is coherent.
	if r.AuthStreak() < 0 || r.KickStreak() < 0 {
		t.Fatalf("negative streak: auth=%d kick=%d", r.AuthStreak(), r.KickStreak())
	}
}

func TestSleepCtxReturnsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	err := SleepCtx(ctx, 10*time.Second)
	if err == nil {
		t.Fatalf("expected ctx.Err after cancel")
	}
	if time.Since(start) > time.Second {
		t.Fatalf("SleepCtx did not honor cancel promptly")
	}
}

func TestSleepCtxSleepsForDuration(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	if err := SleepCtx(ctx, 30*time.Millisecond); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("returned too early: %v", elapsed)
	}
}
