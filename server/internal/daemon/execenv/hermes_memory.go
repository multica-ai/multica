package execenv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Background
//
// Hermes keeps its long-term memory (MEMORY.md / USER.md and whatever else the
// agent writes back) in `<HERMES_HOME>/memories/`. Multica has to take over
// HERMES_HOME to inject an agent's bound skills — Hermes discovers skills only
// from its own home, never from the workspace (see hermes_home.go) — so the
// overlay inherited the decision of where memory lives and made it task-local.
//
// Nobody designed the two behaviours that produced (issue #6638):
//
//   - An agent WITH skills bound got a fresh memories/ per task, so it forgot
//     everything between tasks. Users read that as "Hermes 失忆", and it changed
//     the moment they bound an unrelated skill, with no UI signal.
//   - An agent with NO skills bound built no overlay at all, ran against the
//     user's real ~/.hermes, and therefore shared one memories/ with every
//     other skill-less agent, task, and issue on the runtime.
//
// This file makes memory an agent-scoped store Multica owns:
//
//	<multica profile dir>/hermes-state/<agent>/<hermes profile>/memories
//
// linked into the per-task overlay. Memory then survives across tasks and issues
// for the same agent, never touches the user's home, and is invisible to other
// agents. The shape is deliberately identical to the per-issue Codex session
// store (codex_home.go) and the per-agent Reasonix state home — same lifecycle,
// same GC, same active-store guard.
//
// Scoping to (hermes profile, agent) rather than agent alone mirrors Hermes' own
// model, where a profile is an isolated instance: pointing an agent at a
// different profile gives it a different identity, so it gets a different memory
// line. Same semantics as the Codex store keying on (profile, agent, issue).
//
// Deliberately NOT covered here:
//
//   - The session database (state.db and its WAL sidecars) stays task-local.
//     Sharing a live SQLite database between one agent's concurrent tasks needs
//     lock and consistency work — plus Windows byte-range locks on -shm — that
//     plain memory files do not. Users get "remembers the agreements", not
//     "remembers the transcript".
//   - Skill-less Hermes agents still run against the host home unchanged.
//     Tightening that would break the only workaround #6638's reporter has, and
//     it belongs with the platform-wide memory_scope work (MUL-5969).
//
// Known trade-off, shared with the host-passthrough behaviour it replaces: one
// agent's concurrent tasks write the same files and Hermes rewrites them whole,
// so concurrent memory updates are last-writer-wins.

// hermesMemoryStoreRoot is the directory under the daemon's Multica profile dir
// that holds every agent's persistent Hermes state. `hermes-state` (not
// `hermes-memories`) because state.db joins it when the session half is solved.
const hermesMemoryStoreRoot = "hermes-state"

// hermesMemoriesEntry is the name of the memory directory inside both the store
// and the overlay home — Hermes resolves it relative to HERMES_HOME.
const hermesMemoriesEntry = "memories"

// HermesMemoryStorePath returns the persistent memory store for (daemonProfile,
// agentID, sourceHome), or "" when memory must stay task-local — there is no
// agent to key on, or the Multica profile dir cannot be resolved. The daemon
// marks the returned path in-use for the task's duration so
// PruneHermesMemoryStores never reclaims it mid-mount.
//
// daemonProfile namespaces by Multica profile for free: profile dirs are already
// disjoint, so a staging daemon's GC can never see a production daemon's stores
// and no hashed namespace segment is needed (unlike the Codex store, which lives
// in the shared ~/.codex).
func HermesMemoryStorePath(daemonProfile, agentID, sourceHome string) string {
	agent := sanitizePathSegment(agentID)
	if agent == "" {
		return ""
	}
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return ""
	}
	return filepath.Join(profileDir, hermesMemoryStoreRoot, agent, hermesMemoryProfileSegment(sourceHome))
}

// hermesMemoryProfileSegment maps a resolved Hermes source home to the store
// segment that identifies it. Named profiles and the default home keep their
// readable name so an operator (and the documented one-off import) can find the
// directory; an out-of-tree custom HERMES_HOME falls back to a hash of its path,
// which is opaque but cannot collide with a differently-located home of the same
// basename.
func hermesMemoryProfileSegment(sourceHome string) string {
	home := strings.TrimSpace(sourceHome)
	if home == "" {
		return "default"
	}
	if abs, err := filepath.Abs(home); err == nil {
		home = abs
	}
	home = filepath.Clean(home)
	nativeRoot := filepath.Clean(platformDefaultHermesHome())
	if home == nativeRoot {
		return "default"
	}
	// <root>/profiles/<name> — the shape ResolveHermesProfile produces for an
	// explicit -p/--profile, a sticky active_profile, or a profile-scoped
	// custom_env HERMES_HOME. Profiles of the native root keep their bare name;
	// a profile of some other root gets its root hashed in, so `<a>/profiles/x`
	// and `<b>/profiles/x` cannot share one memory line.
	if root := filepath.Dir(filepath.Dir(home)); filepath.Base(filepath.Dir(home)) == "profiles" {
		if name := sanitizePathSegment(filepath.Base(home)); name != "" {
			if root == nativeRoot {
				return name
			}
			return name + "_" + hashHermesHomePath(root)
		}
	}
	return "h_" + hashHermesHomePath(home)
}

// hashHermesHomePath returns a short, filesystem-safe digest of an absolute
// home path — enough to keep two same-named homes in different locations apart
// without pushing the segment near the 255-byte filename limit.
func hashHermesHomePath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}

// mountHermesMemories points the overlay's memories/ at storeDir, so the agent's
// memory outlives the task. Idempotent across Reuse: a link already pointing at
// the store is left alone.
//
// A real memories/ directory left by an older daemon (or by a task that ran
// without a store to key on) is migrated into the store rather than discarded —
// but only into an empty store, so an upgrade never overwrites memory the agent
// has already accumulated. A migration that cannot complete fails the overlay
// rather than dropping the directory it could not carry over.
func mountHermesMemories(hermesHome, storeDir string, logger *slog.Logger) error {
	dst := filepath.Join(hermesHome, hermesMemoriesEntry)

	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if target, rlErr := os.Readlink(dst); rlErr == nil && filepath.Clean(target) == filepath.Clean(storeDir) {
				if err := os.MkdirAll(storeDir, 0o700); err != nil {
					return fmt.Errorf("create hermes memory store %s: %w", storeDir, err)
				}
				touchHermesMemoryStore(storeDir, logger)
				return nil
			}
		} else if fi.IsDir() {
			// Runs before the store dir is created, so migration can publish a
			// fully-copied tree with one atomic rename. Fails closed: the source
			// dir below is only removed once every entry is safely in the store.
			if err := migrateHermesTaskMemories(dst, storeDir, logger); err != nil {
				return err
			}
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("remove stale memories path %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat memories path %s: %w", dst, err)
	}

	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return fmt.Errorf("create hermes memory store %s: %w", storeDir, err)
	}
	if err := createDirLink(storeDir, dst); err != nil {
		return fmt.Errorf("link memories to store %s: %w", storeDir, err)
	}
	// Stamp the store as just-used: mounting it does not touch its mtime, so
	// without this the GC's idle check could reclaim a long-idle agent's memory
	// right as a task picks it back up.
	touchHermesMemoryStore(storeDir, logger)
	return nil
}

// migrateHermesTaskMemories copies the contents of a pre-existing task-local
// memories dir into an empty store, so upgrading a daemon does not drop what an
// in-flight task had already remembered.
//
// The caller removes the source directory as soon as this returns nil, so
// "nil" has to mean "the source is safe to delete" and nothing else. Three
// rules follow from that:
//
//   - It copies rather than moves. A move is faster but fails across
//     filesystems — the workspaces root and the Multica profile dir can be on
//     different volumes — and a partial move leaves data in neither place.
//   - It never reports success for anything it did not carry over. An I/O
//     error reading either directory, or an entry it will not copy (symlink,
//     device node — a link copied verbatim could point the store outside
//     itself), is an error, not a silent skip.
//   - It publishes with a single atomic rename from a staging directory, so
//     two tasks of the same agent migrating at once cannot interleave into one
//     half-populated store. First writer wins; the loser discards its staging
//     and takes the winner's store, which is the same outcome as finding a
//     store that was already populated.
//
// Only an absent or empty store is migrated into, so an upgrade never
// overwrites memory the agent has already accumulated.
func migrateHermesTaskMemories(taskDir, storeDir string, logger *slog.Logger) error {
	stored, err := os.ReadDir(storeDir)
	switch {
	case err == nil && len(stored) > 0:
		return nil // store already holds this agent's memory — never overwrite it
	case err != nil && !os.IsNotExist(err):
		// Treating this as "nothing to migrate" would delete the source below.
		return fmt.Errorf("read hermes memory store %s: %w", storeDir, err)
	}

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return fmt.Errorf("read task-local hermes memories %s: %w", taskDir, err)
	}
	if len(entries) == 0 {
		return nil
	}

	staging, err := newHermesStoreStaging(storeDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging) // no-op once the staging dir has been promoted

	for _, entry := range entries {
		src := filepath.Join(taskDir, entry.Name())
		if err := copyHermesMemoryEntry(src, filepath.Join(staging, entry.Name()), entry); err != nil {
			return fmt.Errorf("migrate task-local hermes memory %s into %s: %w", src, storeDir, err)
		}
	}

	promoted, err := promoteHermesStoreStaging(staging, storeDir, hermesStorePopulated)
	if err != nil {
		return err
	}
	if !promoted {
		logger.Info("execenv: another task populated the hermes memory store first; keeping it", "store", storeDir)
		return nil
	}
	logger.Info("execenv: migrated task-local hermes memories into agent store", "store", storeDir, "entries", len(entries))
	return nil
}

// newHermesStoreStaging creates the scratch directory a store migration copies
// into. It is a sibling of the store so the promoting rename stays within one
// filesystem, and dot-prefixed so it is obvious it is not a store of its own.
// Shared by the memory migration here and the session-database migration in
// hermes_sessions.go: both delete their source once the copy reports success,
// so both need the same all-or-nothing publish.
// MkdirTemp already creates it 0700, the same mode the store itself is created
// with, so there is no follow-up chmod that could fail and strand it.
func newHermesStoreStaging(storeDir string) (string, error) {
	parent := filepath.Dir(storeDir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create hermes memory store parent %s: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, "."+filepath.Base(storeDir)+".migrating-")
	if err != nil {
		return "", fmt.Errorf("create hermes memory staging dir in %s: %w", parent, err)
	}
	return staging, nil
}

// promoteHermesStoreStaging publishes a fully-copied staging dir as the store,
// reporting whether this caller won. The empty store dir (if any) is removed
// first: os.Remove only succeeds on an empty directory, so a store another task
// already populated makes this a no-op, and Windows rejects a rename onto an
// existing directory outright.
//
// Losing the race is not an error — the winner's store holds the same agent's
// state — but it has to be positively confirmed, never inferred from any
// failure. The caller deletes the source directory whenever this returns
// (false, nil), so a permission error, a read-only filesystem or a Windows
// sharing violation must fail closed instead of passing for "someone else
// published".
//
// published answers "did a competitor already publish real state here?" and is
// the caller's to define, because the two stores disagree about what an
// occupied directory looks like: any entry at all means memory, while a session
// store can hold a zero-length database or orphan journal sidecars that carry no
// transcript. Sharing one definition let a session migration read the store as
// empty, copy into staging, then read it as occupied at publish time and report
// a lost race — after which the caller deleted a source database that had never
// been carried over.
func promoteHermesStoreStaging(staging, storeDir string, published func(string) bool) (bool, error) {
	if err := os.Remove(storeDir); err != nil && !os.IsNotExist(err) {
		if published(storeDir) {
			return false, nil // another task published first
		}
		return false, fmt.Errorf("clear empty hermes store %s before publishing: %w", storeDir, err)
	}
	if err := os.Rename(staging, storeDir); err != nil {
		if published(storeDir) {
			return false, nil // lost a narrow race between the remove and the rename
		}
		return false, fmt.Errorf("publish hermes store %s: %w", storeDir, err)
	}
	return true, nil
}

// hermesStorePopulated reports whether storeDir is *confirmed* to hold
// entries. Anything it cannot confirm — an unreadable directory, a path that is
// not a directory — is false, so a caller asking "did another task publish?"
// treats uncertainty as no.
func hermesStorePopulated(storeDir string) bool {
	entries, err := os.ReadDir(storeDir)
	return err == nil && len(entries) > 0
}

// copyHermesMemoryEntry copies one entry of a memories dir into the staging
// tree. Anything that is not a regular file or a directory is refused rather
// than skipped: the caller deletes the source on success, so a silent skip
// would be a silent loss.
func copyHermesMemoryEntry(src, dst string, entry os.DirEntry) error {
	switch {
	case entry.IsDir():
		return copyHermesMemoryTree(src, dst)
	case entry.Type().IsRegular():
		return copyFile(src, dst)
	default:
		return fmt.Errorf("refusing to migrate %s: unsupported entry type %s", src, entry.Type())
	}
}

// copyHermesMemoryTree copies a directory under a memories dir recursively.
// It refuses (rather than silently skips) nested symlinks and other irregular
// files: this copy is the only thing standing between the source and its
// deletion.
func copyHermesMemoryTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			return copyFile(path, target)
		default:
			return fmt.Errorf("refusing to migrate %s: unsupported entry type %s", path, d.Type())
		}
	})
}

// detachHermesMemories gives the overlay a real, task-local memories dir. It is
// the path taken when there is no store to key on (no agent, or an unresolvable
// Multica profile dir), so it must actively replace a link left by a previous
// run against the persistent store: a reused overlay still carries that link,
// and MkdirAll would follow it and silently succeed, leaving the task writing to
// a store the daemon no longer guards from the GC. An existing real directory is
// kept — that is this task's own memory.
func detachHermesMemories(hermesHome string) error {
	dst := filepath.Join(hermesHome, hermesMemoriesEntry)
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 && fi.IsDir() {
			return nil
		}
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("detach memories path %s: %w", dst, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat memories path %s: %w", dst, err)
	}
	return os.MkdirAll(dst, 0o700)
}

// touchHermesMemoryStore refreshes storeDir's mtime — the signal the GC reads as
// last activity. Best-effort: a failed touch only risks an over-eager prune,
// which the daemon's active-store guard still prevents.
func touchHermesMemoryStore(storeDir string, logger *slog.Logger) {
	now := time.Now()
	if err := os.Chtimes(storeDir, now, now); err != nil {
		logger.Warn("execenv: refresh hermes memory store activity failed", "store", storeDir, "error", err)
	}
}

// PruneHermesMemoryStores reclaims per-agent Hermes memory stores untouched
// within retention. The stores live outside the task-scoped envRoot the task GC
// reclaims — that is the point, memory has to outlive the task — so without this
// a deleted agent's memory would sit on disk forever. retention <= 0 disables
// pruning entirely.
//
// The default retention is deliberately long: unlike Codex rollouts (gigabytes
// of transcript) these are a handful of markdown files, and reclaiming an
// agent's memory is user-visible amnesia, not just a cache miss.
//
// reserve (may be nil) atomically claims a store for deletion, exactly as
// PruneCodexSessionStores uses it: ok=false means a live task holds the store,
// so it is left alone. nil disables the guard (tests).
func PruneHermesMemoryStores(daemonProfile string, retention time.Duration, now time.Time, reserve func(storeDir string) (commit func(), ok bool), logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return 0, 0
	}
	root := filepath.Join(profileDir, hermesMemoryStoreRoot)
	agents, err := os.ReadDir(root)
	if err != nil {
		return 0, 0 // not created yet, or unreadable — nothing to prune
	}
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		agentDir := filepath.Join(root, a.Name())
		profiles, err := os.ReadDir(agentDir)
		if err != nil {
			continue
		}
		kept := 0
		for _, p := range profiles {
			if !p.IsDir() {
				continue
			}
			storeDir := filepath.Join(agentDir, p.Name())
			newest, size := hermesMemoryStoreStat(storeDir)
			if newest.IsZero() || now.Sub(newest) <= retention {
				kept++
				continue
			}
			var commit func()
			if reserve != nil {
				c, ok := reserve(storeDir)
				if !ok {
					kept++
					continue
				}
				commit = c
			}
			err := os.RemoveAll(storeDir)
			if commit != nil {
				commit()
			}
			if err != nil {
				logger.Warn("execenv: prune hermes memory store failed", "store", storeDir, "error", err)
				kept++
				continue
			}
			removed++
			bytesFreed += size
		}
		// Drop the agent dir once its last profile store is gone, so the tree
		// does not leave empty <agent>/ shells behind.
		if kept == 0 {
			_ = os.Remove(agentDir)
		}
	}
	return removed, bytesFreed
}

// hermesMemoryStoreStat walks dir once, returning the newest modification time
// seen (the store's last activity) and its total byte size (for GC accounting).
func hermesMemoryStoreStat(dir string) (newest time.Time, size int64) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		if !d.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return newest, size
}
