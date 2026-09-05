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
			if provider == "codex" {
				params.Task.DisabledRuntimeSkills = []RuntimeSkillRefForEnv{
					{Root: "provider", Key: "review-multica"},
					{Root: "provider", Key: "review-multica-multica"},
				}
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
				if provider == "codex" {
					config, err := os.ReadFile(filepath.Join(discoveryRoot, "config.toml"))
					if err != nil {
						t.Fatalf("%s: read Codex skill policy: %v", phase, err)
					}
					userPath := filepath.ToSlash(filepath.Join(discoveryRoot, "skills", "review-multica", "SKILL.md"))
					assignedPath := filepath.ToSlash(filepath.Join(discoveryRoot, "skills", "review-multica-multica", "SKILL.md"))
					if !strings.Contains(string(config), userPath) {
						t.Errorf("%s: Codex policy stopped disabling the user skill:\n%s", phase, config)
					}
					if strings.Contains(string(config), assignedPath) {
						t.Errorf("%s: Codex policy disabled the allocated workspace skill:\n%s", phase, config)
					}
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

func TestRuntimeSkillPolicyUsesAllocatedClaudeName(t *testing.T) {
	for _, mode := range []string{"prepare", "reuse"} {
		t.Run(mode, func(t *testing.T) {
			task := TaskContextForEnv{
				IssueID:     "issue-claude-policy",
				AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Assigned review."}},
				DisabledRuntimeSkills: []RuntimeSkillRefForEnv{
					{Root: "provider", Key: "review", Name: "review"},
					{Root: "provider", Key: "review-multica", Name: "review-multica"},
				},
			}
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-claude-policy",
				TaskID: "task-claude-policy-" + mode, Provider: "claude", Task: task,
			}
			var env *Environment
			var err error
			if mode == "prepare" {
				params.LocalWorkDir = t.TempDir()
				mustWrite(t, filepath.Join(params.LocalWorkDir, ".claude", "skills", "review", "SKILL.md"), "User-owned skill.")
				env, err = Prepare(params, testLogger())
			} else {
				initial := params
				initial.Task = TaskContextForEnv{IssueID: task.IssueID}
				env, err = Prepare(initial, testLogger())
				if err == nil {
					mustWrite(t, filepath.Join(env.WorkDir, ".claude", "skills", "review", "SKILL.md"), "User-owned skill.")
					env = Reuse(ReuseParams{
						WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
						Provider: params.Provider, Task: task,
					}, testLogger())
				}
			}
			if err != nil || env == nil {
				t.Fatalf("%s environment = %#v, error = %v", mode, env, err)
			}
			defer env.Cleanup(true)
			if !reflect.DeepEqual(env.PreparedSkillNames, []string{"review-multica"}) {
				t.Fatalf("prepared names = %v, want [review-multica]", env.PreparedSkillNames)
			}
			policy, err := os.ReadFile(env.ClaudeSettingsPath)
			if err != nil {
				t.Fatalf("read Claude skill policy: %v", err)
			}
			if strings.Contains(string(policy), "review-multica") {
				t.Fatalf("Claude policy disabled the allocated workspace skill:\n%s", policy)
			}
			if !strings.Contains(string(policy), "Skill(review)") {
				t.Fatalf("Claude policy stopped disabling the user-owned collision:\n%s", policy)
			}
		})
	}
}

func TestReuseFailsClosedWhenSkillNamesAreUnavailable(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			sharedHome := t.TempDir()
			t.Setenv("CODEX_HOME", sharedHome)
			params := PrepareParams{
				WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-reuse-failure",
				TaskID: "task-reuse-failure-" + provider, Provider: provider,
				Task: TaskContextForEnv{IssueID: "issue-reuse-failure"},
			}
			env, err := Prepare(params, testLogger())
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			defer env.Cleanup(true)

			if provider == "claude" {
				mustWrite(t, filepath.Join(env.WorkDir, ".claude", "skills", "review", "SKILL.md"), "User-owned skill.")
			}
			task := TaskContextForEnv{
				IssueID: "issue-reuse-failure",
				AgentSkills: []SkillContextForEnv{{
					Name: "Review", Content: "Assigned review.",
					Files: []SkillFileContextForEnv{
						{Path: "blocker", Content: "file"},
						{Path: "blocker/child", Content: "cannot be written below a file"},
					},
				}},
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			reused, err := ReuseIsolated(ctx, preparationHelperTestCommand(), ReuseParams{
				WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
				Provider: provider, Task: task,
			}, testLogger())
			if err != nil {
				t.Fatalf("ReuseIsolated: %v", err)
			}
			if reused != nil {
				t.Fatalf("Reuse continued with incomplete skill materialization: %#v", reused.PreparedSkillNames)
			}
			if _, err := os.Stat(filepath.Join(env.WorkDir, TaskContextMarkerRelPath)); !os.IsNotExist(err) {
				t.Errorf("incomplete reuse left the task marker behind: %v", err)
			}
			if provider == "claude" {
				if _, err := os.Stat(filepath.Join(env.WorkDir, ".claude", "skills", "review-multica")); !os.IsNotExist(err) {
					t.Errorf("incomplete reuse left the allocated skill behind: %v", err)
				}
				if got, err := os.ReadFile(filepath.Join(env.WorkDir, ".claude", "skills", "review", "SKILL.md")); err != nil || string(got) != "User-owned skill." {
					t.Errorf("incomplete reuse changed the user skill: %q, %v", got, err)
				}
			} else if _, err := os.Stat(filepath.Join(env.RootDir, codexHomeDirName, "skills")); !os.IsNotExist(err) {
				t.Errorf("incomplete reuse left Codex skills behind: %v", err)
			}
		})
	}
}

func TestReuseKeepsProjectResourceWriteFailureBestEffort(t *testing.T) {
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-resource-collision",
		TaskID: "task-resource-collision", Provider: "claude",
		Task: TaskContextForEnv{IssueID: "issue-resource-collision"},
	}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	task := TaskContextForEnv{
		IssueID:     "issue-resource-collision",
		AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Assigned review."}},
		ProjectID:   "project-resource-collision",
		ProjectResources: []ProjectResourceForEnv{{
			ID: "resource-collision", ResourceType: "github_repo",
			ResourceRef: []byte(`{`),
		}},
	}
	reused := Reuse(ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
		Provider: params.Provider, Task: task,
	}, testLogger())
	if reused == nil || !reflect.DeepEqual(reused.PreparedSkillNames, []string{"review"}) {
		t.Fatalf("best-effort project resource failure prevented reuse: %#v", reused)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, ".multica", "project", "resources.json")); !os.IsNotExist(err) {
		t.Fatalf("invalid project resource unexpectedly produced a sidecar: %v", err)
	}
}

func TestReuseFallsBackWhenClaudeSkillPolicyCannotBeWritten(t *testing.T) {
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-claude-policy-error",
		TaskID: "task-claude-policy-error", Provider: "claude",
		Task: TaskContextForEnv{IssueID: "issue-claude-policy-error"},
	}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	userSkill := filepath.Join(env.WorkDir, ".claude", "skills", "review", "SKILL.md")
	mustWrite(t, userSkill, "User-owned skill.")
	if err := os.Mkdir(filepath.Join(env.RootDir, claudeRuntimeSkillSettingsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	task := TaskContextForEnv{
		IssueID:     "issue-claude-policy-error",
		AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Assigned review."}},
		DisabledRuntimeSkills: []RuntimeSkillRefForEnv{
			{Root: "provider", Key: "review", Name: "review"},
		},
	}
	if reused := Reuse(ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
		Provider: params.Provider, Task: task,
	}, testLogger()); reused != nil {
		t.Fatalf("Reuse continued without the required Claude skill policy: %#v", reused)
	}
	for _, path := range []string{
		filepath.Join(env.WorkDir, TaskContextMarkerRelPath),
		filepath.Join(env.WorkDir, ".claude", "skills", "review-multica"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("failed Claude policy refresh left %q behind: %v", path, err)
		}
	}
	if got, err := os.ReadFile(userSkill); err != nil || string(got) != "User-owned skill." {
		t.Fatalf("failed Claude policy refresh changed the user skill: %q, %v", got, err)
	}
}

func TestReuseFallsBackWhenCodexHomeRefreshFails(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-codex-home-error",
		TaskID: "task-codex-home-error", Provider: "codex",
		Task: TaskContextForEnv{IssueID: "issue-codex-home-error"},
	}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	codexHome := filepath.Join(env.RootDir, codexHomeDirName)
	if err := os.RemoveAll(codexHome); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexHome, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if reused := Reuse(ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot, WorkDir: env.WorkDir,
		Provider: params.Provider, Task: params.Task,
	}, testLogger()); reused != nil {
		t.Fatalf("Reuse continued after Codex home refresh failed: %#v", reused)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, TaskContextMarkerRelPath)); !os.IsNotExist(err) {
		t.Errorf("failed Codex home refresh left the task marker behind: %v", err)
	}
}
