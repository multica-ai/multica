package codechange

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	return dir
}

func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", message)
	return git(t, dir, "rev-parse", "HEAD")
}

func TestCollectListsEveryKindOfChange(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "keep.go", "package a\n\nfunc A() {}\n")
	write(t, dir, "gone.txt", "bye\n")
	write(t, dir, "old/name.md", strings.Repeat("stable line\n", 20))
	write(t, dir, "logo.bin", "\x00\x01\x02")
	base := commitAll(t, dir, "base")

	write(t, dir, "keep.go", "package a\n\nfunc A() { B() }\n\nfunc B() {}\n")
	git(t, dir, "rm", "-q", "gone.txt")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "mv", "old/name.md", "docs/新名字.md")
	write(t, dir, "docs/新名字.md", strings.Repeat("stable line\n", 20)+"one more\n")
	write(t, dir, "added.ts", "export const x = 1;\n")
	write(t, dir, "logo.bin", "\x00\x03\x04\x05")
	head := commitAll(t, dir, "head")

	diff, err := Collect(context.Background(), dir, base, head)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	byPath := map[string]File{}
	for _, f := range diff.Files {
		byPath[f.Path] = f
	}
	want := map[string]string{
		"keep.go":     "modified",
		"gone.txt":    "deleted",
		"docs/新名字.md": "renamed",
		"added.ts":    "added",
		"logo.bin":    "modified",
	}
	if diff.FileCount != len(want) || len(diff.Files) != len(want) {
		t.Fatalf("files = %+v", diff.Files)
	}
	for path, status := range want {
		if byPath[path].Status != status {
			t.Errorf("%s status = %q, want %q", path, byPath[path].Status, status)
		}
	}
	if f := byPath["docs/新名字.md"]; f.OldPath != "old/name.md" || f.Additions != 1 || f.Deletions != 0 {
		t.Errorf("rename = %+v", f)
	}
	if f := byPath["keep.go"]; f.Additions != 3 || f.Deletions != 1 {
		t.Errorf("keep.go counts = +%d -%d", f.Additions, f.Deletions)
	}
	if !byPath["logo.bin"].Binary {
		t.Error("logo.bin should be binary")
	}
	if diff.Additions != 3+1+1 || diff.Deletions != 1+1 {
		t.Errorf("totals = +%d -%d", diff.Additions, diff.Deletions)
	}
	for _, s := range []string{"diff --git a/keep.go b/keep.go", "rename to docs/新名字.md", "+export const x = 1;", "new file mode"} {
		if !strings.Contains(diff.Patch, s) {
			t.Errorf("patch missing %q", s)
		}
	}
}

// User configuration must not change the patch's shape: the viewer parses
// a/ b/ prefixes and plain text.
func TestCollectIgnoresUserDiffConfig(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	base := commitAll(t, dir, "base")
	write(t, dir, "a.txt", "two\n")
	head := commitAll(t, dir, "head")
	git(t, dir, "config", "diff.noprefix", "true")
	git(t, dir, "config", "color.diff", "always")

	diff, err := Collect(context.Background(), dir, base, head)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !strings.Contains(diff.Patch, "--- a/a.txt\n+++ b/a.txt") || strings.Contains(diff.Patch, "\x1b[") {
		t.Fatalf("patch shape changed by user config:\n%q", diff.Patch)
	}
}

func TestCollectOmitsAnOversizedPatch(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "seed.txt", "x\n")
	base := commitAll(t, dir, "base")
	write(t, dir, "big.txt", strings.Repeat("a fairly long line of generated output\n", MaxPatchBytes/30))
	head := commitAll(t, dir, "head")

	diff, err := Collect(context.Background(), dir, base, head)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diff.PatchOmitted != "too_large" || diff.Patch != "" {
		t.Fatalf("patch omitted = %q, patch bytes = %d", diff.PatchOmitted, len(diff.Patch))
	}
	if diff.FileCount != 1 || diff.Files[0].Path != "big.txt" || diff.Additions == 0 {
		t.Fatalf("file list must survive an oversized patch: %+v", diff.Files)
	}
}

func TestCollectEmptyRange(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "one\n")
	base := commitAll(t, dir, "base")
	head := commitAll(t, dir, "empty")
	diff, err := Collect(context.Background(), dir, base, head)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !diff.Empty() || diff.Patch != "" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestIdentify(t *testing.T) {
	dir := newRepo(t)
	if got := Identify(context.Background(), dir, "/work/app"); got.Key != "path:/work/app" || got.Label != "app" || got.URL != "" {
		t.Fatalf("no remote: %+v", got)
	}
	git(t, dir, "remote", "add", "origin", "https://x-access-token:ghs_secret@github.com/Acme/App.git")
	got := Identify(context.Background(), dir, "/work/app")
	if got.URL != "https://github.com/Acme/App.git" || got.Label != "Acme/App" || got.Key != "https://github.com/acme/app" {
		t.Fatalf("with remote: %+v", got)
	}
}

func TestOwnerRepoFromRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:acme/app.git":           "acme/app",
		"https://gitlab.com/group/sub/proj.git": "sub/proj",
		"ssh://git@host:22/acme/app":            "acme/app",
		"/local/path":                           "local/path",
		"repo":                                  "",
	}
	for in, want := range cases {
		if got := ownerRepoFromRemote(in); got != want {
			t.Errorf("ownerRepoFromRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteDefaultRef(t *testing.T) {
	upstream := newRepo(t)
	write(t, upstream, "a.txt", "one\n")
	commitAll(t, upstream, "base")

	clone := t.TempDir()
	git(t, clone, "clone", "-q", upstream, ".")
	ref, sha := RemoteDefaultRef(context.Background(), clone)
	if ref != "origin/main" || sha == "" {
		t.Fatalf("RemoteDefaultRef = %q, %q", ref, sha)
	}
}
