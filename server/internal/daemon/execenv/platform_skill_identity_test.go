package execenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlatformSkillHintsRequireBuiltinIdentity(t *testing.T) {
	t.Parallel()
	user := platformSkillFixture()
	user.BuiltinName = ""
	hidden := platformSkillFixture()
	hidden.Content = "---\nname: multica-platform\ndisable-model-invocation: true\n---\n\nHidden contracts."
	for _, tc := range []struct {
		name   string
		skills []SkillContextForEnv
		want   string
	}{
		{name: "builtin", skills: []SkillContextForEnv{platformSkillFixture()}, want: platformSkillName},
		{name: "user precedes builtin", skills: []SkillContextForEnv{user, platformSkillFixture()}, want: "multica-platform-multica"},
		{name: "user only", skills: []SkillContextForEnv{user}},
		{name: "frontmatter alone is not identity", skills: []SkillContextForEnv{{Name: "helper", Content: user.Content}}},
		{name: "hidden builtin alongside visible user", skills: []SkillContextForEnv{user, hidden}},
		{name: "hidden builtin only", skills: []SkillContextForEnv{hidden}},
		{name: "absent"},
	} {
		for _, provider := range []string{"claude", "codex", "traecli"} {
			t.Run(tc.name+"/"+provider, func(t *testing.T) {
				t.Parallel()
				brief := buildMetaSkillContent(provider, TaskContextForEnv{IssueID: "issue-platform-identity", AgentSkills: tc.skills})
				assertPlatformSkillHints(t, provider, brief, tc.want)
			})
		}
	}
}

func TestPlatformSkillHintsFollowPreparedDirectories(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"claude", "traecli"} {
		for _, phase := range []string{"prepare", "reuse"} {
			for collisions := 0; collisions <= 2; collisions++ {
				t.Run(fmt.Sprintf("%s/%s/collisions-%d", provider, phase, collisions), func(t *testing.T) {
					t.Parallel()
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					task := TaskContextForEnv{IssueID: "issue-platform-identity", AgentSkills: []SkillContextForEnv{platformSkillFixture()}}
					params := PrepareParams{
						WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-platform-identity",
						TaskID: "task-platform-identity", Provider: provider, Task: task,
					}
					workDir := t.TempDir()
					if phase == "reuse" {
						initial := params
						initial.Task.AgentSkills = nil
						env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), initial, testLogger())
						if err != nil {
							t.Fatal(err)
						}
						workDir = env.WorkDir
					}
					const userContent = "---\nname: multica-platform\n---\n\nUser-owned skill."
					for i := 0; i < collisions; i++ {
						mustWrite(t, filepath.Join(skillsDirPath(workDir, provider), skillSlugCandidate(platformSkillName, i), "SKILL.md"), userContent)
					}
					var env *Environment
					var err error
					if phase == "prepare" {
						params.LocalWorkDir = workDir
						env, err = PrepareIsolated(ctx, preparationHelperTestCommand(), params, testLogger())
					} else {
						// Refresh again after the first reuse materializes the platform
						// skill, so the assertions also cover reclaiming its old directory.
						for attempt := 0; attempt < 2; attempt++ {
							env, err = ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
								WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: provider, Task: task,
							}, testLogger())
							if err != nil || env == nil {
								t.Fatalf("reuse %d: environment=%+v, error=%v", attempt, env, err)
							}
						}
					}
					if err != nil || env == nil {
						t.Fatalf("%s: environment=%+v, error=%v", phase, env, err)
					}
					wantSlug := skillSlugCandidate(platformSkillName, collisions)
					if len(env.PreparedSkillNames) != 1 || env.PreparedSkillNames[0] != wantSlug {
						t.Fatalf("prepared names = %v, want [%s]", env.PreparedSkillNames, wantSlug)
					}
					prepared := ApplyPreparedSkillNames(task, env.PreparedSkillNames)
					visible := modelVisibleSkills(prepared.AgentSkills)
					if len(visible) != 1 || visible[0].BuiltinName != platformSkillName || task.AgentSkills[0].Name != platformSkillName {
						t.Fatalf("builtin identity lost or original mutated: prepared=%+v, original=%+v", visible, task.AgentSkills)
					}
					brief, err := InjectRuntimeConfig(workDir, provider, prepared)
					if err != nil {
						t.Fatal(err)
					}
					assertPlatformSkillHints(t, provider, brief, wantSlug)
					if !strings.Contains(brief, "**"+wantSlug+"**") {
						t.Fatal("allocated platform skill missing from listing")
					}
					data, err := os.ReadFile(filepath.Join(skillsDirPath(workDir, provider), wantSlug, "SKILL.md"))
					if err != nil || !frontmatterNameIs(string(data), wantSlug) {
						t.Fatalf("allocated skill = %q, error=%v", data, err)
					}
					for i := 0; i < collisions; i++ {
						data, err := os.ReadFile(filepath.Join(skillsDirPath(workDir, provider), skillSlugCandidate(platformSkillName, i), "SKILL.md"))
						if err != nil || string(data) != userContent {
							t.Fatalf("user skill changed: %q, error=%v", data, err)
						}
					}
				})
			}
		}
	}
}

// TestPlatformSkillDomainsMatchDescription keeps the brief's recall hint and
// the skill's description naming the same domains (MUL-9256). The description
// is guarded against the skill's references in the service package; this
// closes the last hop, so a reference added there reaches every runtime's brief.
func TestPlatformSkillDomainsMatchDescription(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "service", "builtin_skills", platformSkillName, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	var description string
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "description: "); ok {
			description = strings.Trim(value, `"`)
			break
		}
	}
	_, after, ok := strings.Cut(description, "does not fully cover: ")
	domains, _, ok2 := strings.Cut(after, ". ")
	if !ok || !ok2 {
		t.Fatalf("cannot find the domain list in the description %q; update this parser with the new wording", description)
	}
	if domains != platformSkillDomains {
		t.Errorf("brief domains drifted from the skill description:\n description: %s\n brief:       %s", domains, platformSkillDomains)
	}

	for _, provider := range []string{"claude", "codex", "traecli"} {
		brief := buildMetaSkillContent(provider, TaskContextForEnv{IssueID: "issue-platform-domains", AgentSkills: []SkillContextForEnv{platformSkillFixture()}})
		if !strings.Contains(brief, "does not fully cover — "+platformSkillDomains+" — ") {
			t.Errorf("%s brief does not list the platform skill's domains", provider)
		}
	}
}

func assertPlatformSkillHints(t *testing.T, provider, brief, slug string) {
	t.Helper()
	if slug == "" {
		for _, unwanted := range []string{"For a Multica platform action this brief does not fully cover", "Before creating sub-issues, read"} {
			if strings.Contains(brief, unwanted) {
				t.Errorf("brief advertised a platform contract without a visible builtin: %q", unwanted)
			}
		}
		return
	}
	platformHint := "load the `" + slug + "` skill and open the reference(s)"
	if provider == "traecli" {
		platformHint = "read `.traecli/skills/" + slug + "/SKILL.md` and open the reference(s)"
	}
	for _, want := range []string{platformHint, "Before creating sub-issues, read `references/sub-issues.md` in the `" + slug + "` skill"} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief missing platform hint %q", want)
		}
	}
}
