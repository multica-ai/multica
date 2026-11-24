package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
)

// CommandTaskInputVersion is the schema version of the JSON payload delivered
// to Command Agents over stdin.
const CommandTaskInputVersion = 1

// CommandTaskInput is the bounded, deterministic JSON payload passed over stdin
// when the daemon executes a Command Agent (`provider == "command"`).
type CommandTaskInput struct {
	SchemaVersion int                        `json:"schema_version"`
	TaskID        string                     `json:"task_id"`
	WorkspaceID   string                     `json:"workspace_id"`
	WorkspaceSlug string                     `json:"workspace_slug,omitempty"`
	AgentID       string                     `json:"agent_id"`
	AgentName     string                     `json:"agent_name,omitempty"`
	RuntimeID     string                     `json:"runtime_id"`
	Kind          string                     `json:"kind"`
	WorkDir       string                     `json:"work_dir"`
	Git           *CommandTaskGitInput       `json:"git,omitempty"`
	Workflow      *ProjectWorkflowData       `json:"workflow,omitempty"`
	Issue         *CommandTaskIssueInput     `json:"issue,omitempty"`
	Trigger       *CommandTaskTriggerInput   `json:"trigger,omitempty"`
	Autopilot     *CommandTaskAutopilotInput `json:"autopilot,omitempty"`
	Chat          *CommandTaskChatInput      `json:"chat,omitempty"`
	Project       *CommandTaskProjectInput   `json:"project,omitempty"`
	Repos         []RepoData                 `json:"repos,omitempty"`
	Initiator     *CommandTaskActorInput     `json:"initiator,omitempty"`
}

type CommandTaskGitInput struct {
	RepoDir    string `json:"repo_dir,omitempty"`
	WorkBranch string `json:"work_branch,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
	BaseCommit string `json:"base_commit,omitempty"`
}

type CommandTaskIssueInput struct {
	ID            string           `json:"id"`
	Identifier    string           `json:"identifier,omitempty"`
	Status        string           `json:"status,omitempty"`
	AssigneeType  string           `json:"assignee_type,omitempty"`
	AssigneeID    string           `json:"assignee_id,omitempty"`
	BranchName    string           `json:"branch_name,omitempty"`
	ChangedFields []string         `json:"changed_fields,omitempty"`
	Parent        *ParentIssueData `json:"parent,omitempty"`
}

type CommandTaskTriggerInput struct {
	CommentID         string                 `json:"comment_id,omitempty"`
	ThreadID          string                 `json:"thread_id,omitempty"`
	Content           string                 `json:"content,omitempty"`
	AuthorType        string                 `json:"author_type,omitempty"`
	AuthorName        string                 `json:"author_name,omitempty"`
	EditedAt          string                 `json:"edited_at,omitempty"`
	Revision          int64                  `json:"revision,omitempty"`
	CoalescedComments []CoalescedCommentData `json:"coalesced_comments,omitempty"`
	WakeupID          string                 `json:"wakeup_id,omitempty"`
	WakeupSystemRule  string                 `json:"wakeup_system_rule,omitempty"`
	HandoffNote       string                 `json:"handoff_note,omitempty"`
}

type CommandTaskAutopilotInput struct {
	ID             string          `json:"id,omitempty"`
	RunID          string          `json:"run_id,omitempty"`
	Title          string          `json:"title,omitempty"`
	Description    string          `json:"description,omitempty"`
	Source         string          `json:"source,omitempty"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
}

type CommandTaskChatInput struct {
	SessionID   string               `json:"session_id"`
	ChannelType string               `json:"channel_type,omitempty"`
	ChatType    string               `json:"chat_type,omitempty"`
	InThread    bool                 `json:"in_thread,omitempty"`
	Message     string               `json:"message,omitempty"`
	Attachments []ChatAttachmentMeta `json:"attachments,omitempty"`
}

type CommandTaskProjectInput struct {
	ID          string                `json:"id"`
	Title       string                `json:"title,omitempty"`
	Description string                `json:"description,omitempty"`
	Archived    bool                  `json:"archived,omitempty"`
	Resources   []ProjectResourceData `json:"resources,omitempty"`
}

type CommandTaskActorInput struct {
	Type  string `json:"type,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

func commandTaskKind(task Task) string {
	switch {
	case task.AutopilotRunID != "" || task.AutopilotID != "":
		return "autopilot"
	case task.ChatSessionID != "":
		return "chat"
	case task.QuickCreatePrompt != "":
		return "quick_create"
	case task.TriggerCommentID != "":
		return "comment"
	default:
		return "direct"
	}
}

func buildCommandTaskInput(task Task, workDir string, gitInfo ...*CommandTaskGitInput) CommandTaskInput {
	var agentName string
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	var resolvedGit *CommandTaskGitInput
	if len(gitInfo) > 0 {
		resolvedGit = gitInfo[0]
	}
	input := CommandTaskInput{
		SchemaVersion: CommandTaskInputVersion,
		TaskID:        task.ID,
		WorkspaceID:   task.WorkspaceID,
		WorkspaceSlug: task.WorkspaceSlug,
		AgentID:       task.AgentID,
		AgentName:     agentName,
		RuntimeID:     task.RuntimeID,
		Kind:          commandTaskKind(task),
		WorkDir:       workDir,
		Git:           resolvedGit,
		Workflow:      task.ProjectWorkflow,
		Repos:         task.Repos,
	}
	if task.IssueID != "" {
		branchName := task.IssueBranchName
		if branchName == "" && resolvedGit != nil {
			branchName = resolvedGit.WorkBranch
		}
		input.Issue = &CommandTaskIssueInput{
			ID:            task.IssueID,
			Identifier:    task.IssueIdentifier,
			Status:        task.IssueStatus,
			AssigneeType:  task.IssueAssigneeType,
			AssigneeID:    task.IssueAssigneeID,
			BranchName:    branchName,
			ChangedFields: task.IssueChangedFields,
			Parent:        task.ParentIssue,
		}
	}
	if task.TriggerCommentID != "" || task.TriggerCommentContent != "" || len(task.CoalescedComments) > 0 || task.WakeupID != "" || task.HandoffNote != "" {
		input.Trigger = &CommandTaskTriggerInput{
			CommentID:         task.TriggerCommentID,
			ThreadID:          task.TriggerThreadID,
			Content:           task.TriggerCommentContent,
			AuthorType:        task.TriggerAuthorType,
			AuthorName:        task.TriggerAuthorName,
			EditedAt:          task.TriggerCommentEditedAt,
			Revision:          task.TriggerCommentRevision,
			CoalescedComments: task.CoalescedComments,
			WakeupID:          task.WakeupID,
			WakeupSystemRule:  task.WakeupSystemRule,
			HandoffNote:       task.HandoffNote,
		}
	}
	if task.AutopilotID != "" || task.AutopilotRunID != "" {
		var payload json.RawMessage
		if trimmed := strings.TrimSpace(string(task.AutopilotTriggerPayload)); trimmed != "" && trimmed != "null" && json.Valid([]byte(trimmed)) {
			payload = json.RawMessage(trimmed)
		}
		input.Autopilot = &CommandTaskAutopilotInput{
			ID:             task.AutopilotID,
			RunID:          task.AutopilotRunID,
			Title:          task.AutopilotTitle,
			Description:    task.AutopilotDescription,
			Source:         task.AutopilotSource,
			TriggerPayload: payload,
		}
	}
	if task.ChatSessionID != "" {
		input.Chat = &CommandTaskChatInput{
			SessionID:   task.ChatSessionID,
			ChannelType: task.ChatChannelType,
			ChatType:    task.ChatType,
			InThread:    task.ChatInThread,
			Message:     task.ChatMessage,
			Attachments: task.ChatMessageAttachments,
		}
	}
	if task.ProjectID != "" {
		input.Project = &CommandTaskProjectInput{
			ID:          task.ProjectID,
			Title:       task.ProjectTitle,
			Description: task.ProjectDescription,
			Archived:    task.ProjectArchived,
			Resources:   task.ProjectResources,
		}
	}
	if task.InitiatorID != "" || task.InitiatorName != "" || task.InitiatorEmail != "" {
		input.Initiator = &CommandTaskActorInput{
			Type:  task.InitiatorType,
			ID:    task.InitiatorID,
			Name:  task.InitiatorName,
			Email: task.InitiatorEmail,
		}
	}
	return input
}

func buildCommandTaskInputJSON(task Task, workDir string, gitInfo ...*CommandTaskGitInput) (string, error) {
	raw, err := json.Marshal(buildCommandTaskInput(task, workDir, gitInfo...))
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}

// expandCommandString expands ${VAR} and $VAR placeholders against env,
// falling back to os.Getenv when a key is not present in env.
func expandCommandString(s string, env map[string]string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	return os.Expand(s, func(key string) string {
		if env != nil {
			if v, ok := env[key]; ok {
				return v
			}
		}
		return os.Getenv(key)
	})
}

// expandCommandArgs returns a copy of args with ${VAR} and $VAR placeholders
// expanded against env.
func expandCommandArgs(args []string, env map[string]string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = expandCommandString(arg, env)
	}
	return out
}

func runCommandGitTrimmed(dir string, args ...string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("empty git directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fullArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isCommandGitWorkTree(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	out, err := runCommandGitTrimmed(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

func commandGitRefExists(dir, ref string) bool {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(ref) == "" {
		return false
	}
	_, err := runCommandGitTrimmed(dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// findCommandGitRepoInDir returns dir if it is itself a git worktree, or the
// first immediate child directory of dir that is a git worktree (such as a
// `<workdir>/<repo-name>` checkout created by an earlier task's `multica repo checkout`).
func findCommandGitRepoInDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if isCommandGitWorkTree(dir) {
		return dir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		if isCommandGitWorkTree(candidate) {
			return candidate
		}
	}
	return ""
}

// selectCommandTaskRepo picks the repository from task.Repos that matches
// task.IssueRepoURL using daemon.repoIdentity.
//
//   - When task.IssueRepoURL is set, it matches by canonical repoIdentity and
//     returns an error on a miss rather than guessing task.Repos[0].
//   - When task.IssueRepoURL is empty and there is a single candidate repo, it
//     selects that repo.
//   - When task.IssueRepoURL is empty across multiple candidate repos and
//     task.IssueBranchName is set, it consults hasRefInRepo (when provided) and
//     selects the unique repository containing that branch.
func selectCommandTaskRepo(task Task, hasRefInRepo func(repoURL, ref string) bool) (RepoData, error) {
	if len(task.Repos) == 0 {
		return RepoData{}, nil
	}
	if strings.TrimSpace(task.IssueRepoURL) != "" {
		target := repoIdentity(task.IssueRepoURL)
		if target != "" {
			for _, r := range task.Repos {
				if strings.EqualFold(repoIdentity(r.URL), target) {
					return r, nil
				}
			}
		}
		return RepoData{}, fmt.Errorf("issue repository %q is not among the %d configured task repositories", task.IssueRepoURL, len(task.Repos))
	}
	if len(task.Repos) == 1 && strings.TrimSpace(task.Repos[0].URL) != "" {
		return task.Repos[0], nil
	}
	branch := strings.TrimSpace(task.IssueBranchName)
	if branch != "" && hasRefInRepo != nil {
		var matches []RepoData
		for _, r := range task.Repos {
			if strings.TrimSpace(r.URL) != "" && hasRefInRepo(r.URL, branch) {
				matches = append(matches, r)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return RepoData{}, fmt.Errorf("branch %q exists in %d configured repositories; attach a repository resource to the project or link a pull request to disambiguate", branch, len(matches))
		}
	}
	return RepoData{}, fmt.Errorf("cannot select an unambiguous repository among %d configured repositories (issue_repo_url=%q, issue_branch_name=%q)", len(task.Repos), task.IssueRepoURL, branch)
}

func (d *Daemon) checkoutCommandTaskRepo(ctx context.Context, task Task, workDir string, taskLog *slog.Logger) (string, error) {
	if d == nil || d.repoCache == nil || len(task.Repos) == 0 || strings.TrimSpace(workDir) == "" {
		return "", nil
	}
	hasRefInRepo := func(rawURL, ref string) bool {
		registeredURL := d.resolveWorkspaceRepoURL(task.WorkspaceID, strings.TrimSpace(rawURL))
		if registeredURL == "" {
			registeredURL = strings.TrimSpace(rawURL)
		}
		barePath := d.repoCache.Lookup(task.WorkspaceID, registeredURL)
		if barePath == "" {
			return false
		}
		return commandGitRefExists(barePath, "refs/heads/"+ref) ||
			commandGitRefExists(barePath, "refs/remotes/origin/"+ref) ||
			commandGitRefExists(barePath, ref)
	}
	selectedRepo, selErr := selectCommandTaskRepo(task, hasRefInRepo)
	if selErr != nil {
		if taskLog != nil {
			taskLog.Warn("command agent: could not select unambiguous repository for task",
				"issue_repo_url", task.IssueRepoURL,
				"issue_branch_name", task.IssueBranchName,
				"candidate_repos", len(task.Repos),
				"error", selErr,
			)
		}
		return "", selErr
	}
	repoURL := strings.TrimSpace(selectedRepo.URL)
	if repoURL == "" {
		return "", nil
	}
	registeredURL, err := d.ensureRepoReady(ctx, task.WorkspaceID, repoURL)
	if err != nil {
		if taskLog != nil {
			taskLog.Warn("command agent: automatic repo readiness failed", "url", repoURL, "error", err)
		}
		return "", fmt.Errorf("prepare repository %s: %w", repoURL, err)
	}
	checkoutRef := strings.TrimSpace(task.IssueBranchName)
	if checkoutRef == "" {
		checkoutRef = strings.TrimSpace(selectedRepo.Ref)
	}
	if checkoutRef == "" {
		checkoutRef = d.taskRepoDefaultRef(task.WorkspaceID, task.ID, registeredURL)
	}
	var agentName string
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	params := repocache.WorktreeParams{
		WorkspaceID:         task.WorkspaceID,
		RepoURL:             registeredURL,
		WorkDir:             workDir,
		Ref:                 checkoutRef,
		AgentName:           agentName,
		TaskID:              task.ID,
		CoAuthoredByEnabled: d.workspaceCoAuthoredByEnabled(task.WorkspaceID),
		LockWaitTimeout:     repoCheckoutLockWaitTimeout,
	}
	var result *repocache.WorktreeResult
	if cache, ok := d.repoCache.(interface {
		CreateWorktreeContext(context.Context, repocache.WorktreeParams) (*repocache.WorktreeResult, error)
	}); ok {
		result, err = cache.CreateWorktreeContext(ctx, params)
	} else {
		result, err = d.repoCache.CreateWorktree(params)
	}
	if err != nil || result == nil {
		if taskLog != nil {
			taskLog.Warn("command agent: automatic repo checkout failed", "url", registeredURL, "ref", checkoutRef, "error", err)
		}
		if err != nil {
			return "", fmt.Errorf("check out repository %s (ref %q): %w", registeredURL, checkoutRef, err)
		}
		return "", fmt.Errorf("check out repository %s (ref %q): empty worktree result", registeredURL, checkoutRef)
	}
	return result.Path, nil
}

// prepareDetachedCommandWorktree creates a disposable detached git worktree
// inside envRoot checked out at targetRef (or HEAD when targetRef is empty or
// not found), without creating any `agent/<command-agent>/...` branch in the
// source repository. The returned cleanup function removes the worktree on exit.
func prepareDetachedCommandWorktree(sourceRepoDir, envRoot, targetRef string) (worktreeCwd string, cleanup func(), ok bool) {
	if !isCommandGitWorkTree(sourceRepoDir) || strings.TrimSpace(envRoot) == "" {
		return "", nil, false
	}
	gitRoot, err := runCommandGitTrimmed(sourceRepoDir, "rev-parse", "--show-toplevel")
	if err != nil || gitRoot == "" {
		return "", nil, false
	}
	sourceCanonical := sourceRepoDir
	if resolved, evalErr := filepath.EvalSymlinks(sourceRepoDir); evalErr == nil {
		sourceCanonical = resolved
	}
	rel, relErr := filepath.Rel(gitRoot, sourceCanonical)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = "."
	}

	wtPath := filepath.Join(envRoot, "command-worktree")
	_ = os.RemoveAll(wtPath)
	_, _ = runCommandGitTrimmed(gitRoot, "worktree", "prune")

	checkoutTarget := "HEAD"
	if trimmedRef := strings.TrimSpace(targetRef); trimmedRef != "" {
		switch {
		case commandGitRefExists(gitRoot, "refs/heads/"+trimmedRef):
			checkoutTarget = "refs/heads/" + trimmedRef
		case commandGitRefExists(gitRoot, "refs/remotes/origin/"+trimmedRef):
			checkoutTarget = "refs/remotes/origin/" + trimmedRef
		case commandGitRefExists(gitRoot, trimmedRef):
			checkoutTarget = trimmedRef
		}
	}

	if _, err := runCommandGitTrimmed(gitRoot, "worktree", "add", "--detach", wtPath, checkoutTarget); err != nil {
		return "", nil, false
	}
	cleanup = func() {
		_, _ = runCommandGitTrimmed(gitRoot, "worktree", "remove", "--force", wtPath)
		_ = os.RemoveAll(wtPath)
		_, _ = runCommandGitTrimmed(gitRoot, "worktree", "prune")
	}
	return filepath.Join(wtPath, rel), cleanup, true
}

// resolveCommandGitContext locates or prepares the git working tree for a
// Command Agent task and resolves MULTICA_WORK_BRANCH, MULTICA_BASE_BRANCH,
// and MULTICA_BASE_COMMIT.
//
// When the project has a local_directory resource in worktree mode (or a prior
// issue checkout on disk), it creates a disposable detached worktree directly
// at the issue's active branch without creating a new `agent/<command-agent>/...`
// branch or auto-committing tool cache artifacts.
func resolveCommandGitContext(
	ctx context.Context,
	d *Daemon,
	task Task,
	env *execenv.Environment,
	localAssignment *localDirectoryAssignment,
	configuredWorkDir string,
	taskLog *slog.Logger,
) (execCwd string, gitInfo *CommandTaskGitInput, cleanup func(), err error) {
	var baseDir string
	var envRoot string
	if env != nil {
		baseDir = env.WorkDir
		envRoot = env.RootDir
	}

	var sourceRepoDir string
	if localAssignment != nil && isCommandGitWorkTree(localAssignment.AbsPath) {
		sourceRepoDir = localAssignment.AbsPath
		if !localAssignment.UsesWorktree() {
			baseDir = localAssignment.AbsPath
		} else if wtCwd, wtCleanup, ok := prepareDetachedCommandWorktree(localAssignment.AbsPath, envRoot, task.IssueBranchName); ok {
			baseDir = wtCwd
			cleanup = wtCleanup
		} else {
			baseDir = localAssignment.AbsPath
		}
	} else if !isCommandGitWorkTree(baseDir) {
		if found := findCommandGitRepoInDir(task.IssueWorkDir); found != "" {
			sourceRepoDir = found
			if wtCwd, wtCleanup, ok := prepareDetachedCommandWorktree(found, envRoot, task.IssueBranchName); ok {
				baseDir = wtCwd
				cleanup = wtCleanup
			} else {
				baseDir = found
			}
		} else if found := findCommandGitRepoInDir(task.PriorWorkDir); found != "" {
			sourceRepoDir = found
			baseDir = found
		} else if checkedOut, checkoutErr := d.checkoutCommandTaskRepo(ctx, task, baseDir, taskLog); checkoutErr != nil {
			return baseDir, nil, nil, checkoutErr
		} else if checkedOut != "" {
			sourceRepoDir = checkedOut
			baseDir = checkedOut
		}
	}

	execCwd = baseDir
	if trimmedCfgDir := strings.TrimSpace(configuredWorkDir); trimmedCfgDir != "" {
		if filepath.IsAbs(trimmedCfgDir) {
			execCwd = trimmedCfgDir
		} else if baseDir != "" {
			execCwd = filepath.Join(baseDir, trimmedCfgDir)
		} else {
			execCwd = trimmedCfgDir
		}
	}

	if !isCommandGitWorkTree(execCwd) {
		return execCwd, nil, cleanup, nil
	}

	repoRoot, err := runCommandGitTrimmed(execCwd, "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		repoRoot = execCwd
	}

	workBranch := strings.TrimSpace(task.IssueBranchName)
	if workBranch != "" && !commandGitRefExists(execCwd, "refs/heads/"+workBranch) && commandGitRefExists(execCwd, "refs/remotes/origin/"+workBranch) {
		_, _ = runCommandGitTrimmed(execCwd, "branch", "-f", workBranch, "refs/remotes/origin/"+workBranch)
	}

	var baseBranch string
	if originHead, err := runCommandGitTrimmed(execCwd, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && originHead != "" {
		baseBranch = strings.TrimPrefix(originHead, "origin/")
	}
	if baseBranch == "" && sourceRepoDir != "" {
		if rootBranch, err := runCommandGitTrimmed(sourceRepoDir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && rootBranch != "" && rootBranch != workBranch {
			baseBranch = rootBranch
		}
	}
	if baseBranch == "" {
		for _, candidate := range []string{"main", "master", "trunk", "develop"} {
			if commandGitRefExists(execCwd, "refs/heads/"+candidate) || commandGitRefExists(execCwd, "refs/remotes/origin/"+candidate) {
				baseBranch = candidate
				break
			}
		}
	}
	if baseBranch == "" {
		baseBranch = "main"
	}

	if workBranch == "" {
		if headRef, err := runCommandGitTrimmed(execCwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && headRef != "" && headRef != "HEAD" {
			workBranch = headRef
		} else if sourceRepoDir != "" {
			if srcHeadRef, err := runCommandGitTrimmed(sourceRepoDir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && srcHeadRef != "" && srcHeadRef != "HEAD" {
				workBranch = srcHeadRef
			}
		}
		if workBranch == "" {
			if headSHA, err := runCommandGitTrimmed(execCwd, "rev-parse", "--verify", "HEAD"); err == nil {
				workBranch = headSHA
			}
		}
	}

	var baseCommit string
	if baseBranch != "" && workBranch != "" {
		if mb, err := runCommandGitTrimmed(execCwd, "merge-base", "origin/"+baseBranch, workBranch); err == nil && mb != "" {
			baseCommit = mb
		} else if mb, err := runCommandGitTrimmed(execCwd, "merge-base", baseBranch, workBranch); err == nil && mb != "" {
			baseCommit = mb
		}
	}
	if baseCommit == "" && baseBranch != "" {
		if sha, err := runCommandGitTrimmed(execCwd, "rev-parse", "--verify", baseBranch); err == nil {
			baseCommit = sha
		}
	}

	return execCwd, &CommandTaskGitInput{
		RepoDir:    repoRoot,
		WorkBranch: workBranch,
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
	}, cleanup, nil
}
