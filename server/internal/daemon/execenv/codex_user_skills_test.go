package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// User skills reach the per-task CODEX_HOME as links, not copies (MUL-6000).
// Copying charged every task directory the full skill tree — ~100 MB for a
// user with npm-backed skills — re-paid on every task start and never
// reclaimed while the issue stayed open.

func writeUserSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md in %s: %v", dir, err)
	}
}

// assertIsLink fails when path is a real directory rather than a link to one.
// A POSIX symlink reports ModeSymlink; a Windows junction reports
// ModeDir|ModeIrregular (Go 1.23+), which is why the mask covers both.
func assertIsLink(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		t.Fatalf("%s is a real directory (mode %v), want a link", path, fi.Mode())
	}
}

func TestSeedUserCodexSkillsLinksInsteadOfCopying(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	userSkill := filepath.Join(sharedHome, "skills", "alpha")
	writeUserSkill(t, userSkill, validUserSkillFixture("alpha v1"))

	codexHome := t.TempDir()
	if err := seedUserCodexSkills(codexHome, nil, testLogger()); err != nil {
		t.Fatalf("seedUserCodexSkills: %v", err)
	}

	dst := filepath.Join(codexHome, "skills", "alpha")
	assertIsLink(t, dst)

	body, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatalf("read seeded SKILL.md: %v", err)
	}
	if string(body) != validUserSkillFixture("alpha v1") {
		t.Errorf("seeded SKILL.md = %q, want %q", body, validUserSkillFixture("alpha v1"))
	}

	// The decisive proof it is not a copy: content added to the user's real
	// skill directory after seeding is visible through the per-task path.
	if err := os.WriteFile(filepath.Join(userSkill, "reference.md"), []byte("added later"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "reference.md")); err != nil {
		t.Errorf("per-task skill dir is a snapshot copy, not a link: %v", err)
	}
}

func TestSeedUserCodexSkillsResolvesInstallerSymlink(t *testing.T) {
	// Installers like lark-cli put a symlink in ~/.codex/skills/<name>
	// pointing into a shared ~/.agents/skills/<name>/.
	agentsRoot := t.TempDir()
	realSkill := filepath.Join(agentsRoot, "lark-base")
	writeUserSkill(t, realSkill, validUserSkillFixture("lark-base v1"))

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	if err := os.MkdirAll(filepath.Join(sharedHome, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	installerLink := filepath.Join(sharedHome, "skills", "lark-base")
	if err := os.Symlink(realSkill, installerLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	codexHome := t.TempDir()
	if err := seedUserCodexSkills(codexHome, nil, testLogger()); err != nil {
		t.Fatalf("seedUserCodexSkills: %v", err)
	}

	dst := filepath.Join(codexHome, "skills", "lark-base")
	assertIsLink(t, dst)
	body, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatalf("symlink-installed skill unreadable through the per-task link: %v", err)
	}
	if string(body) != validUserSkillFixture("lark-base v1") {
		t.Errorf("SKILL.md = %q, want %q", body, validUserSkillFixture("lark-base v1"))
	}

	// The per-task link must point at the real directory, not at the
	// installer's own link, so it stays valid if the installer re-points it.
	if runtime.GOOS != "windows" {
		target, err := os.Readlink(dst)
		if err != nil {
			t.Fatalf("readlink %s: %v", dst, err)
		}
		want, err := filepath.EvalSymlinks(realSkill)
		if err != nil {
			t.Fatal(err)
		}
		if target != want {
			t.Errorf("link target = %q, want the resolved skill dir %q", target, want)
		}
	}
}

func TestSeedUserCodexSkillsYieldsToWorkspaceSkill(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	writeUserSkill(t, filepath.Join(sharedHome, "skills", "issue-review"), "user version")
	writeUserSkill(t, filepath.Join(sharedHome, "skills", "keeper"), "keeper version")

	codexHome := t.TempDir()
	skills := []SkillContextForEnv{{Name: "Issue Review", Content: "workspace version"}}
	if err := seedUserCodexSkills(codexHome, skills, testLogger()); err != nil {
		t.Fatalf("seedUserCodexSkills: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(codexHome, "skills", "issue-review")); !os.IsNotExist(err) {
		t.Errorf("user skill claimed a workspace skill's slot (err = %v)", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, "skills", "keeper")); err != nil {
		t.Errorf("unrelated user skill was dropped: %v", err)
	}
}

func TestSeedUserCodexSkillsWithNoEligibleSkillsCreatesNoDir(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	if err := os.MkdirAll(filepath.Join(sharedHome, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	codexHome := t.TempDir()
	if err := seedUserCodexSkills(codexHome, nil, testLogger()); err != nil {
		t.Fatalf("seedUserCodexSkills: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(codexHome, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills dir created with nothing to seed (err = %v)", err)
	}
}

// hydrateCodexSkills wipes the skills dir before every seed, on both the
// Prepare and the Reuse path. With links in place that RemoveAll must delete
// the link and never reach through it into the user's real skill directory.
func TestHydrateCodexSkillsWipeKeepsUserSkillContent(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	userSkill := filepath.Join(sharedHome, "skills", "alpha")
	writeUserSkill(t, userSkill, validUserSkillFixture("alpha v1"))
	if err := os.WriteFile(filepath.Join(userSkill, "reference.md"), []byte("support"), 0o644); err != nil {
		t.Fatal(err)
	}

	codexHome := t.TempDir()
	workspaceSkills := []SkillContextForEnv{{Name: "Beta", Description: "workspace skill", Content: "beta body"}}

	// Twice: Prepare hydrates, then a follow-up task on the same env re-hydrates.
	for i := 0; i < 2; i++ {
		if err := hydrateCodexSkills(codexHome, workspaceSkills, nil, testLogger()); err != nil {
			t.Fatalf("hydrateCodexSkills run %d: %v", i+1, err)
		}
	}

	for _, name := range []string{"SKILL.md", "reference.md"} {
		if _, err := os.Stat(filepath.Join(userSkill, name)); err != nil {
			t.Fatalf("hydrate deleted %s from the user's real skill dir: %v", name, err)
		}
	}
	assertIsLink(t, filepath.Join(codexHome, "skills", "alpha"))
	if _, err := os.Stat(filepath.Join(codexHome, "skills", "beta", "SKILL.md")); err != nil {
		t.Errorf("workspace skill missing after re-hydration: %v", err)
	}
}

// A workspace skill whose name only collides after directory naming — the
// sanitized-name pre-filter does not catch it — must land in a sibling
// directory instead of being written through the user's link.
func TestHydrateCodexSkillsNeverWritesThroughAUserSkillLink(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	userSkill := filepath.Join(sharedHome, "skills", "alpha")
	writeUserSkill(t, userSkill, validUserSkillFixture("user version"))

	codexHome := t.TempDir()
	skillsDir := filepath.Join(codexHome, "skills")
	if err := seedUserCodexSkills(codexHome, nil, testLogger()); err != nil {
		t.Fatalf("seedUserCodexSkills: %v", err)
	}
	// writeSkillFiles runs without the pre-filter that hydrateCodexSkills
	// applies, which is exactly the case allocateCollisionFreeSkillDir guards.
	if err := writeSkillFiles(skillsDir, []SkillContextForEnv{{Name: "alpha", Content: "workspace version"}}, nil); err != nil {
		t.Fatalf("writeSkillFiles: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(userSkill, "SKILL.md"))
	if err != nil {
		t.Fatalf("read user skill: %v", err)
	}
	if string(body) != validUserSkillFixture("user version") {
		t.Fatalf("workspace skill was written through the link into the user's skill dir: %q", body)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "alpha-multica", "SKILL.md")); err != nil {
		t.Errorf("colliding workspace skill did not land in a sibling dir: %v", err)
	}
}

func validUserSkillFixture(body string) string {
	return "---\nname: user-skill\ndescription: Test instructions.\n---\n\n" + body
}

func TestSeedUserCodexSkillsNormalizesWithoutChangingSource(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	source := filepath.Join(sharedHome, "skills", "koc-publish-registration")
	original := "# Publish\n\nPlain-text instructions.\n"
	writeUserSkill(t, source, original)
	if err := os.MkdirAll(filepath.Join(source, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := hydrateCodexSkills(home, nil, nil, testLogger()); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(home, "skills", "koc-publish-registration")
		data, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		fm := parseFrontmatter(t, string(data))
		if fm["name"] != "koc-publish-registration" || fm["description"] == "" || !strings.Contains(string(data), "Plain-text instructions.") {
			t.Fatalf("not loadable: %s", data)
		}
		unchanged, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
		if err != nil || string(unchanged) != original {
			t.Fatalf("shared source changed: %q, %v", unchanged, err)
		}
		if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("after"), 0o644); err != nil {
			t.Fatal(err)
		}
		guide, err := os.ReadFile(filepath.Join(dst, "references", "guide.md"))
		if err != nil || string(guide) != "after" {
			t.Fatalf("support tree was copied: %q, %v", guide, err)
		}
	}
}

func TestNormalizeUserCodexSkillContentRetainsPolicies(t *testing.T) {
	original := "---\nname: alpha\ndisable-model-invocation: true\nallowed-tools: [Read]\n---\nbody\n"
	normalized, err := normalizeUserCodexSkillContent(original, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	fm := parseFrontmatter(t, normalized)
	if fm["disable-model-invocation"] != true || fm["description"] == "" {
		t.Fatalf("policy lost: %#v", fm)
	}
	if _, ok := fm["allowed-tools"].([]interface{}); !ok {
		t.Fatalf("tools lost: %#v", fm)
	}
}

func TestSeedUserCodexSkillsSkipsMalformedSibling(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	writeUserSkill(t, filepath.Join(sharedHome, "skills", "broken"), "---\nname: broken\ndescription: [invalid\n---\nbody")
	writeUserSkill(t, filepath.Join(sharedHome, "skills", "good"), validUserSkillFixture("working"))
	home := t.TempDir()
	if err := seedUserCodexSkills(home, nil, testLogger()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, "skills", "broken")); !os.IsNotExist(err) {
		t.Fatalf("malformed skill published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "good", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeUserCodexSkillContentPreservesFormatting(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "missing description keeps comments and order",
			in:   "---\nname: alpha\n# keep me\nallowed-tools: [Read]\nmetadata:\n  short-description: x\n---\nbody\n",
			want: "---\ndescription: \"Instructions for the alpha skill.\"\nname: alpha\n# keep me\nallowed-tools: [Read]\nmetadata:\n  short-description: x\n---\nbody\n",
		},
		{
			name: "CRLF line endings stay CRLF",
			in:   "---\r\nname: alpha\r\n---\r\nbody\r\n",
			want: "---\r\ndescription: \"Instructions for the alpha skill.\"\r\nname: alpha\r\n---\r\nbody\r\n",
		},
		{
			name: "empty value replaced in place",
			in:   "---\n# keep me\nzeta: 1\nname: alpha\ndescription:\n---\nbody\n",
			want: "---\n# keep me\nzeta: 1\nname: alpha\ndescription: Instructions for the alpha skill.\n---\nbody\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeUserCodexSkillContent(tc.in, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q\nwant %q", got, tc.want)
			}
			raw, _, ok := frontmatterParts(got)
			var fm map[string]any
			if !ok || yaml.Unmarshal([]byte(raw), &fm) != nil {
				t.Fatalf("unparseable: %q", got)
			}
			if fm["name"] != "alpha" || fm["description"] != "Instructions for the alpha skill." {
				t.Fatalf("not loadable: %#v", fm)
			}
		})
	}
}

func TestNormalizeUserCodexSkillContentRejectsDuplicateKeys(t *testing.T) {
	if _, err := normalizeUserCodexSkillContent("---\nname: a\nname: b\n---\nbody", "alpha"); err == nil {
		t.Fatal("duplicate-key frontmatter accepted")
	}
}

// Review regressions from MUL-8095: every repair must parse, keep other
// fields equal, and never touch a skill Codex already accepts.
func TestNormalizeUserCodexSkillContentRepairsEveryMappingStyle(t *testing.T) {
	cases := []struct {
		name, in string
		check    func(t *testing.T, out string, fm map[string]any)
	}{
		{name: "flow mapping missing description", in: "---\n{name: alpha, allowed-tools: [Read]}\n---\nbody\n",
			check: func(t *testing.T, out string, fm map[string]any) {
				if tools, _ := fm["allowed-tools"].([]any); len(tools) != 1 || tools[0] != "Read" {
					t.Fatalf("allowed-tools lost: %#v", fm)
				}
			}},
		{name: "indented mapping missing description", in: "---\n  name: alpha\n  allowed-tools: [Read]\n---\nbody\n"},
		{name: "anchored empty description keeps aliases and comment", in: "---\nname: alpha\ndescription: &shared \"\" # retain me\nmetadata:\n  short-description: *shared\n---\nbody\n",
			check: func(t *testing.T, out string, fm map[string]any) {
				if md, _ := fm["metadata"].(map[string]any); md["short-description"] != "" {
					t.Fatalf("alias value changed: %#v", fm)
				}
				if !strings.Contains(out, "# retain me") {
					t.Fatalf("line comment lost: %s", out)
				}
			}},
		{name: "non-string name replaced", in: "---\nname: [a]\ndescription: Demo\n---\nbody\n",
			check: func(t *testing.T, out string, fm map[string]any) {
				if fm["name"] != "alpha" {
					t.Fatalf("name not repaired: %#v", fm)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := normalizeUserCodexSkillContent(tc.in, "alpha")
			if err != nil {
				t.Fatal(err)
			}
			raw, body, ok := frontmatterParts(out)
			var fm map[string]any
			if !ok || yaml.Unmarshal([]byte(raw), &fm) != nil {
				t.Fatalf("repair produced invalid YAML:\n%s", out)
			}
			if d, _ := fm["description"].(string); strings.TrimSpace(d) == "" || body != "body\n" {
				t.Fatalf("not loadable: %q", out)
			}
			if tc.check != nil {
				tc.check(t, out, fm)
			}
		})
	}
}

func TestNormalizeUserCodexSkillContentLeavesLoadableSkillsUntouched(t *testing.T) {
	// Codex falls back to the directory name for a missing or blank name, so
	// these already load and must stay linked byte-for-byte.
	for _, in := range []string{
		"---\n{description: Demo skill}\n---\nbody\n",
		"---\ndescription: Demo skill\n---\nbody\n",
		"---\nname: \"\"\ndescription: Demo skill\n---\nbody\n",
	} {
		out, err := normalizeUserCodexSkillContent(in, "alpha")
		if err != nil || out != in {
			t.Fatalf("loadable skill rewritten: %q -> %q (%v)", in, out, err)
		}
	}
}
