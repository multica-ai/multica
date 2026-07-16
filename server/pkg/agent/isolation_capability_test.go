package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTaskExecutionCapabilityProbeRejectsUnsupportedOS(t *testing.T) {
	called := false
	err := probeTaskExecutionCapability(context.Background(), taskExecutionProbe{
		goos:    "darwin",
		helper:  linuxTaskIsolationHelper,
		timeout: time.Second,
		validateHelper: func(string) error {
			called = true
			return nil
		},
		smoke: func(context.Context, string) error {
			called = true
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported on darwin") {
		t.Fatalf("probe error = %v, want unsupported OS", err)
	}
	if called {
		t.Fatal("unsupported OS invoked Linux probe dependencies")
	}
}

func TestTaskExecutionCapabilityProbeRejectsInvalidHelper(t *testing.T) {
	want := errors.New("untrusted helper")
	smokeCalled := false
	err := probeTaskExecutionCapability(context.Background(), taskExecutionProbe{
		goos:    "linux",
		helper:  linuxTaskIsolationHelper,
		timeout: time.Second,
		validateHelper: func(helper string) error {
			if helper != linuxTaskIsolationHelper {
				t.Fatalf("helper = %q", helper)
			}
			return want
		},
		smoke: func(context.Context, string) error {
			smokeCalled = true
			return nil
		},
	})
	if !errors.Is(err, want) {
		t.Fatalf("probe error = %v, want %v", err, want)
	}
	if smokeCalled {
		t.Fatal("smoke ran after helper validation failed")
	}
}

func TestTaskExecutionCapabilityProbeRequiresSuccessfulSmoke(t *testing.T) {
	want := errors.New("fd mounts unavailable")
	err := probeTaskExecutionCapability(context.Background(), taskExecutionProbe{
		goos:           "linux",
		helper:         linuxTaskIsolationHelper,
		timeout:        time.Second,
		validateHelper: func(string) error { return nil },
		smoke:          func(context.Context, string) error { return want },
	})
	if !errors.Is(err, want) {
		t.Fatalf("probe error = %v, want %v", err, want)
	}
}

func TestTaskExecutionCapabilityProbeSucceedsOnlyAfterValidationAndSmoke(t *testing.T) {
	var calls []string
	err := probeTaskExecutionCapability(context.Background(), taskExecutionProbe{
		goos:    "linux",
		helper:  linuxTaskIsolationHelper,
		timeout: time.Second,
		validateHelper: func(string) error {
			calls = append(calls, "validate")
			return nil
		},
		smoke: func(ctx context.Context, helper string) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("smoke context has no deadline")
			}
			if helper != linuxTaskIsolationHelper {
				t.Fatalf("helper = %q", helper)
			}
			calls = append(calls, "smoke")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if got := strings.Join(calls, ","); got != "validate,smoke" {
		t.Fatalf("calls = %q, want validate,smoke", got)
	}
}

func TestTaskExecutionCapabilityProbeFailsOnTimeout(t *testing.T) {
	err := probeTaskExecutionCapability(context.Background(), taskExecutionProbe{
		goos:           "linux",
		helper:         linuxTaskIsolationHelper,
		timeout:        time.Millisecond,
		validateHelper: func(string) error { return nil },
		smoke: func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probe error = %v, want deadline exceeded", err)
	}
}
