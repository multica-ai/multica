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
		Enabled:   true,
		Mechanism: autostartMechanismSystemd,
		Location:  path,
		Command:   autostartCommandDisplay(spec),
		Note:      ensureLinger(),
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
	changed := readErr != nil || string(previous) != content
	if changed {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return autostartState{}, false, err
		}
	}
	return autostartState{
		Enabled:   true,
		Mechanism: autostartMechanismXDG,
		Location:  path,
		Command:   autostartCommandDisplay(spec),
	}, changed, nil
}

// ensureLinger tries to make this user's manager start at boot (not first
// login) — the difference between "boot autostart" and "login autostart" on a
// server nobody logs into. Returns a user-facing note when it could not, and
// never fails the registration: login-time start still works, the note just
// says how to get boot-time.
func ensureLinger() string {
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
	if out, err := exec.Command("loginctl", "enable-linger", u.Username).CombinedOutput(); err != nil {
		return "starts at login; to start at boot without logging in, run: loginctl enable-linger " + u.Username +
			systemdOutputSuffix(out)
	}
	return ""
}

// platformRemoveAutostart clears the registration from both mechanisms: a
// profile registered under the fallback and later re-registered under
// systemd must not leave either half behind, and each half is a no-op when
// it was never written.
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
// whether anything was actually removed. A missing file is not an error —
// removal is expected to be idempotent.
func removeXdgAutostartFile(profile string) bool {
	path, err := xdgAutostartPath(profile)
	if err != nil {
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
			return autostartState{
				Enabled:   systemdUnitLinked(path),
				Mechanism: autostartMechanismSystemd,
				Location:  path,
				Command:   systemdStoredCommand(string(content)),
			}, nil
		case !errors.Is(err, fs.ErrNotExist):
			return autostartState{}, err
		}
		// Unit file absent — an XDG entry may still predate systemd.
		if state, xdgErr := readXdgAutostart(profile); xdgErr == nil && state.Enabled {
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
		Enabled:   true,
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
		Enabled:   true,
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

func systemdOutputSuffix(out []byte) string {
	if s := strings.TrimSpace(string(out)); s != "" {
		return " (" + s + ")"
	}
	return ""
}
