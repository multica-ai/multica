package daemon

import "testing"

// TestModelDiscoveryExecutable verifies model discovery resolves the binary the
// same way task execution does: a custom runtime profile (e.g. a `pi` fork
// registered with a different command_name) is probed through its own resolved
// command, not the family-default Agents[provider] entry — which may expose a
// different catalog or not be installed on this host at all. Built-in runtimes
// keep using the family-default agent path. See GitHub #4482.
func TestModelDiscoveryExecutable(t *testing.T) {
	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{"pi": {Path: "/usr/bin/pi"}}
	d.profileLaunchSpecs = map[string]profileLaunchSpec{
		"prof-omp": {path: "/opt/bin/omp"},
	}
	d.runtimeIndex["rt-omp"] = Runtime{ID: "rt-omp", Provider: "pi", ProfileID: "prof-omp"}
	d.runtimeIndex["rt-builtin"] = Runtime{ID: "rt-builtin", Provider: "pi"}
	d.runtimeIndex["rt-unresolved"] = Runtime{ID: "rt-unresolved", Provider: "pi", ProfileID: "prof-missing"}
	d.runtimeIndex["rt-none"] = Runtime{ID: "rt-none", Provider: "unknown"}

	// Custom profile -> its own resolved command, not the family-default pi.
	if path, ok := d.modelDiscoveryExecutable(d.runtimeIndex["rt-omp"]); !ok || path != "/opt/bin/omp" {
		t.Errorf("custom profile: got (%q, %v), want (/opt/bin/omp, true)", path, ok)
	}
	// Built-in runtime -> family-default agent path.
	if path, ok := d.modelDiscoveryExecutable(d.runtimeIndex["rt-builtin"]); !ok || path != "/usr/bin/pi" {
		t.Errorf("built-in: got (%q, %v), want (/usr/bin/pi, true)", path, ok)
	}
	// Custom profile whose command was never resolved on this host -> fall back
	// to the family-default agent path rather than reporting unresolvable.
	if path, ok := d.modelDiscoveryExecutable(d.runtimeIndex["rt-unresolved"]); !ok || path != "/usr/bin/pi" {
		t.Errorf("unresolved profile: got (%q, %v), want (/usr/bin/pi, true)", path, ok)
	}
	// No profile and no configured agent for the family -> not resolvable, so
	// handleModelList reports the existing "no agent configured" failure.
	if path, ok := d.modelDiscoveryExecutable(d.runtimeIndex["rt-none"]); ok || path != "" {
		t.Errorf("unknown provider: got (%q, %v), want empty false", path, ok)
	}
}
