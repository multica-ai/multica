package agent

import (
	"log/slog"
	"testing"
)

// TestLangcliRuntimeIdentity pins the langcli descriptor: it is a Claude Code
// fork, so it must dispatch to the claude backend under its own executable and
// label while inheriting the family's protocol.
func TestLangcliRuntimeIdentity(t *testing.T) {
	desc, ok := BuiltinRuntimeByID("langcli")
	if !ok {
		t.Fatal("BuiltinRuntimeByID(langcli) = false, want true")
	}
	if desc.ProtocolFamily != "claude" {
		t.Errorf("ProtocolFamily = %q, want %q", desc.ProtocolFamily, "claude")
	}
	if got := ProtocolFamilyFor("langcli"); got != "claude" {
		t.Errorf("ProtocolFamilyFor(langcli) = %q, want %q", got, "claude")
	}
	if desc.SkillsDir != ".claude/skills" {
		t.Errorf("SkillsDir = %q, want %q", desc.SkillsDir, ".claude/skills")
	}

	b, err := NewRuntime("langcli", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("NewRuntime(langcli): %v", err)
	}
	cb, ok := b.(*claudeBackend)
	if !ok {
		t.Fatalf("NewRuntime(langcli) returned %T, want *claudeBackend", b)
	}
	if cb.defaultExecutable != "langcli" {
		t.Errorf("defaultExecutable = %q, want %q", cb.defaultExecutable, "langcli")
	}
	if cb.providerLabel != "langcli" {
		t.Errorf("providerLabel = %q, want %q", cb.providerLabel, "langcli")
	}
	if cb.configHomeDir != ".langcli" {
		t.Errorf("configHomeDir = %q, want %q", cb.configHomeDir, ".langcli")
	}
	if got := cb.executable(); got != "langcli" {
		t.Errorf("executable() = %q, want %q", got, "langcli")
	}
	if got := cb.label(); got != "langcli" {
		t.Errorf("label() = %q, want %q", got, "langcli")
	}
}

// TestClaudeBackendDefaults guards the protocol family: without descriptor
// overrides the claude backend keeps its historical executable and label.
func TestClaudeBackendDefaults(t *testing.T) {
	b, err := New("claude", Config{Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(claude): %v", err)
	}
	cb, ok := b.(*claudeBackend)
	if !ok {
		t.Fatalf("New(claude) returned %T, want *claudeBackend", b)
	}
	if got := cb.executable(); got != "claude" {
		t.Errorf("executable() = %q, want %q", got, "claude")
	}
	if got := cb.label(); got != "claude" {
		t.Errorf("label() = %q, want %q", got, "claude")
	}
}

// TestBuiltinRuntimeDescriptorsAreConsistent guards the descriptor contract:
// every identity must name a supported protocol family, carry a unique command
// and identity, and declare the directories the daemon resolves from it.
func TestBuiltinRuntimeDescriptorsAreConsistent(t *testing.T) {
	ids := map[string]bool{}
	commands := map[string]bool{}
	for _, desc := range BuiltinRuntimes {
		if !IsSupportedType(desc.ProtocolFamily) {
			t.Errorf("%s: ProtocolFamily %q is not in SupportedTypes", desc.ID, desc.ProtocolFamily)
		}
		if IsSupportedType(desc.ID) {
			t.Errorf("%s: runtime identity must not also be a protocol family", desc.ID)
		}
		if ids[desc.ID] {
			t.Errorf("%s: duplicate runtime identity", desc.ID)
		}
		ids[desc.ID] = true
		if desc.DefaultCommand == "" || commands[desc.DefaultCommand] {
			t.Errorf("%s: DefaultCommand %q is empty or duplicated", desc.ID, desc.DefaultCommand)
		}
		commands[desc.DefaultCommand] = true
		if desc.DisplayName == "" || desc.SkillsDir == "" || desc.UserSkillsDir == "" || desc.LaunchHeader == "" {
			t.Errorf("%s: descriptor is missing display/skills/launch fields", desc.ID)
		}
	}
}
