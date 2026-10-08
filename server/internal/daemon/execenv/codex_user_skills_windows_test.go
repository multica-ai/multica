//go:build windows

package execenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// On Windows createDirLink falls back to a directory junction (mklink /J) when
// os.Symlink is denied — the default without Developer Mode. hydrateCodexSkills
// wipes codex-home/skills on every task start, so the wipe must delete the
// junction itself and never reach through it into the user's real skill files.
// os.RemoveAll drops a junction with RemoveDirectory, but only a real Windows
// filesystem proves it; mklink /J is used directly so the junction shape is
// exercised even on a runner where symlinks are permitted.
func TestHydrateCodexSkillsWipeKeepsJunctionTarget(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	userSkill := filepath.Join(sharedHome, "skills", "alpha")
	writeUserSkill(t, userSkill, "alpha v1")

	codexHome := t.TempDir()
	junction := filepath.Join(codexHome, "skills", "alpha")
	if err := os.MkdirAll(filepath.Dir(junction), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, userSkill).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %s: %v", out, err)
	}
	if fi, err := os.Lstat(junction); err != nil {
		t.Fatal(err)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("expected a junction, got a symlink (mode %v)", fi.Mode())
	}

	if err := hydrateCodexSkills(codexHome, nil, nil, testLogger()); err != nil {
		t.Fatalf("hydrateCodexSkills: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(userSkill, "SKILL.md"))
	if err != nil {
		t.Fatalf("the skills wipe reached through the junction: %v", err)
	}
	if string(body) != "alpha v1" {
		t.Errorf("user SKILL.md = %q, want %q", body, "alpha v1")
	}
	assertIsLink(t, junction)
}
