package projectspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRootDefaultAndOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	got, err := ResolveRoot("", DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(DefaultRoot)
	if got != want {
		t.Fatalf("default root = %q, want %q", got, want)
	}

	custom := filepath.Join(t.TempDir(), "custom")
	got, err = ResolveRoot(custom, DefaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got != custom {
		t.Fatalf("custom root = %q, want %q", got, custom)
	}
}

func TestNormalizeRelativePath(t *testing.T) {
	valid := map[string]string{
		"folder/file.pdf":    "folder/file.pdf",
		"folder\\nested.txt": "folder/nested.txt",
	}
	for input, want := range valid {
		got, err := NormalizeRelativePath(input)
		if err != nil || got != want {
			t.Errorf("NormalizeRelativePath(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"", "/etc/passwd", "../secret", "a/../../b", ".ai/file", ".multica/x", "a/\x00b"} {
		if _, err := NormalizeRelativePath(input); err == nil {
			t.Errorf("NormalizeRelativePath(%q) unexpectedly succeeded", input)
		}
	}
}

func TestNormalizeBatchNameRejectsNestedPath(t *testing.T) {
	if _, err := NormalizeBatchName("nested/batch"); err == nil {
		t.Fatal("expected nested batch name to be rejected")
	}
	if got, err := NormalizeBatchName("research"); err != nil || got != "research" {
		t.Fatalf("NormalizeBatchName() = %q, %v", got, err)
	}
}

func TestResolveProjectPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	staging := t.TempDir()
	svc, err := NewForTest(root, staging)
	if err != nil {
		t.Fatal(err)
	}
	projectRoot, err := svc.EnsureProject("workspace-1", "project-1")
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(projectRoot, "knowledge", "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := svc.ResolveProjectPath("workspace-1", "project-1", "knowledge/escape/file.txt"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestEnsureProjectAndList(t *testing.T) {
	svc, err := NewForTest(filepath.Join(t.TempDir(), "spaces"), filepath.Join(t.TempDir(), "staging"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := svc.EnsureProject("workspace-1", "project-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.md")); err != nil {
		t.Fatalf("index.md missing: %v", err)
	}
	target, err := svc.ResolveProjectPath("workspace-1", "project-1", "knowledge/note.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("note"), 0o640); err != nil {
		t.Fatal(err)
	}
	entries, err := svc.List("workspace-1", "project-1", "knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RelativePath != "knowledge/note.md" {
		t.Fatalf("entries = %#v", entries)
	}
}
