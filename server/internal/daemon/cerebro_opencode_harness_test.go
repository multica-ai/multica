package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareOpenCodeHarnessInstallsTheGate(t *testing.T) {
	workdir := t.TempDir()

	if err := prepareOpenCodeHarness("opencode", workdir); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(workdir, ".opencode", "plugin", "multica-tool-policy.js"))
	if err != nil {
		t.Fatalf("plugin not installed where OpenCode loads it: %v", err)
	}
	if !strings.Contains(string(body), "tool.execute.before") {
		t.Fatal("installed plugin does not register the before-call hook")
	}
}

// The plugin is OpenCode's only call-time gate, so a workdir the daemon cannot
// write must stop the spawn instead of starting an unenforced run.
func TestPrepareOpenCodeHarnessFailsClosedOnUnwritableWorkdir(t *testing.T) {
	parent := t.TempDir()
	workdir := filepath.Join(parent, "locked")
	if err := os.Mkdir(workdir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workdir, 0o700) })

	if err := prepareOpenCodeHarness("opencode", workdir); err == nil {
		t.Fatal("prepare = nil error, want the spawn refused when the gate cannot be installed")
	}
}

// Every other provider keeps its own adapter and must be untouched.
func TestPrepareOpenCodeHarnessIgnoresOtherProviders(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "pi", "hermes"} {
		workdir := t.TempDir()
		if err := prepareOpenCodeHarness(provider, workdir); err != nil {
			t.Fatalf("prepare(%q) = %v, want no-op", provider, err)
		}
		if _, err := os.Stat(filepath.Join(workdir, ".opencode")); !os.IsNotExist(err) {
			t.Fatalf("prepare(%q) wrote an OpenCode plugin dir", provider)
		}
	}
}
