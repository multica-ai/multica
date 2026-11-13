package execenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillReceiptIncludesWrittenSupportingFiles(t *testing.T) {
	env := &Environment{WorkDir: t.TempDir()}
	skills := []SkillContextForEnv{{Name: "check", Content: "Check it.", Files: []SkillFileContextForEnv{
		{Path: "b.md", Content: "two"},
		{Path: "a.md", Content: "one"},
		{Path: "./SKILL.md", Content: "duplicate primary content is not written"},
	}}}
	dir := skillsDirPath(env.WorkDir, "kiro")
	if err := writeSkillFiles(dir, skills, nil); err != nil {
		t.Fatal(err)
	}
	before := env.SkillReceipts("kiro", skills)[0]
	primary, err := os.ReadFile(filepath.Join(dir, "check", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.Available || before.Bytes != len(primary)+6 || len(before.SHA256) != 12 {
		t.Fatalf("receipt = %+v, primary bytes = %d", before, len(primary))
	}
	// Listing order does not change the package, and the duplicate SKILL.md
	// entry must not inflate the size or replace the prepared frontmatter.
	skills[0].Files[0], skills[0].Files[1] = skills[0].Files[1], skills[0].Files[0]
	if reordered := env.SkillReceipts("kiro", skills)[0]; reordered != before {
		t.Fatalf("reordering file declarations changed the receipt: %+v -> %+v", before, reordered)
	}
	// A same-size change on disk must change the fingerprint even though
	// the configured primary content and total bytes remain identical.
	if err := os.WriteFile(filepath.Join(dir, "check", "a.md"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := env.SkillReceipts("kiro", skills)[0]
	if after.SHA256 == before.SHA256 || after.Bytes != before.Bytes {
		t.Fatalf("supporting-file change: before=%+v after=%+v", before, after)
	}
	if err := os.Remove(filepath.Join(dir, "check", "a.md")); err != nil {
		t.Fatal(err)
	}
	if missing := env.SkillReceipts("kiro", skills)[0]; missing.Available || missing.SHA256 != "" || missing.Bytes != 0 {
		t.Fatalf("partial package got a fingerprint: %+v", missing)
	}
}

func TestSkillReceiptUsesRuntimeHome(t *testing.T) {
	for _, tc := range []struct{ provider, display string }{
		{"codex", "$CODEX_HOME/skills"},
		{"hermes", "$HERMES_HOME/skills"},
		{"qwenpaw", "<QwenPaw workspace>/skills"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			home := t.TempDir()
			env := &Environment{WorkDir: t.TempDir(), CodexHome: home, HermesHome: home, QwenpawWorkspace: home}
			skills := []SkillContextForEnv{{Name: "check", Content: "check"}}
			if err := writeSkillFiles(filepath.Join(home, "skills"), skills, nil); err != nil {
				t.Fatal(err)
			}
			got := env.SkillReceipts(tc.provider, skills)[0]
			if !got.Available || got.Path != tc.display+"/check/SKILL.md" {
				t.Fatalf("receipt = %+v", got)
			}
		})
	}
}
