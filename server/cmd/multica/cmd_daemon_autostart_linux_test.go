//go:build linux

package main

import (
	"os"
	"path/filepath"
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
