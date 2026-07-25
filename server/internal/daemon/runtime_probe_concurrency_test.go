package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDetectBuiltinRuntimes_ProbesRunConcurrently proves the registration
// version probes fan out instead of running serially (MUL-5119). Each stubbed
// `--version` probe blocks briefly and records the peak number of in-flight
// probes; a serial loop would never exceed 1 and would take N×block, while the
// parallel path overlaps them and finishes in roughly one block.
func TestDetectBuiltinRuntimes_ProbesRunConcurrently(t *testing.T) {
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})

	const probeBlock = 100 * time.Millisecond
	var inFlight, maxInFlight int32
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		time.Sleep(probeBlock)
		atomic.AddInt32(&inFlight, -1)
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{
		"claude":   {Path: "/usr/bin/true"},
		"codex":    {Path: "/usr/bin/true"},
		"cursor":   {Path: "/usr/bin/true"},
		"opencode": {Path: "/usr/bin/true"},
		"hermes":   {Path: "/usr/bin/true"},
		"pi":       {Path: "/usr/bin/true"},
	}

	start := time.Now()
	runtimes := d.detectBuiltinRuntimes(context.Background())
	elapsed := time.Since(start)

	if len(runtimes) != len(d.cfg.Agents) {
		t.Fatalf("expected %d runtimes, got %d", len(d.cfg.Agents), len(runtimes))
	}
	if got := atomic.LoadInt32(&maxInFlight); got < 2 {
		t.Fatalf("probes did not overlap (peak in-flight = %d); registration is still serial", got)
	}
	if serialFloor := time.Duration(len(d.cfg.Agents)) * probeBlock; elapsed >= serialFloor {
		t.Fatalf("detectBuiltinRuntimes took %v (>= serial floor %v); not parallel", elapsed, serialFloor)
	}

	// Output is sorted by provider so the registration payload is deterministic
	// despite random map iteration and nondeterministic completion order.
	for i := 1; i < len(runtimes); i++ {
		if runtimes[i-1]["type"] > runtimes[i]["type"] {
			t.Fatalf("runtimes not sorted by type: %q before %q", runtimes[i-1]["type"], runtimes[i]["type"])
		}
	}
}

// TestDetectBuiltinRuntimes_SkipsFailedProbes confirms a probe that fails
// version detection or the min-version gate is dropped from the payload while
// the healthy ones still register — matching the old serial loop's semantics.
func TestDetectBuiltinRuntimes_SkipsFailedProbes(t *testing.T) {
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})

	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		if path == "/broken" {
			return "", context.DeadlineExceeded
		}
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "tooold" {
			return context.Canceled // stand-in for a below-minimum version
		}
		return nil
	}

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{
		"claude": {Path: "/usr/bin/true"},
		"codex":  {Path: "/usr/bin/true"},
		"broken": {Path: "/broken"},
		"tooold": {Path: "/usr/bin/true"},
	}

	runtimes := d.detectBuiltinRuntimes(context.Background())
	got := map[string]bool{}
	for _, rt := range runtimes {
		got[rt["type"]] = true
	}
	if len(runtimes) != 2 || !got["claude"] || !got["codex"] {
		t.Fatalf("expected only claude+codex to register, got %v", runtimes)
	}
}
