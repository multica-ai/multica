package pkmfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestFS creates an allowlist root + workspace base inside t.TempDir().
// Layout on disk:
//
//	<tmp>/root/                  ← allowlist root (MULTICA_PKM_ROOT)
//	<tmp>/root/PKM/PROJECTS/     ← workspace base (pkm_path)
func newTestFS(t *testing.T) (*FS, string) {
	t.Helper()
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "root")
	if err := os.MkdirAll(filepath.Join(allowRoot, "PKM", "PROJECTS"), 0o755); err != nil {
		t.Fatal(err)
	}
	fs, err := New(allowRoot, "PKM/PROJECTS")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs, allowRoot
}

func TestCleanRel_Traversal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", nil},
		{"dot", ".", nil},
		{"slash", "/", nil},
		{"valid file", "notes.md", nil},
		{"valid nested", "sub/notes.md", nil},
		{"absolute", "/etc/passwd", ErrTraversal},
		{"dotdot leading", "../etc/passwd", ErrTraversal},
		{"dotdot only", "..", ErrTraversal},
		{"dotdot middle", "a/../../b", ErrTraversal},
		{"dotdot inside cleans-out", "a/../b", nil},
		{"null byte", "a\x00b", ErrInvalidPath},
		{"backslash", "a\\b", ErrInvalidPath},
		{"trailing dotdot", "foo/..", nil},
		{"trailing dotdot escapes", "foo/../..", ErrTraversal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cleanRel(tc.in)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("expected ok, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestNew_RejectsBaseOutsideRoot(t *testing.T) {
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "root")
	if err := os.MkdirAll(allowRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := New(allowRoot, "../escape"); !errors.Is(err, ErrTraversal) {
		t.Fatalf("expected ErrTraversal, got %v", err)
	}
}

func TestNew_RejectsMissingBase(t *testing.T) {
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "root")
	if err := os.MkdirAll(allowRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := New(allowRoot, "PKM/missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestWriteFile_AtomicAndReadable(t *testing.T) {
	fs, root := newTestFS(t)
	if err := fs.WriteFile("notes.md", []byte("hello")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "PKM", "PROJECTS", "notes.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	// No leftover temp files in the directory.
	entries, err := os.ReadDir(filepath.Join(root, "PKM", "PROJECTS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file leaked: %s", e.Name())
		}
	}
}

func TestWriteFile_OverwriteExisting(t *testing.T) {
	fs, root := newTestFS(t)
	if err := fs.WriteFile("notes.md", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("notes.md", []byte("v2")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "PKM", "PROJECTS", "notes.md"))
	if string(got) != "v2" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteFile_RejectsNonMarkdown(t *testing.T) {
	fs, _ := newTestFS(t)
	for _, name := range []string{"notes.txt", "notes", "notes.md.exe", "../notes.md"} {
		if err := fs.WriteFile(name, []byte("x")); err == nil {
			t.Fatalf("expected error for %q", name)
		}
	}
}

func TestWriteFile_RejectsTraversalVectors(t *testing.T) {
	fs, _ := newTestFS(t)
	cases := []string{
		"../escape.md",
		"../../escape.md",
		"sub/../../escape.md",
		"/abs.md",
		"a\x00.md",
	}
	for _, p := range cases {
		err := fs.WriteFile(p, []byte("x"))
		if err == nil {
			t.Fatalf("expected error for %q", p)
		}
	}
}

func TestWriteFile_RefusesSymlinkAtLeaf(t *testing.T) {
	fs, root := newTestFS(t)
	target := filepath.Join(root, "outside.md")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "PKM", "PROJECTS", "evil.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := fs.WriteFile("evil.md", []byte("pwn"))
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("expected ErrSymlink, got %v", err)
	}
	// Original target untouched.
	got, _ := os.ReadFile(target)
	if string(got) != "secret" {
		t.Fatalf("symlink follow leaked write: %q", got)
	}
}

func TestWriteFile_RejectsSymlinkEscapeInBase(t *testing.T) {
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "root")
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(filepath.Join(allowRoot, "PKM"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink PKM/PROJECTS → outside (the workspace base points OUT of the root).
	if err := os.Symlink(outside, filepath.Join(allowRoot, "PKM", "PROJECTS")); err != nil {
		t.Fatal(err)
	}
	// New should refuse to open this base.
	if _, err := New(allowRoot, "PKM/PROJECTS"); err == nil {
		t.Fatalf("expected base symlink escape to be rejected")
	}
}

func TestWriteFile_RejectsParentSymlinkEscape(t *testing.T) {
	tmp := t.TempDir()
	allowRoot := filepath.Join(tmp, "root")
	outside := filepath.Join(tmp, "outside")
	if err := os.MkdirAll(filepath.Join(allowRoot, "PKM", "PROJECTS"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink PKM/PROJECTS/escape → outside (a child of base points out).
	if err := os.Symlink(outside, filepath.Join(allowRoot, "PKM", "PROJECTS", "escape")); err != nil {
		t.Fatal(err)
	}
	fs, err := New(allowRoot, "PKM/PROJECTS")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	// Writing through the escape symlink must fail (os.Root rejects symlinks
	// that resolve outside the root).
	err = fs.WriteFile("escape/pwn.md", []byte("x"))
	if err == nil {
		t.Fatalf("expected escape write to fail")
	}
	// Outside dir untouched.
	if _, err := os.Stat(filepath.Join(outside, "pwn.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("write escaped allowlist root: %v", err)
	}
}

func TestCreateFile_NewAndConflict(t *testing.T) {
	fs, _ := newTestFS(t)
	if err := fs.CreateFile("a.md", []byte("x")); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := fs.CreateFile("a.md", []byte("y"))
	if !errors.Is(err, ErrExist) {
		t.Fatalf("expected ErrExist, got %v", err)
	}
}

func TestCreateFile_ParentMissing(t *testing.T) {
	fs, _ := newTestFS(t)
	err := fs.CreateFile("missing/a.md", []byte("x"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateFolder_NewAndConflict(t *testing.T) {
	fs, _ := newTestFS(t)
	if err := fs.CreateFolder("sub"); err != nil {
		t.Fatal(err)
	}
	if err := fs.CreateFolder("sub"); !errors.Is(err, ErrExist) {
		t.Fatalf("expected ErrExist, got %v", err)
	}
}

func TestCreateFolder_ParentMissing(t *testing.T) {
	fs, _ := newTestFS(t)
	err := fs.CreateFolder("a/b/c")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	fs, root := newTestFS(t)
	if err := fs.CreateFile("a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteFile("a.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "PKM", "PROJECTS", "a.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file not removed: %v", err)
	}
	if err := fs.DeleteFile("a.md"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteFile_RejectsSymlink(t *testing.T) {
	fs, root := newTestFS(t)
	target := filepath.Join(root, "outside.md")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "PKM", "PROJECTS", "evil.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteFile("evil.md"); !errors.Is(err, ErrSymlink) {
		t.Fatalf("expected ErrSymlink, got %v", err)
	}
}

func TestDeleteFolder_EmptyAndForce(t *testing.T) {
	fs, root := newTestFS(t)
	if err := fs.CreateFolder("empty"); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteFolder("empty", false); err != nil {
		t.Fatalf("delete empty: %v", err)
	}
	// Non-empty: should refuse without force.
	if err := fs.CreateFolder("full"); err != nil {
		t.Fatal(err)
	}
	if err := fs.CreateFile("full/a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteFolder("full", false); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("expected ErrNotEmpty, got %v", err)
	}
	// Force succeeds.
	if err := fs.DeleteFolder("full", true); err != nil {
		t.Fatalf("force delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "PKM", "PROJECTS", "full")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder not removed")
	}
}

func TestDeleteFolder_RejectsBaseAndFile(t *testing.T) {
	fs, _ := newTestFS(t)
	if err := fs.DeleteFolder("", false); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("delete base: expected ErrInvalidPath, got %v", err)
	}
	if err := fs.CreateFile("a.md", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := fs.DeleteFolder("a.md", false); !errors.Is(err, ErrNotFolder) {
		t.Fatalf("expected ErrNotFolder, got %v", err)
	}
}
