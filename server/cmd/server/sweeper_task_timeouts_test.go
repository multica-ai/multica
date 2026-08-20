package main

import (
	"testing"
	"time"
)

func TestTaskSweeperTimeoutsFromEnv(t *testing.T) {
	const (
		dispatchEnv = "MULTICA_TASK_DISPATCH_TIMEOUT"
		runningEnv  = "MULTICA_TASK_RUNNING_TIMEOUT"
		queuedEnv   = "MULTICA_TASK_QUEUED_TTL"
	)

	tests := []struct {
		name   string
		env    map[string]string
		unset  []string
		expect sweeperTaskTimeouts
	}{
		{
			name:   "unset values keep the built-in defaults",
			unset:  []string{dispatchEnv, runningEnv, queuedEnv},
			expect: defaultSweeperTaskTimeouts(),
		},
		{
			name: "positive durations override the defaults",
			env: map[string]string{
				dispatchEnv: "10m",
				runningEnv:  "8h",
				queuedEnv:   "24h",
			},
			expect: sweeperTaskTimeouts{
				DispatchTimeout: 10 * time.Minute,
				RunningTimeout:  8 * time.Hour,
				QueuedTTL:       24 * time.Hour,
			},
		},
		{
			name: "one override leaves the other two at their defaults",
			env: map[string]string{
				queuedEnv: "12h",
			},
			expect: sweeperTaskTimeouts{
				DispatchTimeout: defaultTaskDispatchTimeout,
				RunningTimeout:  defaultTaskRunningTimeout,
				QueuedTTL:       12 * time.Hour,
			},
		},
		{
			name: "invalid or non-positive values fall back to the defaults",
			env: map[string]string{
				dispatchEnv: "not-a-duration",
				runningEnv:  "-5m",
				queuedEnv:   "0s",
			},
			expect: defaultSweeperTaskTimeouts(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range tc.unset {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			got := taskSweeperTimeoutsFromEnv()
			if got != tc.expect {
				t.Errorf("taskSweeperTimeoutsFromEnv() = %+v, want %+v", got, tc.expect)
			}
		})
	}
}

func TestDefaultSweeperTaskTimeoutsMatchDocumentedWindow(t *testing.T) {
	defaults := defaultSweeperTaskTimeouts()
	if defaults.DispatchTimeout != 300*time.Second {
		t.Errorf("default dispatch timeout = %s, want 300s", defaults.DispatchTimeout)
	}
	if defaults.RunningTimeout != 9000*time.Second {
		t.Errorf("default running timeout = %s, want 9000s", defaults.RunningTimeout)
	}
	if defaults.QueuedTTL != 2*time.Hour {
		t.Errorf("default queued TTL = %s, want 2h", defaults.QueuedTTL)
	}
}
