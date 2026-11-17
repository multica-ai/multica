package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexDeveloperInstructionsRecord(t *testing.T) {
	store := filepath.Join(t.TempDir(), "store")
	const session = "019a0000-1111-2222-3333-444455556666"

	if CodexDeveloperInstructionsMatch(store, session, "brief A") {
		t.Fatal("a thread without a record must not count as matching")
	}
	if err := RecordCodexDeveloperInstructions(store, session, "brief A"); err != nil {
		t.Fatal(err)
	}
	if !CodexDeveloperInstructionsMatch(store, session, "brief A") {
		t.Fatal("recorded instructions must match")
	}
	if CodexDeveloperInstructionsMatch(store, session, "brief B") {
		t.Fatal("changed instructions must not match")
	}
	if CodexDeveloperInstructionsMatch(store, "019a0000-other", "brief A") {
		t.Fatal("a record belongs to one thread only")
	}

	// The record sits beside Codex's rollout tree without looking like part
	// of it: no date directory, no rollout-*.jsonl, and no leftover temp file.
	entries, err := os.ReadDir(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].IsDir() || !strings.HasPrefix(entries[0].Name(), ".") || strings.HasPrefix(entries[0].Name(), "rollout-") {
		t.Fatalf("unexpected store contents: %v", entries)
	}

	if CodexDeveloperInstructionsMatch("", session, "brief A") || RecordCodexDeveloperInstructions("", session, "brief A") == nil {
		t.Fatal("no store means no record")
	}
	if RecordCodexDeveloperInstructions(store, "../..", "brief A") == nil {
		t.Fatal("a session id without safe characters must be refused")
	}
}

// The app-server parameter replaces config.toml's developer_instructions, so
// the user's configured value must ride along in front of the brief.
func TestComposeCodexDeveloperInstructions(t *testing.T) {
	home := t.TempDir()
	if got, err := ComposeCodexDeveloperInstructions(home, "BRIEF"); err != nil || got != "BRIEF" {
		t.Fatalf("no config: %q, %v", got, err)
	}
	writeFile(t, filepath.Join(home, "config.toml"), "model = \"gpt\"\n")
	if got, err := ComposeCodexDeveloperInstructions(home, "BRIEF"); err != nil || got != "BRIEF" {
		t.Fatalf("config without developer_instructions: %q, %v", got, err)
	}
	writeFile(t, filepath.Join(home, "config.toml"), "developer_instructions = \"\"\"\nAlways run the linter.\n\"\"\"\n")
	if got, err := ComposeCodexDeveloperInstructions(home, "BRIEF"); err != nil || got != "Always run the linter.\n\nBRIEF" {
		t.Fatalf("configured instructions: %q, %v", got, err)
	}
	writeFile(t, filepath.Join(home, "config.toml"), "developer_instructions = [\n")
	if got, err := ComposeCodexDeveloperInstructions(home, "BRIEF"); err == nil || got != "BRIEF" {
		t.Fatalf("broken config: %q, %v", got, err)
	}
}
