package daemon

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCleanupTaskTempDirWithRetriesTransientFailure(t *testing.T) {
	wantErr := errors.New("file is in use")
	removeCalls := 0
	var sleeps []time.Duration

	attempts, err := cleanupTaskTempDirWith(
		"task-temp",
		func(string) error {
			removeCalls++
			if removeCalls < 3 {
				return wantErr
			}
			return nil
		},
		func(delay time.Duration) { sleeps = append(sleeps, delay) },
		[]time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond},
	)

	if err != nil {
		t.Fatalf("cleanupTaskTempDirWith(): %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if !reflect.DeepEqual(sleeps, []time.Duration{time.Millisecond, 2 * time.Millisecond}) {
		t.Fatalf("sleeps = %v, want [1ms 2ms]", sleeps)
	}
}

func TestCleanupTaskTempDirWithRetriesExhausted(t *testing.T) {
	wantErr := errors.New("file is still in use")
	removeCalls := 0

	attempts, err := cleanupTaskTempDirWith(
		"task-temp",
		func(string) error {
			removeCalls++
			return wantErr
		},
		func(time.Duration) {},
		[]time.Duration{time.Millisecond, 2 * time.Millisecond},
	)

	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
	if !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("error = %q, want attempt count", err)
	}
}
