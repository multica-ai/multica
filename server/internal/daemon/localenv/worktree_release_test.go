//go:build !windows

package localenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentReleasePreservesPostDeliveryEdits(t *testing.T) {
	for _, kind := range []string{"tracked", "staged", "index-only", "deleted", "untracked", "saved-leftover"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorktreeFixture(t, "24h")
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
			f.m.Tick()
			f.removed()
			if snapshot := f.m.Snapshot(); len(snapshot) != 1 || snapshot[0].RetainedWorktree != "" {
				t.Fatalf("released checkout still blocks GC: %+v", snapshot)
			}
		})
	}
}

func TestEnvironmentRealTTLPreservesPostDeliveryEdits(t *testing.T) {
	f := newWorktreeFixture(t, "1m")
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
	t.Logf("POSTDELIVERY real TTL checkout=%s expires=%s status=%s", f.wt.Path, e.ExpiresAt, fixtureGit(t, f.wt.Path, "status", "--porcelain"))
	for time.Now().Before(*e.ExpiresAt) {
		time.Sleep(250 * time.Millisecond)
	}
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
					t.Errorf("post-delivery %s lost after real TTL: %q %v", name, got, err)
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
	t.Fatal("real TTL did not collect environment")
}

func TestEnvironmentReleaseRechecksReplacedCheckout(t *testing.T) {
	f := newWorktreeFixture(t, "24h")
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
	f.m.Tick()
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
