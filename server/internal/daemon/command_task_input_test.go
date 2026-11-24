package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestBuildCommandTaskInputJSON(t *testing.T) {
	t.Parallel()

	task := Task{
		ID:                      "task-1",
		WorkspaceID:             "ws-1",
		WorkspaceSlug:           "acme",
		AgentID:                 "agent-1",
		RuntimeID:               "rt-1",
		IssueID:                 "issue-1",
		IssueIdentifier:         "MUL-6202",
		IssueStatus:             "in_review",
		IssueBranchName:         "agent/claude/mul-6202",
		TriggerCommentID:        "comment-1",
		TriggerThreadID:         "comment-1",
		TriggerCommentContent:   "run deterministic check",
		TriggerAuthorType:       "member",
		TriggerAuthorName:       "Alice",
		AutopilotID:             "ap-1",
		AutopilotRunID:          "ap-run-1",
		AutopilotTitle:          "Nightly Check",
		AutopilotSource:         "schedule",
		AutopilotTriggerPayload: json.RawMessage(`{"branch":"main"}`),
		ProjectWorkflow: &ProjectWorkflowData{
			Brief:         "workflow brief",
			StatusKey:     "in_review",
			NextStatusKey: "done",
			BackStatusKey: "in_progress",
			IsAssignee:    true,
		},
		Agent: &AgentData{
			ID:   "agent-1",
			Name: "Guardrail Runner",
		},
	}

	gitInfo := &CommandTaskGitInput{
		RepoDir:    "/tmp/workdir",
		WorkBranch: "agent/claude/mul-6202",
		BaseBranch: "main",
		BaseCommit: "abc1234",
	}

	payload, err := buildCommandTaskInputJSON(task, "/tmp/workdir", gitInfo)
	if err != nil {
		t.Fatalf("buildCommandTaskInputJSON: %v", err)
	}

	var decoded CommandTaskInput
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if decoded.SchemaVersion != CommandTaskInputVersion {
		t.Errorf("SchemaVersion = %d, want %d", decoded.SchemaVersion, CommandTaskInputVersion)
	}
	if decoded.TaskID != "task-1" || decoded.WorkspaceID != "ws-1" || decoded.AgentID != "agent-1" {
		t.Errorf("unexpected top-level identity: %+v", decoded)
	}
	if decoded.Kind != "autopilot" {
		t.Errorf("Kind = %q, want autopilot", decoded.Kind)
	}
	if decoded.Issue == nil || decoded.Issue.Identifier != "MUL-6202" || decoded.Issue.BranchName != "agent/claude/mul-6202" {
		t.Errorf("Issue = %+v, want MUL-6202 with branch agent/claude/mul-6202", decoded.Issue)
	}
	if decoded.Git == nil || decoded.Git.BaseBranch != "main" || decoded.Git.WorkBranch != "agent/claude/mul-6202" {
		t.Errorf("Git = %+v, want base=main work=agent/claude/mul-6202", decoded.Git)
	}
	if decoded.Workflow == nil || decoded.Workflow.NextStatusKey != "done" || decoded.Workflow.BackStatusKey != "in_progress" || !decoded.Workflow.IsAssignee {
		t.Errorf("Workflow = %+v, want structured workflow moves", decoded.Workflow)
	}
	if decoded.Trigger == nil || decoded.Trigger.Content != "run deterministic check" {
		t.Errorf("Trigger = %+v, want trigger content", decoded.Trigger)
	}
	if decoded.Autopilot == nil || decoded.Autopilot.RunID != "ap-run-1" {
		t.Errorf("Autopilot = %+v, want ap-run-1", decoded.Autopilot)
	}
}

func TestExpandCommandArgs(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"MULTICA_BASE_BRANCH":      "main",
		"MULTICA_WORK_BRANCH":      "agent/claude/mul-123",
		"MULTICA_ISSUE_IDENTIFIER": "MUL-123",
	}
	args := []string{"review", "--from", "${MULTICA_BASE_BRANCH}", "--to", "$MULTICA_WORK_BRANCH", "--label=${MULTICA_ISSUE_IDENTIFIER}"}
	got := expandCommandArgs(args, env)
	want := []string{"review", "--from", "main", "--to", "agent/claude/mul-123", "--label=MUL-123"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolveCommandGitContext_DetachedWorktree(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	repoDir := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repoDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, string(out))
		}
	}

	runGit("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit("add", "README.md")
	runGit("commit", "-m", "initial")
	runGit("checkout", "-b", "agent/claude/mul-42")
	if err := os.WriteFile(filepath.Join(repoDir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write feature: %v", err)
	}
	runGit("add", "feature.txt")
	runGit("commit", "-m", "add feature")
	runGit("checkout", "main")

	task := Task{
		ID:              "task-review",
		IssueID:         "issue-42",
		IssueBranchName: "agent/claude/mul-42",
		IssueWorkDir:    repoDir,
	}
	envRoot := t.TempDir()
	env := &execenv.Environment{
		RootDir: envRoot,
		WorkDir: filepath.Join(envRoot, "workdir"),
	}

	execCwd, gitInfo, cleanup, err := resolveCommandGitContext(context.Background(), nil, task, env, nil, "", nil)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("resolveCommandGitContext: %v", err)
	}
	if gitInfo == nil {
		t.Fatal("gitInfo = nil, want non-nil")
	}
	if gitInfo.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q, want main", gitInfo.BaseBranch)
	}
	if gitInfo.WorkBranch != "agent/claude/mul-42" {
		t.Errorf("WorkBranch = %q, want agent/claude/mul-42", gitInfo.WorkBranch)
	}
	if gitInfo.BaseCommit == "" {
		t.Error("BaseCommit is empty, want merge-base SHA")
	}
	// The detached worktree must have `feature.txt` from `agent/claude/mul-42`
	// checked out on disk while the primary repoDir remains on `main`.
	if _, err := os.Stat(filepath.Join(execCwd, "feature.txt")); err != nil {
		t.Errorf("expected feature.txt from work branch in %s: %v", execCwd, err)
	}
}

func TestSelectCommandTaskRepo(t *testing.T) {
	t.Parallel()

	repos := []RepoData{
		{URL: "git@github.com:acme/alpha.git"},
		{URL: "git@github.com:acme/beta.git"},
	}

	t.Run("matches HTTPS IssueRepoURL against SSH workspace repo URL via repoIdentity", func(t *testing.T) {
		t.Parallel()
		got, err := selectCommandTaskRepo(Task{
			IssueRepoURL: "https://github.com/acme/beta",
			Repos:        repos,
		}, nil)
		if err != nil || got.URL != "git@github.com:acme/beta.git" {
			t.Fatalf("selectCommandTaskRepo = (%+v, %v), want acme/beta", got, err)
		}
	})

	t.Run("refuses to guess when IssueRepoURL misses", func(t *testing.T) {
		t.Parallel()
		got, err := selectCommandTaskRepo(Task{
			IssueRepoURL: "https://github.com/acme/other-repo",
			Repos:        repos,
		}, nil)
		if err == nil {
			t.Fatalf("selectCommandTaskRepo = (%+v, nil), want error on miss", got)
		}
	})

	t.Run("disambiguates multiple repos by matching IssueBranchName in repo cache", func(t *testing.T) {
		t.Parallel()
		got, err := selectCommandTaskRepo(Task{
			IssueBranchName: "agent/dev/1c5ca77845f5",
			Repos:           repos,
		}, func(repoURL, ref string) bool {
			return repoURL == "git@github.com:acme/beta.git" && ref == "agent/dev/1c5ca77845f5"
		})
		if err != nil || got.URL != "git@github.com:acme/beta.git" {
			t.Fatalf("selectCommandTaskRepo = (%+v, %v), want acme/beta matched by branch", got, err)
		}
	})

	t.Run("refuses to guess when IssueRepoURL is empty and multiple repos remain ambiguous", func(t *testing.T) {
		t.Parallel()
		got, err := selectCommandTaskRepo(Task{
			Repos: repos,
		}, nil)
		if err == nil {
			t.Fatalf("selectCommandTaskRepo = (%+v, nil), want error when ambiguous", got)
		}
	})

	t.Run("selects single candidate when IssueRepoURL is empty", func(t *testing.T) {
		t.Parallel()
		got, err := selectCommandTaskRepo(Task{
			Repos: repos[:1],
		}, nil)
		if err != nil || got.URL != "git@github.com:acme/alpha.git" {
			t.Fatalf("selectCommandTaskRepo = (%+v, %v), want single repo", got, err)
		}
	})
}

func TestProbeBuiltinRuntime_CommandNeedsNoExternalBinary(t *testing.T) {
	t.Parallel()

	d := &Daemon{
		cfg:           Config{CLIVersion: "0.4.0"},
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		agentVersions: map[string]string{},
	}
	res := d.probeBuiltinRuntime(context.Background(), "command", AgentEntry{Command: "command"})
	if res.verdict != builtinProbeOK {
		t.Fatalf("verdict = %v (reason %q), want builtinProbeOK", res.verdict, res.reason)
	}
	if res.version != "0.4.0" {
		t.Errorf("version = %q, want 0.4.0", res.version)
	}
}

func TestLoadConfig_DisableCommandPrecedence(t *testing.T) {
	stageFakeAgent(t)
	homeDir := t.TempDir()
	redirectTestHome(t, homeDir)
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))
	base := Overrides{ServerURL: "http://localhost:0", WorkspacesRoot: t.TempDir()}

	// Default: command runtime is enabled.
	t.Setenv(agent.CommandDisabledEnv, "")
	cfg, err := LoadConfig(base)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if cfg.DisableCommand {
		t.Fatal("DisableCommand = true, want false by default")
	}
	if _, ok := cfg.Agents["command"]; !ok {
		t.Fatal("command runtime missing from default Agents")
	}

	// Persisted config.json disable_command=true disables it when env is unset.
	if err := cli.SaveCLIConfig(cli.CLIConfig{DisableCommand: true}); err != nil {
		t.Fatalf("SaveCLIConfig: %v", err)
	}
	cfg, err = LoadConfig(base)
	if err != nil {
		t.Fatalf("LoadConfig config.json: %v", err)
	}
	if !cfg.DisableCommand {
		t.Fatal("DisableCommand = false, want true from config.json")
	}
	if _, ok := cfg.Agents["command"]; ok {
		t.Fatal("command runtime present despite disable_command=true in config.json")
	}

	// Explicit MULTICA_COMMAND_DISABLED=false overrides config.json.
	t.Setenv(agent.CommandDisabledEnv, "false")
	cfg, err = LoadConfig(base)
	if err != nil {
		t.Fatalf("LoadConfig env false: %v", err)
	}
	if cfg.DisableCommand {
		t.Fatal("DisableCommand = true, want false when MULTICA_COMMAND_DISABLED=false")
	}
	if _, ok := cfg.Agents["command"]; !ok {
		t.Fatal("command runtime missing when MULTICA_COMMAND_DISABLED=false overrides config.json")
	}

	// --no-command CLI override beats MULTICA_COMMAND_DISABLED=false.
	flagOverrides := base
	flagOverrides.DisableCommand = true
	cfg, err = LoadConfig(flagOverrides)
	if err != nil {
		t.Fatalf("LoadConfig --no-command: %v", err)
	}
	if !cfg.DisableCommand {
		t.Fatal("DisableCommand = false, want true when Overrides.DisableCommand=true")
	}
	if _, ok := cfg.Agents["command"]; ok {
		t.Fatal("command runtime present despite Overrides.DisableCommand=true")
	}

	// MULTICA_COMMAND_DISABLED=true overrides default/false config.json.
	if err := cli.SaveCLIConfig(cli.CLIConfig{DisableCommand: false}); err != nil {
		t.Fatalf("SaveCLIConfig false: %v", err)
	}
	t.Setenv(agent.CommandDisabledEnv, "true")
	cfg, err = LoadConfig(base)
	if err != nil {
		t.Fatalf("LoadConfig env true: %v", err)
	}
	if !cfg.DisableCommand {
		t.Fatal("DisableCommand = false, want true when MULTICA_COMMAND_DISABLED=true")
	}
	if _, ok := cfg.Agents["command"]; ok {
		t.Fatal("command runtime present despite MULTICA_COMMAND_DISABLED=true")
	}
}
