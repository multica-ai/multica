//go:build !windows

package localenv

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEnvironmentReleasePreservesPostDeliveryEdits(t *testing.T) {
	for _, kind := range []string{"tracked", "staged", "index-only", "deleted", "untracked", "saved-leftover"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorktreeFixture(t, "24h")
			advance := fixtureClock(f)
			fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
			if kind == "saved-leftover" {
				fixtureGit(t, f.wt.Path, "add", "delivered.txt")
				fixtureGit(t, f.wt.Path, "commit", "-m", "agent fixture delivery")
				fixtureWrite(t, filepath.Join(f.wt.Path, "leftover.txt"), "already saved\n")
			}
			f.start("web")
			out, err := f.m.FinalizeWorktree(f.wt)
			if err != nil || out.RetainedWorktree == nil {
				t.Fatalf("Finalize: %+v %v", out, err)
			}
			tip := fixtureGit(t, f.repo, "rev-parse", out.Branch)
			records := fixtureGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/multica/")
			name := "delivered.txt"
			if kind == "untracked" {
				name = "new-review.txt"
			}
			if kind == "saved-leftover" {
				name = "leftover.txt"
			}
			const edit = "new work after delivery\n"
			fixtureWrite(t, filepath.Join(f.wt.Path, name), edit)
			if kind == "staged" || kind == "index-only" {
				fixtureGit(t, f.wt.Path, "add", name)
			}
			if kind == "index-only" {
				fixtureWrite(t, filepath.Join(f.wt.Path, name), "agent delivery\n")
			}
			if kind == "deleted" {
				if err := os.Remove(filepath.Join(f.wt.Path, name)); err != nil {
					t.Fatal(err)
				}
			}
			t.Logf("POSTDELIVERY checkout=%s kind=%s status=%s", f.wt.Path, kind, fixtureGit(t, f.wt.Path, "status", "--porcelain"))
			// Exercise the persisted cleanup record too, rather than only an in-memory token.
			restored := NewManager(f.m.dir, f.m.logger)
			restored.now = f.m.now
			if err := restored.Load(); err != nil {
				t.Fatal(err)
			}
			f.m = restored
			f.stop("web")
			f.m.Tick()
			snapshot := f.m.Snapshot()
			if len(snapshot) != 1 || snapshot[0].State != StateStopped || snapshot[0].RetainedWorktree != f.wt.Path {
				t.Fatalf("stopped edited checkout lost its local GC protection: %+v", snapshot)
			}
			got, readErr := os.ReadFile(filepath.Join(f.wt.Path, name))
			switch kind {
			case "deleted":
				if !os.IsNotExist(readErr) {
					t.Errorf("tracked deletion changed after Stop/Tick: %q %v", got, readErr)
				}
			case "index-only":
				if readErr != nil || string(got) != "agent delivery\n" {
					t.Errorf("index-only worktree changed: %q %v", got, readErr)
				}
				if staged := fixtureGit(t, f.wt.Path, "show", ":"+name); staged != strings.TrimSpace(edit) {
					t.Errorf("staged content lost: %q", staged)
				}
			default:
				if readErr != nil || string(got) != edit {
					t.Errorf("post-delivery %s work lost after Stop/Tick: %q %v", kind, got, readErr)
				}
			}
			if !strings.Contains(fixtureGit(t, f.repo, "worktree", "list", "--porcelain"), f.wt.Path) {
				t.Error("edited checkout unregistered")
			}
			if got := fixtureGit(t, f.repo, "rev-parse", out.Branch); got != tip {
				t.Error("delivered branch rewritten")
			}
			if got := fixtureGit(t, f.repo, "for-each-ref", "--format=%(refname) %(objectname)", "refs/multica/"); got != records {
				t.Error("delivery/leftover records rewritten")
			}
			// Once later edits are discarded back to the saved delivery, normal
			// cleanup can still collect ignored dependencies and saved leftovers.
			fixtureGit(t, f.wt.Path, "reset", tip, "--", name)
			if kind == "untracked" {
				if err := os.Remove(filepath.Join(f.wt.Path, name)); err != nil {
					t.Fatal(err)
				}
			} else if kind == "saved-leftover" {
				fixtureWrite(t, filepath.Join(f.wt.Path, name), "already saved\n")
			} else {
				fixtureWrite(t, filepath.Join(f.wt.Path, name), "agent delivery\n")
			}
			advance(f.m.now().Add(time.Minute + time.Nanosecond))
			f.m.Tick()
			f.eventuallyRemoved()
			if snapshot := f.m.Snapshot(); len(snapshot) != 1 || snapshot[0].RetainedWorktree != "" {
				t.Fatalf("released checkout still blocks GC: %+v", snapshot)
			}
		})
	}
}

func TestEnvironmentClockTTLPreservesPostDeliveryEdits(t *testing.T) {
	f := newWorktreeFixture(t, "1m")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	e := f.start("web")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil || out.RetainedWorktree == nil {
		t.Fatalf("Finalize: %+v %v", out, err)
	}
	tip := fixtureGit(t, f.repo, "rev-parse", out.Branch)
	edits := map[string]string{"delivered.txt": "tracked after delivery\n", "new-review.txt": "untracked after delivery\n"}
	for name, text := range edits {
		fixtureWrite(t, filepath.Join(f.wt.Path, name), text)
	}
	if e.ExpiresAt == nil {
		t.Fatal("missing real expiry")
	}
	t.Logf("POSTDELIVERY advanced clock TTL checkout=%s expires=%s status=%s", f.wt.Path, e.ExpiresAt, fixtureGit(t, f.wt.Path, "status", "--porcelain"))
	advance(e.ExpiresAt.Add(time.Nanosecond))
	f.auditProcess(e)
	f.m.Tick()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		list := f.m.List(f.task)
		if len(list) == 1 && list[0].State == StateStopped && !groupAlive(e.pgid) {
			if list[0].Detail != "expired" {
				t.Fatalf("TTL detail=%q", list[0].Detail)
			}
			f.m.Tick()
			if snapshot := f.m.Snapshot(); len(snapshot) != 1 || snapshot[0].RetainedWorktree != f.wt.Path {
				t.Fatalf("TTL lost local GC protection: %+v", snapshot)
			}
			for name, text := range edits {
				if got, err := os.ReadFile(filepath.Join(f.wt.Path, name)); err != nil || string(got) != text {
					t.Errorf("post-delivery %s lost after clock TTL: %q %v", name, got, err)
				}
			}
			if !strings.Contains(fixtureGit(t, f.repo, "worktree", "list", "--porcelain"), f.wt.Path) {
				t.Error("edited checkout unregistered")
			}
			if fixtureGit(t, f.repo, "rev-parse", out.Branch) != tip {
				t.Error("delivered branch rewritten")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("clock TTL did not collect environment")
}

func TestEnvironmentReleaseRechecksReplacedCheckout(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	f.start("web")
	out, err := f.m.FinalizeWorktree(f.wt)
	if err != nil || out.RetainedWorktree == nil {
		t.Fatalf("Finalize: %+v %v", out, err)
	}
	tip := fixtureGit(t, f.wt.Path, "rev-parse", "HEAD")
	fixtureGit(t, f.wt.Path, "checkout", "-b", "changed-after-delivery")
	f.stop("web") // First rejection must not make cached path identity authoritative.
	if _, err := os.Stat(f.wt.Path); err != nil {
		t.Fatalf("first refusal removed checkout: %v", err)
	}
	aside := filepath.Join(t.TempDir(), "original")
	fixtureGit(t, f.repo, "worktree", "move", f.wt.Path, aside)
	replacement := filepath.Join(t.TempDir(), "replacement")
	fixtureGit(t, f.repo, "clone", "--no-hardlinks", "--template="+t.TempDir(), f.repo, replacement)
	fixtureGit(t, replacement, "checkout", f.wt.Branch)
	if fixtureGit(t, replacement, "rev-parse", "HEAD") != tip {
		t.Fatal("replacement commit differs")
	}
	if err := os.Rename(replacement, f.wt.Path); err != nil {
		t.Fatal(err)
	}
	common := fixtureGit(t, f.wt.Path, "rev-parse", "--git-common-dir")
	if common != ".git" {
		t.Fatalf("replacement should own its separate repository: %s", common)
	}
	t.Logf("REPLACED checkout=%s branch=%s tip=%s independentCommon=%s", f.wt.Path, f.wt.Branch, tip, common)
	advance(f.m.now().Add(time.Minute + time.Nanosecond))
	f.m.Tick()
	deadline := time.Now().Add(10 * time.Second)
	for f.m.RetainsWorktree(f.wt.Path) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.m.RetainsWorktree(f.wt.Path) {
		t.Fatal("replacement check did not complete")
	}
	if got, err := os.ReadFile(filepath.Join(f.wt.Path, "delivered.txt")); err != nil || string(got) != "agent delivery\n" {
		t.Errorf("replacement checkout deleted by repeated release: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(f.wt.Path, ".git")); err != nil {
		t.Error("replacement repository lost")
	}
	if _, err := os.Stat(filepath.Join(aside, "delivered.txt")); err != nil {
		t.Error("original moved checkout lost")
	}
}

func TestEditedWorktreeCleanupHasPersistentRetryDelay(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	events := &worktreeEvents{waits: make(chan struct{}, 10)}
	f.m.logger = slog.New(slog.NewTextHandler(io.MultiWriter(events), nil))
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "new edits\n")
	f.stop("web")
	first := events.warningCount()
	if first != 1 {
		t.Errorf("first refusal warned %d times; want exactly one", first)
	}
	for range 4 {
		f.m.Tick()
	}
	if got := events.warningCount(); got != first {
		t.Errorf("Ticks before retry repeated cleanup warnings: %d -> %d", first, got)
	}
	restored := NewManager(f.m.dir, f.m.logger)
	restored.now = f.m.now
	if err := restored.Load(); err != nil {
		t.Fatal(err)
	}
	f.m = restored
	f.m.Tick()
	if got := events.warningCount(); got != first {
		t.Errorf("Load bypassed persisted retry delay: %d -> %d", first, got)
	}
	advance(f.m.now().Add(30 * time.Second))
	f.m.Tick()
	if got := events.warningCount(); got != first {
		t.Errorf("30s Tick bypassed retry delay: %d -> %d", first, got)
	}
	if data, err := os.ReadFile(filepath.Join(f.wt.Path, "delivered.txt")); err != nil || string(data) != "new edits\n" {
		t.Fatalf("edits lost: %q %v", data, err)
	}
	if f.m.RetainsWorktree(f.wt.Path) {
		t.Fatal("cleanup launched before retry was due")
	}
	advance(f.m.now().Add(30*time.Second + time.Nanosecond))
	var triggers sync.WaitGroup
	for range 8 {
		triggers.Go(func() { f.m.Tick() })
	}
	triggers.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for (events.warningCount() < first+1 || f.m.RetainsWorktree(f.wt.Path)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := events.warningCount(); got != first+1 {
		t.Fatalf("one due attempt warned %d times; want %d", got, first+1)
	}
	// Reverting the edit must not disable future safe cleanup.
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	advance(f.m.now().Add(time.Minute + time.Nanosecond))
	f.m.Tick()
	f.eventuallyRemoved()

}

func TestSharedWorktreeRetrySurvivesLoadAndOldRecords(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	f.start("web")
	f.start("api")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	events := &worktreeEvents{waits: make(chan struct{}, 10)}
	f.m.logger = slog.New(slog.NewTextHandler(events, nil))
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "new work\n")
	f.stop("web")
	if events.warningCount() != 0 {
		t.Fatal("released while another environment was live")
	}
	f.stop("api")
	if events.warningCount() != 1 {
		t.Fatalf("last user attempts=%d; want1", events.warningCount())
	}
	views := f.m.Snapshot()
	if len(views) != 2 {
		t.Fatalf("shared records=%d", len(views))
	}
	// One historical record has no new field. The other persisted record must
	// still supply the shared path's retry gate after a normal Load.
	id, _ := IDFromKey(views[0].Key)
	state := filepath.Join(f.m.dir, id, stateFile)
	raw, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]json.RawMessage
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	delete(old, "worktree_retry_at")
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, raw, 0600); err != nil {
		t.Fatal(err)
	}
	restored := NewManager(f.m.dir, f.m.logger)
	restored.now = f.m.now
	if err := restored.Load(); err != nil {
		t.Fatal(err)
	}
	f.m = restored
	for range 4 {
		f.m.Tick()
	}
	if f.m.RetainsWorktree(f.wt.Path) || events.warningCount() != 1 {
		t.Fatal("legacy shared record bypassed persisted path delay")
	}
	advance(f.m.now().Add(time.Minute + time.Nanosecond))
	var ticks sync.WaitGroup
	for range 8 {
		ticks.Go(func() { f.m.Tick() })
	}
	ticks.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for (events.warningCount() < 2 || f.m.RetainsWorktree(f.wt.Path)) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if events.warningCount() != 2 {
		t.Fatalf("shared due attempts=%d; want2", events.warningCount())
	}
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	advance(f.m.now().Add(time.Minute + time.Nanosecond))
	f.m.Tick()
	f.eventuallyRemoved()
	for _, e := range f.m.Snapshot() {
		if e.RetainedWorktree != "" {
			t.Fatal("success did not clear every shared cleanup record")
		}
	}
}

func TestWorktreeReleaseRetriesTransientGitFailure(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	advance := fixtureClock(f)
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	gitdir := filepath.Join(f.repo, ".git")
	aside := filepath.Join(f.repo, ".git-disabled")
	if err := os.Rename(gitdir, aside); err != nil {
		t.Fatal(err)
	}
	f.stop("web")
	if err := os.Rename(aside, gitdir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.wt.Path); err != nil {
		t.Fatal("transient failure discarded checkout")
	}
	f.m.Tick()
	if f.m.RetainsWorktree(f.wt.Path) {
		t.Fatal("transient failure retried before delay")
	}
	advance(f.m.now().Add(time.Minute + time.Nanosecond))
	f.m.Tick()
	f.eventuallyRemoved()
}

func TestLegacyWorktreeCleanupRecordGetsFirstAttempt(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	e := f.start("web")
	if _, err := f.m.FinalizeWorktree(f.wt); err != nil {
		t.Fatal(err)
	}
	// Stop without a cleanup token in the original manager, leaving the saved
	// live record as data for the fresh manager to adopt after the process exits.
	id, _ := IDFromKey(e.Key)
	state := filepath.Join(f.m.dir, id, stateFile)
	raw, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "new work\n")
	f.stop("web")
	var old map[string]json.RawMessage
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	delete(old, "worktree_retry_at")
	raw, err = json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fixtureWrite(t, filepath.Join(f.wt.Path, "delivered.txt"), "agent delivery\n")
	restored := NewManager(f.m.dir, f.m.logger)
	if err := restored.Load(); err != nil {
		t.Fatal(err)
	}
	f.m = restored
	f.eventuallyRemoved()
}
