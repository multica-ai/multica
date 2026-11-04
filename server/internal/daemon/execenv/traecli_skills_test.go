package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraecliBriefLoadsSkillsFromFiles(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskContextForEnv{
		{IssueID: "issue-1"},
		{ChatSessionID: "chat-1"},
		{AutopilotRunID: "run-1"},
		{QuickCreatePrompt: "create an issue"},
	} {
		kind.AgentSkills = []SkillContextForEnv{
			{Name: "Review Helper", Description: "Use when reviewing code.", Content: "Private review instructions."},
			platformSkillFixture(),
		}
		brief := buildMetaSkillContent("traecli", kind)
		for _, want := range []string{
			"paths are relative to the task working directory",
			"use your file tools to read its `SKILL.md`",
			"`references/` and `scripts/`",
			"from that skill's directory",
			"- **review-helper** — Use when reviewing code. (file: `.traecli/skills/review-helper/SKILL.md`)",
			"read `.traecli/skills/multica-platform/SKILL.md` and open the reference(s)",
			"for the domains your task touches",
		} {
			if !strings.Contains(brief, want) {
				t.Errorf("%v brief missing %q", classifyTask(kind), want)
			}
		}
		for _, unwanted := range []string{"discovered automatically", "Private review instructions."} {
			if strings.Contains(brief, unwanted) {
				t.Errorf("%v brief contains %q", classifyTask(kind), unwanted)
			}
		}
	}
}

func TestTraecliBriefDescriptionsAreSingleLineAndBounded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, description, want string
	}{
		{name: "empty"},
		{
			name:        "multiline markdown",
			description: "Use `reviews`\r\nfor **code**.\t",
			want:        " — Use \\`reviews\\` for \\*\\*code\\*\\*.",
		},
		{
			name:        "long unicode",
			description: strings.Repeat("审", 241) + "omitted",
			want:        " — " + strings.Repeat("审", 239) + "…",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			brief := buildMetaSkillContent("traecli", TaskContextForEnv{AgentSkills: []SkillContextForEnv{
				{Name: "Review", Description: tc.description},
			}})
			want := "- **review**" + tc.want + " (file: `.traecli/skills/review/SKILL.md`)\n"
			if !strings.Contains(brief, want) {
				t.Errorf("brief missing bounded, single-line skill entry %q", want)
			}
		})
	}
}

func TestTraecliBriefOmitsSkillsWithoutVisibleEntries(t *testing.T) {
	t.Parallel()
	for _, skills := range [][]SkillContextForEnv{
		nil,
		{{Name: "multica-platform", Content: "---\nname: multica-platform\ndisable-model-invocation: true\n---\n\nHidden body."}},
	} {
		brief := buildMetaSkillContent("traecli", TaskContextForEnv{AgentSkills: skills})
		if strings.Contains(brief, "## Skills") || strings.Contains(brief, ".traecli/skills/") {
			t.Error("brief advertised skills without visible entries")
		}
	}
}

func TestTraecliBriefPathsMatchPreparedSkills(t *testing.T) {
	t.Parallel()
	for _, phase := range []string{"prepare", "reuse"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			task := TaskContextForEnv{
				IssueID: "issue-traecli-skills",
				AgentSkills: []SkillContextForEnv{
					{
						Name: "Review", Description: "Review code.", Content: "Review body v1.",
						Files: []SkillFileContextForEnv{{Path: "references/checks.md", Content: "Checks v1."}},
					},
					{Name: "Review", Description: "Hidden description.", Content: "---\nname: hidden\ndisable-model-invocation: true\n---\n\nHidden body."},
					platformSkillFixture(),
				},
			}
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-traecli-skills",
				TaskID: "task-traecli-skills", Provider: "traecli", Task: task,
			}
			workDir := t.TempDir()
			if phase == "reuse" {
				initial := params
				initial.Task.AgentSkills = nil
				env, err := Prepare(initial, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer env.Cleanup(true)
				workDir = env.WorkDir
			}
			const userSkill = "User-owned skill."
			for _, name := range []string{"review", "multica-platform"} {
				mustWrite(t, filepath.Join(workDir, ".traecli", "skills", name, "SKILL.md"), userSkill)
			}
			var env *Environment
			if phase == "prepare" {
				params.LocalWorkDir = workDir
				var err error
				env, err = Prepare(params, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer env.Cleanup(true)
			} else {
				reuse := ReuseParams{
					WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: "traecli", Task: task,
				}
				if Reuse(reuse, testLogger()) == nil {
					t.Fatal("initial Reuse returned nil")
				}
				task.AgentSkills[0].Content = "Review body v2."
				task.AgentSkills[0].Files[0].Content = "Checks v2."
				reuse.Task = task
				env = Reuse(reuse, testLogger())
				if env == nil {
					t.Fatal("Reuse returned nil")
				}
			}
			brief, err := InjectRuntimeConfig(workDir, "traecli", ApplyPreparedSkillNames(task, env.PreparedSkillNames))
			if err != nil {
				t.Fatal(err)
			}
			for i, name := range []string{"review-multica", "review-multica-multica", "multica-platform-multica"} {
				relPath := ".traecli/skills/" + name + "/SKILL.md"
				data, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(relPath)))
				if err != nil {
					t.Fatal(err)
				}
				if i == 1 {
					if strings.Contains(brief, name) || strings.Contains(brief, "Hidden description.") {
						t.Error("brief advertised a hidden skill")
					}
					continue
				}
				if !strings.Contains(brief, fmt.Sprintf("(file: `%s`)", relPath)) {
					t.Errorf("brief does not point at actual skill %q", relPath)
				}
				if i == 0 && !strings.Contains(string(data), task.AgentSkills[0].Content) {
					t.Errorf("skill body is stale: %s", data)
				}
			}
			if !strings.Contains(brief, "read `.traecli/skills/multica-platform-multica/SKILL.md`") {
				t.Error("platform hint does not follow its allocated skill path")
			}
			ref, err := os.ReadFile(filepath.Join(workDir, ".traecli", "skills", "review-multica", "references", "checks.md"))
			if err != nil || string(ref) != task.AgentSkills[0].Files[0].Content {
				t.Errorf("supporting reference = %q, error = %v", ref, err)
			}
			for _, name := range []string{"review", "multica-platform"} {
				data, err := os.ReadFile(filepath.Join(workDir, ".traecli", "skills", name, "SKILL.md"))
				if err != nil || string(data) != userSkill {
					t.Errorf("user-owned skill %q changed: %q, error = %v", name, data, err)
				}
			}
		})
	}
}
