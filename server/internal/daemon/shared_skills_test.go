package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSharedSkillScanRootUsesProviderDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root, ok := sharedSkillScanRoot(Config{}, "pi")
	if !ok {
		t.Fatal("expected pi shared root")
	}
	want := filepath.Join(home, ".pi", "share", "skills")
	if root != want {
		t.Fatalf("got %q want %q", root, want)
	}

	if _, ok := sharedSkillScanRoot(Config{}, "codex"); ok {
		t.Fatal("expected codex to have no default shared root")
	}
}

func TestSharedSkillScanRootGlobalOverride(t *testing.T) {
	root, ok := sharedSkillScanRoot(Config{SharedSkillsDir: "/custom/shared"}, "pi")
	if !ok || root != "/custom/shared" {
		t.Fatalf("expected global override, got %q ok=%v", root, ok)
	}
}

func TestLocalSkillScanFingerprintChangesWhenFileChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "demo-skill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := localSkillScanFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("version-2"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := localSkillScanFingerprint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected fingerprint to change after file edit")
	}
}
