package execenv

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestPreparePiSkillPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, provider, existing, managed string
		bound                             bool
	}{
		{"bound", "pi", "", "review", true},
		{"unassigned", "pi", "untrusted", "", false},
		{"unrelated", "pi", "untrusted", "review", true},
		{"collision", "pi", "review", "review-multica", true},
		{"omp", "omp", "untrusted", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userDir := t.TempDir()
			var existingPath string
			if tc.existing != "" {
				existingPath = filepath.Join(userDir, ".pi", "skills", tc.existing, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(existingPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(existingPath, []byte("user skill"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			task := TaskContextForEnv{IssueID: "pi-skills"}
			if tc.bound {
				task.AgentSkills = []SkillContextForEnv{{
					Name: "review", Content: "Managed skill.",
					Files: []SkillFileContextForEnv{{Path: "references/SKILL.md", Content: "Supporting file."}},
				}}
			}
			env, err := Prepare(PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "ws-pi-skills",
				TaskID:   "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
				Provider: tc.provider, LocalWorkDir: userDir, Task: task,
			}, testLogger())
			if err != nil {
				t.Fatal(err)
			}
			defer env.Cleanup(true)
			var want []string
			if tc.managed != "" {
				want = []string{filepath.Join(userDir, ".pi", "skills", tc.managed, "SKILL.md")}
				if _, err := os.Stat(want[0]); err != nil {
					t.Fatalf("managed skill missing: %v", err)
				}
			}
			if !slices.Equal(env.PiSkillPaths, want) {
				t.Fatalf("PiSkillPaths = %v, want only %v", env.PiSkillPaths, want)
			}
			if tc.provider == "pi" && tc.bound {
				visible := modelVisibleSkills(ApplyPreparedSkillNames(task, env.PreparedSkillNames).AgentSkills)
				if len(visible) != 1 || visible[0].Name != tc.managed {
					t.Fatalf("brief skill names do not match Pi's explicit discovery paths: %+v", visible)
				}
			}
			if existingPath != "" {
				if data, err := os.ReadFile(existingPath); err != nil || string(data) != "user skill" {
					t.Fatalf("user skill changed: %q, %v", data, err)
				}
			}
		})
	}
}

func TestReusePiSkillPathsRefresh(t *testing.T) {
	t.Parallel()
	task := TaskContextForEnv{IssueID: "pi-skills-reuse", AgentSkills: []SkillContextForEnv{{Name: "first", Content: "First."}}}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws-pi-reuse",
		TaskID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890", Provider: "pi", Task: task,
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	skillsDir := filepath.Join(env.WorkDir, ".pi", "skills")
	if want := []string{filepath.Join(skillsDir, "first", "SKILL.md")}; !slices.Equal(env.PiSkillPaths, want) {
		t.Fatalf("initial PiSkillPaths = %v, want %v", env.PiSkillPaths, want)
	}
	userPath := filepath.Join(skillsDir, "untrusted", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userPath, []byte("user skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		invalid bool
	}{{name: "second"}, {name: "broken", invalid: true}, {}} {
		task.AgentSkills = nil
		var want []string
		if tc.name != "" {
			task.AgentSkills = []SkillContextForEnv{{Name: tc.name, Content: "Updated."}}
			if tc.invalid {
				// A failed bundle write must not explicitly load a partial skill.
				task.AgentSkills[0].Files = []SkillFileContextForEnv{
					{Path: "duplicate.txt", Content: "first"},
					{Path: "duplicate.txt", Content: "second"},
				}
			} else {
				want = []string{filepath.Join(skillsDir, tc.name, "SKILL.md")}
			}
		}
		reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "pi", Task: task}, testLogger())
		if tc.invalid {
			// Without complete materialization there is no name to advertise.
			if reused != nil {
				t.Fatal("Reuse accepted a partial skill bundle")
			}
			if _, err := os.Stat(filepath.Join(skillsDir, tc.name)); !os.IsNotExist(err) {
				t.Fatalf("partial skill survived reuse rollback: %v", err)
			}
		} else {
			if reused == nil {
				t.Fatal("Reuse returned nil")
			}
			if !slices.Equal(reused.PiSkillPaths, want) {
				t.Fatalf("refreshed PiSkillPaths = %v, want %v", reused.PiSkillPaths, want)
			}
		}
		if data, err := os.ReadFile(userPath); err != nil || string(data) != "user skill" {
			t.Fatalf("user skill changed on reuse: %q, %v", data, err)
		}
	}
}
