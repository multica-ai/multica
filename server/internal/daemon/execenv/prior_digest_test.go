package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initDigestRepo creates a repo with a main branch, then `count` commits on
// the given feature branch (checked out), returning the workdir.
func initDigestRepo(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	git("commit", "--allow-empty", "-m", "base")
	if count > 0 {
		git("checkout", "-b", "agent/fix-1")
		for i := 0; i < count; i++ {
			git("commit", "--allow-empty", "-m", "prior run commit")
		}
	}
	return dir
}

func writeDigestContext(t *testing.T, dir string, resumed bool) string {
	t.Helper()
	if err := writeContextFiles(dir, "", TaskContextForEnv{
		IssueID:             "issue-digest",
		PriorSessionResumed: resumed,
	}, nil); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("read issue_context.md: %v", err)
	}
	return string(data)
}

func TestPriorRunDigestRenderedOnResume(t *testing.T) {
	dir := initDigestRepo(t, 2)
	md := writeDigestContext(t, dir, true)
	for _, want := range []string{"## Prior Run Digest", "`agent/fix-1`", "**Commits ahead of main:** 2", "prior run commit"} {
		if !strings.Contains(md, want) {
			t.Errorf("digest section missing %q\n---\n%s", want, md)
		}
	}
}

func TestNoPriorRunDigestOnColdStart(t *testing.T) {
	dir := initDigestRepo(t, 2)
	if md := writeDigestContext(t, dir, false); strings.Contains(md, "Prior Run Digest") {
		t.Errorf("cold start must not render digest\n---\n%s", md)
	}
}

func TestNoPriorRunDigestOutsideGitRepo(t *testing.T) {
	dir := t.TempDir() // no git repo
	if md := writeDigestContext(t, dir, true); strings.Contains(md, "Prior Run Digest") {
		t.Errorf("non-repo resume must omit digest, not crash\n---\n%s", md)
	}
}

func TestCollectPriorRunDigestTruncatesAt1KB(t *testing.T) {
	dir := initDigestRepo(t, 0)
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("config", "user.email", "t@t")
	git("config", "user.name", "t")
	long := strings.Repeat("x", 3000)
	git("commit", "--allow-empty", "-m", long)
	d := collectPriorRunDigest(dir)
	if len(d) > 1024+len("\n…(truncated)")+64 {
		t.Errorf("digest = %d bytes, want capped near 1KB", len(d))
	}
}
