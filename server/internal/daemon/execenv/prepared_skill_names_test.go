package execenv

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Existing context-file tests only need the write error, not allocated names.
func writeContextFiles(workDir, provider string, task TaskContextForEnv, manifest *sidecarManifest) error {
	_, err := writeContextFilesWithSkillNames(workDir, provider, task, manifest)
	return err
}

func TestPreparationHelperReturnsAllocatedSkillNames(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"prepare", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			task := TaskContextForEnv{
				IssueID: "issue-skill-names",
				AgentSkills: []SkillContextForEnv{
					{Name: "Review", Content: "---\nname: first\n---\n\nFirst skill."},
					{Name: "Review", Content: "---\nname: hidden\ndisable-model-invocation: true\n---\n\nHidden skill."},
					{Name: "Review Multica", Content: "---\nname: last\n---\n\nLast skill."},
				},
			}
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-skill-names",
				TaskID: "task-skill-names", Provider: "claude", Task: task,
			}
			workDir := t.TempDir()
			if mode == "reuse" {
				initial := params
				initial.Task.AgentSkills = nil
				env, err := PrepareIsolated(ctx, preparationHelperTestCommand(), initial, logger)
				if err != nil {
					t.Fatalf("initial PrepareIsolated: %v", err)
				}
				workDir = env.WorkDir
			}
			userSkillPath := filepath.Join(workDir, ".claude", "skills", "review", "SKILL.md")
			const userSkill = "---\nname: review\n---\n\nUser-owned skill.\n"
			if err := os.MkdirAll(filepath.Dir(userSkillPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(userSkillPath, []byte(userSkill), 0o600); err != nil {
				t.Fatal(err)
			}
			var env *Environment
			var err error
			if mode == "prepare" {
				params.LocalWorkDir = workDir
				env, err = PrepareIsolated(ctx, preparationHelperTestCommand(), params, logger)
			} else {
				env, err = ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
					WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: params.Provider, Task: task,
				}, logger)
			}
			if err != nil || env == nil {
				t.Fatalf("%s environment = %#v, error = %v", mode, env, err)
			}
			want := []string{"review-multica", "review-multica-multica", "review-multica-multica-multica"}
			if !reflect.DeepEqual(env.PreparedSkillNames, want) {
				t.Fatalf("prepared names = %v, want %v", env.PreparedSkillNames, want)
			}
			prepared := ApplyPreparedSkillNames(task, env.PreparedSkillNames)
			for i, name := range want {
				data, err := os.ReadFile(filepath.Join(workDir, ".claude", "skills", name, "SKILL.md"))
				if err != nil {
					t.Fatalf("read allocated skill %q: %v", name, err)
				}
				if !strings.Contains(string(data), "\nname: "+name+"\n") {
					t.Errorf("skill %d frontmatter does not match allocated name %q: %s", i, name, data)
				}
				if prepared.AgentSkills[i].Content != task.AgentSkills[i].Content {
					t.Errorf("skill %d content changed while applying names", i)
				}
			}
			visible := modelVisibleSkills(prepared.AgentSkills)
			if len(visible) != 2 || visible[0].Name != want[0] || visible[1].Name != want[2] {
				t.Fatalf("visible skills lost allocated names or included hidden skill: %+v", visible)
			}
			for _, kind := range []TaskContextForEnv{
				{IssueID: task.IssueID}, {ChatSessionID: "chat"},
				{AutopilotRunID: "autopilot"}, {QuickCreatePrompt: "create"},
			} {
				kind.AgentSkills = prepared.AgentSkills
				brief := buildMetaSkillContent(params.Provider, kind)
				for i, name := range want {
					if got := strings.Contains(brief, "**"+name+"**"); got != (i != 1) {
						t.Errorf("brief lists %q = %v, want %v", name, got, i != 1)
					}
				}
			}
			if mode == "reuse" {
				reused, err := ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
					WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: params.Provider, Task: task,
				}, logger)
				if err != nil || reused == nil || !reflect.DeepEqual(reused.PreparedSkillNames, want) {
					t.Fatalf("repeated reuse drifted: environment = %#v, error = %v", reused, err)
				}
			}
			if err := CleanupSidecars(env.RootDir); err != nil {
				t.Fatalf("cleanup sidecars: %v", err)
			}
			if got, err := os.ReadFile(userSkillPath); err != nil || string(got) != userSkill {
				t.Fatalf("user-owned skill changed: %q, %v", got, err)
			}
			for _, name := range want {
				if _, err := os.Stat(filepath.Join(workDir, ".claude", "skills", name)); !os.IsNotExist(err) {
					t.Errorf("allocated skill %q survived cleanup: %v", name, err)
				}
			}
		})
	}
}

func TestApplyPreparedSkillNamesDoesNotMutateInput(t *testing.T) {
	t.Parallel()
	task := TaskContextForEnv{AgentSkills: []SkillContextForEnv{{Name: "Review"}, {Name: "Review"}}}
	want := []string{"review-multica", "review-multica-multica"}
	prepared := ApplyPreparedSkillNames(task, want)
	for i, name := range want {
		if prepared.AgentSkills[i].Name != name || task.AgentSkills[i].Name != "Review" {
			t.Fatalf("skill %d: prepared = %q, original = %q", i, prepared.AgentSkills[i].Name, task.AgentSkills[i].Name)
		}
	}
	for _, names := range [][]string{nil, {"incomplete"}} {
		if got := ApplyPreparedSkillNames(task, names); !reflect.DeepEqual(got, task) {
			t.Errorf("incomplete prepared names changed task: %+v", got)
		}
	}
}

func TestPreparedSkillNamesMatchPrivateDiscoveryDirectories(t *testing.T) {
	for _, provider := range []string{"codex", "hermes", "qwenpaw"} {
		t.Run(provider, func(t *testing.T) {
			// No provider config or skills may be read from the developer's home.
			sharedHome := t.TempDir()
			t.Setenv("CODEX_HOME", sharedHome)
			const userSkill = "User-owned skill must stay unchanged."
			for _, name := range []string{"review", "review-multica"} {
				mustWrite(t, filepath.Join(sharedHome, "skills", name, "SKILL.md"), userSkill)
			}
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-private-skills",
				TaskID: "task-private-skills", Provider: provider, HermesSourceHome: sharedHome,
				Task: TaskContextForEnv{IssueID: "issue-private-skills", AgentSkills: []SkillContextForEnv{
					{Name: "Review", Content: "First assigned review."},
					{Name: "Review", Content: "Second assigned review."},
				}},
			}
			env, err := Prepare(params, testLogger())
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			defer env.Cleanup(true)
			for _, phase := range []string{"prepare", "reuse"} {
				if phase == "reuse" {
					env = Reuse(ReuseParams{
						WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
						Provider: provider, HermesSourceHome: sharedHome, Task: params.Task,
					}, testLogger())
					if env == nil {
						t.Fatal("Reuse returned nil")
					}
				}
				discoveryRoot := env.CodexHome
				if provider == "hermes" {
					discoveryRoot = env.HermesHome
				} else if provider == "qwenpaw" {
					discoveryRoot = env.QwenpawWorkspace
				}
				if discoveryRoot == "" {
					t.Fatalf("%s: no private discovery root for %s", phase, provider)
				}
				want := []string{"review", "review-multica"}
				if !reflect.DeepEqual(env.PreparedSkillNames, want) {
					t.Fatalf("%s: names = %v, want %v", phase, env.PreparedSkillNames, want)
				}
				for i, name := range want {
					data, err := os.ReadFile(filepath.Join(discoveryRoot, "skills", name, "SKILL.md"))
					if err != nil {
						t.Fatalf("%s: read discovered skill %q: %v", phase, name, err)
					}
					if !strings.Contains(string(data), "\nname: "+name+"\n") || !strings.Contains(string(data), params.Task.AgentSkills[i].Content) {
						t.Errorf("%s: discovered skill %q has wrong name or content: %s", phase, name, data)
					}
					if got, err := os.ReadFile(filepath.Join(sharedHome, "skills", name, "SKILL.md")); err != nil || string(got) != userSkill {
						t.Fatalf("%s: source user skill changed: %q, %v", phase, got, err)
					}
				}
			}
		})
	}
}

func TestPrepareQwenpawUsesAllocatedWorkdirSkillNames(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	mustWrite(t, filepath.Join(skillsDirPath(workDir, "qwenpaw"), "review", "SKILL.md"), "User-owned skill.")
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-qwenpaw", TaskID: "task-qwenpaw",
		Provider: "qwenpaw", LocalWorkDir: workDir,
		Task: TaskContextForEnv{AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Assigned review."}}},
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	if !reflect.DeepEqual(env.PreparedSkillNames, []string{"review-multica"}) {
		t.Fatalf("prepared names = %v, want [review-multica]", env.PreparedSkillNames)
	}
	data, err := os.ReadFile(filepath.Join(env.QwenpawWorkspace, "skills", "review-multica", "SKILL.md"))
	if err != nil || !strings.Contains(string(data), "\nname: review-multica\n") {
		t.Fatalf("private discovery path did not preserve allocated workdir name: %s, %v", data, err)
	}
}
