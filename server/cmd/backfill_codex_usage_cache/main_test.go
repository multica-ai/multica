package main

import (
	"strings"
	"testing"
	"time"
)

func TestCorrectedInputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int64
		cache int64
		want  int64
	}{
		{name: "subtracts cached input", input: 1000, cache: 300, want: 700},
		{name: "zero cache leaves input unchanged", input: 1000, cache: 0, want: 1000},
		{name: "clamps negative values", input: 100, cache: 300, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := correctedInputTokens(tt.input, tt.cache); got != tt.want {
				t.Fatalf("correctedInputTokens(%d, %d) = %d, want %d", tt.input, tt.cache, got, tt.want)
			}
		})
	}
}

func TestConfigParseAndValidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 3, 30, 0, 0, time.UTC)
	t.Run("requires cutoff", func(t *testing.T) {
		t.Parallel()
		cfg := config{batchSize: 100}
		if err := cfg.parseAndValidate(now); err == nil || !strings.Contains(err.Error(), "--cutoff is required") {
			t.Fatalf("expected missing cutoff error, got %v", err)
		}
	})
	t.Run("rejects future cutoff", func(t *testing.T) {
		t.Parallel()
		cfg := config{cutoffRaw: now.Add(time.Minute).Format(time.RFC3339), batchSize: 100}
		if err := cfg.parseAndValidate(now); err == nil || !strings.Contains(err.Error(), "before now") {
			t.Fatalf("expected future cutoff error, got %v", err)
		}
	})
	t.Run("rejects invalid batch size", func(t *testing.T) {
		t.Parallel()
		cfg := config{cutoffRaw: now.Add(-time.Minute).Format(time.RFC3339), batchSize: 0}
		if err := cfg.parseAndValidate(now); err == nil || !strings.Contains(err.Error(), "batch-size") {
			t.Fatalf("expected batch-size error, got %v", err)
		}
	})
	t.Run("normalizes cutoff to UTC", func(t *testing.T) {
		t.Parallel()
		cfg := config{cutoffRaw: "2026-06-18T10:00:00+08:00", batchSize: 100}
		if err := cfg.parseAndValidate(now); err != nil {
			t.Fatalf("parseAndValidate: %v", err)
		}
		want := time.Date(2026, 6, 18, 2, 0, 0, 0, time.UTC)
		if !cfg.cutoff.Equal(want) {
			t.Fatalf("cutoff = %s, want %s", cfg.cutoff, want)
		}
	})
}

func TestRollupWindowPadsDatabaseClockRange(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 6, 18, 3, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Second)
	from, to := rollupWindow(started, finished)
	if want := started.Add(-time.Second); !from.Equal(want) {
		t.Fatalf("from = %s, want %s", from, want)
	}
	if want := finished.Add(time.Second); !to.Equal(want) {
		t.Fatalf("to = %s, want %s", to, want)
	}
}
