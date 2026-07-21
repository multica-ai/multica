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

	rendered := map[string]string{
		"runtime config": buildMetaSkillContent("codex", TaskContextForEnv{
			IssueID:     ctx.IssueID,
			AgentName:   ctx.AgentName,
			AgentID:     ctx.AgentID,
			AgentSkills: ctx.AgentSkills,
		}),
		"issue context": renderIssueContext("codex", TaskContextForEnv{
			IssueID:     ctx.IssueID,
			AgentSkills: ctx.AgentSkills,
		}),
		"quick create": renderQuickCreateContext(TaskContextForEnv{
			QuickCreatePrompt: ctx.QuickCreatePrompt,
			AgentSkills:       ctx.AgentSkills,
		}),
		"autopilot": renderAutopilotContext(TaskContextForEnv{
			AutopilotRunID: ctx.AutopilotRunID,
			AgentSkills:    ctx.AgentSkills,
		}),
	}

	for name, out := range rendered {
		if !strings.Contains(out, "Visible Skill") {
			t.Errorf("%s missing visible skill:\n%s", name, out)
		}
		if strings.Contains(out, "Hidden Skill") || strings.Contains(out, "hidden description") {
			t.Errorf("%s advertised disable-model-invocation skill:\n%s", name, out)
		}
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
