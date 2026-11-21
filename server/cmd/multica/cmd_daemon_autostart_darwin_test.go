//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchAgentRoundTrip exercises the real LaunchAgent file mechanism
// under a throwaway HOME. It calls the platform functions directly (not the
// writeAutostart seam) so other tests' stubs cannot leak in. PATH is emptied
// so the best-effort `launchctl enable` cannot record an override for the
// test label in the machine's real launchd database.
func TestLaunchAgentRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	agents := filepath.Join(home, "Library", "LaunchAgents")
	stagingPath := filepath.Join(agents, "ai.multica.daemon.staging.plist")

	spec := autostartSpec{
		Exe:     "/opt/homebrew/bin/multica",
		Args:    autostartArgs("staging"),
		PathEnv: "/usr/bin:/opt/homebrew/bin",
	}

	// Nothing registered yet: status reports the location it would use.
	read, err := platformReadAutostart("staging")
	if err != nil {
		t.Fatalf("platformReadAutostart before write = %v", err)
	}
	if read.Present || read.Enabled || read.Location != stagingPath {
		t.Fatalf("read before write = %+v, want absent at %q", read, stagingPath)
	}

	// First registration creates LaunchAgents and reports a change.
	state, changed, err := platformWriteAutostart("staging", spec)
	if err != nil {
		t.Fatalf("platformWriteAutostart = %v", err)
	}
	if !changed || !state.Present || !state.Enabled || !state.Managed {
		t.Fatalf("first write = %+v changed=%v, want a managed, enabled entry", state, changed)
	}
	if state.Mechanism != autostartMechanismLaunchd || state.Location != stagingPath {
		t.Errorf("first write mechanism/location = %q/%q, want %q/%q", state.Mechanism, state.Location, autostartMechanismLaunchd, stagingPath)
	}
	wantCommand := "/opt/homebrew/bin/multica daemon start --foreground --profile staging"
	if state.Command != wantCommand {
		t.Errorf("command = %q, want %q", state.Command, wantCommand)
	}

	// launchd silently ignores a plist it cannot parse, so validate what we
	// wrote with the system's own parser.
	if out, err := exec.Command("/usr/bin/plutil", "-lint", stagingPath).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint rejected the LaunchAgent: %v\n%s", err, out)
	}

	// Refreshing with the same spec is a no-op — the property that keeps
	// every `daemon start` after the first read-only.
	if _, changed, err := platformWriteAutostart("staging", spec); err != nil || changed {
		t.Fatalf("identical rewrite: changed=%v err=%v, want a no-op", changed, err)
	}

	// status reads back exactly what was stored.
	read, err = platformReadAutostart("staging")
	if err != nil {
		t.Fatalf("platformReadAutostart = %v", err)
	}
	if !read.Present || !read.Enabled || !read.Managed || read.Command != wantCommand {
		t.Fatalf("read = %+v, want a managed, enabled entry with command %q", read, wantCommand)
	}

	// A moved binary rewrites the file (the self-update heal path).
	moved := spec
	moved.Exe = "/usr/local/bin/multica"
	if _, changed, err := platformWriteAutostart("staging", moved); err != nil || !changed {
		t.Fatalf("path change: changed=%v err=%v, want a rewrite", changed, err)
	}
	if read, _ = platformReadAutostart("staging"); !strings.HasPrefix(read.Command, "/usr/local/bin/multica ") {
		t.Errorf("command after move = %q, want the new executable", read.Command)
	}

	// A different profile owns a different file — registrations coexist.
	if _, _, err := platformWriteAutostart("", autostartSpec{Exe: moved.Exe, Args: autostartArgs("")}); err != nil {
		t.Fatalf("write default profile: %v", err)
	}
	if _, changed, err := platformRemoveAutostart("staging"); err != nil || !changed {
		t.Fatalf("remove staging: changed=%v err=%v, want true", changed, err)
	}
	if _, err := os.Stat(stagingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging plist after remove: stat err = %v, want not exist", err)
	}
	if read, err := platformReadAutostart(""); err != nil || !read.Enabled {
		t.Fatalf("default profile after removing staging: enabled=%v err=%v, want still enabled", read.Enabled, err)
	}

	// Removal is idempotent: the second call finds nothing to remove.
	if _, changed, err := platformRemoveAutostart("staging"); err != nil || changed {
		t.Fatalf("second remove: changed=%v err=%v, want false/nil", changed, err)
	}
}

// TestLaunchAgentLeavesForeignPlistAlone pins the second line of defense: a
// plist at our path without the Multica marker is the user's own agent, and
// refresh must neither overwrite it nor report it as ours.
func TestLaunchAgentLeavesForeignPlistAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	path := filepath.Join(home, "Library", "LaunchAgents", "ai.multica.daemon.plist")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict><key>Label</key><string>ai.multica.daemon</string><key>ProgramArguments</key><array><string>/usr/local/bin/mine</string></array></dict></plist>
`
	if err := os.WriteFile(path, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	read, err := platformReadAutostart("")
	if err != nil {
		t.Fatalf("platformReadAutostart = %v", err)
	}
	if !read.Present || read.Managed || read.Command != "/usr/local/bin/mine" {
		t.Fatalf("read = %+v, want a present, unmanaged entry reporting its own command", read)
	}

	_, changed, err := platformWriteAutostart("", autostartSpec{Exe: "/opt/homebrew/bin/multica", Args: autostartArgs("")})
	if !errors.Is(err, errAutostartUnmanaged) || changed {
		t.Fatalf("write over a foreign plist: changed=%v err=%v, want errAutostartUnmanaged", changed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != foreign {
		t.Fatalf("foreign plist was modified:\n%s", got)
	}
}
