package cli

import (
	"path/filepath"
	"testing"
)

func TestCLIConfigPathForInstance_UsesExplicitConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, "custom", "dev.json")
	got, err := CLIConfigPathForInstance("ignored", want)
	if err != nil {
		t.Fatalf("CLIConfigPathForInstance: %v", err)
	}
	if got != want {
		t.Fatalf("CLIConfigPathForInstance() = %q, want %q", got, want)
	}
}

func TestStateDirForInstance_UsesConfigParentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, "instances", "local", "config.json")
	got, err := StateDirForInstance("ignored", configPath)
	if err != nil {
		t.Fatalf("StateDirForInstance: %v", err)
	}
	want := filepath.Dir(configPath)
	if got != want {
		t.Fatalf("StateDirForInstance() = %q, want %q", got, want)
	}
}
