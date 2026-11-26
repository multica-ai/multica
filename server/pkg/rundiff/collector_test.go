package rundiff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func runGit(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, out)
	}
	return strings.TrimSpace(string(out))
}
func put(t testing.TB, dir, p, body string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, p), []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
}
func repo(t testing.TB, dir string) {
	t.Helper()
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.name", "fixture")
	runGit(t, dir, "config", "user.email", "fixture@example.invalid")
	put(t, dir, "same.txt", "base\n")
	put(t, dir, "delete.txt", "gone\n")
	put(t, dir, "rename.txt", "rename\n")
	put(t, dir, "revert.txt", "stable\n")
	commit(t, dir)
}
func commit(t testing.TB, dir string) {
	t.Helper()
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-qm", "fixture")
}
func paths(c Collection) []string {
	var out []string
	for _, f := range c.Files {
		out = append(out, f.Path)
	}
	sort.Strings(out)
	return out
}
func TestCollectorActualBaselineAndCommittedNetChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/mode/symlink fixture; requires Windows-specific acceptance")
	}
	root := t.TempDir()
	for _, n := range []string{"a", "b"} {
		r := filepath.Join(root, n)
		repo(t, r)
		put(t, r, "same.txt", "prior dirty\n")
		put(t, r, "staged.txt", "prior staged\n")
		runGit(t, r, "add", "staged.txt")
		put(t, r, "untracked.txt", "prior untracked\n")
	}
	c := New(root)
	c.Start(context.Background())
	defer c.Abort()
	for _, n := range []string{"a", "b"} {
		r := filepath.Join(root, n)
		cmd := exec.Command("sh", "-c", "printf 'final shell\\n' > same.txt")
		cmd.Dir = r
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("shell: %v %s", e, out)
		}
		put(t, r, "revert.txt", "temp")
		put(t, r, "revert.txt", "stable\n")
		if e := os.Remove(filepath.Join(r, "delete.txt")); e != nil {
			t.Fatal(e)
		}
		if e := os.Rename(filepath.Join(r, "rename.txt"), filepath.Join(r, "renamed.txt")); e != nil {
			t.Fatal(e)
		}
		put(t, r, "large.txt", strings.Repeat("line\n", 70000))
		put(t, r, "binary", "\x00binary")
		commit(t, r)
	}
	indexes := map[string]string{}
	for _, n := range []string{"a", "b"} {
		indexes[n] = runGit(t, filepath.Join(root, n), "write-tree")
	}
	out := c.Finish(context.Background())
	if out.Status != "ready" || len(out.Files) != 12 {
		t.Fatalf("%s: %+v", out.Status, out)
	}
	for _, f := range out.Files {
		// bodies never ride the manifest — both sides are addressed by
		// the pinned trees. "prior dirty\n" / "final shell\n" are 12 bytes each,
		// so the sizes prove both snapshots captured the actual worktree.
		if f.Path == "same.txt" && (f.Before != nil || f.After != nil || f.BeforeBytes != 12 || f.AfterBytes != 12) {
			t.Fatalf("dirty baseline: %+v", f)
		}
		if f.Path == "binary" && (f.BodyStatus != "binary" || f.After != nil) {
			t.Fatal("binary leaked")
		}
	}
	for _, n := range []string{"a", "b"} {
		if got := runGit(t, filepath.Join(root, n), "write-tree"); got != indexes[n] {
			t.Fatal("index mutated")
		}
	}
	next := New(root)
	next.Start(context.Background())
	if got := next.Finish(context.Background()); len(got.Files) != 0 || got.Status != "ready" {
		t.Fatalf("new run accumulated: %+v", got)
	}
	raw, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.RemoveAll(root); e != nil {
		t.Fatal(e)
	}
	var reopened Collection
	if e = json.Unmarshal(raw, &reopened); e != nil || len(reopened.Files) != 12 {
		t.Fatal("persistence depends on directory")
	}
}
func TestCollectorCheckoutAndManualCloneCoverage(t *testing.T) {
	root := t.TempDir()
	put(t, root, "notes.txt", "before")
	c := New(root)
	c.Start(context.Background())
	defer c.Abort()
	controlled := filepath.Join(root, "controlled")
	repo(t, controlled)
	if e := c.Register(context.Background(), controlled); e != nil {
		t.Fatal(e)
	}
	put(t, controlled, "same.txt", "edit")
	if e := c.Register(context.Background(), controlled); e != nil {
		t.Fatal(e)
	} // Never reset on repeated checkout.
	repo(t, filepath.Join(root, "manual"))
	put(t, root, "notes.txt", "after")
	out := c.Finish(context.Background())
	if !reflect.DeepEqual(paths(out), []string{"notes.txt", "same.txt"}) {
		t.Fatalf("%+v", out)
	}
	found := false
	for _, r := range out.Roots {
		if r.Label == "manual" {
			found = r.Reason == "missing_baseline"
		}
	}
	if !found || out.Status != "partial" {
		t.Fatalf("manual clone coverage: %+v", out)
	}
}
func TestCollectorCommitMergeCherryPick(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	base := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "-qb", "merge-source")
	put(t, root, "merge.txt", "merged")
	commit(t, root)
	merge := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "--detach", base)
	runGit(t, root, "checkout", "-qb", "pick-source")
	put(t, root, "pick.txt", "picked")
	commit(t, root)
	pick := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "--detach", base)
	c := New(root)
	c.Start(context.Background())
	runGit(t, root, "merge", "--no-ff", "-m", "merge", merge)
	runGit(t, root, "cherry-pick", pick)
	out := c.Finish(context.Background())
	if !reflect.DeepEqual(paths(out), []string{"merge.txt", "pick.txt"}) {
		t.Fatalf("committed diff lost: %+v", out)
	}
}
func TestCollectorBudgetsRedactionAndIgnoredSecrets(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	put(t, root, ".gitignore", "ignored/\n")
	commit(t, root)
	c := New(root)
	c.Start(context.Background())
	put(t, root, ".env", "TOKEN=must-not-upload")
	put(t, root, "ignored/output", "ignored")
	put(t, root, "secret.txt", "TOKEN=do-not-expose\n")
	put(t, root, "huge.txt", strings.Repeat("x", MaxSideBytes+1))
	put(t, root, "ok.txt", strings.Repeat("x", 210*1024))
	out := c.Finish(context.Background())
	if out.Status != "partial" || len(out.Files) != 3 {
		t.Fatalf("%+v", out)
	}
	for _, f := range out.Files {
		switch f.Path {
		case "secret.txt":
			// bodies never ride the manifest; redaction is flagged and
			// enforced again at relay serve time.
			if f.BodyStatus != "redacted" || f.After != nil {
				t.Fatalf("secret not flagged redacted: %+v", f)
			}
		case "huge.txt":
			if f.BodyStatus != "omitted" || f.After != nil {
				t.Fatal("huge body not omitted")
			}
		case "ok.txt":
			// Under the side budget the row stays servable through the relay;
			// the manifest itself carries no body in either case.
			if f.BodyStatus != "available" || f.After != nil {
				t.Fatalf("ok.txt not servable: %+v", f)
			}
		}
	}
	// MaxBodyBytes still caps the directory-root JSON path (Q10): a workdir
	// without a repository keeps inline bodies, so the total budget applies.
	dirRoot := t.TempDir()
	put(t, dirRoot, "a.txt", "enough\n")
	put(t, dirRoot, "b.txt", "enough too\n")
	small := NewWithLimits(dirRoot, Limits{10, MaxSideBytes, 8, time.Second})
	small.Start(context.Background())
	put(t, dirRoot, "a.txt", "changed\n")
	budget := small.Finish(context.Background())
	if budget.Status != "partial" {
		t.Fatal("body budget ignored")
	}
}
func TestCollectorSharedRootAndCancellation(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	a := New(root)
	a.Start(context.Background())
	b := New(root)
	b.Start(context.Background())
	put(t, root, "same.txt", "changed")
	if out := a.Finish(context.Background()); out.Status != "unavailable" || out.Reason != "shared_root" {
		t.Fatalf("%+v", out)
	}
	b.Finish(context.Background())
	c := New(root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Start(ctx)
	if out := c.Finish(ctx); out.Status == "ready" {
		t.Fatalf("cancelled scan is not no-change: %+v", out)
	}
}
func TestCollectorModeChangeSymlinkAndFileLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/mode/symlink fixture; requires Windows-specific acceptance")
	}
	root := t.TempDir()
	repo(t, root)
	c := New(root)
	c.Start(context.Background())
	if e := os.Chmod(filepath.Join(root, "same.txt"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("same.txt", filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	out := c.Finish(context.Background())
	// symlinks are first-class git objects (mode 120000), so they no
	// longer force a partial scan — they diff like any other path.
	if out.Status != "ready" {
		t.Fatalf("symlink coverage: %+v", out)
	}
	foundMode := false
	foundLink := false
	for _, f := range out.Files {
		if f.Path == "same.txt" {
			foundMode = f.Change == "mode_changed"
		}
		if f.Path == "link" {
			foundLink = f.Change == "added" && f.AfterMode == "120000"
		}
	}
	if !foundMode {
		t.Fatalf("mode change lost: %+v", out.Files)
	}
	if !foundLink {
		t.Fatalf("symlink row lost or misclassified: %+v", out.Files)
	}
	capped := NewWithLimits(root, Limits{1, MaxSideBytes, MaxBodyBytes, time.Second})
	capped.Start(context.Background())
	if out := capped.Finish(context.Background()); out.TotalsComplete || out.Status == "ready" {
		t.Fatal("file cap silently ignored")
	}
}
func BenchmarkCollectorLargeRepository(b *testing.B) {
	root := b.TempDir()
	repo(b, root)
	for i := 0; i < 9000; i++ {
		put(b, root, fmt.Sprintf("files/%05d", i), strings.Repeat("line\n", 800))
	}
	commit(b, root)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := New(root)
		c.Start(context.Background())
		out := c.Finish(context.Background())
		if out.Status != "ready" || len(out.Files) != 0 {
			b.Fatalf("%+v", out)
		}
	}
}

func TestCollectorSymlinkParentCannotReadOutsideRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell/mode/symlink fixture; requires Windows-specific acceptance")
	}
	root := t.TempDir()
	repo(t, root)
	put(t, root, "nested/a", "original")
	commit(t, root)
	c := New(root)
	c.Start(context.Background())
	outside := t.TempDir()
	put(t, outside, "a", "outside secret")
	if e := os.RemoveAll(filepath.Join(root, "nested")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(root, "nested")); e != nil {
		t.Fatal(e)
	}
	out := c.Finish(context.Background())
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "outside secret") || out.Status == "ready" {
		t.Fatalf("unsafe symlink: %s", raw)
	}
}
func TestCollectorMissingRootIsNotDeletion(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	c := New(root)
	c.Start(context.Background())
	if e := os.RemoveAll(root); e != nil {
		t.Fatal(e)
	}
	out := c.Finish(context.Background())
	if out.Status == "ready" || len(out.Files) != 0 {
		t.Fatalf("lost root became deletions: %+v", out)
	}
}
func TestRunDiffQuotedCredentialsAndMultilineKey(t *testing.T) {
	for _, s := range []string{`{"token": "do-not-upload"}`, `password = 'do-not-upload'`, "-----BEGIN PRIVATE KEY-----\ndo-not-upload\n-----END PRIVATE KEY-----"} {
		safe := SafeText(s)
		if strings.Contains(safe, "do-not-upload") {
			t.Fatalf("redaction missing: %s", safe)
		}
		if SafeText(safe) != safe {
			t.Fatal("redaction not idempotent")
		}
	}
}
func BenchmarkCollectorExistingRepository(b *testing.B) {
	root := os.Getenv("WS346_BENCH_REPO")
	if root == "" {
		b.Skip("set WS346_BENCH_REPO to an existing repository")
	}
	for i := 0; i < b.N; i++ {
		c := New(root)
		start := time.Now()
		c.Start(context.Background())
		baseline := time.Since(start)
		dirty := c.bytes
		count := c.filesSeen
		start = time.Now()
		out := c.Finish(context.Background())
		finish := time.Since(start)
		if out.Status != "ready" {
			b.Fatalf("%s %+v", out.Status, out.Roots)
		}
		raw, _ := json.Marshal(out)
		b.ReportMetric(float64(baseline.Microseconds())/1000, "baseline_ms")
		b.ReportMetric(float64(finish.Microseconds())/1000, "finish_ms")
		b.ReportMetric(float64(dirty), "dirty_bytes")
		b.ReportMetric(float64(count), "files")
		b.ReportMetric(float64(len(raw)), "upload_bytes")
	}
}

func TestCollectorRuntimeBlockDoesNotHideUserWhitespace(t *testing.T) {
	root := t.TempDir()
	put(t, root, "AGENTS.md", "user\n")
	c := New(root)
	c.Start(context.Background())
	put(t, root, "AGENTS.md", "user\n\n")
	out := c.Finish(context.Background())
	if len(out.Files) != 1 || *out.Files[0].Before != "user\n" || *out.Files[0].After != "user\n\n" {
		t.Fatal("user whitespace was stripped")
	}
}

func TestCollectorJournalsPinBeforeCreatingIt(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	c := New(root)
	called := false
	c.SetRefJournal(func(refs []RootRef) error {
		called = true
		for _, anchor := range refs {
			for _, ref := range []string{anchor.BeforeRef, anchor.AfterRef} {
				if ref == "" {
					continue
				}
				cmd := exec.Command("git", "-C", root, "show-ref", "--verify", ref)
				if cmd.Run() == nil {
					t.Fatal("ref created before recovery journal")
				}
			}
		}
		return fmt.Errorf("disk unavailable")
	})
	c.Start(context.Background())
	out := c.Finish(context.Background())
	if !called || out.Status != "unavailable" {
		t.Fatalf("journal failure not surfaced: %+v", out)
	}
}

func BenchmarkCollectorMultipleRepositories(b *testing.B) {
	root := b.TempDir()
	for _, name := range []string{"a", "b"} {
		r := filepath.Join(root, name)
		repo(b, r)
		for i := 0; i < 4000; i++ {
			put(b, r, fmt.Sprintf("files/%05d", i), strings.Repeat("line\n", 800))
		}
		commit(b, r)
		put(b, r, "same.txt", "actual dirty baseline\n")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := New(root)
		start := time.Now()
		c.Start(context.Background())
		baseline := time.Since(start)
		dirty := c.bytes
		start = time.Now()
		out := c.Finish(context.Background())
		finish := time.Since(start)
		if out.Status != "ready" || len(out.Files) != 0 {
			b.Fatalf("%+v", out)
		}
		b.ReportMetric(float64(baseline.Microseconds())/1000, "baseline_ms")
		b.ReportMetric(float64(finish.Microseconds())/1000, "finish_ms")
		b.ReportMetric(float64(dirty), "dirty_bytes")
	}
}

func TestCollectorMissingBaselineDominatesPresentation(t *testing.T) {
	for _, body := range []string{"password=supersecret\n", strings.Repeat("x", MaxSideBytes+1), "\x00binary", "TOKEN=body-budget-secret\n"} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			dir := t.TempDir()
			repo(t, dir)
			limits := DefaultLimits()
			if strings.HasPrefix(body, "TOKEN=") {
				limits.BodyBytes = 1
			}
			c := NewWithLimits(dir, limits)
			c.Start(context.Background())
			defer c.Abort()
			oid := runGit(t, dir, "rev-parse", "HEAD:same.txt")
			if err := os.Remove(filepath.Join(dir, ".git", "objects", oid[:2], oid[2:])); err != nil {
				t.Fatal(err)
			}
			put(t, dir, "same.txt", body)
			got := c.Finish(context.Background())
			if got.Status != "partial" || len(got.Files) != 1 {
				t.Fatalf("%+v", got)
			}
			f := got.Files[0]
			if f.BodyStatus != "unavailable" || f.Reason != "baseline_object_missing" || f.Before != nil || f.After != nil {
				t.Fatalf("%+v", f)
			}
			if err := got.ValidateAndSanitize(); err != nil {
				t.Fatal(err)
			}
			if got.Status != "partial" || got.Files[0].BodyStatus != "unavailable" {
				t.Fatalf("sanitize changed failure: %+v", got)
			}
		})
	}
}

func TestCollectorNestedRootMigrationDoesNotDelete(t *testing.T) {
	for _, parentGit := range []bool{false, true} {
		for _, controlled := range []bool{false, true} {
			t.Run(fmt.Sprintf("git=%v/controlled=%v", parentGit, controlled), func(t *testing.T) {
				dir := t.TempDir()
				if parentGit {
					repo(t, dir)
				}
				put(t, dir, "existing/a.txt", "unchanged\n")
				put(t, dir, "outside.txt", "before\n")
				c := New(dir)
				c.Start(context.Background())
				defer c.Abort()
				child := filepath.Join(dir, "existing")
				runGit(t, child, "init", "-q")
				if controlled {
					if err := c.Register(context.Background(), child); err != nil {
						t.Fatal(err)
					}
				}
				put(t, dir, "outside.txt", "after\n")
				got := c.Finish(context.Background())
				if len(got.Files) != 1 || got.Files[0].Path != "outside.txt" {
					t.Fatalf("scope migration fabricated changes: %+v", got)
				}
				want := "partial"
				if controlled {
					want = "ready"
				}
				if got.Status != want {
					t.Fatalf("%+v", got)
				}
				if !controlled && got.TotalsComplete {
					t.Fatal("missing root baseline claimed complete")
				}
			})
		}
	}
}

// seedRepo grows a repo() fixture with n numbered tracked files.
func seedRepo(t testing.TB, dir string, n int) {
	t.Helper()
	repo(t, dir)
	for i := 0; i < n; i++ {
		put(t, dir, fmt.Sprintf("f%03d.txt", i), fmt.Sprintf("base %d\n", i))
	}
	if n > 0 {
		commit(t, dir)
	}
}

func coverageByLabel(t testing.TB, c Collection) map[string]Root {
	t.Helper()
	byRoot := map[string]Root{}
	for _, r := range c.Roots {
		byRoot[r.Label] = r
	}
	return byRoot
}

// The file budget is per root. Two roots whose combined entries exceed
// limits.Files must not starve each other: under the old shared counter the
// second root was cut off mid-scan and edits beyond the cut were silently
// dropped. Regression: two large
// clones (6145 + 6143 tracked files, > MaxFiles combined) left the second
// root and the workdir root labeled partial/file_limit with lost edits.
func TestCollectorFileBudgetIsPerRoot(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "repo-a")
	b := filepath.Join(root, "repo-b")
	repo(t, a)        // 4 tracked files
	seedRepo(t, b, 2) // 6 tracked files; 4 + 6 > the shared old cap of 6
	put(t, root, "AGENTS.md", "workdir\n")

	c := NewWithLimits(root, Limits{6, MaxSideBytes, MaxBodyBytes, time.Second})
	c.Start(context.Background())
	put(t, b, "delete.txt", "edited early\n") // inside the old cut
	put(t, b, "f001.txt", "edited late\n")    // beyond the old cut
	out := c.Finish(context.Background())

	byRoot := coverageByLabel(t, out)
	if r := byRoot["repo-a"]; r.Coverage != "complete" {
		t.Fatalf("repo-a: %+v", r)
	}
	if r := byRoot["repo-b"]; r.Coverage != "complete" {
		t.Fatalf("repo-b: %+v", r)
	}
	if r := byRoot["."]; r != (Root{}) && r.Coverage != "complete" {
		t.Fatalf("workdir root: %+v", r)
	}
	edited := map[string]bool{}
	for _, f := range out.Files {
		if f.RootID == byRoot["repo-b"].ID {
			edited[f.Path] = f.Change == "modified"
		}
	}
	if !edited["delete.txt"] || !edited["f001.txt"] {
		t.Fatalf("repo-b edits lost: %v", paths(out))
	}
	if out.Status != "ready" || !out.TotalsComplete {
		t.Fatalf("%+v", out)
	}
}

// A root that alone exceeds its budget is labeled truthfully and cannot hide
// behind a sibling: the overflowing root reports partial/file_limit while the
// sibling keeps its own budget and stays complete.
func TestCollectorFileLimitAttribution(t *testing.T) {
	root := t.TempDir()
	seedRepo(t, filepath.Join(root, "big"), 4)   // 8 tracked files > budget 6
	seedRepo(t, filepath.Join(root, "small"), 0) // 4 tracked files
	put(t, root, "AGENTS.md", "workdir\n")

	c := NewWithLimits(root, Limits{6, MaxSideBytes, MaxBodyBytes, time.Second})
	c.Start(context.Background())
	out := c.Finish(context.Background())

	byRoot := coverageByLabel(t, out)
	if r := byRoot["big"]; r.Coverage != "partial" || r.Reason != "file_limit" {
		t.Fatalf("big: %+v", r)
	}
	if r := byRoot["small"]; r.Coverage != "complete" {
		t.Fatalf("small: %+v", r)
	}
	if out.Status != "partial" || out.TotalsComplete {
		t.Fatalf("%+v", out)
	}
}

// A non-git directory root that overflows its budget reports file_limit,
// not enumeration_failed.
func TestCollectorDirectoryRootFileLimitLabel(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		put(t, root, fmt.Sprintf("f%d.txt", i), "x\n")
	}
	c := NewWithLimits(root, Limits{3, MaxSideBytes, MaxBodyBytes, time.Second})
	c.Start(context.Background())
	out := c.Finish(context.Background())
	if len(out.Roots) != 1 || out.Roots[0].Kind != "directory" {
		t.Fatalf("roots: %+v", out.Roots)
	}
	if out.Roots[0].Coverage != "partial" || out.Roots[0].Reason != "file_limit" {
		t.Fatalf("directory root: %+v", out.Roots[0])
	}
}

// Line statistics are computed at finish from the same attached texts the
// diff view renders: per-file +/- plus collection totals, with binary files
// contributing nothing by nature and uncountable files (budgeted-away bodies)
// marking the sums partial instead of publishing a guess.
func TestCollectorFileLineStats(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	put(t, root, "mod.txt", "a\nb\nc\n")
	put(t, root, "del.txt", "x\ny\nz\nw\n")
	put(t, root, "mode.txt", "keep\n")
	commit(t, root)

	c := New(root)
	c.Start(context.Background())
	put(t, root, "mod.txt", "a\nX\nc\nd\n")       // +2 −1
	put(t, root, "add.txt", "l1\nl2\n")           // +2 −0
	put(t, root, "bin.dat", "\x00\x01\x02\n\x03") // added binary: no line counts
	os.Remove(filepath.Join(root, "del.txt"))
	os.Chmod(filepath.Join(root, "mode.txt"), 0700)
	out := c.Finish(context.Background())

	stats := map[string][2]*int64{}
	for _, f := range out.Files {
		stats[f.Path] = [2]*int64{f.Insertions, f.Deletions}
	}
	want := map[string]bool{"mod.txt": true, "add.txt": true, "del.txt": true, "mode.txt": true, "bin.dat": true}
	for path := range want {
		if _, ok := stats[path]; !ok {
			t.Fatalf("%s missing from files: %+v", path, out.Files)
		}
	}
	assertStats := func(path string, ins, del int64) {
		t.Helper()
		got := stats[path]
		if got[0] == nil || got[1] == nil || *got[0] != ins || *got[1] != del {
			t.Fatalf("%s: got %v %v, want %d %d", path, got[0], got[1], ins, del)
		}
	}
	assertStats("mod.txt", 2, 1)
	assertStats("add.txt", 2, 0)
	assertStats("del.txt", 0, 4)
	assertStats("mode.txt", 0, 0)
	if stats["bin.dat"][0] != nil || stats["bin.dat"][1] != nil {
		t.Fatalf("binary file must carry no line counts: %v", stats["bin.dat"])
	}
	if out.InsertionsTotal != 4 || out.DeletionsTotal != 5 {
		t.Fatalf("totals: %d %d", out.InsertionsTotal, out.DeletionsTotal)
	}
	if !out.StatsComplete {
		t.Fatalf("stats_complete should stay true when everything countable was counted")
	}
	// Pagination keeps the collection-level totals.
	page := out.Page(0, 2)
	if page.InsertionsTotal != 4 || page.DeletionsTotal != 5 {
		t.Fatalf("page totals: %d %d", page.InsertionsTotal, page.DeletionsTotal)
	}
}

// line counts come from git numstat, independent of body capture — a
// side over the body budget keeps its counts and its metadata-only status,
// while an unreadable blob (degraded numstat) marks the sums partial.
func TestCollectorFileLineStatsPartialBody(t *testing.T) {
	root := t.TempDir()
	repo(t, root)
	put(t, root, "same.txt", "same\n")
	put(t, root, "small.txt", "a\nb\nc\n")
	put(t, root, "big.txt", strings.Repeat("line\n", 40))
	commit(t, root)

	c := NewWithLimits(root, Limits{MaxFiles, 64, MaxBodyBytes, time.Second})
	c.Start(context.Background())
	os.Remove(filepath.Join(root, "same.txt"))            // −1
	put(t, root, "small.txt", "a\nB\nc\n")                // +1 −1
	put(t, root, "big.txt", strings.Repeat("LINE\n", 40)) // +40 −40, over the side budget
	out := c.Finish(context.Background())

	byPath := map[string]File{}
	for _, f := range out.Files {
		byPath[f.Path] = f
	}
	if f := byPath["small.txt"]; f.Insertions == nil || *f.Insertions != 1 || *f.Deletions != 1 {
		t.Fatalf("small.txt: %+v", f)
	}
	if f := byPath["big.txt"]; f.BodyStatus != "omitted" || f.Insertions == nil || *f.Insertions != 40 || *f.Deletions != 40 {
		t.Fatalf("big.txt must keep numstat counts while omitted: %+v", f)
	}
	if out.InsertionsTotal != 41 || out.DeletionsTotal != 42 {
		t.Fatalf("totals must cover all numstat files: %d %d", out.InsertionsTotal, out.DeletionsTotal)
	}
	if !out.StatsComplete {
		t.Fatalf("numstat counted everything; stats must stay complete")
	}
}

// FIXME(test-integration): exercises real Git's racy-index detection. Copying
// cached stat data into a newer index must not hide same-size worktree edits.
func TestCollectorCapturesSameSizeEditsWithRacyIndex(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.name", "fixture")
	runGit(t, root, "config", "user.email", "fixture@example.invalid")
	// Ignore ctime to reproduce a coarse stat-cache match on every platform.
	runGit(t, root, "config", "core.trustctime", "false")
	path := filepath.Join(root, "small.txt")
	stamp := time.Unix(1700000000, 0)
	write := func(body string) {
		t.Helper()
		put(t, root, "small.txt", body)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	write("a\nb\nc\n")
	commit(t, root)
	index := filepath.Join(root, ".git", "index")
	if err := os.Chtimes(index, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	seed, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	// Both snapshots must capture dirty content, rather than the committed
	// blob whose cached size and mtime still match the live file.
	write("a\nB\nc\n")
	c := New(root)
	c.Start(context.Background())
	defer c.Abort()
	write("a\nC\nc\n")
	out := c.Finish(context.Background())
	if out.Status != "ready" || len(out.Files) != 1 || out.Files[0].Path != "small.txt" {
		t.Fatalf("same-size edit missing from snapshots: %+v", out)
	}
	refs := c.RootRefs()
	if len(refs) != 1 {
		t.Fatalf("expected one pinned root, got %+v", refs)
	}
	for ref, want := range map[string]string{refs[0].BeforeRef: "a\nB\nc\n", refs[0].AfterRef: "a\nC\nc\n"} {
		body, err := git(context.Background(), root, 64, "show", ref+":small.txt")
		if err != nil || string(body) != want {
			t.Fatalf("snapshot %s: body %q, err %v, want %q", ref, body, err, want)
		}
	}
	current, err := os.ReadFile(index)
	if err != nil || !reflect.DeepEqual(current, seed) {
		t.Fatalf("collector changed the real index: %v", err)
	}
	info, err := os.Stat(index)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("collector changed the real index timestamp: %v", err)
	}
}
