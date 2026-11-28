package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCommittedHermesInstalls(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, "installs", "synthetic-install", "pm-runtime", "selected.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"generation\":\"test\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHermesDependencyEnvironmentCommitted(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("empty home should be cold")
	}
	if err := os.MkdirAll(filepath.Join(home, "installs", "id"), 0o700); err != nil {
		t.Fatal(err)
	}
	if hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("empty installs should be cold")
	}

	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Unrelated files do not count as a committed environment.
	mustWrite("installs/id/README.txt", "notes")
	if hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("unrelated files should not count as committed")
	}

	mustWrite("installs/id/pm-runtime/selected.json", `{"generation":"source"}`)
	if !hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("selected.json should count as committed")
	}
}

func TestHermesDependencyEnvironmentCommittedSymlink(t *testing.T) {
	t.Parallel()
	source, home := t.TempDir(), t.TempDir()
	record := filepath.Join(source, "id", "pm-runtime", "current.json")
	if err := os.MkdirAll(filepath.Dir(record), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"generation":"source"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(home, "installs")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if !hermesDependencyEnvironmentCommitted(home) {
		t.Fatal("directory symlink to a committed source should count")
	}
}
