package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Background
//
// Hermes persists every ACP session — the actual conversation transcript — in
// the SQLite database at `<HERMES_HOME>/state.db` (acp_adapter/session.py).
// Because Multica has to take over HERMES_HOME to inject bound skills, that
// database landed inside the per-task overlay, which made a Hermes agent's
// conversation last exactly one task.
//
// The failure was silent, which is what made it expensive. `session/resume`
// against a database that does not hold the session does not fail: Hermes
// creates a fresh session and answers with an ordinary success frame
// (acp_adapter/server.py resume_session), so the daemon believed it had
// resumed, re-sent the same dead id on every later turn, and the user saw a
// conversation that restarted from zero with nothing in the logs (GH #6806).
// The backend now detects that rebind (resolveHermesResumedSessionID), and
// this file removes the cause.
//
// The store is keyed by (agent, hermes profile, conversation):
//
//	<multica profile dir>/hermes-sessions/<agent>/<hermes profile>/<conversation>/state.db
//
// Keying to the conversation — the issue, or `chat_<id>` for a chat session —
// is what makes this safe to share where #6693 could not. Its stated reason
// for leaving state.db task-local was concurrency: one agent's parallel tasks
// would write one live SQLite database, which needs lock and consistency work
// (plus Windows byte-range locks on `-shm`) that plain memory files do not.
// Tasks of the SAME conversation are serial by construction — a follow-up is
// dispatched after its predecessor reports — so the shard has one writer at a
// time, while two different issues get two different databases and never meet.
//
// The (agent, profile) prefix matches the memory store's scoping exactly, so
// pointing an agent at another Hermes profile gives it a fresh conversation
// line just as it gives it a fresh memory line. It is a sibling root rather
// than a subdirectory of `hermes-state/<agent>/<profile>` because that
// directory IS the agent's memories dir (the overlay links `memories/`
// straight at it), so nesting sessions inside it would surface them to Hermes
// as a memory entry.
//
// The overlay mounts the shard by symlinking `<overlay>/state.db` at
// `<store>/state.db`. SQLite resolves the link when it derives the `-wal` and
// `-shm` paths, so the journal sidecars are created in the store next to the
// database rather than in the task directory, and a task teardown can never
// take the write-ahead log with it.

// hermesSessionStoreRoot is the directory under the daemon's Multica profile
// dir that holds every agent's persistent Hermes conversation shards.
const hermesSessionStoreRoot = "hermes-sessions"

// hermesSessionDBEntry is the database file name inside both the store and the
// overlay home — Hermes resolves it relative to HERMES_HOME.
const hermesSessionDBEntry = "state.db"

// HermesSessionStorePath returns the persistent session store for
// (daemonProfile, agentID, sourceHome, conversation), or "" when the session
// database must stay task-local — no agent to key on, no conversation to key
// on (neither an issue nor a chat session), or an unresolvable Multica profile
// dir. The daemon marks the returned path in-use for the task's duration so
// PruneHermesSessionStores never reclaims it mid-mount.
func HermesSessionStorePath(daemonProfile, agentID, sourceHome string, task TaskContextForEnv) string {
	agent := sanitizePathSegment(agentID)
	if agent == "" {
		return ""
	}
	conversation := hermesConversationSegment(task)
	if conversation == "" {
		return ""
	}
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return ""
	}
	return filepath.Join(profileDir, hermesSessionStoreRoot, agent,
		hermesMemoryProfileSegment(sourceHome), conversation)
}

// hermesConversationSegment maps a task to the conversation its transcript
// belongs to: the issue it runs on, or the chat session when there is no
// issue. Same derivation as the per-issue Codex session store
// (codexSessionStoreKey), so both providers agree on what "one conversation"
// means. Returns "" for a task that belongs to neither, which keeps its
// session database task-local rather than inventing a shard nothing will
// resume.
func hermesConversationSegment(task TaskContextForEnv) string {
	if issue := sanitizePathSegment(task.IssueID); issue != "" {
		return issue
	}
	if chat := sanitizePathSegment(task.ChatSessionID); chat != "" {
		return "chat_" + chat
	}
	return ""
}

// hermesSessionMount is what a mount attempt actually achieved.
//
// The two facts are deliberately separate. Mounted says state.db resolves to
// the store; HistoryPresent says the store holds a database to resume. A
// mounted-but-empty store is the normal shape of a conversation's first turn,
// and also what a GC reclaim, a profile switch or an operator's `rm` leaves
// behind — treating "mounted" as "resumable" would forward a session id into an
// empty database, which is the silent restart this whole change exists to kill.
type hermesSessionMount struct {
	Mounted        bool
	HistoryPresent bool
}

// hermesSessionLink creates the store link. Indirected so a test can simulate a
// host that cannot create symlinks — the degradation path is the one that must
// never destroy a database, so it needs coverage everywhere, not only on
// Windows.
var hermesSessionLink = os.Symlink

// hermesSessionCopy copies one file of a migration. Indirected so a test can
// fail the second file of the state.db family and prove the store is never left
// holding a partially migrated conversation.
var hermesSessionCopy = copyFile

// mountHermesSessionDB points the overlay's state.db at the conversation's
// store, so the transcript outlives the task.
//
// The order of operations is the contract. Nothing that could destroy an
// existing database happens until the link has actually been created (under a
// staging name), because the alternative — migrate, delete, then discover this
// host cannot symlink — turns a working task-local conversation into an empty
// one on exactly the hosts the degradation path exists to protect. A false
// Mounted with a nil error therefore means "nothing was touched; keep the
// database task-local", which is the pre-existing behaviour.
//
// Idempotent across Reuse: a link already pointing at the store is left alone.
// A real state.db left by an older daemon — or by a task that ran before this
// conversation had a store — is migrated into an empty store rather than
// dropped, so upgrading mid-conversation does not discard the transcript the
// user is in the middle of.
func mountHermesSessionDB(hermesHome, storeDir string, logger *slog.Logger) (hermesSessionMount, error) {
	dst := filepath.Join(hermesHome, hermesSessionDBEntry)
	target := filepath.Join(storeDir, hermesSessionDBEntry)

	if err := os.MkdirAll(storeDir, 0o700); err != nil {
		return hermesSessionMount{}, fmt.Errorf("create hermes session store %s: %w", storeDir, err)
	}

	fi, err := os.Lstat(dst)
	switch {
	case err == nil && fi.Mode()&os.ModeSymlink != 0:
		if current, rlErr := os.Readlink(dst); rlErr == nil && filepath.Clean(current) == filepath.Clean(target) {
			// Already mounted (Reuse). The link can still be dangling — the GC
			// may have reclaimed the store since the last turn — so the history
			// question is answered from the store, never from the link.
			touchHermesSessionStore(storeDir, logger)
			return hermesSessionMount{Mounted: true, HistoryPresent: hermesStoreHasSessionDB(storeDir)}, nil
		}
	case err != nil && !os.IsNotExist(err):
		return hermesSessionMount{}, fmt.Errorf("stat hermes session db %s: %w", dst, err)
	}

	// Prove the host can link BEFORE anything destructive. A real symlink,
	// never a copy: createFileLink falls back to copying on a Windows host that
	// cannot symlink, and a copied SQLite database would absorb every write of
	// this turn into a file the next task throws away — worse than staying
	// task-local, because it looks like it worked.
	staged := filepath.Join(hermesHome, hermesSessionLinkStagingEntry)
	if err := os.RemoveAll(staged); err != nil {
		return hermesSessionMount{}, fmt.Errorf("clear stale session link staging %s: %w", staged, err)
	}
	if err := hermesSessionLink(target, staged); err != nil {
		logger.Warn("execenv: hermes session store not mounted; conversation history stays task-local",
			"store", storeDir,
			"error", err,
		)
		return hermesSessionMount{}, nil
	}
	defer os.RemoveAll(staged) // no-op once the staging link has been renamed into place

	if err := migrateHermesTaskSessionDB(hermesHome, storeDir, logger); err != nil {
		return hermesSessionMount{}, err
	}
	if err := removeHermesSessionDBFamily(hermesHome); err != nil {
		return hermesSessionMount{}, err
	}
	// Publish the link. Fails closed: the database is already safe in the store
	// by this point, so a rename failure means a broken filesystem, not lost
	// history, and the task must not start against a home with no state.db path
	// at all.
	if err := os.Rename(staged, dst); err != nil {
		return hermesSessionMount{}, fmt.Errorf("publish hermes session link %s: %w", dst, err)
	}
	// Stamp the store as just-used: mounting it does not touch its mtime, so
	// without this the GC's idle check could reclaim a long-idle conversation
	// right as a task picks it back up.
	touchHermesSessionStore(storeDir, logger)
	return hermesSessionMount{Mounted: true, HistoryPresent: hermesStoreHasSessionDB(storeDir)}, nil
}

// hermesSessionLinkStagingEntry is where the store link is created before it is
// published over state.db. Dot-prefixed so isHermesTaskLocalStateEntry does not
// match it and the family cleanup cannot remove the link it is about to
// publish.
const hermesSessionLinkStagingEntry = ".multica-session-link"

// hermesStoreHasSessionDB reports whether storeDir holds a session database
// with content. A zero-length file is what SQLite leaves after an `open` that
// never wrote a page, and resuming against it is the same amnesia as an absent
// one — so it counts as no history, not as history.
func hermesStoreHasSessionDB(storeDir string) bool {
	fi, err := os.Stat(filepath.Join(storeDir, hermesSessionDBEntry))
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// migrateHermesTaskSessionDB carries a task-local state.db into an empty store
// so a daemon upgrade does not restart the conversation that is already in
// flight. The journal sidecars come with it: a database whose task was killed
// mid-write holds its most recent commits in `-wal`, and carrying the database
// alone would silently truncate the transcript to the last checkpoint.
//
// The whole family is copied into a staging directory and published with one
// rename, exactly as the memory store's migration does, because the caller
// deletes the source as soon as this returns nil. Copying straight into the
// store would let a failure after the main database land leave a store that
// looks migrated: the next attempt would skip the migration, delete the source,
// and take the un-checkpointed tail of the conversation with it.
//
// Only an empty store is migrated into — a store that already holds a database
// is this conversation's real history and must never be overwritten.
func migrateHermesTaskSessionDB(hermesHome, storeDir string, logger *slog.Logger) error {
	if hermesStoreHasSessionDB(storeDir) {
		return nil // store already holds this conversation — never overwrite it
	}

	entries, err := os.ReadDir(hermesHome)
	if err != nil {
		return fmt.Errorf("read overlay home %s: %w", hermesHome, err)
	}
	family := make([]string, 0, 3)
	for _, entry := range entries {
		if isHermesTaskLocalStateEntry(entry.Name()) && entry.Type().IsRegular() {
			family = append(family, entry.Name())
		}
	}
	if len(family) == 0 {
		return nil
	}

	staging, err := newHermesStoreStaging(storeDir)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging) // no-op once the staging dir has been promoted

	for _, name := range family {
		if err := hermesSessionCopy(filepath.Join(hermesHome, name), filepath.Join(staging, name)); err != nil {
			return fmt.Errorf("migrate task-local hermes session db %s into %s: %w", name, storeDir, err)
		}
	}

	promoted, err := publishHermesSessionStaging(staging, storeDir)
	if err != nil {
		return err
	}
	if !promoted {
		logger.Info("execenv: another task published this conversation's session store first; keeping it", "store", storeDir)
		return nil
	}
	logger.Info("execenv: migrated task-local hermes session db into conversation store",
		"store", storeDir,
		"files", len(family),
	)
	return nil
}

// publishHermesSessionStaging publishes a fully-copied staging dir as the
// conversation's store, reporting whether this caller won.
//
// It exists because "occupied" means something different here than it does for
// the memory store. A session store can hold state.db family remnants that
// carry no transcript at all — a zero-length database from an `open` that never
// wrote a page, or journal sidecars orphaned by a killed task — and the
// migration above has already decided those are not history. The generic
// publish would disagree: it reads any entry as a competitor's published state,
// report a lost race, and the caller would then delete a source database that
// was never carried over. So the remnants are cleared first (they are, by the
// definition this file uses everywhere, nothing), and the occupancy question is
// answered with the same predicate the migration asked.
//
// A store that cannot be cleared is NOT reported as a lost race: the shared
// promote fails closed, the migration returns the error, and the caller keeps
// the source database.
//
// The check, the cleanup and the promote hold hermesSessionPublishMu as one
// step. Deciding the remnants are nothing and then deleting them by path name
// is only sound while nothing can replace them in between: without the lock, a
// competitor could publish a real transcript after this caller's check, and the
// cleanup would delete the winner's database as a remnant — after the winner
// already reported success and discarded its source.
func publishHermesSessionStaging(staging, storeDir string) (bool, error) {
	hermesSessionPublishMu.Lock()
	defer hermesSessionPublishMu.Unlock()
	if hermesStoreHasSessionDB(storeDir) {
		return false, nil // a competitor published a real transcript first
	}
	if hermesSessionPublishBarrier != nil {
		hermesSessionPublishBarrier(storeDir)
	}
	if err := removeHermesSessionDBFamily(storeDir); err != nil {
		return false, err
	}
	return promoteHermesStoreStaging(staging, storeDir, hermesStoreHasSessionDB)
}

// hermesSessionPublishMu serializes every session-store publish in this
// process. In-process mutual exclusion is the real thing here, not an
// approximation: the daemon is the store tree's only writer — tasks, the
// migration and even the GC's reservation guard (Daemon.activeStores) all live
// in one process — so two publishers can only ever be two goroutines of this
// daemon. One process-wide mutex rather than a per-store map because a publish
// happens at most once per conversation (the first mount that finds a
// task-local database) and its critical section is a stat, a few unlinks and a
// rename; contention is not a factor worth bookkeeping for.
var hermesSessionPublishMu sync.Mutex

// hermesSessionPublishBarrier, when non-nil, runs inside the locked region
// between the occupancy check and the remnant cleanup — the exact window the
// lock exists to close. Tests park a publisher here to prove a competitor
// cannot interleave a publish into that window. Always nil in production.
var hermesSessionPublishBarrier func(storeDir string)

// removeHermesSessionDBFamily clears a directory's state.db family. Called
// once the database is either safely in the store or deliberately being
// replaced by the link; leaving a stale `-wal` next to the link would be
// dead weight SQLite never reads (it derives the sidecar paths from the
// resolved store path) and a confusing thing to find in a task directory.
//
// Only entries this code can identify are removed, and never recursively: a
// regular file is SQLite's, and a symlink is a prior mount's store link (left
// when the store path changed under a live overlay, e.g. a profile switch
// mid-conversation) — os.Remove unlinks it without touching its target. A
// directory or any other irregular type under a state.db name is not a
// database remnant this code recognises, and deleting content it cannot
// identify is exactly the failure this file exists to prevent, so it fails
// closed and the caller keeps its source.
func removeHermesSessionDBFamily(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !isHermesTaskLocalStateEntry(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if t := entry.Type(); !t.IsRegular() && t&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to remove hermes session state %s: unexpected %s under a state.db name", path, t)
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove task-local hermes session state %s: %w", path, err)
		}
	}
	return nil
}

// touchHermesSessionStore refreshes storeDir's mtime — the signal the GC reads
// as last activity. Best-effort: a failed touch only risks an over-eager
// prune, which the daemon's active-store guard still prevents.
func touchHermesSessionStore(storeDir string, logger *slog.Logger) {
	now := time.Now()
	if err := os.Chtimes(storeDir, now, now); err != nil {
		logger.Warn("execenv: refresh hermes session store activity failed", "store", storeDir, "error", err)
	}
}

// PruneHermesSessionStores reclaims per-conversation Hermes session stores
// untouched within retention. They live outside the task-scoped envRoot the
// task GC reclaims — that is the point, the transcript has to outlive the
// task — so without this a finished issue's conversation would sit on disk
// forever and deleting the issue or the agent would not remove it.
// retention <= 0 disables pruning entirely.
//
// Retention is shorter than the memory store's by design: this holds full
// transcripts rather than a handful of markdown notes, and losing an idle
// conversation's replay is a resume that starts fresh (with a continuity
// notice), not the agent forgetting what it learned.
//
// reserve (may be nil) atomically claims a store for deletion exactly as
// PruneCodexSessionStores uses it: ok=false means a live task holds the store,
// so it is left alone. nil disables the guard (tests).
func PruneHermesSessionStores(daemonProfile string, retention time.Duration, now time.Time, reserve func(storeDir string) (commit func(), ok bool), logger *slog.Logger) (removed int, bytesFreed int64) {
	if retention <= 0 {
		return 0, 0
	}
	profileDir, err := cli.ProfileDir(daemonProfile)
	if err != nil {
		return 0, 0
	}
	root := filepath.Join(profileDir, hermesSessionStoreRoot)
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
		keptProfiles := 0
		for _, p := range profiles {
			if !p.IsDir() {
				continue
			}
			profileStoreDir := filepath.Join(agentDir, p.Name())
			conversations, err := os.ReadDir(profileStoreDir)
			if err != nil {
				keptProfiles++
				continue
			}
			kept := 0
			for _, conv := range conversations {
				if !conv.IsDir() {
					continue
				}
				storeDir := filepath.Join(profileStoreDir, conv.Name())
				// Same "newest mtime is last activity, plus total size" walk the
				// memory store prunes on; SQLite bumps the database's mtime on
				// every commit, so an advancing conversation keeps its shard fresh.
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
					logger.Warn("execenv: prune hermes session store failed", "store", storeDir, "error", err)
					kept++
					continue
				}
				removed++
				bytesFreed += size
			}
			// Drop the profile dir once its last conversation is gone, then the
			// agent dir once its last profile is, so the tree does not leave
			// empty shells behind. os.Remove only succeeds on an empty
			// directory, so anything still holding content keeps its parents.
			if kept > 0 {
				keptProfiles++
				continue
			}
			if err := os.Remove(profileStoreDir); err != nil {
				keptProfiles++
			}
		}
		if keptProfiles == 0 {
			_ = os.Remove(agentDir)
		}
	}
	return removed, bytesFreed
}
