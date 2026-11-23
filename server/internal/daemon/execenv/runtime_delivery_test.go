package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func prepareRuntimeWorktree(t *testing.T, provider string, tracked bool) (string, *Environment, string) {
	t.Helper()
	t.Setenv("CODEX_HOME", t.TempDir())
	repo := newTestRepo(t)
	if tracked {
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			writeFile(t, filepath.Join(repo, name), "Project guidance\n")
		}
		gitRun(t, repo, "add", "-A")
		gitRun(t, repo, "commit", "-m", "project instructions")
	}
	ctx := TaskContextForEnv{IssueID: "issue", AgentName: "runtime-test", ProjectID: "project", ProjectResources: []ProjectResourceForEnv{{ID: "resource", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://github.com/example/repo"}`)}}, AgentSkills: []SkillContextForEnv{{ID: "skill", Name: "runtime-probe", Content: "Fixture skill"}}}
	version := "2.1.281"
	if provider == "codex" {
		version = "0.169.0"
	}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", AgentName: "runtime-test", Provider: provider, RuntimeVersion: version, CodexVersion: version, LocalWorktree: &LocalWorktreeParams{LocalPath: repo}, Task: ctx}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { env.LocalWorktree.Discard(worktreeTestLogger()); env.Cleanup(true) })
	brief, err := env.InjectRuntimeConfig(provider, ApplyPreparedSkillNames(ctx, env.PreparedSkillNames))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.ProtectRuntimeConfig(provider); err != nil {
		t.Fatal(err)
	}
	return repo, env, brief
}

func TestRuntimeWorktreeLeavesNoFilesInCheckout(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			_, env, brief := prepareRuntimeWorktree(t, provider, true)
			if got := gitRun(t, env.WorkDir, "status", "--porcelain", "--ignored"); got != "" {
				t.Fatalf("runtime dirtied checkout: %s", got)
			}
			if strings.Contains(brief, env.RootDir) {
				t.Fatal("brief embeds the task root path")
			}
			if !strings.Contains(brief, "`.multica/project/resources.json` in the task directory that contains this checkout") {
				t.Fatal("brief does not point at the external resources file")
			}
			if _, err := os.Stat(filepath.Join(env.RootDir, TaskContextMarkerRelPath)); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(env.RootDir, ".multica", "project", "resources.json")); err != nil {
				t.Fatal(err)
			}
			// The task's marker remains an ancestor signal after every injected
			// environment variable is dropped, including a nested resource cwd.
			for dir := env.WorkDir; ; dir = filepath.Dir(dir) {
				if _, err := os.Stat(filepath.Join(dir, TaskContextMarkerRelPath)); err == nil {
					break
				}
				if dir == filepath.Dir(dir) {
					t.Fatal("marker not reachable from checkout")
				}
			}
			var manifest sidecarManifest
			if err := json.Unmarshal([]byte(readFile(t, filepath.Join(env.RootDir, sidecarManifestFile))), &manifest); err != nil {
				t.Fatal(err)
			}
			for _, path := range append(manifest.Files, manifest.Dirs...) {
				if rel, err := filepath.Rel(env.LocalWorktree.Path, path); err == nil && filepath.IsLocal(rel) {
					t.Errorf("sidecar inside checkout: %s", rel)
				}
			}
			// Preparation-helper round trips must retain the selected layout.
			data, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			var decoded Environment
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.RuntimeDir != env.RuntimeDir {
				t.Fatal("runtime layout lost over helper boundary")
			}
			if got := env.SkillReceipts(provider, []SkillContextForEnv{{ID: "skill", Name: "runtime-probe"}}); len(got) != 1 || !got[0].Available {
				t.Fatalf("runtime skills missing from receipt: %+v", got)
			}
		})
	}
}

// A continued worktree gets a new task root on every run. The brief must not
// follow it: MUL-5377 requires byte-identical briefs per (agent, issue,
// provider), and a resumed Codex thread keeps the developer instructions it
// started with, so any task-root path in them would point at a reclaimed
// directory.
func TestRuntimeWorktreeBriefByteIdenticalAcrossTaskRoots(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			_, first, firstBrief := prepareRuntimeWorktree(t, provider, true)
			_, second, secondBrief := prepareRuntimeWorktree(t, provider, true)
			if first.RootDir == second.RootDir {
				t.Fatal("fixture did not produce distinct task roots")
			}
			if firstBrief != secondBrief {
				t.Fatalf("brief changed with the task root:\n--- first\n%s\n--- second\n%s", firstBrief, secondBrief)
			}
		})
	}
}

// The two original reproduction paths, plus partial staging and both new and
// existing project instruction files. Git runs without any task Git overrides.
func TestRuntimeWorktreeAgentCommits(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, tracked := range []bool{true, false} {
			for _, mode := range []string{"add-all", "commit-a", "partial"} {
				name := provider + "/new/" + mode
				if tracked {
					name = provider + "/tracked/" + mode
				}
				t.Run(name, func(t *testing.T) {
					repo, env, _ := prepareRuntimeWorktree(t, provider, tracked)
					for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
						writeFile(t, filepath.Join(env.WorkDir, name), "Business guidance\n")
					}
					writeFile(t, filepath.Join(env.WorkDir, "tracked.txt"), "Business change\n")
					if mode == "partial" {
						gitRun(t, env.WorkDir, "add", "CLAUDE.md", "AGENTS.md")
						for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
							writeFile(t, filepath.Join(env.WorkDir, name), "Business guidance\nUnstaged guidance\n")
						}
						gitRun(t, env.WorkDir, "commit", "-m", "selected guidance")
					} else if mode == "commit-a" && tracked {
						gitRun(t, env.WorkDir, "commit", "-a", "-m", "business work")
					} else {
						gitRun(t, env.WorkDir, "add", "-A")
						gitRun(t, env.WorkDir, "commit", "-m", "business work")
					}
					if err := env.CleanupRuntimeConfig(provider); err != nil {
						t.Fatal(err)
					}
					if err := CleanupSidecars(env.RootDir); err != nil {
						t.Fatal(err)
					}
					out := finalizeOK(t, env.LocalWorktree)
					for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
						if got := gitRun(t, repo, "show", out.Branch+":"+name); got != "Business guidance" {
							t.Errorf("%s: %q", name, got)
						}
					}
					files := strings.Fields(gitRun(t, repo, "diff", "--name-only", env.LocalWorktree.BaseCommit, out.Branch))
					for _, path := range files {
						if !slices.Contains([]string{"CLAUDE.md", "AGENTS.md", "tracked.txt"}, path) {
							t.Errorf("unexpected committed sidecar: %s", path)
						}
					}
					if got := gitRun(t, repo, "status", "--porcelain", "--ignored"); got != "" {
						t.Fatalf("user checkout changed: %s", got)
					}
				})
			}
		}
	}
}

func TestRuntimeWorktreeClaudeSkillCollision(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".claude", "skills", "runtime-probe", "SKILL.md"), "User skill")
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-m", "project skill")
	ctx := TaskContextForEnv{AgentSkills: []SkillContextForEnv{{ID: "skill", Name: "runtime-probe", Content: "Multica skill"}}}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", Provider: "claude", RuntimeVersion: "2.1.281", LocalWorktree: &LocalWorktreeParams{LocalPath: repo}, Task: ctx}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.LocalWorktree.Discard(worktreeTestLogger())
	defer env.Cleanup(true)
	if !slices.Equal(env.PreparedSkillNames, []string{"runtime-probe-multica"}) {
		t.Fatalf("names=%v", env.PreparedSkillNames)
	}
	if got := readFile(t, filepath.Join(env.WorkDir, ".claude", "skills", "runtime-probe", "SKILL.md")); got != "User skill" {
		t.Fatal("project skill modified")
	}
	if got := gitRun(t, env.WorkDir, "status", "--porcelain", "--ignored"); got != "" {
		t.Fatal(got)
	}
}

// In a linked worktree without its own .claude/skills, Claude Code loads the
// main checkout's project skills, including gitignored ones the worktree
// never receives. A runtime skill must not collide with those either, or
// Claude would rename it and the brief would name a skill that does not exist.
func TestRuntimeWorktreeClaudeSkillAvoidsMainCheckoutSkills(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".claude/skills/\n")
	gitRun(t, repo, "add", ".gitignore")
	gitRun(t, repo, "commit", "-m", "ignore personal skills")
	writeFile(t, filepath.Join(repo, ".claude", "skills", "runtime-probe", "SKILL.md"), "Ignored user skill")
	ctx := TaskContextForEnv{AgentSkills: []SkillContextForEnv{{ID: "skill", Name: "runtime-probe", Content: "Multica skill"}}}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", Provider: "claude", RuntimeVersion: "2.1.281", LocalWorktree: &LocalWorktreeParams{LocalPath: repo}, Task: ctx}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.LocalWorktree.Discard(worktreeTestLogger())
	defer env.Cleanup(true)
	if _, err := os.Stat(filepath.Join(env.LocalWorktree.Path, ".claude", "skills")); !os.IsNotExist(err) {
		t.Fatalf("fixture needs a worktree without .claude/skills (err=%v)", err)
	}
	if !slices.Equal(env.PreparedSkillNames, []string{"runtime-probe-multica"}) {
		t.Fatalf("names=%v", env.PreparedSkillNames)
	}
}

// The resource may point at a linked worktree (B) of a repository whose main
// checkout is A. Claude falls back to A's project skills, not B's, so A's
// gitignored skills must be avoided too.
func TestRuntimeWorktreeClaudeSkillAvoidsMainCheckoutOfLinkedSource(t *testing.T) {
	main := newTestRepo(t)
	writeFile(t, filepath.Join(main, ".gitignore"), ".claude/skills/\n")
	gitRun(t, main, "add", ".gitignore")
	gitRun(t, main, "commit", "-m", "ignore personal skills")
	writeFile(t, filepath.Join(main, ".claude", "skills", "runtime-probe", "SKILL.md"), "Ignored user skill")
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, main, "worktree", "add", "-b", "linked-source", linked)
	ctx := TaskContextForEnv{AgentSkills: []SkillContextForEnv{{ID: "skill", Name: "runtime-probe", Content: "Multica skill"}}}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", Provider: "claude", RuntimeVersion: "2.1.281", LocalWorktree: &LocalWorktreeParams{LocalPath: linked}, Task: ctx}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.LocalWorktree.Discard(worktreeTestLogger())
	defer env.Cleanup(true)
	if !slices.Equal(env.PreparedSkillNames, []string{"runtime-probe-multica"}) {
		t.Fatalf("names=%v", env.PreparedSkillNames)
	}
}

func TestMainCheckoutRoot(t *testing.T) {
	main := newTestRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, main, "worktree", "add", "-b", "linked-root", linked)
	want, err := filepath.EvalSymlinks(main)
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{main, linked} {
		got, err := mainCheckoutRoot(from)
		if err != nil {
			t.Fatal(err)
		}
		if resolved, err := filepath.EvalSymlinks(got); err != nil || resolved != want {
			t.Fatalf("mainCheckoutRoot(%s) = %s, want %s", from, got, want)
		}
	}
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitRun(t, main, "clone", "--bare", main, bare)
	if got, err := mainCheckoutRoot(bare); err != nil || got != "" {
		t.Fatalf("bare repository: %q, %v", got, err)
	}
}

func TestRuntimeDeliveryLegacyScope(t *testing.T) {
	for _, tc := range []struct {
		provider, version string
		worktree          bool
	}{
		{"claude", "2.0.0", true}, {"claude", "", true}, {"codex", "0.100.0", true},
		{"pi", "9.9.9", true}, {"claude", "2.1.281", false}, {"codex", "0.169.0", false},
	} {
		t.Run(tc.provider+tc.version, func(t *testing.T) {
			env := &Environment{RootDir: t.TempDir(), WorkDir: t.TempDir()}
			if tc.worktree {
				env.LocalWorktree = &LocalWorktree{}
			}
			if err := env.selectRuntimeDelivery(tc.provider, tc.version, worktreeTestLogger()); err != nil {
				t.Fatal(err)
			}
			if env.RuntimeDir != "" {
				t.Fatal("changed an unvalidated mode/provider")
			}
			if got := env.CodexDeveloperInstructions(tc.provider); got != "" {
				t.Fatalf("developer instructions for a brief written into the checkout: %q", got)
			}
			if _, err := env.InjectRuntimeConfig(tc.provider, TaskContextForEnv{}); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, runtimeConfigPath(env.WorkDir, tc.provider)); !strings.Contains(got, runtimeMarkerBegin) {
				t.Fatal("legacy brief lost")
			}
		})
	}
}

// Codex reads the brief as global instructions from the task CODEX_HOME. The
// repository's AGENTS.md is never written, and the user's own global AGENTS.md
// is still not copied in, as for every other Codex task.
func TestRuntimeWorktreeCodexBriefInTaskHome(t *testing.T) {
	source := t.TempDir()
	t.Setenv("CODEX_HOME", source)
	writeFile(t, filepath.Join(source, "AGENTS.md"), "Global fallback")
	writeFile(t, filepath.Join(source, "AGENTS.override.md"), "Global override")
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "Project guidance\n")
	gitRun(t, repo, "add", "AGENTS.md")
	gitRun(t, repo, "commit", "-m", "project instructions")
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace", TaskID: "11112222-3333-4444-5555-666677778888", Provider: "codex", RuntimeVersion: "0.169.0", CodexVersion: "0.169.0", LocalWorktree: &LocalWorktreeParams{LocalPath: repo}}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	defer env.LocalWorktree.Discard(worktreeTestLogger())
	brief, err := env.InjectRuntimeConfig("codex", TaskContextForEnv{IssueID: "issue"})
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(env.CodexHome, "AGENTS.md")); !strings.Contains(got, strings.TrimSpace(brief)) || strings.Contains(got, "Global fallback") {
		t.Fatalf("task CODEX_HOME AGENTS.md = %q", got)
	}
	if _, err := os.Lstat(filepath.Join(env.CodexHome, "AGENTS.override.md")); !os.IsNotExist(err) {
		t.Fatalf("user's global override copied into the task home (err=%v)", err)
	}
	if got := readFile(t, filepath.Join(env.WorkDir, "AGENTS.md")); got != "Project guidance\n" {
		t.Fatalf("repository AGENTS.md changed: %q", got)
	}
	if got := env.RuntimeConfigReceiptPath("codex"); got != "$CODEX_HOME/AGENTS.md" {
		t.Fatalf("receipt path = %q", got)
	}
}

// The brief in CODEX_HOME loads ahead of the repository's AGENTS.md, so Codex
// also receives developer instructions that point back to it by its heading.
func TestRuntimeWorktreeCodexDeveloperInstructions(t *testing.T) {
	_, env, brief := prepareRuntimeWorktree(t, "codex", true)
	if got := env.CodexDeveloperInstructions("codex"); got != RuntimeBriefPrecedence {
		t.Fatalf("developer instructions = %q", got)
	}
	if !strings.HasPrefix(brief, runtimeBriefHeading+"\n") || !strings.Contains(RuntimeBriefPrecedence, runtimeBriefHeading) {
		t.Fatalf("developer instructions name a heading the brief does not open with: brief starts %q", brief[:min(len(brief), 40)])
	}
	_, claudeEnv, _ := prepareRuntimeWorktree(t, "claude", true)
	if got := claudeEnv.CodexDeveloperInstructions("claude"); got != "" {
		t.Fatalf("claude developer instructions = %q", got)
	}
}
