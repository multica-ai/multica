package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteSkillFilesIgnoresBundledSkillMd is the daemon-side regression guard
// for #3489 / MUL-2928. A skill whose Files include the skill's own SKILL.md
// (stored as a supporting file by older builds or direct create/update API
// calls) used to fail task prep with errPathPreExists: writeSkillFiles writes
// the primary content to dir/SKILL.md first, then the supporting-files loop
// tried to write the duplicate over it. The non-canonical "./SKILL.md"
// spelling resolves onto the same path and must be skipped too. Every
// non-codex provider hit this; codex is unaffected because it writes with a
// nil manifest (plain os.WriteFile, no refuse-to-overwrite).
func TestWriteSkillFilesIgnoresBundledSkillMd(t *testing.T) {
	t.Parallel()
	skillsDir := filepath.Join(t.TempDir(), ".claude", "skills")

	skills := []SkillContextForEnv{
		{
			Name:    "Issue Review",
			Content: "Primary skill body.",
			Files: []SkillFileContextForEnv{
				{Path: "README.md", Content: "readme"},
				{Path: "SKILL.md", Content: "duplicate primary, must be skipped"},
				{Path: "./SKILL.md", Content: "non-canonical duplicate, must be skipped"},
				{Path: "helper.go", Content: "package main"},
			},
		},
	}

	// A non-nil manifest is the production Prepare path: recordWriteFile
	// enforces refuse-to-overwrite, so a duplicate SKILL.md would error here
	// if it were not skipped.
	manifest := &sidecarManifest{}
	if err := writeSkillFiles(skillsDir, skills, manifest); err != nil {
		t.Fatalf("writeSkillFiles errored on a bundled SKILL.md: %v", err)
	}

	skillDir := filepath.Join(skillsDir, "issue-review")

	// SKILL.md must hold the primary content, never a bundled duplicate.
	got, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(got), "Primary skill body.") {
		t.Errorf("SKILL.md = %q, want primary content", string(got))
	}
	if strings.Contains(string(got), "must be skipped") {
		t.Error("SKILL.md was overwritten by a bundled duplicate")
	}

	// Unique supporting files are still written — this also proves the loop
	// continued past the skipped duplicates instead of aborting on them.
	for _, name := range []string{"README.md", "helper.go"} {
		if _, err := os.Stat(filepath.Join(skillDir, name)); err != nil {
			t.Errorf("expected supporting file %s to be written: %v", name, err)
		}
	}
}
