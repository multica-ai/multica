//go:build !windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeCLI(t *testing.T, dir string, mode os.FileMode) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "multica")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestShadowingCLIOnPath(t *testing.T) {
	root := t.TempDir()
	selfDir := filepath.Join(root, "desktop", "bin")
	self := writeFakeCLI(t, selfDir, 0o755)

	linkDir := filepath.Join(root, "link")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(linkDir, "multica")); err != nil {
		t.Fatal(err)
	}
	brewDir := filepath.Join(root, "brew", "bin")
	brew := writeFakeCLI(t, brewDir, 0o755)
	plainDir := filepath.Join(root, "plain")
	writeFakeCLI(t, plainDir, 0o644)
	emptyDir := filepath.Join(root, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	join := func(dirs ...string) string { return strings.Join(dirs, string(os.PathListSeparator)) }
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "only self", path: join(emptyDir, selfDir), want: ""},
		{name: "symlink to self", path: join(linkDir, selfDir), want: ""},
		{name: "other copy ahead", path: join(brewDir, selfDir), want: brew},
		{name: "other copy behind self still shadows after rc reordering", path: join(selfDir, brewDir), want: brew},
		{name: "non-executable file ignored", path: join(plainDir, selfDir), want: ""},
		{name: "relative and empty entries ignored", path: join("", "brew/bin", selfDir), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shadowingCLIOnPath(self, tt.path); got != tt.want {
				t.Fatalf("shadowingCLIOnPath() = %q, want %q", got, tt.want)
			}
		})
	}

	if got := shadowingCLIOnPath(filepath.Join(root, "missing"), join(brewDir)); got != "" {
		t.Fatalf("missing self binary must not report a shadow, got %q", got)
	}
}

func TestBuildPromptShadowedCLIBlock(t *testing.T) {
	task := Task{QuickCreatePrompt: "fix the login button color"}
	plain := BuildPrompt(task, "claude")
	if strings.Contains(plain, "## Multica CLI binary") {
		t.Fatal("block must not render without a detected shadow")
	}

	self := "/Applications/Multica Dev.app/Contents/Resources/bin/multica"
	out := BuildPrompt(task, "claude", WithShadowedCLI(self, "/opt/homebrew/bin/multica"))
	for _, want := range []string{
		"## Multica CLI binary",
		"Another copy is installed at `/opt/homebrew/bin/multica`",
		"`'/Applications/Multica Dev.app/Contents/Resources/bin/multica' issue create ...`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, plain) {
		t.Fatal("shadow block must be appended after the stable prompt body")
	}
}

func TestBuildShadowedCLIBlockRejectsUnrenderablePaths(t *testing.T) {
	if got := buildShadowedCLIBlock("/bin/mul`tica", "/opt/homebrew/bin/multica"); got != "" {
		t.Fatalf("path with a backtick must not render, got %q", got)
	}
	if got := buildShadowedCLIBlock("/usr/local/bin/multica", ""); got != "" {
		t.Fatalf("missing shadow must not render, got %q", got)
	}
}

func TestPosixShellQuote(t *testing.T) {
	tests := map[string]string{
		"/usr/local/bin/multica":    "/usr/local/bin/multica",
		"/Apps/Multica Dev/multica": "'/Apps/Multica Dev/multica'",
		"/it's/multica":             `'/it'\''s/multica'`,
	}
	for in, want := range tests {
		if got := posixShellQuote(in); got != want {
			t.Fatalf("posixShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
