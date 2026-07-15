package sessionmode

import "testing"

func TestNextVersionIncrementsPositiveVersion(t *testing.T) {
	if got := nextVersion(1); got != 2 {
		t.Fatalf("nextVersion(1) = %d, want 2", got)
	}
	if got := nextVersion(0); got != 1 {
		t.Fatalf("nextVersion(0) = %d, want 1", got)
	}
}

func TestConfigForVersionReturnsIndependentSnapshot(t *testing.T) {
	config := DefaultConfigs()[Plan]
	config.AllowedTools = []string{"read_file"}
	got := configForVersion(config, 4)
	got.AllowedTools[0] = "write_file"
	if config.Version != "" && config.Version != "1" {
		t.Fatalf("source version changed: %q", config.Version)
	}
	if config.AllowedTools[0] != "read_file" {
		t.Fatal("source slice was mutated")
	}
	if got.Version != "4" {
		t.Fatalf("snapshot version = %q, want 4", got.Version)
	}
}
