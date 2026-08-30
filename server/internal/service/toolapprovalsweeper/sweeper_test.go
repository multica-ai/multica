package toolapprovalsweeper

import (
	"context"
	"testing"
	"time"
)

type recordingStore struct {
	expiredAt time.Time
	cutoff    time.Time
	batchSize int32
}

func (s *recordingStore) ExpireDue(_ context.Context, asOf time.Time, batchSize int32) (int, error) {
	s.expiredAt = asOf
	s.batchSize = batchSize
	return 2, nil
}

func (s *recordingStore) DeleteRetained(_ context.Context, cutoff time.Time, batchSize int32) (RetentionResult, error) {
	s.cutoff = cutoff
	s.batchSize = batchSize
	return RetentionResult{ApprovalsDeleted: 3, ActionEventsDeleted: 4}, nil
}

func TestRunOnceUsesNinetyDayRetentionDefault(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC)
	store := &recordingStore{}
	sweeper := New(store, Config{BatchSize: 25})
	sweeper.now = func() time.Time { return now }

	result, err := sweeper.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Expired != 2 || result.ApprovalsDeleted != 3 || result.ActionEventsDeleted != 4 {
		t.Fatalf("result = %#v", result)
	}
	if !store.expiredAt.Equal(now) {
		t.Fatalf("expiry as_of = %s, want %s", store.expiredAt, now)
	}
	if !store.cutoff.Equal(now.AddDate(0, 0, -90)) {
		t.Fatalf("retention cutoff = %s, want %s", store.cutoff, now.AddDate(0, 0, -90))
	}
	if store.batchSize != 25 {
		t.Fatalf("batch size = %d, want 25", store.batchSize)
	}
}

func TestRunOnceHonorsConfiguredRetentionDays(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC)
	store := &recordingStore{}
	sweeper := New(store, Config{RetentionDays: 30, BatchSize: 10})
	sweeper.now = func() time.Time { return now }

	if _, err := sweeper.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !store.cutoff.Equal(now.AddDate(0, 0, -30)) {
		t.Fatalf("retention cutoff = %s, want %s", store.cutoff, now.AddDate(0, 0, -30))
	}
}
