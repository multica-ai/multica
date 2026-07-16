package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ownershipLockFileName is the machine-global file whose advisory lock decides
// which daemon process owns this host. It lives directly under the base config
// directory (`~/.multica`), NOT under a profile subdirectory, so every daemon
// on the machine — the CLI-spawned one (default profile) and the Desktop-spawned
// one (a `--profile desktop-<host>` daemon) — resolves the SAME path and
// contends for the SAME lock. That is the invariant the per-profile health-port
// and PID-file guards miss: they key on the profile, so two profiles never see
// each other and both start, each holding an independent in-process
// local-directory mutex that lets two agents write the same checkout at once
// (VWO-364 / VWO-365).
const ownershipLockFileName = "daemon.lock"

// OwnerInfo is the diagnostic record the owning daemon writes into the lock
// file body. It is advisory only — the OS advisory lock on the file, not this
// content, is what enforces ownership. It exists so a would-be second daemon
// (and `daemon stop/restart --takeover`) can name the incumbent and reach its
// health port for a graceful takeover.
type OwnerInfo struct {
	PID        int       `json:"pid"`
	HealthPort int       `json:"health_port"`
	DaemonID   string    `json:"daemon_id"`
	Version    string    `json:"version"`
	Profile    string    `json:"profile,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

// OwnershipLock is a held machine-level daemon ownership lock. The advisory
// lock is held for the lifetime of the open file handle: the kernel releases
// it automatically when the process exits (clean or crashed), so there is no
// TTL to tune and no stale-lock window — a crashed owner's lock is gone the
// instant it dies and the next daemon acquires immediately, with no manual
// database or file surgery. Call Release on clean shutdown.
type OwnershipLock struct {
	path string
	f    *os.File
}

// OwnershipConflict is returned by AcquireOwnership when another live daemon
// already holds the machine lock. Incumbent carries whatever could be read
// from the lock body (best-effort; HasInfo is false when it was empty or
// unreadable, e.g. an owner that has not finished writing it yet).
type OwnershipConflict struct {
	Path      string
	Incumbent OwnerInfo
	HasInfo   bool
}

func (e *OwnershipConflict) Error() string {
	if e.HasInfo {
		// The stop hint must target the OWNER's profile: a bare `multica daemon
		// stop` only checks the default profile's health port, which is exactly
		// the cross-profile blindness this lock exists to catch.
		stopHint := "`multica daemon stop`"
		if e.Incumbent.Profile != "" {
			stopHint = fmt.Sprintf("`multica --profile %s daemon stop`", e.Incumbent.Profile)
		}
		return fmt.Sprintf(
			"another Multica daemon already owns this machine (pid %d, version %s, profile %q, health port %d, since %s) via lock %s; stop it with %s or take over with `multica daemon start --takeover`",
			e.Incumbent.PID, orUnknown(e.Incumbent.Version), e.Incumbent.Profile, e.Incumbent.HealthPort,
			e.Incumbent.StartedAt.Format(time.RFC3339), e.Path, stopHint,
		)
	}
	return fmt.Sprintf(
		"another Multica daemon already owns this machine via lock %s (owner details unavailable); stop it with `multica daemon stop` (add --profile <name> if it runs under a named profile) or take over with `multica daemon start --takeover`",
		e.Path,
	)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// OwnershipLockPath returns the machine-global daemon lock path for baseDir.
// baseDir is the base config directory (cli.ProfileDir("") == ~/.multica),
// shared by every profile on the machine.
func OwnershipLockPath(baseDir string) string {
	return filepath.Join(baseDir, ownershipLockFileName)
}

// AcquireOwnership takes the machine-global daemon ownership lock at
// baseDir/daemon.lock WITHOUT blocking, and records self in the lock body on
// success. It is the single authoritative ownership mechanism: acquire it
// before a daemon binds its health port, registers runtimes, or claims any
// task, so a second process never advertises the same identity or runtime IDs
// while an owner is live.
//
// Returns:
//   - (*OwnershipLock, nil) when this process is now the owner.
//   - (nil, *OwnershipConflict) when another live daemon holds it. The error
//     names the incumbent so the caller can print an actionable message.
//   - (nil, err) on an I/O failure opening or locking the file.
//
// The lock file is intentionally NOT removed on release: a persistent inode
// avoids the classic flock+unlink race (unlinking a locked file lets a racing
// acquirer create a fresh inode and both "win"). Stale content from a prior
// owner is harmless — the OS lock state, not the body, is authoritative, and
// the body is truncated and rewritten on every successful acquire.
func AcquireOwnership(baseDir string, self OwnerInfo) (*OwnershipLock, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("acquire daemon ownership: empty base directory")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir for daemon lock: %w", err)
	}
	path := OwnershipLockPath(baseDir)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	locked, err := tryLockExclusive(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lock daemon lock %s: %w", path, err)
	}
	if !locked {
		info, ok := readOwnerInfo(path)
		f.Close()
		return nil, &OwnershipConflict{Path: path, Incumbent: info, HasInfo: ok}
	}
	if err := writeOwnerInfo(f, self); err != nil {
		_ = unlock(f)
		f.Close()
		return nil, fmt.Errorf("record daemon lock owner: %w", err)
	}
	return &OwnershipLock{path: path, f: f}, nil
}

// Release drops the advisory lock and closes the handle. Safe to call on a nil
// receiver or an already-released lock. The lock file itself is left in place
// (see AcquireOwnership).
func (l *OwnershipLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	unlockErr := unlock(l.f)
	closeErr := l.f.Close()
	l.f = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}

// writeOwnerInfo truncates the lock file and writes self as JSON. The caller
// holds the exclusive advisory lock, so this write is uncontended.
func writeOwnerInfo(f *os.File, self OwnerInfo) error {
	data, err := json.Marshal(self)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// readOwnerInfo reads the lock body without taking the lock. It is used on the
// conflict path (to name the incumbent) and by the takeover path (to reach the
// incumbent's health port). Returns (info, false) when the file is missing,
// empty, or unparseable — e.g. a brand-new owner that locked but has not yet
// written its body.
func readOwnerInfo(path string) (OwnerInfo, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return OwnerInfo{}, false
	}
	var info OwnerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return OwnerInfo{}, false
	}
	return info, true
}

// ReadOwnerInfo exposes the lock body to callers outside the daemon package
// (the CLI takeover path). Returns ok=false when there is no readable owner.
func ReadOwnerInfo(baseDir string) (OwnerInfo, bool) {
	return readOwnerInfo(OwnershipLockPath(baseDir))
}

// ProbeOwnership reports whether the machine ownership lock is currently free,
// WITHOUT recording a new owner. When held, it returns the incumbent (ok=true
// when the body was readable). It is used by the CLI launcher to give a fast,
// actionable conflict message (or drive a takeover) before spawning the real
// daemon, which acquires the lock authoritatively in Run.
//
// "free" is an instantaneous observation: a probe that finds the lock free does
// not reserve it, so the caller must still let the spawned daemon acquire it.
func ProbeOwnership(baseDir string) (free bool, incumbent OwnerInfo, ok bool, err error) {
	if baseDir == "" {
		return false, OwnerInfo{}, false, fmt.Errorf("probe daemon ownership: empty base directory")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return false, OwnerInfo{}, false, fmt.Errorf("create config dir for daemon lock: %w", err)
	}
	path := OwnershipLockPath(baseDir)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return false, OwnerInfo{}, false, fmt.Errorf("open daemon lock %s: %w", path, err)
	}
	locked, err := tryLockExclusive(f)
	if err != nil {
		f.Close()
		return false, OwnerInfo{}, false, fmt.Errorf("probe daemon lock %s: %w", path, err)
	}
	if !locked {
		info, has := readOwnerInfo(path)
		f.Close()
		return false, info, has, nil
	}
	_ = unlock(f)
	f.Close()
	return true, OwnerInfo{}, false, nil
}

// OwnershipBypassed reports whether the operator disabled the single-owner
// guard via MULTICA_DAEMON_ALLOW_MULTIPLE. This is the documented break-glass /
// rollback for the ownership lock, and the escape hatch for a deliberate
// multi-backend setup that knowingly accepts the shared-checkout risk. Any
// value other than empty / "0" / "false" enables the bypass.
func OwnershipBypassed() bool {
	v := strings.TrimSpace(os.Getenv("MULTICA_DAEMON_ALLOW_MULTIPLE"))
	return v != "" && v != "0" && !strings.EqualFold(v, "false")
}
