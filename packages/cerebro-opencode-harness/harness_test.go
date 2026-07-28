package opencodeharness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWritesPluginWhereOpenCodeLooksForIt(t *testing.T) {
	workdir := t.TempDir()

	path, err := Prepare(workdir)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	want := filepath.Join(workdir, ".opencode", "plugin", "multica-tool-policy.js")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	if !strings.Contains(string(body), "tool.execute.before") {
		t.Fatal("plugin does not register the before-call hook")
	}
	if !Installed(workdir) {
		t.Fatal("Installed = false right after Prepare")
	}
}

func TestPrepareRequiresWorkdir(t *testing.T) {
	if _, err := Prepare("  "); err == nil {
		t.Fatal("Prepare(\"\") = nil error, want rejection")
	}
}

// A workdir is reused across turns for the same agent+issue, so a stale or
// tampered plugin must be replaced — and must not be reported as installed
// until it is.
func TestPrepareOverwritesTamperedPlugin(t *testing.T) {
	workdir := t.TempDir()
	path, err := Prepare(workdir)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.WriteFile(path, []byte("export const Gone = async () => ({})\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if Installed(workdir) {
		t.Fatal("Installed = true for a tampered plugin")
	}

	if _, err := Prepare(workdir); err != nil {
		t.Fatalf("re-prepare: %v", err)
	}
	if !Installed(workdir) {
		t.Fatal("Installed = false after re-prepare")
	}
}

func TestInstalledFalseWhenMissing(t *testing.T) {
	if Installed(t.TempDir()) {
		t.Fatal("Installed = true with no plugin on disk")
	}
}
