package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/cli"
)

// daemonIDFileName is the file that stores this machine's stable daemon
// identifier. Once created, the UUID inside is the daemon's identity forever
// — hostname changes, .local suffix drift, profile switches and system
// renames no longer mint a new identity.
const daemonIDFileName = "daemon.id"

// EnsureDaemonID returns a stable UUID for this daemon instance, persisting
// it to disk on first call. Identity is profile-scoped: each profile gets its
// own daemon.id stored in its profile directory (e.g. `~/.multica/profiles/member/daemon.id`).
// The default (empty) profile stores it at `~/.multica/daemon.id`.
//
// This allows multiple profiles on the same machine to run independent daemons
// without competing for the same runtime identity on the server.
//
// If the file exists but is corrupt (unparseable), it is regenerated so the
// daemon can continue starting up instead of hard-failing.
func EnsureDaemonID(profile, configPath string) (string, error) {
	dir, err := daemonIDBaseDir(profile, configPath)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, daemonIDFileName)

	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			if _, perr := uuid.Parse(id); perr == nil {
				return id, nil
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read daemon id file: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create profile directory: %w", err)
	}

	// One-time promotion from pre-change per-profile layout.
	if promoted, ok := promoteProfileDaemonID(profile, configPath, path); ok {
		return promoted, nil
	}

	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate daemon id: %w", err)
	}

	if err := writeDaemonIDFile(path, id.String()); err != nil {
		return "", err
	}
	return id.String(), nil
}

// promoteProfileDaemonID copies a pre-change per-profile daemon.id into the
// canonical machine-scoped location. Returns the promoted UUID and true on
// success; returns "", false when there is nothing valid to promote (empty
// profile, missing/corrupt source file, any I/O failure). Promotion is a
// best-effort migration — a failure here falls through to fresh UUID mint.
func promoteProfileDaemonID(profile, configPath, targetPath string) (string, bool) {
	if profile == "" || strings.TrimSpace(configPath) != "" {
		return "", false
	}
	profileDir, err := cli.ProfileDir(profile)
	if err != nil {
		return "", false
	}
	src := filepath.Join(profileDir, daemonIDFileName)
	data, err := os.ReadFile(src)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	if _, err := uuid.Parse(id); err != nil {
		return "", false
	}
	if err := writeDaemonIDFile(targetPath, id); err != nil {
		return "", false
	}
	return id, true
}

// writeDaemonIDFile writes the UUID to path atomically with 0600 mode.
func writeDaemonIDFile(path, id string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daemon-*.id.tmp")
	if err != nil {
		return fmt.Errorf("create temp daemon id file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(id + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp daemon id file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp daemon id file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod temp daemon id file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename daemon id file: %w", err)
	}
	return nil
}

func daemonIDBaseDir(profile, configPath string) (string, error) {
	if strings.TrimSpace(configPath) != "" {
		return cli.StateDirForInstance(profile, configPath)
	}
	return cli.ProfileDir(profile)
}

// LegacyDaemonIDs returns the set of daemon_id values this machine may have
// previously registered under, before the switch to a persistent UUID. The
// server uses this list at registration time to merge old runtime rows into
// the new UUID-keyed row (moving agents/tasks then deleting the stale row).
//
// Three historical formats are covered:
//
//   - pre-#906:  "<hostname>-<profile>"        (profile suffix, no .local strip)
//   - pre-#1070: "<hostname>"                  (raw hostname, often ends in .local)
//   - current:   "<hostname>" with .local drift depending on system state
//
// .local drift is bidirectional — at different times os.Hostname() has
// returned both "foo" and "foo.local" on the same machine (mDNS state,
// system restart, login item order). So regardless of which form is current
// now, we always emit BOTH the bare and .local-suffixed variants so migration
// covers whichever form was persisted previously. Case drift is handled on
// the server side via case-insensitive lookup, so we don't also emit cased
// permutations here.
func LegacyDaemonIDs(hostname, profile string) []string {
	host := strings.TrimSpace(hostname)
	if host == "" {
		return nil
	}
	stripped := strings.TrimSuffix(host, ".local")
	dotLocal := stripped + ".local"

	hostForms := []string{stripped, dotLocal}

	candidates := make([]string, 0, len(hostForms)*2)
	candidates = append(candidates, hostForms...)
	if profile != "" {
		for _, h := range hostForms {
			candidates = append(candidates, h+"-"+profile)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

// LegacyDaemonUUIDs scans `~/.multica/profiles/*/daemon.id` and returns every
// UUID that survives parsing. These are identities that were minted per
// profile before daemon identity became machine-scoped; runtime rows
// registered under them — potentially on multiple backends (prod/dev/self-
// host) — need to be merged into the canonical machine UUID. The list is
// safe to emit to every backend: a UUID that was never registered there
// simply matches nothing in the server's merge lookup.
//
// Errors reading individual profile files are swallowed: a bad file
// shouldn't block daemon startup. A missing profiles directory returns
// (nil, nil) — that's the common case on a clean install.
func LegacyDaemonUUIDs() ([]string, error) {
	root, err := cli.ProfileDir("")
	if err != nil {
		return nil, err
	}
	profilesDir := filepath.Join(root, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(profilesDir, entry.Name(), daemonIDFileName))
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// filterLegacyIDs removes any entry equal to current (e.g. when the user
// explicitly pins MULTICA_DAEMON_ID to the hostname itself, there's nothing
// to migrate — the row is already keyed on the current id).
func filterLegacyIDs(ids []string, current string) []string {
	if current == "" {
		return ids
	}
	out := ids[:0]
	for _, id := range ids {
		if id == current {
			continue
		}
		out = append(out, id)
	}
	return out
}
