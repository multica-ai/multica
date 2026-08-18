package execenv

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

// slowestMeasuredOpenclawCall is the worst `openclaw config file` timing
// reported from the field (#7112): 10.86s on a 2020 Intel MacBook Pro, with
// 8.41s and 10.35s on the same host. It is a floor for what the default
// deadline must tolerate, not a target.
const slowestMeasuredOpenclawCall = 10860 * time.Millisecond

// TestOpenclawCLIDefaultTimeoutCoversMeasuredSlowHosts is the regression for
// the reported bug. The old 5s default was written against an assumed <200ms
// CLI; the reporter's host needs twice that budget for a single call, every
// time, and had no way to raise it — so the OpenClaw runtime was unusable
// there. Anything that lowers this default back under the measured range
// re-breaks that host.
func TestOpenclawCLIDefaultTimeoutCoversMeasuredSlowHosts(t *testing.T) {
	if openclawCLITimeout <= slowestMeasuredOpenclawCall {
		t.Errorf("default openclaw CLI timeout %v must exceed the slowest measured call %v (#7112)",
			openclawCLITimeout, slowestMeasuredOpenclawCall)
	}
	// Headroom, not a bare pass: a host at the measured worst case should not
	// sit one scheduling hiccup away from failing again.
	if openclawCLITimeout < 2*slowestMeasuredOpenclawCall {
		t.Errorf("default openclaw CLI timeout %v leaves less than 2x headroom over %v",
			openclawCLITimeout, slowestMeasuredOpenclawCall)
	}
}

func TestResolveOpenclawCLITimeout(t *testing.T) {
	cases := []struct {
		name     string
		explicit time.Duration
		env      string
		want     time.Duration
	}{
		{name: "unset uses the default", want: openclawCLITimeout},
		{name: "explicit wins over env", explicit: 3 * time.Second, env: "90s", want: 3 * time.Second},
		{name: "duration string", env: "45s", want: 45 * time.Second},
		{name: "bare seconds", env: "45", want: 45 * time.Second},
		{name: "minutes", env: "1m30s", want: 90 * time.Second},
		// A typo must degrade to the default rather than disabling the
		// deadline or failing task preparation outright.
		{name: "unparseable falls back", env: "soon", want: openclawCLITimeout},
		{name: "empty falls back", env: "   ", want: openclawCLITimeout},
		{name: "zero falls back", env: "0", want: openclawCLITimeout},
		{name: "negative falls back", env: "-5s", want: openclawCLITimeout},
		// Clamped, not honoured: too small breaks every host, too large eats
		// the outer preparation budget.
		{name: "below the floor is clamped", env: "10ms", want: openclawCLIMinTimeout},
		{name: "above the ceiling is clamped", env: "30m", want: openclawCLIMaxTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(OpenclawCLITimeoutEnv, tc.env)
			if got := resolveOpenclawCLITimeout(tc.explicit, nil); got != tc.want {
				t.Errorf("resolveOpenclawCLITimeout(%v, env=%q) = %v, want %v", tc.explicit, tc.env, got, tc.want)
			}
		})
	}
}

// TestExecOpenclawCLITimeoutIsTypedSentinel is what lets the daemon classify a
// local CLI stall structurally. Before this, the only signal was the words
// "deadline exceeded" in the message, which the shared classifier maps to
// agent_error.provider_network — "check your network" copy plus an auto-retry,
// for a failure that is local and repeats identically.
//
// The pre-existing context contract must survive alongside it: callers that
// check errors.Is(err, context.DeadlineExceeded) keep working.
func TestExecOpenclawCLITimeoutIsTypedSentinel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim shape is covered by the windows-tagged tests")
	}
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary available to build a slow shim: %v", err)
	}
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\n"+sleepBin+" 1\n", "")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = execOpenclawCLI(ctx, shim, "config", "file")
	if err == nil {
		t.Fatal("expected the timed-out invocation to fail")
	}
	if !errors.Is(err, ErrOpenclawCLITimeout) {
		t.Errorf("errors.Is(err, ErrOpenclawCLITimeout) must hold\ngot: %s", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("errors.Is(err, context.DeadlineExceeded) must keep holding\ngot: %s", err)
	}
	if !errors.Is(err, ErrOpenclawCLITimeout) || errors.Is(errors.New("openclaw config file: boom"), ErrOpenclawCLITimeout) {
		t.Errorf("the sentinel must not match unrelated CLI failures")
	}
}

// TestPrepareOpenclawConfigPropagatesTimeoutSentinel proves the sentinel
// survives the wrapping prepareOpenclawConfig does on the way out, which is
// what the daemon actually classifies.
func TestPrepareOpenclawConfigPropagatesTimeoutSentinel(t *testing.T) {
	stub := installOpenclawStub(t, map[string]openclawResponse{
		"config file": {err: &openclawCLITimeoutError{
			msg:   "openclaw config file: context deadline exceeded (process: signal: killed)",
			cause: context.DeadlineExceeded,
		}},
	})

	envRoot := t.TempDir()
	_, err := prepareOpenclawConfig(envRoot, envRoot, OpenclawConfigPrep{OpenclawBin: stub.bin})
	if err == nil {
		t.Fatal("expected preparation to fail on a CLI timeout")
	}
	if !errors.Is(err, ErrOpenclawCLITimeout) {
		t.Errorf("wrapped preparation error must keep the sentinel\ngot: %s", err)
	}
}

// TestExecOpenclawCLICancellationIsNotATimeout keeps the sentinel honest. A
// daemon shutdown or a cancelled task also lands in the cancellation branch,
// but it is not a slow CLI — labelling it as one would tell the user to raise
// a deadline that was never involved.
func TestExecOpenclawCLICancellationIsNotATimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell shim shape is covered by the windows-tagged tests")
	}
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary available to build a slow shim: %v", err)
	}
	shim := writeShim(t, t.TempDir(), "#!/bin/sh\n"+sleepBin+" 5\n", "")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	defer cancel()
	_, err = execOpenclawCLI(ctx, shim, "config", "file")
	if err == nil {
		t.Fatal("expected the cancelled invocation to fail")
	}
	if errors.Is(err, ErrOpenclawCLITimeout) {
		t.Errorf("cancellation must not be reported as a CLI timeout\ngot: %s", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation must keep the wrapped context cause\ngot: %s", err)
	}
}
