// Package execenv manages isolated per-task execution environments for the daemon.
// Each task gets its own directory with injected context files. Repositories are
// checked out on demand by the agent via `multica repo checkout`.
package execenv

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// RepoContextForEnv describes a workspace repo available for checkout.
type RepoContextForEnv struct {
	URL string // remote URL
}

// ProjectResourceForEnv describes a single resource attached to the issue's
// project. The resource_ref payload is type-specific JSON; the agent reads
// resources.json on disk for the full structure. This struct only carries
// fields the meta-skill template needs to render a human-readable summary
// (URL for github_repo, generic label otherwise).
type ProjectResourceForEnv struct {
	ID           string          // server-assigned UUID
	ResourceType string          // e.g. "github_repo"
	ResourceRef  json.RawMessage // raw JSONB payload from the API
	Label        string          // optional user-supplied label
}

// PrepareParams holds all inputs needed to set up an execution environment.
type PrepareParams struct {
	WorkspacesRoot string            // base path for all envs (e.g., ~/multica_workspaces)
	WorkspaceID    string            // workspace UUID — tasks are grouped under this
	TaskID         string            // task UUID — used for directory name
	AgentName      string            // for git branch naming only
	Provider       string            // agent provider (determines runtime config and skill injection paths)
	CodexVersion   string            // detected Codex CLI version (only used when Provider == "codex")
	Task           TaskContextForEnv // context data for writing files
}

// TaskContextForEnv is the subset of task context used for writing context files.
type TaskContextForEnv struct {
	IssueID                 string
	TriggerCommentID        string // comment that triggered this task (empty for on_assign)
	AgentID                 string // unique ID of the dispatched agent
	AgentName               string
	AgentInstructions       string // agent identity/persona instructions, injected into CLAUDE.md
	AgentSkills             []SkillContextForEnv
	Repos                   []RepoContextForEnv     // workspace repos available for checkout
	ProjectID               string                  // issue's project, when present
	ProjectTitle            string                  // human-readable project title
	ProjectResources        []ProjectResourceForEnv // resources attached to the project
	ChatSessionID           string                  // non-empty for chat tasks
	AutopilotRunID          string              // non-empty for autopilot run_only tasks
	AutopilotID             string
	AutopilotTitle          string
	AutopilotDescription    string
	AutopilotSource         string
	AutopilotTriggerPayload string
	QuickCreatePrompt       string // non-empty for quick-create tasks
}

// SkillContextForEnv represents a skill to be written into the execution environment.
type SkillContextForEnv struct {
	Name    string
	Content string
	Files   []SkillFileContextForEnv
}

// SkillFileContextForEnv represents a supporting file within a skill.
type SkillFileContextForEnv struct {
	Path    string
	Content string
}

// Environment represents a prepared, isolated execution environment.
type Environment struct {
	// RootDir is the top-level env directory ({workspacesRoot}/{task_id_short}/).
	RootDir string
	// WorkDir is the directory to pass as Cwd to the agent ({RootDir}/workdir/).
	WorkDir string
	// CodexHome is the path to the per-task CODEX_HOME directory (set only for codex provider).
	CodexHome string

	logger *slog.Logger // for cleanup logging
}

// PredictRootDir returns the env root path that Prepare would create for the
// given task, without performing any I/O. Callers use this to claim ownership
// of the directory (e.g. against the GC loop) before Prepare/Reuse runs.
func PredictRootDir(workspacesRoot, workspaceID, taskID string) string {
	if workspacesRoot == "" || workspaceID == "" || taskID == "" {
		return ""
	}
	return filepath.Join(workspacesRoot, workspaceID, shortID(taskID))
}

// Prepare creates an isolated execution environment for a task.
// The workdir starts empty (no repo checkouts). The agent checks out repos
// on demand via `multica repo checkout <url>`.
func Prepare(params PrepareParams, logger *slog.Logger) (*Environment, error) {
	if params.WorkspacesRoot == "" {
		return nil, fmt.Errorf("execenv: workspaces root is required")
	}
	if params.WorkspaceID == "" {
		return nil, fmt.Errorf("execenv: workspace ID is required")
	}
	if params.TaskID == "" {
		return nil, fmt.Errorf("execenv: task ID is required")
	}

	envRoot := filepath.Join(params.WorkspacesRoot, params.WorkspaceID, shortID(params.TaskID))

	// Remove existing env if present (defensive — task IDs are unique).
	if _, err := os.Stat(envRoot); err == nil {
		if err := os.RemoveAll(envRoot); err != nil {
			return nil, fmt.Errorf("execenv: remove existing env: %w", err)
		}
	}

	// Create directory tree.
	workDir := filepath.Join(envRoot, "workdir")
	for _, dir := range []string{workDir, filepath.Join(envRoot, "output"), filepath.Join(envRoot, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("execenv: create directory %s: %w", dir, err)
		}
	}

	env := &Environment{
		RootDir: envRoot,
		WorkDir: workDir,
		logger:  logger,
	}

	// Write context files into workdir (skills go to provider-native paths).
	if err := writeContextFiles(workDir, params.Provider, params.Task); err != nil {
		return nil, fmt.Errorf("execenv: write context files: %w", err)
	}

	// For Codex, set up a per-task CODEX_HOME seeded from ~/.codex/ with skills.
	if params.Provider == "codex" {
		codexHome := filepath.Join(envRoot, "codex-home")
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: params.CodexVersion}, logger); err != nil {
			return nil, fmt.Errorf("execenv: prepare codex-home: %w", err)
		}
		if err := writeCodexWorkspaceSkills(codexHome, params.Task.AgentSkills); err != nil {
			return nil, fmt.Errorf("execenv: write codex skills: %w", err)
		}
		env.CodexHome = codexHome
	}

	logger.Info("execenv: prepared env", "root", envRoot, "repos_available", len(params.Task.Repos))
	return env, nil
}

// Reuse wraps an existing workdir into an Environment and refreshes context files.
// Returns nil if the workdir does not exist (caller should fall back to Prepare).
//
// codexVersion is the detected Codex CLI version, used (only when provider is
// "codex") to pick the right sandbox policy for the per-task config.toml.
// Pass an empty string when the version is unknown.
func Reuse(workDir, provider, codexVersion string, task TaskContextForEnv, logger *slog.Logger) *Environment {
	if _, err := os.Stat(workDir); err != nil {
		return nil
	}

	env := &Environment{
		RootDir: filepath.Dir(workDir),
		WorkDir: workDir,
		logger:  logger,
	}

	// Refresh context files (issue_context.md, skills).
	if err := writeContextFiles(workDir, provider, task); err != nil {
		logger.Warn("execenv: refresh context files failed", "error", err)
	}

	// Restore CodexHome for Codex provider — the per-task codex-home directory
	// lives alongside the workdir. Re-run prepareCodexHomeWithOpts to ensure
	// config (especially sandbox/network access) is up to date.
	if provider == "codex" {
		codexHome := filepath.Join(env.RootDir, "codex-home")
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: codexVersion}, logger); err != nil {
			logger.Warn("execenv: refresh codex-home failed", "error", err)
		} else {
			env.CodexHome = codexHome
			if err := writeCodexWorkspaceSkills(codexHome, task.AgentSkills); err != nil {
				logger.Warn("execenv: refresh codex skills failed", "error", err)
			}
		}
	}

	logger.Info("execenv: reusing env", "workdir", workDir)
	return env
}

func writeCodexWorkspaceSkills(codexHome string, skills []SkillContextForEnv) error {
	if len(skills) == 0 {
		return nil
	}
	return writeSkillFiles(filepath.Join(codexHome, "skills"), skills)
}

// GCMeta is persisted to .gc_meta.json inside the env root so the GC loop
// can determine which issue this directory belongs to.
type GCMeta struct {
	IssueID     string    `json:"issue_id"`
	WorkspaceID string    `json:"workspace_id"`
	CompletedAt time.Time `json:"completed_at"`
}

const gcMetaFile = ".gc_meta.json"

// WriteGCMeta writes GC metadata into the given directory.
func WriteGCMeta(envRoot, issueID, workspaceID string) error {
	if envRoot == "" {
		return nil
	}
	meta := GCMeta{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		CompletedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal gc meta: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, gcMetaFile), data, 0o644)
}

// ReadGCMeta reads GC metadata from a task directory root.
func ReadGCMeta(envRoot string) (*GCMeta, error) {
	data, err := os.ReadFile(filepath.Join(envRoot, gcMetaFile))
	if err != nil {
		return nil, err
	}
	var meta GCMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// Cleanup tears down the execution environment.
// If removeAll is true, the entire env root is deleted. Otherwise, workdir is
// removed but output/ and logs/ are preserved for debugging.
func (env *Environment) Cleanup(removeAll bool) error {
	if env == nil {
		return nil
	}

	if removeAll {
		if err := os.RemoveAll(env.RootDir); err != nil {
			env.logger.Warn("execenv: cleanup removeAll failed", "error", err)
			return err
		}
		return nil
	}

	// Partial cleanup: remove workdir, keep output/ and logs/.
	if err := os.RemoveAll(env.WorkDir); err != nil {
		env.logger.Warn("execenv: cleanup workdir failed", "error", err)
		return err
	}
	return nil
}
