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
				if provider == "codex" {
					// Batch suffixes must not claim a user skill's name during seeding.
					want[1] = "review-multica-multica"
					if got, err := os.ReadFile(filepath.Join(discoveryRoot, "skills", "review-multica", "SKILL.md")); err != nil || string(got) != userSkill {
						t.Errorf("%s: user skill is no longer discoverable: %q, %v", phase, got, err)
					}
				}
				if !reflect.DeepEqual(env.PreparedSkillNames, want) {
					t.Errorf("%s: names = %v, want %v", phase, env.PreparedSkillNames, want)
				}
				brief := buildMetaSkillContent(provider, ApplyPreparedSkillNames(params.Task, env.PreparedSkillNames))
				for i, name := range want {
					if !strings.Contains(brief, "**"+name+"**") {
						t.Errorf("%s: runtime brief does not list allocated name %q", phase, name)
					}
					data, err := os.ReadFile(filepath.Join(discoveryRoot, "skills", name, "SKILL.md"))
					if err != nil {
						t.Errorf("%s: read discovered skill %q: %v", phase, name, err)
						continue
					}
					if !strings.Contains(string(data), "\nname: "+name+"\n") || !strings.Contains(string(data), params.Task.AgentSkills[i].Content) {
						t.Errorf("%s: discovered skill %q has wrong name or content: %s", phase, name, data)
					}
				}
				if provider == "codex" && strings.Contains(brief, "**review-multica**") {
					t.Errorf("%s: runtime brief advertises the user skill as an assigned skill", phase)
				}
				for _, name := range []string{"review", "review-multica"} {
					if got, err := os.ReadFile(filepath.Join(sharedHome, "skills", name, "SKILL.md")); err != nil || string(got) != userSkill {
						t.Fatalf("%s: source user skill changed: %q, %v", phase, got, err)
					}
				}
			}
		})
	}
}

func TestPrepareQwenpawKeepsPrivateSkillNamesIndependent(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"prepare", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-qwenpaw", TaskID: "task-qwenpaw",
				Provider: "qwenpaw",
				Task:     TaskContextForEnv{AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Assigned review."}}},
			}
			workDir := t.TempDir()
			phases := []string{"prepare"}
			if mode == "reuse" {
				initial := params
				initial.Task.AgentSkills = nil
				env, err := Prepare(initial, testLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer env.Cleanup(true)
				workDir = env.WorkDir
				phases = []string{"reuse", "reuse-again"}
			}
			skillsDir := skillsDirPath(workDir, params.Provider)
			const userSkill = "User-owned skill."
			mustWrite(t, filepath.Join(skillsDir, "review", "SKILL.md"), userSkill)
			for _, phase := range phases {
				var env *Environment
				if mode == "prepare" {
					params.LocalWorkDir = workDir
					var err error
					env, err = Prepare(params, testLogger())
					if err != nil {
						t.Fatal(err)
					}
					defer env.Cleanup(true)
				} else {
					env = Reuse(ReuseParams{
						WorkspacesRoot: params.WorkspacesRoot, WorkDir: workDir, Provider: params.Provider, Task: params.Task,
					}, testLogger())
					if env == nil {
						t.Fatalf("%s returned no environment", phase)
					}
				}
				if !reflect.DeepEqual(env.PreparedSkillNames, []string{"review"}) {
					t.Errorf("%s: prepared names = %v, want [review]", phase, env.PreparedSkillNames)
				}
				// QwenPaw discovers the isolated --workspace, not the workdir sidecar.
				data, err := os.ReadFile(filepath.Join(env.QwenpawWorkspace, "skills", "review", "SKILL.md"))
				if err != nil || !strings.Contains(string(data), "\nname: review\n") {
					t.Errorf("%s: workdir collision changed the independent private skill name: %s, %v", phase, data, err)
				}
				brief := buildMetaSkillContent(params.Provider, ApplyPreparedSkillNames(params.Task, env.PreparedSkillNames))
				if !strings.Contains(brief, "**review**") || strings.Contains(brief, "**review-multica**") {
					t.Errorf("%s: runtime brief does not advertise the private discovery name", phase)
				}
				if _, err := os.Stat(filepath.Join(skillsDir, "review-multica", "SKILL.md")); err != nil {
					t.Errorf("%s: workdir sidecar did not use a collision-free name: %v", phase, err)
				}
				if got, err := os.ReadFile(filepath.Join(skillsDir, "review", "SKILL.md")); err != nil || string(got) != userSkill {
					t.Fatalf("%s: user-owned workdir skill changed: %q, %v", phase, got, err)
				}
			}
		})
	}
}

func TestHydrateCodexSkillsReturnsNamesOnConfigError(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	mustWrite(t, filepath.Join(sharedHome, "skills", "review-multica", "SKILL.md"), "User-owned skill.")
	codexHome := t.TempDir()
	// Force a policy-config error after skill materialization has completed.
	if err := os.Mkdir(filepath.Join(codexHome, "config.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	skills := []SkillContextForEnv{
		{Name: "Review", Content: "First assigned review."},
		{Name: "Review", Content: "Second assigned review."},
	}
	names, err := hydrateCodexSkills(codexHome, skills, []RuntimeSkillRefForEnv{
		{Root: "provider", Key: "review-multica"},
	}, testLogger())
	if err == nil {
		t.Fatal("expected the user skill's disabled-policy config write to fail")
	}
	want := []string{"review", "review-multica-multica"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("allocated names lost after config failure: %v, want %v", names, want)
	}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(codexHome, "skills", name, "SKILL.md")); err != nil {
			t.Errorf("returned name %q has no materialized skill: %v", name, err)
		}
	}
}
