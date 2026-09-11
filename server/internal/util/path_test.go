package util

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSymlinksBestEffort pins the property every containment check built
// on this helper depends on: the returned path is in the SAME namespace as
// filepath.EvalSymlinks would produce for the existing part of the input, no
// matter how much of the tail is missing. filepath.EvalSymlinks alone fails on
// any missing component, and the obvious fallbacks (Clean, or a single Dir
// step) silently return an unresolved path, which makes a path inside a
// symlinked root compare as outside it.
func TestResolveSymlinksBestEffort(t *testing.T) {
	// realRoot is the canonical form of a directory reached through a symlink,
	// so every expectation below can be written against the resolved namespace.
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "existing"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(physical, "existing", "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	logical := filepath.Join(root, "logical")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	realPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatalf("resolve physical: %v", err)
	}
	if err := os.Symlink(filepath.Join(physical, "nowhere"), filepath.Join(physical, "dangling")); err != nil {
		t.Fatalf("symlink dangling: %v", err)
	}

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "existing file behind a symlinked ancestor",
			in:   filepath.Join(logical, "existing", "file.txt"),
			want: filepath.Join(realPhysical, "existing", "file.txt"),
		},
		{
			name: "missing leaf keeps the resolved parent",
			in:   filepath.Join(logical, "existing", "missing.txt"),
			want: filepath.Join(realPhysical, "existing", "missing.txt"),
		},
		{
			name: "missing intermediate directory still resolves the ancestor",
			in:   filepath.Join(logical, "subdir", "file.txt"),
			want: filepath.Join(realPhysical, "subdir", "file.txt"),
		},
		{
			name: "several missing levels still resolve the ancestor",
			in:   filepath.Join(logical, "a", "b", "c", "d", "file.txt"),
			want: filepath.Join(realPhysical, "a", "b", "c", "d", "file.txt"),
		},
		{
			name: "dangling symlink is not followed but its parent is resolved",
			in:   filepath.Join(logical, "dangling"),
			want: filepath.Join(realPhysical, "dangling"),
		},
		{
			name: "directory itself",
			in:   logical,
			want: realPhysical,
		},
		{
			name: "empty input is returned unchanged",
			in:   "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveSymlinksBestEffort(tc.in); got != tc.want {
				t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	t.Run("relative input is made absolute against the working directory", func(t *testing.T) {
		t.Chdir(logical)
		got := ResolveSymlinksBestEffort(filepath.Join("subdir", "file.txt"))
		want := filepath.Join(realPhysical, "subdir", "file.txt")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(relative) = %q, want %q", got, want)
		}
	})

	t.Run("dot-dot is applied after following a symlink, not to the string", func(t *testing.T) {
		// The distinction a containment check lives or dies by: lexically
		// "link/.." cancels out and the path looks like it never left, while the
		// kernel follows the link first and then goes up — landing next to the
		// link's TARGET. Anything that cleans before resolving reports the wrong
		// namespace here, and does so for a path that exists and can be read.
		sep := string(filepath.Separator)
		got := ResolveSymlinksBestEffort(filepath.Join(logical, "existing") + sep + ".." + sep + "sibling.md")
		want := filepath.Join(realPhysical, "sibling.md")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(dot-dot) = %q, want %q", got, want)
		}

		outsideRoot := t.TempDir()
		target := filepath.Join(outsideRoot, "shared")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(outsideRoot, "other-run.md"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		link := filepath.Join(physical, "escape")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		realOutside, err := filepath.EvalSymlinks(outsideRoot)
		if err != nil {
			t.Fatalf("resolve outside: %v", err)
		}
		got = ResolveSymlinksBestEffort(link + sep + ".." + sep + "other-run.md")
		want = filepath.Join(realOutside, "other-run.md")
		if got != want {
			t.Errorf("ResolveSymlinksBestEffort(escape/..) = %q, want %q", got, want)
		}
	})

	t.Run("a fully missing path keeps its lexical tail under the resolved root", func(t *testing.T) {
		// "/" always resolves, so the walk finds an existing ancestor at the
		// root and re-attaches everything below it lexically. Note what this
		// case does NOT cover: the loop's parent == cur guard, which stays
		// unreached because the root resolves. See the comment on that branch —
		// it is a termination invariant, and no cheap test on either platform
		// can reach it.
		in := filepath.Join(string(filepath.Separator), "multica-does-not-exist-0d1f", "a", "b")
		want, err := filepath.Abs(in)
		if err != nil {
			t.Fatalf("abs: %v", err)
		}
		if got := ResolveSymlinksBestEffort(in); got != want {
			t.Errorf("ResolveSymlinksBestEffort(%q) = %q, want %q", in, got, want)
		}
	})
}
