package daemon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestExtractMulticaVersion(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "root version output",
			raw:  "multica v0.3.14 (commit: abc, built: 2026-06-13)\ngo: go1.26.1, os/arch: darwin/arm64\n",
			want: "v0.3.14",
		},
		{
			name: "falls back to first non-empty line",
			raw:  "\ncustom-build 123\nmore\n",
			want: "custom-build 123",
		},
		{
			name: "empty",
			raw:  " \n",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractMulticaVersion(tc.raw); got != tc.want {
				t.Fatalf("extractMulticaVersion(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestVersionReloadReasonDetectsFirstChangedTarget(t *testing.T) {
	prev := map[string]versionProbeResult{
		"agent:codex": {Version: "codex 0.1.0"},
		"multica":     {Version: "v0.3.0"},
	}
	cur := map[string]versionProbeResult{
		"agent:codex": {Version: "codex 0.1.1"},
		"multica":     {Version: "v0.3.0"},
	}

	got := versionReloadReason(prev, cur)
	if !strings.Contains(got, "codex version changed: codex 0.1.0 -> codex 0.1.1") {
		t.Fatalf("reason = %q", got)
	}
}

func TestVersionReloadReasonTreatsErrorStateAsStable(t *testing.T) {
	prev := map[string]versionProbeResult{
		"agent:codex": {Failed: true, Err: "timeout"},
	}
	cur := map[string]versionProbeResult{
		"agent:codex": {Failed: true, Err: "permission denied"},
	}

	if got := versionReloadReason(prev, cur); got != "" {
		t.Fatalf("same failed state should not trigger reload, got %q", got)
	}
}

func TestScheduleReloadPendingWaitsForDrain(t *testing.T) {
	var restarts atomic.Int32
	d := &Daemon{
		logger: discardLogger(),
		cancelFunc: func() {
			restarts.Add(1)
		},
	}
	d.activeTasks.Store(1)
	d.claimsInFlight = 1

	d.scheduleReloadPending("codex version changed: old -> new")

	if restarts.Load() != 0 {
		t.Fatalf("restart fired before active task drained")
	}
	pending, reason := d.reloadPendingState()
	if !pending || reason == "" {
		t.Fatalf("reload pending state = (%v, %q), want pending with reason", pending, reason)
	}
	if !d.pauseClaims {
		t.Fatalf("pauseClaims must be set while reload is pending")
	}

	d.activeTasks.Store(0)
	d.exitClaim()

	if restarts.Load() != 1 {
		t.Fatalf("restart calls = %d, want 1 after drain", restarts.Load())
	}
	if got := d.RestartBinary(); got == "" {
		t.Fatalf("restart binary should be scheduled after drain")
	}
}

func TestCheckReloadOnVersionChangeSchedulesRestart(t *testing.T) {
	origDetectAgent := detectAgentVersion
	origDetectMultica := detectMulticaVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetectAgent
		detectMulticaVersion = origDetectMultica
	})

	agentVersion := "codex 0.1.0"
	detectAgentVersion = func(context.Context, string) (string, error) {
		return agentVersion, nil
	}
	detectMulticaVersion = func(context.Context, string) (string, error) {
		return "v0.3.0", nil
	}

	var restarts atomic.Int32
	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			HeartbeatInterval:         time.Millisecond,
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger:        discardLogger(),
		agentVersions: map[string]string{"codex": "codex 0.1.0"},
		cancelFunc: func() {
			restarts.Add(1)
		},
	}
	d.initVersionReloadBaseline()

	agentVersion = "codex 0.1.1"
	d.checkReloadOnVersionChange(context.Background())

	if restarts.Load() != 1 {
		t.Fatalf("restart calls = %d, want 1", restarts.Load())
	}
	pending, reason := d.reloadPendingState()
	if !pending {
		t.Fatalf("reload pending should be set")
	}
	if !strings.Contains(reason, "codex version changed") {
		t.Fatalf("reload reason = %q, want codex change", reason)
	}
}

func TestCheckReloadOnVersionChangeSkipsWhileUpdating(t *testing.T) {
	origDetectAgent := detectAgentVersion
	origDetectMultica := detectMulticaVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetectAgent
		detectMulticaVersion = origDetectMultica
	})

	agentVersion := "codex 0.1.0"
	detectAgentVersion = func(context.Context, string) (string, error) {
		return agentVersion, nil
	}
	detectMulticaVersion = func(context.Context, string) (string, error) {
		return "v0.3.0", nil
	}

	var restarts atomic.Int32
	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			HeartbeatInterval:         time.Millisecond,
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger:        discardLogger(),
		agentVersions: map[string]string{"codex": "codex 0.1.0"},
		cancelFunc: func() {
			restarts.Add(1)
		},
	}
	d.initVersionReloadBaseline()
	d.updating.Store(true)

	agentVersion = "codex 0.1.1"
	d.checkReloadOnVersionChange(context.Background())

	if restarts.Load() != 0 {
		t.Fatalf("restart fired while update was in progress")
	}
	if pending, _ := d.reloadPendingState(); pending {
		t.Fatalf("reload pending should not be set while update is in progress")
	}
}

func TestCurrentVersionReloadProbeStopsWhenSweepContextExpires(t *testing.T) {
	origDetectAgent := detectAgentVersion
	origDetectMultica := detectMulticaVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetectAgent
		detectMulticaVersion = origDetectMultica
	})

	detectMulticaVersion = func(context.Context, string) (string, error) {
		return "v0.3.0", nil
	}
	detectAgentVersion = func(ctx context.Context, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}

	d := &Daemon{
		cfg: Config{
			CLIVersion: "v0.3.0",
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger: discardLogger(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, complete := d.currentVersionReloadProbe(ctx)

	if complete {
		t.Fatalf("probe should report incomplete when the sweep context expires")
	}
}

func TestInitVersionReloadBaselineDoesNotProbeMissingAgentVersion(t *testing.T) {
	origDetectAgent := detectAgentVersion
	t.Cleanup(func() { detectAgentVersion = origDetectAgent })
	detectAgentVersion = func(context.Context, string) (string, error) {
		t.Fatalf("initVersionReloadBaseline must not shell out for missing agent versions")
		return "", nil
	}

	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger:        discardLogger(),
		agentVersions: map[string]string{},
	}

	d.initVersionReloadBaseline()

	d.versionReloadMu.Lock()
	defer d.versionReloadMu.Unlock()
	if d.versionReloadBaselineReady {
		t.Fatalf("baseline should not be ready until a background probe fills missing agent versions")
	}
	if _, ok := d.versionReloadBaseline["multica"]; !ok {
		t.Fatalf("baseline should still include the multica CLI version")
	}
	if _, ok := d.versionReloadBaseline["agent:codex"]; ok {
		t.Fatalf("baseline should not fabricate an agent version before background probe")
	}
}

func TestFirstReloadCheckCompletesIncompleteBaselineWithoutRestart(t *testing.T) {
	origDetectAgent := detectAgentVersion
	origDetectMultica := detectMulticaVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetectAgent
		detectMulticaVersion = origDetectMultica
	})

	detectMulticaVersion = func(context.Context, string) (string, error) {
		return "v0.3.0", nil
	}
	detectAgentVersion = func(context.Context, string) (string, error) {
		return "codex 0.1.1", nil
	}

	var restarts atomic.Int32
	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger:        discardLogger(),
		agentVersions: map[string]string{},
		cancelFunc: func() {
			restarts.Add(1)
		},
	}
	d.initVersionReloadBaseline()

	d.checkReloadOnVersionChange(context.Background())

	if restarts.Load() != 0 {
		t.Fatalf("first complete probe should establish baseline, not restart")
	}
	d.versionReloadMu.Lock()
	defer d.versionReloadMu.Unlock()
	if !d.versionReloadBaselineReady {
		t.Fatalf("baseline should be ready after first complete probe")
	}
	if got := d.versionReloadBaseline["agent:codex"].Version; got != "codex 0.1.1" {
		t.Fatalf("agent baseline = %q, want codex 0.1.1", got)
	}
}

func TestMaybeTriggerVersionReloadCheckDoesNotBlockHeartbeat(t *testing.T) {
	origDetectAgent := detectAgentVersion
	origDetectMultica := detectMulticaVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetectAgent
		detectMulticaVersion = origDetectMultica
	})

	detectMulticaVersion = func(context.Context, string) (string, error) {
		return "v0.3.0", nil
	}
	detectAgentStarted := make(chan struct{})
	detectAgentVersion = func(ctx context.Context, _ string) (string, error) {
		close(detectAgentStarted)
		<-ctx.Done()
		return "", ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			Agents: map[string]AgentEntry{
				"codex": {Path: "codex"},
			},
		},
		logger:        discardLogger(),
		agentVersions: map[string]string{"codex": "codex 0.1.0"},
	}
	d.initVersionReloadBaseline()
	d.versionReloadMu.Lock()
	d.versionReloadHeartbeatCount = autoReloadHeartbeatTicks - 1
	d.versionReloadMu.Unlock()

	startedAt := time.Now()
	d.maybeTriggerVersionReloadCheck(ctx)
	if elapsed := time.Since(startedAt); elapsed > 50*time.Millisecond {
		t.Fatalf("heartbeat trigger blocked for %s", elapsed)
	}

	select {
	case <-detectAgentStarted:
	case <-time.After(time.Second):
		t.Fatalf("background version probe did not start")
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		d.versionReloadMu.Lock()
		inflight := d.versionReloadCheckInProgress
		d.versionReloadMu.Unlock()
		if !inflight {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("background version probe did not exit after context cancellation")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestInitVersionReloadBaselineSkipsDesktopManagedDaemon(t *testing.T) {
	d := &Daemon{
		cfg: Config{
			CLIVersion:                "v0.3.0",
			AutoReloadOnVersionChange: true,
			LaunchedBy:                "desktop",
		},
		logger: discardLogger(),
	}

	d.initVersionReloadBaseline()

	d.versionReloadMu.Lock()
	defer d.versionReloadMu.Unlock()
	if d.versionReloadBaseline != nil {
		t.Fatalf("desktop-managed daemon should not initialize version reload baseline")
	}
}
