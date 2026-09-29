//go:build linux

package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

func platformAutostartSupported() bool { return true }

// procSelfCgroupPath is a variable so tests can point the ownership check at
// a fixture instead of this process's real cgroup.
var procSelfCgroupPath = "/proc/self/cgroup"

// platformAutostartRefreshAllowed gates the silent heal on `daemon start`.
// Outside systemd it is always fine. Under systemd, a daemon launched by an
// external unit (a hand-written service with its own EnvironmentFile=, a
// container manager that happens to run under systemd) must not let Multica
// rewrite anything: only our own unit gets to refresh its own file.
func platformAutostartRefreshAllowed(profile string) bool {
	if os.Getenv("INVOCATION_ID") == "" {
		return true
	}
	return runningUnderOwnSystemdUnit(profile)
}

// platformDaemonUnderOwnSystemdUnit reports whether THIS process was started
// by the user unit Multica itself generates. Used by the foreground daemon's
// binary-update handoff: under our unit it exits with
// daemonSystemdHandoffExitStatus instead of spawning a successor, because
// systemd's cgroup cleanup would kill a successor spawned before a clean
// exit 0 (see runDaemonForeground).
func platformDaemonUnderOwnSystemdUnit(profile string) bool {
	if os.Getenv("INVOCATION_ID") == "" {
		return false
	}
	return runningUnderOwnSystemdUnit(profile)
}

// runningUnderOwnSystemdUnit matches the unit name against this process's
// cgroup path — systemd puts the unit name there for user services, so a
// daemon started by the user's own differently-named unit (INVOCATION_ID set,
// unit not ours) reads false and keeps the portable spawn handoff.
func runningUnderOwnSystemdUnit(profile string) bool {
	data, err := os.ReadFile(procSelfCgroupPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), systemdUnitName(profile))
}

// systemdAvailable reports whether registration should target a systemd user
// unit. It is the preferred mechanism: it supervises restarts (bounded — see
// systemdUnitContent) and is what headless servers actually have, since a
// desktop autostart entry never fires on a machine nobody logs into.
func systemdAvailable() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

// configSubdir resolves an XDG base dir: $XDG_CONFIG_HOME when set (systemd
// and the session managers honor it too), else ~/.config.
func configSubdir(parts ...string) (string, error) {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(append([]string{xdg}, parts...)...), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home, ".config"}, parts...)...), nil
}

func systemdUnitPath(profile string) (string, error) {
	dir, err := configSubdir("systemd", "user")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, systemdUnitName(profile)), nil
}

// systemdUnitLinked reports whether the unit is enabled the way `systemctl
// enable` leaves it: a wants symlink under default.target.wants. Reading the
// symlink instead of calling `systemctl is-enabled` keeps `status` working
// without a live user bus (e.g. over a bare SSH session).
func systemdUnitLinked(unitPath string) bool {
	wantsDir := filepath.Join(filepath.Dir(unitPath), "default.target.wants")
	info, err := os.Lstat(filepath.Join(wantsDir, filepath.Base(unitPath)))
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

func xdgAutostartPath(profile string) (string, error) {
	dir, err := configSubdir("autostart")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, xdgAutostartFileName(profile)), nil
}

// platformWriteAutostart registers the profile for boot/login start via
// systemd (preferred) or an XDG autostart entry.
func platformWriteAutostart(profile string, spec autostartSpec) (autostartState, bool, error) {
	if systemdAvailable() {
		return writeSystemdAutostart(profile, spec)
	}
	return writeXdgAutostart(profile, spec)
}

func writeSystemdAutostart(profile string, spec autostartSpec) (autostartState, bool, error) {
	path, err := systemdUnitPath(profile)
	if err != nil {
		return autostartState{}, false, err
	}
	content := systemdUnitContent(spec)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return autostartState{}, false, err
	}
	previous, readErr := os.ReadFile(path)
	// Second line of defense behind the shared guard: never overwrite a unit
	// at our path that lacks the Multica marker — a user's hand-written unit
	// (e.g. one with an EnvironmentFile=) lives at this exact name.
	if readErr == nil && !autostartCommentMarked(string(previous)) {
		return autostartState{
			Mechanism: autostartMechanismSystemd,
			Location:  path,
		}, false, errAutostartUnmanaged
	}
	changed := readErr != nil || string(previous) != content
	if changed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return autostartState{}, false, err
		}
	}

	// enable creates the default.target.wants symlink; it does NOT start the
	// unit (`daemon start` already did that) and it is idempotent when the
	// link exists. daemon-reload is best-effort: without it systemd still
	// picks the unit up at the next login, and a user manager that is not
	// running yet has nothing to reload.
	if out, err := exec.Command("systemctl", "--user", "enable", systemdUnitName(profile)).CombinedOutput(); err != nil {
		return autostartState{}, false, systemdCommandError("enable", err, out)
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()

	// Removing a stale XDG entry keeps one registration authoritative after
	// a systemd-capable environment replaced an earlier fallback.
	if removeXdgAutostartFile(profile) {
		changed = true
	}

	return autostartState{
		Present:   true,
		Enabled:   true,
		Managed:   true,
		Mechanism: autostartMechanismSystemd,
		Location:  path,
		Command:   autostartCommandDisplay(spec),
		Note:      lingerNote(),
	}, changed, nil
}

func writeXdgAutostart(profile string, spec autostartSpec) (autostartState, bool, error) {
	path, err := xdgAutostartPath(profile)
	if err != nil {
		return autostartState{}, false, err
	}
	content := xdgAutostartContent(spec)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return autostartState{}, false, err
	}
	previous, readErr := os.ReadFile(path)
	if readErr == nil && !autostartCommentMarked(string(previous)) {
		return autostartState{
			Mechanism: autostartMechanismXDG,
			Location:  path,
		}, false, errAutostartUnmanaged
	}
	changed := readErr != nil || string(previous) != content
	if changed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return autostartState{}, false, err
		}
	}
	return autostartState{
		Present:   true,
		Enabled:   true,
		Managed:   true,
		Mechanism: autostartMechanismXDG,
		Location:  path,
		Command:   autostartCommandDisplay(spec),
	}, changed, nil
}

// lingerNote reports whether this user's manager starts at boot (linger) or
// only at first login — the difference between boot autostart and login
// autostart on a server nobody logs into. It only READS state: enabling
// linger is a system-level change the review explicitly reserved for the
// user to make, so the note hands them the exact command instead of running
// it. Empty when already lingering, when loginctl is missing, or when the
// query fails (we cannot claim either way).
func lingerNote() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	out, err := exec.Command("loginctl", "show-user", "--value", "-p", "Linger", u.Username).CombinedOutput()
	if err != nil {
		return ""
	}
	if strings.TrimSpace(string(out)) == "yes" {
		return ""
	}
	return "starts at login; to start at boot without logging in, run: loginctl enable-linger " + u.Username
}

// platformRemoveAutostart clears the registration from both mechanisms: a
// profile registered under the fallback and later re-registered under
// systemd must not leave either half behind, and each half is a no-op when
// it was never written.
//
// Every removal re-checks the ownership marker on the file itself first —
// the same backstop the writers carry — so an unmarked file at our path is
// never deleted even if a caller skipped the shared guard (or raced one):
// deleting a user's hand-written unit or .desktop is exactly the loss the
// marker exists to prevent.
func platformRemoveAutostart(profile string) (autostartState, bool, error) {
	state := autostartState{Mechanism: autostartMechanismXDG}
	changed := false

	if systemdAvailable() {
		path, err := systemdUnitPath(profile)
		if err != nil {
			return autostartState{}, false, err
		}
		state = autostartState{Mechanism: autostartMechanismSystemd, Location: path}

		if _, statErr := os.Stat(path); statErr == nil {
			// Marker check BEFORE systemctl disable: refusing after the
			// disable would already have unlinked the user's unit.
			previous, readErr := os.ReadFile(path)
			if readErr != nil {
				return autostartState{}, false, readErr
			}
			if !autostartCommentMarked(string(previous)) {
				return state, false, errAutostartUnmanaged
			}
			if _, err := exec.Command("systemctl", "--user", "disable", systemdUnitName(profile)).CombinedOutput(); err != nil {
				// A missing user bus must not strand the unit file: fall
				// back to unlinking the wants symlink directly.
				_ = removeSystemdWantsLink(path)
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return autostartState{}, false, err
			}
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
			changed = true
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return autostartState{}, false, statErr
		}
	} else {
		path, err := xdgAutostartPath(profile)
		if err != nil {
			return autostartState{}, false, err
		}
		state.Location = path
	}

	if removeXdgAutostartFile(profile) {
		changed = true
	}
	return state, changed, nil
}

func removeSystemdWantsLink(unitPath string) error {
	wantsDir := filepath.Join(filepath.Dir(unitPath), "default.target.wants")
	return os.Remove(filepath.Join(wantsDir, filepath.Base(unitPath)))
}

// removeXdgAutostartFile deletes the fallback entry if present, reporting
// whether anything was actually removed. Only a marked (Multica-created)
// file is removed: an unmarked .desktop at our name — reachable from the
// systemd sweep on every enable/refresh and from disable — is the user's,
// and is left in place. A missing file is not an error — removal is
// expected to be idempotent.
func removeXdgAutostartFile(profile string) bool {
	path, err := xdgAutostartPath(profile)
	if err != nil {
		return false
	}
	previous, err := os.ReadFile(path)
	if err != nil {
		// Missing (the common case) and unreadable both mean "nothing we
		// created is here": never delete what we cannot positively identify.
		return false
	}
	if !autostartCommentMarked(string(previous)) {
		return false
	}
	return os.Remove(path) == nil
}

// platformReadAutostart inspects both mechanisms so status stays truthful
// across a fallback/systemd transition, then reports the mechanism `enable`
// would use today when nothing is registered.
func platformReadAutostart(profile string) (autostartState, error) {
	if systemdAvailable() {
		path, err := systemdUnitPath(profile)
		if err != nil {
			return autostartState{}, err
		}
		content, err := os.ReadFile(path)
		switch {
		case err == nil:
			// Present tracks the FILE, Enabled tracks the wants link: an
			// unlinked unit — or one linked under another target — is
			// present-but-disabled, and the ownership guards key off Present.
			return autostartState{
				Present:   true,
				Enabled:   systemdUnitLinked(path),
				Managed:   autostartCommentMarked(string(content)),
				Mechanism: autostartMechanismSystemd,
				Location:  path,
				Command:   systemdStoredCommand(string(content)),
			}, nil
		case !errors.Is(err, fs.ErrNotExist):
			return autostartState{}, err
		}
		// Unit file absent — an XDG entry may still predate systemd.
		if state, xdgErr := readXdgAutostart(profile); xdgErr == nil && state.Present {
			return state, nil
		}
		return autostartState{Mechanism: autostartMechanismSystemd, Location: path}, nil
	}

	path, err := xdgAutostartPath(profile)
	if err != nil {
		return autostartState{}, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return autostartState{Mechanism: autostartMechanismXDG, Location: path}, nil
	}
	if err != nil {
		return autostartState{}, err
	}
	return autostartState{
		Present:   true,
		Enabled:   true,
		Managed:   autostartCommentMarked(string(content)),
		Mechanism: autostartMechanismXDG,
		Location:  path,
		Command:   xdgStoredCommand(string(content)),
	}, nil
}

func readXdgAutostart(profile string) (autostartState, error) {
	path, err := xdgAutostartPath(profile)
	if err != nil {
		return autostartState{}, err
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return autostartState{Mechanism: autostartMechanismXDG, Location: path}, nil
	}
	if err != nil {
		return autostartState{}, err
	}
	return autostartState{
		Present:   true,
		Enabled:   true,
		Managed:   autostartCommentMarked(string(content)),
		Mechanism: autostartMechanismXDG,
		Location:  path,
		Command:   xdgStoredCommand(string(content)),
	}, nil
}

// systemdCommandError renders a failed `systemctl --user` call with its
// output, since systemctl's own message is what names the real cause (no
// user bus, read-only home, ...).
func systemdCommandError(action string, err error, out []byte) error {
	return &autostartCommandFailure{Action: action, Err: err, Output: strings.TrimSpace(string(out))}
}

type autostartCommandFailure struct {
	Action string
	Err    error
	Output string
}

func (e *autostartCommandFailure) Error() string {
	msg := "systemctl --user " + e.Action + " failed: " + e.Err.Error()
	if e.Output != "" {
		msg += " (" + e.Output + ")"
	}
	return msg
}

func (e *autostartCommandFailure) Unwrap() error { return e.Err }
