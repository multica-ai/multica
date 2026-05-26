package service

import (
	"testing"
	"time"
)

func TestNormalizePollingIntervalMinutes(t *testing.T) {
	tests := []struct {
		input int32
		want  int32
	}{
		{30, 30},
		{5, 5},
		{60, 60},
		{0, DefaultPollingIntervalMinutes},
		{-1, DefaultPollingIntervalMinutes},
	}
	for _, tt := range tests {
		if got := normalizePollingIntervalMinutes(tt.input); got != tt.want {
			t.Errorf("normalizePollingIntervalMinutes(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestNormalizePollingStartAt(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	t.Run("zero start defaults to now", func(t *testing.T) {
		got := normalizePollingStartAt(time.Time{}, now)
		if !got.Equal(now.Truncate(time.Second)) {
			t.Errorf("got %v, want %v", got, now.Truncate(time.Second))
		}
	})

	t.Run("non-zero start is truncated to seconds", func(t *testing.T) {
		start := time.Date(2026, 5, 26, 12, 30, 45, 500_000_000, time.UTC)
		got := normalizePollingStartAt(start, now)
		want := start.Truncate(time.Second)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

func TestInitialPollingNextRun(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	t.Run("start in future returns start", func(t *testing.T) {
		start := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
		got := InitialPollingNextRun(start, 30, now)
		if !got.Equal(start) {
			t.Errorf("got %v, want %v", got, start)
		}
	})

	t.Run("zero start returns now", func(t *testing.T) {
		got := InitialPollingNextRun(time.Time{}, 30, now)
		if !got.Equal(now) {
			t.Errorf("got %v, want %v", got, now)
		}
	})

	t.Run("start in past advances to next future slot", func(t *testing.T) {
		start := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)
		got := InitialPollingNextRun(start, 60, now)
		// elapsed=2h, steps=3, next = 8:00 + 3h = 11:00
		want := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("start exactly at now returns start", func(t *testing.T) {
		got := InitialPollingNextRun(now, 30, now)
		if !got.Equal(now) {
			t.Errorf("got %v, want %v", got, now)
		}
	})
}

func TestAdvancePollingNextRun(t *testing.T) {
	now := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)

	t.Run("simple advance", func(t *testing.T) {
		previous := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
		got := AdvancePollingNextRun(previous, 30, now)
		want := time.Date(2026, 5, 26, 10, 30, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("catch up when lagging behind", func(t *testing.T) {
		previous := time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC)
		got := AdvancePollingNextRun(previous, 30, now)
		want := time.Date(2026, 5, 26, 10, 30, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("1 hour interval", func(t *testing.T) {
		previous := time.Date(2026, 5, 26, 10, 0, 0, 0, time.UTC)
		got := AdvancePollingNextRun(previous, 60, now)
		want := time.Date(2026, 5, 26, 11, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}
