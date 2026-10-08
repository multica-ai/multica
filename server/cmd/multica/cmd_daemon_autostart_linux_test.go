//go:build linux

package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCgroupFixture points procSelfCgroupPath at a fixture containing the
// given cgroup table for the duration of the test.
func writeCgroupFixture(t *testing.T, content string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cgroup")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write cgroup fixture: %v", err)
	}
	orig := procSelfCgroupPath
	procSelfCgroupPath = path
	t.Cleanup(func() { procSelfCgroupPath = orig })
}

// TestSystemdOwnUnitDetection pins the matcher behind BOTH systemd decisions:
// the `daemon start` refresh gate and the binary-update handoff. systemd
// embeds the unit name in the cgroup path of a user service, so a daemon
// started by our own unit matches, while one started by the user's own
// differently-named unit does not — that is what keeps the spawn handoff and
// the no-refresh rule pointed at foreign supervisors only.
func TestSystemdOwnUnitDetection(t *testing.T) {
	ownCgroup := "0::/user.slice/user-1000.slice/user@1000.service/app.slice/multica-daemon.service-2841.service\n"
	ownProfileCgroup := "0::/user.slice/user-1000.slice/user@1000.service/app.slice/multica-daemon-staging.service-12.service\n"
	foreignCgroup := "0::/user.slice/user-1000.slice/user@1000.service/app.slice/my-daemon.service-9.service\n"

	t.Run("our unit matches the default profile", func(t *testing.T) {
		writeCgroupFixture(t, ownCgroup)
		if !runningUnderOwnSystemdUnit("") {
			t.Error("runningUnderOwnSystemdUnit(\"\") = false for our own unit's cgroup")
		}
		// A different profile's unit must not match: the '-staging' infix
		// keeps the plain name from reading as a prefix hit.
		if runningUnderOwnSystemdUnit("staging") {
			t.Error("runningUnderOwnSystemdUnit(\"staging\") = true on the default profile's unit")
		}
	})

	t.Run("named profile unit matches its own profile only", func(t *testing.T) {
		writeCgroupFixture(t, ownProfileCgroup)
		if !runningUnderOwnSystemdUnit("staging") {
			t.Error("runningUnderOwnSystemdUnit(\"staging\") = false for that profile's own unit")
		}
		if runningUnderOwnSystemdUnit("") {
			t.Error("runningUnderOwnSystemdUnit(\"\") = true on the staging profile's unit")
		}
	})

	t.Run("a foreign unit never matches", func(t *testing.T) {
		writeCgroupFixture(t, foreignCgroup)
		if runningUnderOwnSystemdUnit("") {
			t.Error("runningUnderOwnSystemdUnit(\"\") = true for the user's own unit")
		}
	})

	t.Run("unreadable cgroup never matches", func(t *testing.T) {
		orig := procSelfCgroupPath
		procSelfCgroupPath = filepath.Join(t.TempDir(), "missing")
		t.Cleanup(func() { procSelfCgroupPath = orig })
		if runningUnderOwnSystemdUnit("") {
			t.Error("runningUnderOwnSystemdUnit(\"\") = true with no readable cgroup")
		}
	})
}

// TestSystemdSupervisorGates pins the two env-gated decisions: outside
// systemd (no INVOCATION_ID) both the refresh and the spawn handoff behave
// as they always have; under systemd only our own unit keeps them.
func TestSystemdSupervisorGates(t *testing.T) {
	own := "0::/app.slice/multica-daemon.service-1.service\n"
	foreign := "0::/app.slice/user-service.service-1.service\n"

	t.Run("outside systemd refresh is always allowed", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "")
		if !platformAutostartRefreshAllowed("") {
			t.Error("platformAutostartRefreshAllowed = false outside systemd")
		}
		if platformDaemonUnderOwnSystemdUnit("") {
			t.Error("platformDaemonUnderOwnSystemdUnit = true outside systemd")
		}
	})

	t.Run("under our unit both refresh and handoff are allowed", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "abc123")
		writeCgroupFixture(t, own)
		if !platformAutostartRefreshAllowed("") {
			t.Error("platformAutostartRefreshAllowed = false under our own unit")
		}
		if !platformDaemonUnderOwnSystemdUnit("") {
			t.Error("platformDaemonUnderOwnSystemdUnit = false under our own unit")
		}
	})

	t.Run("under a foreign unit refresh and handoff are refused", func(t *testing.T) {
		t.Setenv("INVOCATION_ID", "abc123")
		writeCgroupFixture(t, foreign)
		if platformAutostartRefreshAllowed("") {
			t.Error("platformAutostartRefreshAllowed = true under a foreign unit")
		}
		if platformDaemonUnderOwnSystemdUnit("") {
			t.Error("platformDaemonUnderOwnSystemdUnit = true under a foreign unit")
		}
	})
}

// useAutostartConfigHome points XDG_CONFIG_HOME at a fresh temp dir so every
// path helper (the systemd user dir, the autostart dir) resolves into a
// sandbox — never the test host's real ~/.config.
func useAutostartConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

func writeAutostartFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// unmarkedUnitFixture is a plausible hand-written unit at Multica's own name —
// no marker, and WantedBy a target other than default, exactly the shape the
// review found deletable.
const unmarkedUnitFixture = `[Unit]
Description=user's own service with an EnvironmentFile
[Service]
ExecStart=/usr/bin/true
[Install]
WantedBy=graphical-session.target
`

func markedUnitFixture(t *testing.T) string {
	t.Helper()
	return systemdUnitContent(autostartSpec{Exe: "/usr/local/bin/multica", Args: autostartArgs("")})
}

// TestPlatformReadAutostartReportsUnmarkedUnitPresence pins the read side of
// the review's gap: Enabled tracks only default.target.wants, so a
// hand-written unit reads disabled whether it is unlinked or linked under
// another target — while Present stays true and Managed false, which is what
// the ownership guards now key off.
func TestPlatformReadAutostartReportsUnmarkedUnitPresence(t *testing.T) {
	if !systemdAvailable() {
		t.Skip("systemctl not available")
	}
	useAutostartConfigHome(t)

	readUnmarked := func(t *testing.T) autostartState {
		t.Helper()
		path, err := systemdUnitPath("")
		if err != nil {
			t.Fatalf("systemdUnitPath: %v", err)
		}
		writeAutostartFixture(t, path, unmarkedUnitFixture)
		state, err := platformReadAutostart("")
		if err != nil {
			t.Fatalf("platformReadAutostart: %v", err)
		}
		return state
	}

	t.Run("unlinked unmarked unit is present but disabled", func(t *testing.T) {
		state := readUnmarked(t)
		if !state.Present || state.Enabled || state.Managed {
			t.Fatalf("state = %+v, want Present && !Enabled && !Managed", state)
		}
	})

	t.Run("unmarked unit linked under another target reads disabled", func(t *testing.T) {
		state := readUnmarked(t)
		path, err := systemdUnitPath("")
		if err != nil {
			t.Fatalf("systemdUnitPath: %v", err)
		}
		linkUnit(t, path, "graphical-session.target.wants")
		state, err = platformReadAutostart("")
		if err != nil {
			t.Fatalf("platformReadAutostart: %v", err)
		}
		// Linked — but not under default.target, so Enabled stays false and
		// the old Enabled-keyed guard would have missed it entirely.
		if !state.Present || state.Enabled || state.Managed {
			t.Fatalf("state = %+v, want Present && !Enabled && !Managed despite the other-target link", state)
		}
	})

	t.Run("unmarked unit linked under default.target is enabled but still foreign", func(t *testing.T) {
		state := readUnmarked(t)
		path, err := systemdUnitPath("")
		if err != nil {
			t.Fatalf("systemdUnitPath: %v", err)
		}
		linkUnit(t, path, "default.target.wants")
		state, err = platformReadAutostart("")
		if err != nil {
			t.Fatalf("platformReadAutostart: %v", err)
		}
		if !state.Present || !state.Enabled || state.Managed {
			t.Fatalf("state = %+v, want Present && Enabled && !Managed", state)
		}
	})
}

// linkUnit creates the wants symlink systemctl enable would create, under the
// named wants directory relative to the unit's directory.
func linkUnit(t *testing.T, unitPath, wantsSubdir string) {
	t.Helper()
	wantsDir := filepath.Join(filepath.Dir(unitPath), wantsSubdir)
	if err := os.MkdirAll(wantsDir, 0o755); err != nil {
		t.Fatalf("create wants dir: %v", err)
	}
	if err := os.Symlink(unitPath, filepath.Join(wantsDir, filepath.Base(unitPath))); err != nil {
		t.Fatalf("link unit: %v", err)
	}
}

// TestPlatformRemoveAutostartRefusesUnmarkedUnit pins the removal backstop:
// even if a caller skips the shared guard, an unmarked file at our unit path
// is never deleted — and the refusal fires BEFORE systemctl disable, so the
// user's unit is not unlinked as a side effect either.
func TestPlatformRemoveAutostartRefusesUnmarkedUnit(t *testing.T) {
	if !systemdAvailable() {
		t.Skip("systemctl not available")
	}
	useAutostartConfigHome(t)
	path, err := systemdUnitPath("")
	if err != nil {
		t.Fatalf("systemdUnitPath: %v", err)
	}
	writeAutostartFixture(t, path, unmarkedUnitFixture)

	_, changed, err := platformRemoveAutostart("")
	if !errors.Is(err, errAutostartUnmanaged) {
		t.Fatalf("platformRemoveAutostart = %v, want errAutostartUnmanaged", err)
	}
	if changed {
		t.Fatalf("changed = true, want false — nothing may be removed")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("unmarked unit was touched: %v", statErr)
	}
}

// TestPlatformRemoveAutostartKeepsUnmarkedXdgAlongsideManagedUnit is the
// review's coexistence scenario: a Multica-created unit plus a hand-written
// .desktop at our XDG name. Disabling Multica's registration may remove the
// unit but must leave the user's .desktop alone — the sweep funnels through
// the marker-checked removeXdgAutostartFile, which enable and the
// `daemon start` refresh use as well, so this covers all three call sites.
func TestPlatformRemoveAutostartKeepsUnmarkedXdgAlongsideManagedUnit(t *testing.T) {
	if !systemdAvailable() {
		t.Skip("systemctl not available")
	}
	useAutostartConfigHome(t)
	unitPath, err := systemdUnitPath("")
	if err != nil {
		t.Fatalf("systemdUnitPath: %v", err)
	}
	xdgPath, err := xdgAutostartPath("")
	if err != nil {
		t.Fatalf("xdgAutostartPath: %v", err)
	}
	writeAutostartFixture(t, unitPath, markedUnitFixture(t))
	writeAutostartFixture(t, xdgPath, "[Desktop Entry]\nType=Application\nName=hand-written\nExec=/usr/bin/true\n")

	_, changed, err := platformRemoveAutostart("")
	if err != nil {
		t.Fatalf("platformRemoveAutostart = %v", err)
	}
	if !changed {
		t.Fatalf("changed = false, want true — the marked unit should be removed")
	}
	if _, statErr := os.Stat(unitPath); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("marked unit still present (stat err = %v), want it removed", statErr)
	}
	if _, statErr := os.Stat(xdgPath); statErr != nil {
		t.Fatalf("unmarked .desktop was deleted (%v) — only Multica-created files may be removed", statErr)
	}
}

// TestRemoveXdgAutostartFileOnlyRemovesMarkedEntries covers the shared XDG
// sweeper directly: every removal path (enable's stale sweep, the daemon
// start refresh, disable) funnels through it, so an unmarked file there must
// survive all of them.
func TestRemoveXdgAutostartFileOnlyRemovesMarkedEntries(t *testing.T) {
	useAutostartConfigHome(t)
	path, err := xdgAutostartPath("")
	if err != nil {
		t.Fatalf("xdgAutostartPath: %v", err)
	}

	unmarked := "[Desktop Entry]\nType=Application\nName=hand-written\nExec=/usr/bin/true\n"
	writeAutostartFixture(t, path, unmarked)
	if removeXdgAutostartFile("") {
		t.Fatal("removeXdgAutostartFile removed an unmarked file")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("unmarked .desktop no longer exists: %v", statErr)
	}

	marked := xdgAutostartContent(autostartSpec{Exe: "/usr/local/bin/multica", Args: autostartArgs("")})
	writeAutostartFixture(t, path, marked)
	if !removeXdgAutostartFile("") {
		t.Fatal("removeXdgAutostartFile left a marked file in place")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("marked .desktop still present (stat err = %v), want it removed", statErr)
	}
}

// TestDisableKeepsRunningServiceRecoverable pins the review's P2 regression:
// running service → `autostart disable` → binary-update handoff. Disable must
// unlink the unit (so no start happens at the next login) while KEEPING the
// managed unit file: its RestartForceExitStatus is what lets the exit-42
// handoff come back in the current session. Deleting the file — as disable
// used to — left the live service at LoadState=not-found / Restart=no, so the
// handoff exited into failure and the runtime stayed offline. The follow-up
// sync (what `daemon start` runs) must also not silently re-enable it.
func TestDisableKeepsRunningServiceRecoverable(t *testing.T) {
	if !systemdAvailable() {
		t.Skip("systemctl not available")
	}
	useAutostartConfigHome(t)
	unitPath, err := systemdUnitPath("")
	if err != nil {
		t.Fatalf("systemdUnitPath: %v", err)
	}
	writeAutostartFixture(t, unitPath, markedUnitFixture(t))
	linkUnit(t, unitPath, "default.target.wants")

	if _, changed, err := platformRemoveAutostart(""); err != nil || !changed {
		t.Fatalf("platformRemoveAutostart: changed=%v err=%v, want the link removed", changed, err)
	}

	// 1. Restart configuration retained for the running, supervised service.
	content, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("managed unit file was deleted — the running service loses its restart policy: %v", err)
	}
	if !strings.Contains(string(content), "RestartForceExitStatus=") {
		t.Fatalf("retained unit lost its handoff restart policy:\n%s", content)
	}

	// 2. Boot autostart disabled: nothing links the unit anymore.
	if systemdUnitLinkedAnywhere(unitPath) {
		t.Fatal("unit still wants-linked — it would start at the next login")
	}

	// 3. The running process would still take the exit-42 handoff branch
	// (its cgroup still names the unit), so with the policy retained above
	// systemd restarts it — and the next daemon start must not re-enable.
	t.Setenv("INVOCATION_ID", "fixture")
	writeCgroupFixture(t, "0::/app.slice/multica-daemon.service-1.service\n")
	if !platformDaemonUnderOwnSystemdUnit("") {
		t.Fatal("handoff detection lost its own unit after disable — the daemon would fall back to spawn+exit 0")
	}
	t.Setenv("MULTICA_LAUNCHED_BY", "")
	syncDaemonAutostartDefault("", false)
	if systemdUnitLinkedAnywhere(unitPath) {
		t.Fatal("daemon start re-linked the unit — disable must survive a restart cycle")
	}
	if _, statErr := os.Stat(unitPath); statErr != nil {
		t.Fatalf("sync touched the retained unit: %v", statErr)
	}

	// A second disable is a no-op, not a second "disabled".
	if _, changed, err := platformRemoveAutostart(""); err != nil || changed {
		t.Fatalf("second disable: changed=%v err=%v, want false/nil", changed, err)
	}
}
