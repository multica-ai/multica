package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillModelInvocationVisibility(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "no frontmatter is visible",
			content: "body",
			want:    true,
		},
		{
			name: "disable true is hidden",
			content: `---
name: hidden
disable-model-invocation: true
---

body`,
			want: false,
		},
		{
			name: "disable quoted true is hidden",
			content: `---
name: hidden
disable-model-invocation: "true"
---

body`,
			want: false,
		},
		{
			name: "disable false is visible",
			content: `---
name: visible
disable-model-invocation: false
---

body`,
			want: true,
		},
		{
			name: "user invocable false is still visible",
			content: `---
name: visible
user-invocable: false
---

body`,
			want: true,
		},
		{
			name: "invalid frontmatter is visible",
			content: `---
name: visible
description: bad: value
disable-model-invocation: true
---

body`,
			want: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := skillModelInvocationVisible(SkillContextForEnv{Content: tc.content})
			if got != tc.want {
				t.Errorf("skillModelInvocationVisible = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderedSkillListsHideDisableModelInvocationSkills(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID:           "issue-1",
		QuickCreatePrompt: "create something",
		AutopilotRunID:    "run-1",
		AgentName:         "Eve",
		AgentID:           "eve-1",
		AgentSkills: []SkillContextForEnv{
			{
				Name:        "Visible Skill",
				Description: "visible description",
				Content: `---
name: visible-skill
---

Visible body.`,
			},
			{
				Name:        "Hidden Skill",
				Description: "hidden description",
				Content: `---
name: hidden-skill
disable-model-invocation: true
---

Hidden body.`,
			},
		},
	}

	// The brief is now the only Multica-rendered skill listing, for every kind.
	for _, kind := range []TaskContextForEnv{
		{IssueID: ctx.IssueID, AgentName: ctx.AgentName, AgentID: ctx.AgentID, AgentSkills: ctx.AgentSkills},
		{QuickCreatePrompt: ctx.QuickCreatePrompt, AgentName: ctx.AgentName, AgentID: ctx.AgentID, AgentSkills: ctx.AgentSkills},
		{AutopilotRunID: ctx.AutopilotRunID, AgentName: ctx.AgentName, AgentID: ctx.AgentID, AgentSkills: ctx.AgentSkills},
		{ChatSessionID: "c-1", AgentName: ctx.AgentName, AgentID: ctx.AgentID, AgentSkills: ctx.AgentSkills},
	} {
		out := buildMetaSkillContent("codex", kind)
		// Listings carry the on-disk slug, not the display name (MUL-5529).
		if !strings.Contains(out, "visible-skill") {
			t.Errorf("brief missing visible skill:\n%s", out)
		}
		if strings.Contains(out, "Visible Skill") {
			t.Errorf("brief lists the display name instead of the slug:\n%s", out)
		}
		if strings.Contains(out, "hidden-skill") || strings.Contains(out, "hidden description") {
			t.Errorf("brief advertised disable-model-invocation skill:\n%s", out)
		}
	}

	// issue_context.md and its quick-create / autopilot variants no longer
	// carry a skill list at all; nothing read that copy.
	for name, out := range map[string]string{
		"issue context": renderIssueContext("codex", TaskContextForEnv{IssueID: ctx.IssueID, AgentSkills: ctx.AgentSkills}),
		"quick create":  renderQuickCreateContext(TaskContextForEnv{QuickCreatePrompt: ctx.QuickCreatePrompt, AgentSkills: ctx.AgentSkills}),
		"autopilot":     renderAutopilotContext(TaskContextForEnv{AutopilotRunID: ctx.AutopilotRunID, AgentSkills: ctx.AgentSkills}),
	} {
		if strings.Contains(out, "## Agent Skills") || strings.Contains(out, "visible-skill") {
			t.Errorf("%s still renders a skill list:\n%s", name, out)
		}
	}
}

// sanitizeSkillName is not injective — "A B" and "A-B" both reduce to "a-b" —
// so a listing built from it alone names two skills identically while
// writeSkillFiles puts the second in `a-b-multica`. The second skill then has
// no invocable name and the model is silently pointed at the first. This needs
// no user-installed skill and no local_directory: an ordinary task with two
// such skills bound reproduces it in a clean workdir.
//
// The assertion is set equality between what is listed and what exists on
// disk, so any future drift between the two allocators fails here.
func TestSkillListingNamesMatchDirectoriesOnBatchSlugCollision(t *testing.T) {
	t.Parallel()

	skills := []SkillContextForEnv{
		{Name: "A B", Content: "---\nname: one\n---\n\nfirst"},
		{Name: "A-B", Content: "---\nname: two\n---\n\nsecond"},
		{Name: "a.b", Content: "---\nname: three\n---\n\nthird"},
	}

	dir := t.TempDir()
	if err := writeSkillFiles(dir, skills, nil); err != nil {
		t.Fatalf("writeSkillFiles failed: %v", err)
	}

	onDisk := map[string]struct{}{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	for _, e := range entries {
		onDisk[e.Name()] = struct{}{}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err != nil {
			t.Errorf("no SKILL.md under %s: %v", e.Name(), err)
		}
	}

	listed := map[string]struct{}{}
	for _, s := range modelVisibleSkills(skills) {
		if _, dup := listed[s.Name]; dup {
			t.Errorf("listing repeats the name %q, so one skill has no invocable name", s.Name)
		}
		listed[s.Name] = struct{}{}
	}

	if len(listed) != len(skills) {
		t.Errorf("listed %d distinct names for %d skills: %v", len(listed), len(skills), listed)
	}
	for name := range listed {
		if _, ok := onDisk[name]; !ok {
			t.Errorf("listing advertises %q but no such directory exists; on disk: %v", name, onDisk)
		}
	}
	for name := range onDisk {
		if _, ok := listed[name]; !ok {
			t.Errorf("directory %q was written but never listed; listed: %v", name, listed)
		}
	}
}

// Hidden skills still occupy a directory, so they must still consume a slug.
// Allocating over the filtered list instead of the full batch would shift every
// later suffix and desynchronize the listing from disk.
func TestBatchSlugAllocationCountsHiddenSkills(t *testing.T) {
	t.Parallel()

	skills := []SkillContextForEnv{
		{Name: "A B", Content: "---\nname: hidden\ndisable-model-invocation: true\n---\n\nhidden"},
		{Name: "A-B", Content: "---\nname: visible\n---\n\nvisible"},
	}

	dir := t.TempDir()
	if err := writeSkillFiles(dir, skills, nil); err != nil {
		t.Fatalf("writeSkillFiles failed: %v", err)
	}

	visible := modelVisibleSkills(skills)
	if len(visible) != 1 {
		t.Fatalf("expected 1 visible skill, got %d", len(visible))
	}
	// The hidden skill took `a-b`, so the visible one must be listed — and
	// written — as `a-b-multica`.
	if visible[0].Name != "a-b-multica" {
		t.Errorf("visible skill listed as %q, want %q", visible[0].Name, "a-b-multica")
	}
	if _, err := os.Stat(filepath.Join(dir, "a-b-multica", "SKILL.md")); err != nil {
		t.Errorf("listed name has no matching directory: %v", err)
	}
}

func TestRenderedSkillListsOmitSectionWhenOnlyDisabledSkills(t *testing.T) {
	t.Parallel()

	ctx := TaskContextForEnv{
		IssueID: "issue-1",
		AgentSkills: []SkillContextForEnv{
			{
				Name: "Hidden Skill",
				Content: `---
name: hidden-skill
disable-model-invocation: true
---

Hidden body.`,
			},
		},
	}

	runtimeConfig := buildMetaSkillContent("codex", ctx)
	if strings.Contains(runtimeConfig, "## Skills") {
		t.Errorf("runtime config should omit Skills section when every skill disables model invocation:\n%s", runtimeConfig)
	}

	issueContext := renderIssueContext("codex", ctx)
	if strings.Contains(issueContext, "## Agent Skills") {
		t.Errorf("issue context should omit Agent Skills section when every skill disables model invocation:\n%s", issueContext)
	}
}

func TestWriteContextFilesHydratesDisableModelInvocationSkillButDoesNotAdvertiseIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID: "issue-1",
		AgentSkills: []SkillContextForEnv{
			{
				Name: "Visible Skill",
				Content: `---
name: visible-skill
---

Visible body.`,
			},
			{
				Name: "Hidden Skill",
				Content: `---
name: hidden-skill
disable-model-invocation: true
---

Hidden body.`,
			},
		},
	}

	if err := writeContextFiles(dir, "", ctx, nil); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	issueContext, err := os.ReadFile(filepath.Join(dir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("read issue_context.md: %v", err)
	}
	if strings.Contains(string(issueContext), "Hidden Skill") {
		t.Fatalf("issue_context.md advertised hidden skill:\n%s", string(issueContext))
	}

	hiddenSkill, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "hidden-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("disabled skill should still be hydrated on disk: %v", err)
	}
	if !strings.Contains(string(hiddenSkill), "disable-model-invocation: true") {
		t.Errorf("hidden skill frontmatter should be preserved on disk:\n%s", string(hiddenSkill))
	}
}
