package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestHermesSessionStorePathLayout pins the on-disk layout an operator (and
// the GC below) depends on:
// <profile dir>/hermes-sessions/<agent>/<hermes profile>/<conversation>.
func TestHermesSessionStorePathLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	agent := "11111111-2222-3333-4444-555555555555"
	issue := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	got := HermesSessionStorePath("", agent, filepath.Join(platformDefaultHermesHome(), "profiles", "research"),
		TaskContextForEnv{AgentID: agent, IssueID: issue})
	want := filepath.Join(home, ".multica", hermesSessionStoreRoot, agent, "research", issue)
	if got != want {
		t.Fatalf("store path = %q, want %q", got, want)
	}
}

// TestHermesSessionStorePathScoping covers which tasks get a shard at all, and
// that two conversations of one agent never share one.
func TestHermesSessionStorePathScoping(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	const agent = "agent-1"
	issueA := HermesSessionStorePath("", agent, "", TaskContextForEnv{IssueID: "issue-a"})
	issueB := HermesSessionStorePath("", agent, "", TaskContextForEnv{IssueID: "issue-b"})
	if issueA == "" || issueA == issueB {
		t.Fatalf("two issues must get distinct stores, got %q and %q", issueA, issueB)
	}

	chat := HermesSessionStorePath("", agent, "", TaskContextForEnv{ChatSessionID: "sess-1"})
	if filepath.Base(chat) != "chat_sess-1" {
		t.Fatalf("chat conversation segment = %q, want chat_sess-1", filepath.Base(chat))
	}
	// An issue task and a chat task can never collide even on equal ids.
	if same := HermesSessionStorePath("", agent, "", TaskContextForEnv{IssueID: "sess-1"}); same == chat {
		t.Fatalf("issue and chat conversations collided on %q", chat)
	}

	// No agent, or no conversation, means no store: the session database stays
	// task-local rather than landing in a shard nothing will resume.
	if got := HermesSessionStorePath("", "", "", TaskContextForEnv{IssueID: "issue-a"}); got != "" {
		t.Fatalf("store path without an agent = %q, want empty", got)
	}
	if got := HermesSessionStorePath("", agent, "", TaskContextForEnv{}); got != "" {
		t.Fatalf("store path without a conversation = %q, want empty", got)
	}
}

// TestPrepareHermesHomeSessionStorePersistsAcrossTasks is the regression test
// for GH #6806: two tasks of one conversation must see the same state.db, so a
// resume can actually find the transcript. Each task builds a fresh overlay, so
// this can only hold if state.db resolves to the conversation's store.
func TestPrepareHermesHomeSessionStorePersistsAcrossTasks(t *testing.T) {
	requireSymlinks(t)
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "hermes-sessions", "agent-1", "default", "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	firstTask := filepath.Join(t.TempDir(), "hermes-home")
	sessions, err := prepareHermesHome(firstTask, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare first task: %v", err)
	}
	if !sessions.Mounted {
		t.Fatal("first task reported the session store as not mounted")
	}
	if sessions.HistoryPresent {
		t.Fatal("HistoryPresent = true on a conversation's first turn, want false")
	}
	// Hermes creates state.db lazily and writes the transcript into it.
	mustWrite(t, filepath.Join(firstTask, "state.db"), "transcript")

	secondTask := filepath.Join(t.TempDir(), "hermes-home")
	if _, err := prepareHermesHome(secondTask, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("prepare second task: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(secondTask, "state.db"))
	if err != nil {
		t.Fatalf("second task lost the conversation transcript: %v", err)
	}
	if string(got) != "transcript" {
		t.Fatalf("transcript = %q, want %q", got, "transcript")
	}

	// It must be a link into the store, not a copy — a copy would take this
	// turn's writes and throw them away with the task directory.
	fi, err := os.Lstat(filepath.Join(secondTask, "state.db"))
	if err != nil {
		t.Fatalf("lstat state.db: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("state.db is not a link (mode %v)", fi.Mode())
	}
}

// TestPrepareHermesHomeSessionStoreIsolatesConversations guards the property
// that makes sharing safe at all: one conversation's transcript is never
// visible to another, so two issues of the same agent cannot write one database.
func TestPrepareHermesHomeSessionStoreIsolatesConversations(t *testing.T) {
	requireSymlinks(t)
	t.Parallel()
	sharedHome := t.TempDir()
	storeRoot := t.TempDir()
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	homeA := filepath.Join(t.TempDir(), "hermes-home")
	if _, err := prepareHermesHome(homeA, sharedHome, false, skills, nil, "",
		filepath.Join(storeRoot, "agent-1", "default", "issue-a"), testLogger()); err != nil {
		t.Fatalf("prepare conversation A: %v", err)
	}
	mustWrite(t, filepath.Join(homeA, "state.db"), "issue A transcript")

	homeB := filepath.Join(t.TempDir(), "hermes-home")
	if _, err := prepareHermesHome(homeB, sharedHome, false, skills, nil, "",
		filepath.Join(storeRoot, "agent-1", "default", "issue-b"), testLogger()); err != nil {
		t.Fatalf("prepare conversation B: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(homeB, "state.db")); !os.IsNotExist(err) {
		t.Fatalf("conversation B can see another conversation's transcript (err = %v)", err)
	}
}

// TestPrepareHermesHomeWithoutSessionStoreKeepsStateTaskLocal covers the
// no-store path (no agent or no conversation to key on): state.db must stay an
// ordinary task-local file, exactly as before.
func TestPrepareHermesHomeWithoutSessionStoreKeepsStateTaskLocal(t *testing.T) {
	t.Parallel()
	sharedHome := t.TempDir()
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}

	sessions, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger())
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if sessions.Mounted {
		t.Fatal("Mounted = true without a session store")
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
	fi, err := os.Lstat(filepath.Join(hermesHome, "state.db"))
	if err != nil {
		t.Fatalf("lstat state.db: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatal("state.db is a link with no store to point at")
	}
}

// TestPrepareHermesHomeMigratesTaskLocalSessionDB covers the upgrade path: a
// conversation already in flight when the daemon gains a session store must not
// restart. The journal sidecars come along, or the last commits would be lost.
func TestPrepareHermesHomeMigratesTaskLocalSessionDB(t *testing.T) {
	requireSymlinks(t)
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	// A task-local database left by an older daemon.
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store task: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
	mustWrite(t, filepath.Join(hermesHome, "state.db-wal"), "uncheckpointed tail")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("prepare with store: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(store, "state.db"))
	if err != nil {
		t.Fatalf("in-flight transcript was dropped: %v", err)
	}
	if string(got) != "transcript" {
		t.Fatalf("migrated transcript = %q, want %q", got, "transcript")
	}
	wal, err := os.ReadFile(filepath.Join(store, "state.db-wal"))
	if err != nil {
		t.Fatalf("write-ahead log was dropped: %v", err)
	}
	if string(wal) != "uncheckpointed tail" {
		t.Fatalf("migrated wal = %q, want %q", wal, "uncheckpointed tail")
	}
}

// TestPrepareHermesHomeMigrationNeverOverwritesStore is the other half of the
// migration contract: a store that already holds this conversation's history is
// the real transcript and a stray task-local database must not replace it.
func TestPrepareHermesHomeMigrationNeverOverwritesStore(t *testing.T) {
	requireSymlinks(t)
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	mustWrite(t, filepath.Join(store, "state.db"), "the real transcript")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store task: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "a stray task-local db")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("prepare with store: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(store, "state.db"))
	if err != nil {
		t.Fatalf("read store db: %v", err)
	}
	if string(got) != "the real transcript" {
		t.Fatalf("store db = %q, want the pre-existing transcript", got)
	}
}

// TestPrepareHermesHomeSessionMountIsIdempotent covers Reuse: rebuilding the
// overlay for a follow-up turn in the same task directory must leave the link
// (and therefore the live database) alone.
func TestPrepareHermesHomeSessionMountIsIdempotent(t *testing.T) {
	requireSymlinks(t)
	t.Parallel()
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("reuse prepare: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(store, "state.db"))
	if err != nil {
		t.Fatalf("reuse dropped the transcript: %v", err)
	}
	if string(got) != "transcript" {
		t.Fatalf("transcript = %q, want %q", got, "transcript")
	}
}

func TestPruneHermesSessionStores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := filepath.Join(home, ".multica", hermesSessionStoreRoot)
	idle := filepath.Join(root, "agent-1", "default", "issue-idle")
	fresh := filepath.Join(root, "agent-1", "default", "issue-fresh")
	held := filepath.Join(root, "agent-2", "default", "issue-held")
	for _, dir := range []string{idle, fresh, held} {
		mustWrite(t, filepath.Join(dir, "state.db"), "transcript")
	}

	now := time.Now()
	old := now.Add(-30 * 24 * time.Hour)
	for _, dir := range []string{idle, held} {
		if err := os.Chtimes(filepath.Join(dir, "state.db"), old, old); err != nil {
			t.Fatalf("age store: %v", err)
		}
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("age store dir: %v", err)
		}
	}

	reserve := func(storeDir string) (func(), bool) {
		if storeDir == held {
			return nil, false // a live task holds it
		}
		return func() {}, true
	}

	removed, freed := PruneHermesSessionStores("", 14*24*time.Hour, now, reserve, testLogger())
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if freed <= 0 {
		t.Fatalf("bytesFreed = %d, want > 0", freed)
	}
	if _, err := os.Stat(idle); !os.IsNotExist(err) {
		t.Fatalf("idle conversation survived the prune (err = %v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("recently used conversation was reclaimed: %v", err)
	}
	if _, err := os.Stat(held); err != nil {
		t.Fatalf("conversation held by a live task was reclaimed: %v", err)
	}
}

func TestPruneHermesSessionStoresDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	store := filepath.Join(home, ".multica", hermesSessionStoreRoot, "agent-1", "default", "issue-1")
	mustWrite(t, filepath.Join(store, "state.db"), "transcript")
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(store, old, old); err != nil {
		t.Fatalf("age store: %v", err)
	}

	if removed, _ := PruneHermesSessionStores("", 0, time.Now(), nil, testLogger()); removed != 0 {
		t.Fatalf("removed = %d with retention disabled, want 0", removed)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("store was reclaimed with retention disabled: %v", err)
	}
}

// TestPrepareHermesHomeLinkFailureKeepsTaskLocalDB is the degradation contract:
// on a host that cannot symlink, the mount must leave the task-local database
// exactly where it found it. Getting the order wrong — migrate, delete, then
// discover the link cannot be created — would turn a conversation that worked
// via workdir reuse into an empty one, i.e. a regression on precisely the hosts
// the fallback exists to protect.
//
// The failure is injected rather than skipped: this path must be covered on
// every host, not only on a Windows box without symlink privileges.
func TestPrepareHermesHomeLinkFailureKeepsTaskLocalDB(t *testing.T) {
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store task: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
	mustWrite(t, filepath.Join(hermesHome, "state.db-wal"), "uncheckpointed tail")

	restore := stubHermesSessionLink(t, func(string, string) error {
		return errors.New("symlink privilege not held")
	})

	sessions, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare with unlinkable store: %v", err)
	}
	if sessions.Mounted || sessions.HistoryPresent {
		t.Fatalf("mount = %+v, want both false when the link could not be created", sessions)
	}

	// The database — and its un-checkpointed tail — must still be here.
	for name, want := range map[string]string{
		"state.db":     "transcript",
		"state.db-wal": "uncheckpointed tail",
	} {
		got, err := os.ReadFile(filepath.Join(hermesHome, name))
		if err != nil {
			t.Fatalf("%s was destroyed by a failed mount: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if entries, err := os.ReadDir(store); err != nil {
		t.Fatalf("read store: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("store holds %d entries after a failed mount, want 0", len(entries))
	}

	// And once the host can link again, the same overlay mounts and carries the
	// database over — nothing was lost in between.
	restore()
	sessions, err = prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare after link support returns: %v", err)
	}
	if !sessions.Mounted || !sessions.HistoryPresent {
		t.Fatalf("mount = %+v, want mounted with history after recovery", sessions)
	}
	got, err := os.ReadFile(filepath.Join(store, "state.db"))
	if err != nil || string(got) != "transcript" {
		t.Fatalf("store db = %q (err %v), want the preserved transcript", got, err)
	}
}

// TestPrepareHermesHomeMigrationIsAtomic covers a migration that dies between
// the main database and its write-ahead log. Copying straight into the store
// would leave a store that LOOKS migrated: the next attempt would skip the
// migration, delete the source, and take the un-checkpointed tail of the
// conversation with it. The store must stay empty until the whole family is
// there, and the retry must carry everything.
func TestPrepareHermesHomeMigrationIsAtomic(t *testing.T) {
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
		t.Fatalf("prepare pre-store task: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
	mustWrite(t, filepath.Join(hermesHome, "state.db-wal"), "uncheckpointed tail")

	restore := stubHermesSessionCopy(t, func(src, dst string) error {
		if filepath.Base(src) == "state.db-wal" {
			return errors.New("disk full")
		}
		return copyFile(src, dst)
	})

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err == nil {
		t.Fatal("prepare succeeded despite a failed migration, want an error")
	}

	// Nothing published, nothing deleted.
	if hermesStoreHasSessionDB(store) {
		t.Fatal("store holds a database after a half-failed migration")
	}
	for _, name := range []string{"state.db", "state.db-wal"} {
		if _, err := os.Stat(filepath.Join(hermesHome, name)); err != nil {
			t.Fatalf("source %s was removed after a failed migration: %v", name, err)
		}
	}

	restore()
	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("retry after a failed migration: %v", err)
	}
	for name, want := range map[string]string{
		"state.db":     "transcript",
		"state.db-wal": "uncheckpointed tail",
	} {
		got, err := os.ReadFile(filepath.Join(store, name))
		if err != nil {
			t.Fatalf("retry did not carry %s over: %v", name, err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestPrepareHermesHomeMountedEmptyStoreReportsNoHistory covers the two shapes
// where the plumbing is fine but there is nothing to resume: a store the GC
// reclaimed between turns, and a dangling link an older overlay left pointing at
// a store that no longer exists. Reading "mounted" as "resumable" here is what
// would let a dead session id through to Hermes, which answers it by silently
// starting over.
func TestPrepareHermesHomeMountedEmptyStoreReportsNoHistory(t *testing.T) {
	requireSymlinks(t)
	sharedHome := t.TempDir()
	store := filepath.Join(t.TempDir(), "issue-1")
	skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
	hermesHome := filepath.Join(t.TempDir(), "hermes-home")

	if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err != nil {
		t.Fatalf("prepare first turn: %v", err)
	}
	mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")

	sessions, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare second turn: %v", err)
	}
	if !sessions.HistoryPresent {
		t.Fatal("HistoryPresent = false with a written transcript, want true")
	}

	// The GC reclaims the store between turns; the overlay's link survives and
	// now dangles.
	if err := os.RemoveAll(store); err != nil {
		t.Fatalf("simulate gc reclaim: %v", err)
	}
	sessions, err = prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare after gc reclaim: %v", err)
	}
	if !sessions.Mounted {
		t.Fatal("Mounted = false after the store was recreated, want true")
	}
	if sessions.HistoryPresent {
		t.Fatal("HistoryPresent = true over a reclaimed store — a dead session id would be forwarded")
	}

	// An empty file is the other shape of nothing: SQLite leaves one behind for
	// an open that never wrote a page, and resuming against it is the same
	// amnesia as resuming against an absent one.
	mustWrite(t, filepath.Join(store, "state.db"), "")
	sessions, err = prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
	if err != nil {
		t.Fatalf("prepare with an empty db: %v", err)
	}
	if sessions.HistoryPresent {
		t.Fatal("HistoryPresent = true for a zero-length database, want false")
	}
}

// TestPrepareHermesHomeMigrationIntoSemanticallyEmptyStore covers the store
// that holds state.db family remnants but no transcript — a zero-length
// database from an `open` that never wrote a page, or orphan sidecars left by a
// killed task. The migration reads those as "no history" and proceeds; the
// publish step must agree, because the caller deletes the source the moment the
// migration reports success. Disagreeing means the source database and its
// un-checkpointed tail are deleted in favour of a database with nothing in it.
func TestPrepareHermesHomeMigrationIntoSemanticallyEmptyStore(t *testing.T) {
	requireSymlinks(t)

	tests := []struct {
		name     string
		remnants map[string]string
		// wantMoved expects the source transcript to end up in the store;
		// wantFailClosed expects the migration to refuse instead — either is
		// acceptable, silently dropping the source is not.
		wantMoved      bool
		wantFailClosed bool
	}{
		{
			name:      "zero-length database",
			remnants:  map[string]string{"state.db": ""},
			wantMoved: true,
		},
		{
			name:      "orphan sidecars, no database",
			remnants:  map[string]string{"state.db-wal": "orphan tail", "state.db-shm": "orphan index"},
			wantMoved: true,
		},
		{
			// The one case that must NOT be overwritten: a real transcript
			// published by a concurrent task of the same conversation.
			name:      "real database is never overwritten",
			remnants:  map[string]string{"state.db": "the winner's transcript"},
			wantMoved: false,
		},
		{
			// Something in the store that is neither history nor a clearable
			// remnant. There is no safe publish here, so the migration must
			// fail rather than report a lost race and let the caller delete the
			// source.
			name:           "unclearable store fails closed",
			remnants:       map[string]string{"state.db": "", "stray.txt": "not ours"},
			wantFailClosed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sharedHome := t.TempDir()
			store := filepath.Join(t.TempDir(), "issue-1")
			skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
			hermesHome := filepath.Join(t.TempDir(), "hermes-home")

			for name, content := range tt.remnants {
				mustWrite(t, filepath.Join(store, name), content)
			}

			if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
				t.Fatalf("prepare pre-store task: %v", err)
			}
			mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")
			mustWrite(t, filepath.Join(hermesHome, "state.db-wal"), "uncheckpointed tail")

			sessions, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
			if err != nil {
				if !tt.wantFailClosed {
					t.Fatalf("prepare: %v", err)
				}
				// Failing closed is only acceptable with the source intact.
				for _, name := range []string{"state.db", "state.db-wal"} {
					got, statErr := os.ReadFile(filepath.Join(hermesHome, name))
					if statErr != nil {
						t.Fatalf("prepare failed (%v) AND dropped the source %s: %v", err, name, statErr)
					}
					if len(got) == 0 {
						t.Fatalf("source %s was emptied by a failed migration", name)
					}
				}
				return
			}
			if tt.wantFailClosed {
				t.Fatal("prepare succeeded on a store it cannot safely publish into, want an error")
			}
			if !sessions.Mounted || !sessions.HistoryPresent {
				t.Fatalf("mount = %+v, want mounted with history", sessions)
			}

			got, err := os.ReadFile(filepath.Join(store, "state.db"))
			if err != nil {
				t.Fatalf("read store db: %v", err)
			}
			if tt.wantMoved {
				if string(got) != "transcript" {
					t.Fatalf("store db = %q, want the migrated transcript — the source was dropped for an empty store", got)
				}
				wal, err := os.ReadFile(filepath.Join(store, "state.db-wal"))
				if err != nil || string(wal) != "uncheckpointed tail" {
					t.Fatalf("store wal = %q (err %v), want the migrated tail", wal, err)
				}
			} else if string(got) != "the winner's transcript" {
				t.Fatalf("store db = %q, want the pre-existing transcript left untouched", got)
			}
		})
	}
}

// TestHermesSessionStorePublishSerializesCompetitors pins the publish
// protocol's mutual exclusion with a deterministic interleaving. The first
// publisher is parked inside the critical section — after its occupancy check,
// before its remnant cleanup — which is exactly the window where an
// unsynchronized competitor used to be able to publish a real transcript and
// have it deleted as a remnant by the parked publisher's stale decision. With
// the lock, the competitor cannot enter that window at all: it waits, its own
// check sees the winner's database, and it reports a lost race without
// touching anything.
func TestHermesSessionStorePublishSerializesCompetitors(t *testing.T) {
	store := filepath.Join(t.TempDir(), "issue-1")
	// The semantically-empty shape both publishers read as "no history".
	mustWrite(t, filepath.Join(store, "state.db"), "")

	mkStaging := func(label string) string {
		staging, err := newHermesStoreStaging(store)
		if err != nil {
			t.Fatalf("staging for %s: %v", label, err)
		}
		mustWrite(t, filepath.Join(staging, "state.db"), label+" transcript")
		mustWrite(t, filepath.Join(staging, "state.db-wal"), label+" tail")
		return staging
	}
	firstStaging := mkStaging("first")
	secondStaging := mkStaging("second")

	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var parkedFirst atomic.Bool
	stubHermesSessionPublishBarrier(t, func(string) {
		entered <- struct{}{}
		if parkedFirst.CompareAndSwap(false, true) {
			<-release
		}
	})

	type outcome struct {
		label    string
		promoted bool
		err      error
	}
	results := make(chan outcome, 2)
	publish := func(label, staging string) {
		promoted, err := publishHermesSessionStaging(staging, store)
		results <- outcome{label, promoted, err}
	}

	go publish("first", firstStaging)
	<-entered // the first publisher is parked between its check and its cleanup
	go publish("second", secondStaging)

	close(release)
	byLabel := map[string]outcome{}
	for range 2 {
		r := <-results
		byLabel[r.label] = r
	}
	for _, r := range byLabel {
		if r.err != nil {
			t.Fatalf("publish %s: %v", r.label, r.err)
		}
	}
	if !byLabel["first"].promoted || byLabel["second"].promoted {
		t.Fatalf("promoted = first:%v second:%v, want the parked first publisher to win and the second to positively lose",
			byLabel["first"].promoted, byLabel["second"].promoted)
	}

	// The loser never reached the destructive window: its occupancy check
	// already saw the winner's published database.
	select {
	case <-entered:
		t.Fatal("the losing publisher entered the check-to-cleanup window — the interleaving that deletes a freshly published transcript")
	default:
	}

	// The winner's family is in the store byte-intact.
	for name, want := range map[string]string{
		"state.db":     "first transcript",
		"state.db-wal": "first tail",
	} {
		got, err := os.ReadFile(filepath.Join(store, name))
		if err != nil || string(got) != want {
			t.Fatalf("store %s = %q (err %v), want the winner's %q untouched", name, got, err, want)
		}
	}
	// The loser's staging was not consumed, so its caller still holds a
	// complete copy of the source it will now yield with.
	for _, name := range []string{"state.db", "state.db-wal"} {
		if _, err := os.Stat(filepath.Join(secondStaging, name)); err != nil {
			t.Fatalf("loser's staging %s was destroyed: %v", name, err)
		}
	}
}

// TestPrepareHermesHomeStoreCleanupHandlesUnexpectedEntryTypes covers the
// state.db-named entries the cleanup must not treat as removable SQLite
// remnants. A directory under a family name is content this code cannot
// identify — the publish must fail closed with the source preserved rather
// than delete it recursively. A symlink is a prior mount's store link: it is
// unlinked (never followed), and its target must survive.
func TestPrepareHermesHomeStoreCleanupHandlesUnexpectedEntryTypes(t *testing.T) {
	requireSymlinks(t)

	t.Run("directory under a family name fails closed", func(t *testing.T) {
		sharedHome := t.TempDir()
		store := filepath.Join(t.TempDir(), "issue-1")
		skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
		hermesHome := filepath.Join(t.TempDir(), "hermes-home")

		mustWrite(t, filepath.Join(store, "state.db"), "")
		mustWrite(t, filepath.Join(store, "state.db-wal", "nested.txt"), "content this code cannot identify")

		if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
			t.Fatalf("prepare pre-store task: %v", err)
		}
		mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")

		if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger()); err == nil {
			t.Fatal("prepare succeeded over a store entry it cannot identify, want fail closed")
		}
		if got, err := os.ReadFile(filepath.Join(store, "state.db-wal", "nested.txt")); err != nil || string(got) != "content this code cannot identify" {
			t.Fatalf("unidentified store content = %q (err %v), want it untouched", got, err)
		}
		if got, err := os.ReadFile(filepath.Join(hermesHome, "state.db")); err != nil || string(got) != "transcript" {
			t.Fatalf("source db = %q (err %v), want it preserved on a failed publish", got, err)
		}
	})

	t.Run("symlink remnant is unlinked without touching its target", func(t *testing.T) {
		sharedHome := t.TempDir()
		store := filepath.Join(t.TempDir(), "issue-1")
		skills := []SkillContextForEnv{{Name: "deploy", Content: "# Deploy"}}
		hermesHome := filepath.Join(t.TempDir(), "hermes-home")

		target := filepath.Join(t.TempDir(), "not-a-remnant.db")
		mustWrite(t, target, "someone else's database")
		mustWrite(t, filepath.Join(store, "state.db"), "")
		if err := os.Symlink(target, filepath.Join(store, "state.db-shm")); err != nil {
			t.Fatalf("plant symlink remnant: %v", err)
		}

		if _, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", "", testLogger()); err != nil {
			t.Fatalf("prepare pre-store task: %v", err)
		}
		mustWrite(t, filepath.Join(hermesHome, "state.db"), "transcript")

		sessions, err := prepareHermesHome(hermesHome, sharedHome, false, skills, nil, "", store, testLogger())
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if !sessions.Mounted || !sessions.HistoryPresent {
			t.Fatalf("mount = %+v, want mounted with history", sessions)
		}
		if got, err := os.ReadFile(filepath.Join(store, "state.db")); err != nil || string(got) != "transcript" {
			t.Fatalf("store db = %q (err %v), want the migrated transcript", got, err)
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != "someone else's database" {
			t.Fatalf("symlink target = %q (err %v), want it untouched by the unlink", got, err)
		}
		if _, err := os.Lstat(filepath.Join(store, "state.db-shm")); !os.IsNotExist(err) {
			t.Fatalf("symlink remnant still present (lstat err %v), want it unlinked", err)
		}
	})
}

// stubHermesSessionLink swaps the symlink primitive for the duration of a test
// and returns a restore func, so a test can assert both the failure and the
// recovery in one run.
func stubHermesSessionLink(t *testing.T, fn func(target, link string) error) (restore func()) {
	t.Helper()
	prev := hermesSessionLink
	hermesSessionLink = fn
	restored := false
	restore = func() {
		if !restored {
			hermesSessionLink = prev
			restored = true
		}
	}
	t.Cleanup(restore)
	return restore
}

// stubHermesSessionCopy swaps the migration's copy primitive, same contract as
// stubHermesSessionLink.
func stubHermesSessionCopy(t *testing.T, fn func(src, dst string) error) (restore func()) {
	t.Helper()
	prev := hermesSessionCopy
	hermesSessionCopy = fn
	restored := false
	restore = func() {
		if !restored {
			hermesSessionCopy = prev
			restored = true
		}
	}
	t.Cleanup(restore)
	return restore
}

// stubHermesSessionPublishBarrier swaps the publish barrier for the duration
// of a test, same contract as stubHermesSessionLink.
func stubHermesSessionPublishBarrier(t *testing.T, fn func(storeDir string)) (restore func()) {
	t.Helper()
	prev := hermesSessionPublishBarrier
	hermesSessionPublishBarrier = fn
	restored := false
	restore = func() {
		if !restored {
			hermesSessionPublishBarrier = prev
			restored = true
		}
	}
	t.Cleanup(restore)
	return restore
}

// requireSymlinks skips a test on a host that cannot create symlinks. The
// production path degrades to a task-local database there rather than copying,
// so there is nothing to assert about the mounted layout.
func requireSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return
	}
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
}
