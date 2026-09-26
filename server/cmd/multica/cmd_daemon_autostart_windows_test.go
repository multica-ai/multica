//go:build windows

package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// TestWindowsAutostartRegistryRoundTrip exercises the real HKCU Run
// mechanism against a throwaway key. It calls the platform functions
// directly (not the writeAutostart seam) so other tests' stubs cannot leak
// in, and deletes the key afterwards — Windows-only, so it runs on the
// developer's machine but not in the ubuntu CI job; the shared command/
// quoting logic is covered platform-independently by the untagged tests.
func TestWindowsAutostartRegistryRoundTrip(t *testing.T) {
	origPath := autostartRunKeyPath
	testKey := "Software\\MulticaAutostartRoundTripTest"
	autostartRunKeyPath = testKey
	t.Cleanup(func() {
		autostartRunKeyPath = origPath
		_ = registry.DeleteKey(registry.CURRENT_USER, testKey)
	})
	// Start from a clean slate if a previous run left the key behind.
	// DeleteKey removes a key together with its values; only subkeys would
	// block it, and this test creates none.
	_ = registry.DeleteKey(registry.CURRENT_USER, testKey)

	spec := autostartSpec{
		Exe:  `C:\Program Files\multica\multica.exe`,
		Args: autostartArgs("staging"),
	}

	// First registration reports a change and lands in the registry.
	state, changed, err := platformWriteAutostart("staging", spec)
	if err != nil {
		t.Fatalf("platformWriteAutostart = %v", err)
	}
	if !changed || !state.Enabled {
		t.Fatalf("first write: changed=%v enabled=%v, want true/true", changed, state.Enabled)
	}
	if state.Mechanism != autostartMechanismWindowsRun {
		t.Errorf("mechanism = %q, want %q", state.Mechanism, autostartMechanismWindowsRun)
	}
	if !strings.Contains(state.Location, `Multica (staging)`) {
		t.Errorf("location = %q, want it to name the profile's value", state.Location)
	}
	wantCommand := windowsAutostartCommand(spec)
	if state.Command != wantCommand {
		t.Errorf("command = %q, want %q", state.Command, wantCommand)
	}

	// Refreshing with the same spec is a no-op — the property that keeps
	// every `daemon start` after the first read-only.
	if _, changed, err := platformWriteAutostart("staging", spec); err != nil || changed {
		t.Fatalf("identical rewrite: changed=%v err=%v, want a no-op", changed, err)
	}

	// status reads back exactly what was stored.
	read, err := platformReadAutostart("staging")
	if err != nil {
		t.Fatalf("platformReadAutostart = %v", err)
	}
	if !read.Enabled || read.Command != wantCommand {
		t.Fatalf("read = %+v, want enabled with command %q", read, wantCommand)
	}

	// A moved binary rewrites the value (the self-update heal path).
	moved := spec
	moved.Exe = `D:\tools\multica.exe`
	if _, changed, err := platformWriteAutostart("staging", moved); err != nil || !changed {
		t.Fatalf("path change: changed=%v err=%v, want a rewrite", changed, err)
	}
	read, _ = platformReadAutostart("staging")
	if !strings.HasPrefix(read.Command, `D:\tools\multica.exe `) {
		t.Errorf("command after move = %q, want the new executable", read.Command)
	}

	// A different profile owns a different value — registrations coexist.
	if _, _, err := platformWriteAutostart("", autostartSpec{Exe: moved.Exe, Args: autostartArgs("")}); err != nil {
		t.Fatalf("write default profile: %v", err)
	}
	if _, changed, err := platformRemoveAutostart("staging"); err != nil || !changed {
		t.Fatalf("remove staging: changed=%v err=%v, want true", changed, err)
	}
	if read, err := platformReadAutostart("staging"); err != nil || read.Enabled {
		t.Fatalf("staging after remove: enabled=%v err=%v, want disabled", read.Enabled, err)
	}
	// Removing the default profile's value must not have been affected by
	// staging's removal: it is still there.
	if read, err := platformReadAutostart(""); err != nil || !read.Enabled {
		t.Fatalf("default profile after removing staging: enabled=%v err=%v, want still enabled", read.Enabled, err)
	}

	// Removal is idempotent: the second call finds nothing to remove.
	if _, changed, err := platformRemoveAutostart("staging"); err != nil || changed {
		t.Fatalf("second remove: changed=%v err=%v, want false/nil", changed, err)
	}

	// Cleanup of the default profile's value (the key itself is removed by
	// the outer cleanup).
	if _, _, err := platformRemoveAutostart(""); err != nil {
		t.Fatalf("remove default profile: %v", err)
	}
}
