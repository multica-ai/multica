package execenv

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
		{name: "minutes past the ceiling are clamped", env: "1m30s", want: openclawCLIMaxTimeout},
		{name: "large bare seconds are clamped", env: "3600", want: openclawCLIMaxTimeout},
		// Past time.Duration's range there is nothing to clamp: parsing fails
		// and the default applies. Worth pinning because the old Atoi path
		// silently produced a negative duration here instead of failing.
		{name: "bare seconds beyond time.Duration fall back", env: "99999999999999999999", want: openclawCLITimeout},
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

// TestOpenclawCLIMaxTimeoutFitsPreparationBudget is the arithmetic the
// override ceiling rests on. Each CLI call gets its own deadline, so the
// budget that matters is worst-case-calls x ceiling, and it has to stay under
// the daemon's 5-minute task preparation deadline with room for the rest of
// Prepare (repo checkout, skills, context files). Otherwise a user who raises
// the timeout turns a specific, non-retryable CLI timeout back into the
// generic — and retryable — prepare timeout, which is the failure mode this
// change set out to remove.
func TestOpenclawCLIMaxTimeoutFitsPreparationBudget(t *testing.T) {
	// Mirrors daemon.defaultTaskPrepareTimeout, which execenv cannot import
	// (the daemon imports this package, not the other way round).
	const taskPrepareBudget = 5 * time.Minute
	// Everything Prepare does besides openclaw config discovery.
	const nonOpenclawPrepareSlack = time.Minute

	worst := openclawMaxCLICallsPerPreparation * openclawCLIMaxTimeout
	if worst+nonOpenclawPrepareSlack > taskPrepareBudget {
		t.Errorf("worst-case openclaw discovery %v (%d calls x %v) leaves less than %v under the %v preparation budget",
			worst, openclawMaxCLICallsPerPreparation, openclawCLIMaxTimeout, nonOpenclawPrepareSlack, taskPrepareBudget)
	}
	if openclawCLITimeout > openclawCLIMaxTimeout {
		t.Errorf("default %v exceeds the override ceiling %v", openclawCLITimeout, openclawCLIMaxTimeout)
	}
}

// TestPrepareOpenclawConfigWorstCaseCLICallCount pins the multiplier the
// budget above depends on. The worst case is not the common two calls: a
// 2026.6+ host falls back to the registry subcommand, and an agent with a
// managed mcp_config additionally reads the full resolved config. Adding a
// fifth call site without re-deriving the ceiling would silently push the
// worst case past the preparation budget, so this fails instead.
func TestPrepareOpenclawConfigWorstCaseCLICallCount(t *testing.T) {
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	userConfigPath := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(userConfigPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write user cfg: %v", err)
	}

	stub := installOpenclawStub(t, map[string]openclawResponse{
		// 1. locate the active config
		"config file": {stdout: userConfigPath},
		// 2. pre-2026.6 schema read, which this host does not have
		"config get agents.list --json": {err: errors.New("Config path not found: agents.list")},
		// 3. registry fallback
		"agents list --json": {stdout: `[{"id":"scout"}]`},
		// 4. full resolved config, reached only via a managed mcp_config
		"config get --json": {stdout: `{"agents":{"list":[{"id":"scout"}]}}`},
	})

	if _, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
		OpenclawBin: stub.bin,
		McpConfig:   []byte(`{"mcpServers":{"fs":{"command":"fs-server"}}}`),
	}); err != nil {
		t.Fatalf("prepareOpenclawConfig: %v", err)
	}

	if got := len(stub.calls); got != openclawMaxCLICallsPerPreparation {
		var args []string
		for _, call := range stub.calls {
			args = append(args, strings.Join(call.args, " "))
		}
		t.Errorf("worst-case preparation made %d CLI calls, but openclawMaxCLICallsPerPreparation is %d\ncalls: %v",
			got, openclawMaxCLICallsPerPreparation, args)
	}
}
