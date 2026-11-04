package service

import (
	"context"
	"testing"
	"time"
)

func TestPulseCacheReusesAWorkspacePulseForAMinute(t *testing.T) {
	clock := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	cache := NewPulseCache(time.Minute)
	cache.now = func() time.Time { return clock }

	builds := 0
	build := func(context.Context) (Pulse, error) {
		builds++
		return Pulse{DecisionsWaiting: builds}, nil
	}

	first, _ := cache.Get(context.Background(), "ws-1", build)
	clock = clock.Add(59 * time.Second)
	second, _ := cache.Get(context.Background(), "ws-1", build)
	if builds != 1 || second.DecisionsWaiting != first.DecisionsWaiting {
		t.Fatalf("within a minute the cached pulse is reused: builds=%d", builds)
	}

	clock = clock.Add(2 * time.Second)
	third, _ := cache.Get(context.Background(), "ws-1", build)
	if builds != 2 || third.DecisionsWaiting != 2 {
		t.Fatalf("after a minute the pulse is built again: builds=%d", builds)
	}

	cache.Get(context.Background(), "ws-2", build)
	if builds != 3 {
		t.Fatalf("another workspace has its own entry: builds=%d", builds)
	}
}
