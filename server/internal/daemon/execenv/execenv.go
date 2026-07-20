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

	"github.com/multica-ai/multica/server/internal/runtimeapps"
)

// RepoContextForEnv describes a workspace repo available for checkout.
type RepoContextForEnv struct {
	URL         string // remote URL
	Description string // optional repo description
	Ref         string // optional default checkout ref for this task
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
	WorkspacesRoot string // base path for all envs (e.g., ~/multica_workspaces)
	WorkspaceID    string // workspace UUID — tasks are grouped under this
	TaskID         string // task UUID — used for directory name
	AgentName      string // for git branch naming only
	// Profile is the daemon's profile name (empty = default). It namespaces the
	// per-issue Codex session store so a second profile-daemon sharing the same
	// ~/.codex cannot see or GC this daemon's stores (MUL-4424).
	Profile      string
	Provider     string // agent provider (determines runtime config and skill injection paths)
	CodexVersion string // detected Codex CLI version (only used when Provider == "codex")
	OpenclawBin  string // resolved openclaw CLI path (only used when Provider == "openclaw"); empty = look up on PATH
	// McpConfig is the agent's saved `mcp_config` JSON, forwarded to the
	// provider-specific config preparer when that provider materialises MCP
	// via a per-task config file. Cursor and OpenClaw consume it here; other
	// providers wire MCP via ExecOptions.McpConfig in the agent backend.
	McpConfig json.RawMessage
	// CursorMcpAuthSource is an explicit opt-in path to a Cursor mcp-auth.json
	// file, or the Cursor project data directory containing it. Only Cursor's
	// managed MCP path consumes it.
	CursorMcpAuthSource string
	// OpenclawGateway pins the OpenClaw Gateway endpoint inside the per-task
	// wrapper. Only consulted when Provider == "openclaw" and the agent's
	// runtime_config selected gateway mode (issue #3260). Zero means "inherit
	// whatever the user's global openclaw.json already configures".
	OpenclawGateway OpenclawGatewayPin
	// LocalWorkDir, when non-empty, redirects the agent's working directory
	// to a user-supplied absolute path instead of the synthesised envRoot/
	// workdir. The path is NOT copied or mounted — the agent operates on
	// the user's directory in place. The daemon still creates envRoot for
	// output/, logs/, and .gc_meta.json; only the workdir slot is
	// substituted. Used by the local_directory project_resource flow
	// (MUL-2663). When set, the envRoot/workdir directory is not created.
	LocalWorkDir string
	// HermesSourceHome is the shared Hermes home the per-task overlay is seeded
	// from — resolved by the daemon via execenv.ResolveHermesProfile so it honors
	// the agent's custom_env HERMES_HOME and any -p/--profile or sticky selection.
	// Only used for the hermes provider; empty falls back to the platform default.
	HermesSourceHome string
	// HermesSourceMustExist fails the overlay build closed when HermesSourceHome
	// is absent — set when an explicit named profile was requested so a typo
	// doesn't silently seed from an empty home and drop the user's auth/config.
	HermesSourceMustExist bool
	// HermesEnv is the sanitized effective env (agent custom_env minus the daemon
	// blocklisted keys) used to expand ${VAR} in Hermes external_dirs so it
	// matches what the Hermes child process actually sees. Only used for hermes.
	HermesEnv map[string]string
	Task      TaskContextForEnv // context data for writing files
}

// TaskContextForEnv is the subset of task context used for writing context files.
type TaskContextForEnv struct {
	IssueID          string
	TriggerCommentID string // comment that triggered this task (empty for on_assign)
	TriggerThreadID  string // root comment ID for the triggering thread; falls back to TriggerCommentID when empty
	// CommentReplyTargets is set for a comment run that coalesced comments
	// spanning MORE THAN ONE root thread (MUL-4348). When it has >=2 entries the
	// workflow's reply step fans out — one reply per thread — instead of the
	// single --parent=trigger cookbook, keeping this persistent brief in sync
	// with the per-turn prompt so a cross-thread run cannot get one source
	// telling it "one comment" and the other "one per thread". Same-thread
	// follow-ups collapse to a single group upstream, so this stays empty and
	// the single-parent path is used (no duplicate replies).
	CommentReplyTargets []ThreadReplyTarget
	NewCommentCount     int    // issue-wide comments since this agent's last run (excludes its own and the injected trigger)
	NewCommentsSince    string // RFC3339 anchor (last run's started_at) the count is measured from; empty on cold start
	PriorSessionResumed bool   // true when the daemon will resume an existing provider session for this task
	// PriorSessionResumeUnavailable is true when this task carried a prior
	// session the daemon expected to resume but could NOT (the reused workdir was
	// gone, or the Codex rollout was not present in the task CODEX_HOME). The
	// brief surfaces this so the agent tells the user its previous conversation
	// context is gone and this run starts fresh — turning a silent context loss
	// into a user-visible one (MUL-4424). Distinct from an ordinary cold start,
	// which never had a prior session to lose.
	PriorSessionResumeUnavailable bool
	AgentID                       string // unique ID of the dispatched agent
	AgentName                     string
	AgentInstructions             string // agent identity/persona instructions, injected into CLAUDE.md
	AgentSkills                   []SkillContextForEnv
	Repos                         []RepoContextForEnv     // workspace repos available for checkout
	ProjectID                     string                  // issue's project, when present
	ProjectTitle                  string                  // human-readable project title
	ProjectDescription            string                  // durable project-level context, rendered into the brief's Project Context section
	ProjectResources              []ProjectResourceForEnv // resources attached to the project
	ChatSessionID                 string                  // non-empty for chat tasks
	// ChatChannelType is the IM platform behind a chat session ("slack",
	// "feishu"); empty for a web/mobile chat. The brief reads it for DELIVERY
	// policy only: any non-empty value means the reply leaves Multica for an
	// external channel, so `multica attachment upload` cannot deliver a file and
	// the Output section says text-only instead (MUL-4899). The orthogonal
	// history-command policy is Slack-only and lives in the per-turn chat prompt
	// (daemon/prompt.go) — the server has no Feishu history reader.
	ChatChannelType         string
	AutopilotRunID          string // non-empty for autopilot run_only tasks
	AutopilotID             string
	AutopilotTitle          string
	AutopilotDescription    string
	AutopilotSource         string
	AutopilotTriggerPayload string
	QuickCreatePrompt       string // non-empty for quick-create tasks
	HandoffNote             string // assignment handoff instruction; rendered into issue_context.md (MUL-3375)
	IsSquadLeader           bool   // true when the agent is acting as a squad leader (may exit silently on no_action)
	// WorkspaceContext is the workspace-level system prompt (workspace.context
	// in the DB). Rendered into the brief as `## Workspace Context` when
	// non-empty so every agent in the workspace sees the same shared context,
	// regardless of issue / chat / autopilot / quick-create.
	WorkspaceContext string
	// ConnectedApps lists per-run external app capabilities mounted through
	// MCP overlays. Rendered briefly so the agent can map app names such as
	// Notion to the actual MCP server name (`composio`).
	ConnectedApps []runtimeapps.ConnectedApp
	// RequestingUserName + RequestingUserProfileDescription describe the
	// human the agent is acting on behalf of. v1 sources them from the
	// runtime owner (the user who registered the daemon). Rendered into the
	// brief as the `## Requesting User` section only when description is
	// non-empty — empty means the user opted out of injecting profile
	// context and the agent stays anonymous-user mode.
	RequestingUserName               string
	RequestingUserProfileDescription string
	// Initiator* identify the actor who triggered THIS task (the real
	// requester) as distinct from the runtime owner. Rendered into the brief
	// as `## Task Initiator` when a name is present; InitiatorEmail is shown
	// only for member initiators. Empty for on-assign / autopilot /
	// quick-create tasks, which have no attributable human initiator. See
	// MUL-2645.
	InitiatorType  string
	InitiatorID    string
	InitiatorName  string
	InitiatorEmail string
}

// SkillContextForEnv represents a skill to be written into the execution environment.
type SkillContextForEnv struct {
	Name        string
	Description string
	Content     string
	Files       []SkillFileContextForEnv
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
	// WorkDir is the directory to pass as Cwd to the agent. Normally
	// ({RootDir}/workdir/); when the task is bound to a local_directory
	// project_resource, it is the user's path instead. See LocalDirectory.
	WorkDir string
	// LocalDirectory is true when WorkDir points at a user-supplied path
	// outside RootDir (the local_directory flow). Callers that key behavior
	// on "may I remove WorkDir as scratch?" must check this — for example
	// the GC loop never deletes the user's directory.
	LocalDirectory bool
	// CodexHome is the path to the per-task CODEX_HOME directory (set only for codex provider).
	CodexHome string
	// TaskHome is the per-task writable HOME directory (set only for the codex
	// provider on Linux, where the workspace-write Landlock sandbox makes the
	// real HOME read-only). When non-empty the daemon redirects
	// HOME/XDG/npm_config_cache here so tools that write to `~` (npm, Prisma, …)
	// land in a sandbox-writable location. Empty on macOS/Windows and for
	// non-sandboxed providers, where the real HOME stays in place. See
	// task_home.go and MUL-4856.
	TaskHome string
	// OpenclawConfigPath is the path to the per-task synthesized OpenClaw
	// config (set only for openclaw provider). The daemon exports this as
	// OPENCLAW_CONFIG_PATH on the openclaw subprocess so its native skill
	// scanner pins workspaceDir to WorkDir.
	OpenclawConfigPath string
	// OpenclawIncludeRoot is the directory of the user's active OpenClaw
	// config (set only for openclaw provider with an on-disk user config).
	// The daemon must prepend it to OPENCLAW_INCLUDE_ROOTS so OpenClaw is
	// allowed to follow the wrapper's `$include` link out of envRoot into
	// the user's config — by default OpenClaw confines `$include` to the
	// directory holding the wrapper file. Empty when no $include is
	// emitted (fresh install).
	OpenclawIncludeRoot string
	// CursorDataDir is the per-task Cursor data directory (set only for
	// cursor provider when the agent has managed mcp_config). The daemon
	// exports this as CURSOR_DATA_DIR so project-level MCP approvals are
	// isolated from the user's persistent ~/.cursor/projects state.
	CursorDataDir string
	// HermesHome is the path to the per-task HERMES_HOME overlay (set only for
	// the hermes provider, and only when the agent has skills bound — empty
	// otherwise, leaving the user's real home in place). It mirrors ~/.hermes/
	// via symlink, derives a config.yaml that references the user's real skills
	// as an external root, and holds the bound skills in its skills/ subdir. The
	// daemon exports it as HERMES_HOME so the hermes CLI discovers those skills
	// natively — Hermes has no workspace-relative discovery, so the previous
	// .agent_context/skills/ fallback was never read (issue #5242). See
	// hermes_home.go.
	HermesHome string

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

	// Self-heal the root-level daemon marker on every task start so a marker
	// removed while the daemon runs is restored before the agent spawns. The
	// per-workdir marker written below only covers cwds inside the workdir;
	// the root marker keeps the CLI fail-closed guard active for subprocesses
	// that lose all MULTICA_* env vars AND escape above the workdir. Non-fatal:
	// without it the workdir marker still protects the common case.
	if err := EnsureWorkspacesRootMarker(params.WorkspacesRoot); err != nil && logger != nil {
		logger.Warn("execenv: workspaces root marker not written; fail-closed guard limited to the task workdir", "error", err)
	}

	// Remove existing env if present (defensive — task IDs are unique).
	if _, err := os.Stat(envRoot); err == nil {
		if err := os.RemoveAll(envRoot); err != nil {
			return nil, fmt.Errorf("execenv: remove existing env: %w", err)
		}
	}

	// Create directory tree. For the standard flow the agent's workdir is
	// envRoot/workdir; for local_directory tasks the user's path takes its
	// place and we only need to create the scratch directories under
	// envRoot.
	workDir := filepath.Join(envRoot, "workdir")
	scratchDirs := []string{filepath.Join(envRoot, "output"), filepath.Join(envRoot, "logs")}
	if params.LocalWorkDir == "" {
		scratchDirs = append(scratchDirs, workDir)
	} else {
		workDir = params.LocalWorkDir
	}
	for _, dir := range scratchDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("execenv: create directory %s: %w", dir, err)
		}
	}

	env := &Environment{
		RootDir:        envRoot,
		WorkDir:        workDir,
		LocalDirectory: params.LocalWorkDir != "",
		logger:         logger,
	}

	// Write context files into workdir (skills go to provider-native paths).
	// Track every file/dir we create in a manifest so CleanupSidecars can
	// roll a local_directory workdir back to its pre-Prepare state. Cloud
	// tasks don't need the manifest (the GC loop wipes envRoot wholesale),
	// but we always write one — it's cheap, keeps Prepare/Reuse symmetric,
	// and avoids a conditional that would silently disable cleanup if the
	// local_directory detection logic ever drifts.
	manifest := &sidecarManifest{}
	if err := writeContextFiles(workDir, params.Provider, params.Task, manifest); err != nil {
		return nil, fmt.Errorf("execenv: write context files: %w", err)
	}

	// Persist managed-env provenance for non-local issue envs at Prepare time
	// (not on completion, where .gc_meta.json is written). A same-issue
	// follow-up can be claimed the instant the prior task completes — before
	// the prior handler writes .gc_meta.json — so reuse eligibility must be
	// provable from an artifact that exists the moment the env is created. Only
	// managed (non-local_directory) issue envs get this marker; that is exactly
	// the set squad-leader reuse targets (MUL-4886). Non-fatal: a write failure
	// only costs the next follow-up its session reuse (it falls back to a fresh
	// session), which must never block dispatching this task.
	if params.LocalWorkDir == "" && params.Task.IssueID != "" {
		if err := WriteManagedEnvProvenance(envRoot, ManagedEnvProvenance{
			WorkspaceID: params.WorkspaceID,
			IssueID:     params.Task.IssueID,
			AgentID:     params.Task.AgentID,
		}); err != nil && logger != nil {
			logger.Warn("execenv: write managed env provenance failed (non-fatal); a follow-up may start a fresh session", "error", err)
		}
	}

	// For Codex, set up a per-task CODEX_HOME seeded from ~/.codex/ with skills.
	if params.Provider == "codex" {
		codexHome := filepath.Join(envRoot, "codex-home")
		// Under the Linux workspace-write sandbox the real HOME is read-only;
		// give the task a writable HOME and grant write access to it in the
		// Codex config so npm/Prisma can write their caches (MUL-4856).
		taskHome, writableRoots, err := prepareCodexSandboxHome(envRoot, "", params.CodexVersion, logger)
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare task home: %w", err)
		}
		env.TaskHome = taskHome
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: params.CodexVersion, IsLocalDirectory: params.LocalWorkDir != "", SessionStoreKey: codexSessionStoreKey(params.Profile, params.Task.AgentID, params.Task.IssueID), WritableRoots: writableRoots}, logger); err != nil {
			return nil, fmt.Errorf("execenv: prepare codex-home: %w", err)
		}
		if err := hydrateCodexSkills(codexHome, params.Task.AgentSkills, logger); err != nil {
			return nil, fmt.Errorf("execenv: hydrate codex skills: %w", err)
		}
		env.CodexHome = codexHome
	}

	// For Hermes, redirect HERMES_HOME to a per-task compatibility overlay ONLY
	// when the agent has skills bound. A skill-less Hermes task keeps the user's
	// real home and its original behavior untouched. The overlay makes the bound
	// skills visible — Hermes discovers skills only from its home, so the old
	// .agent_context/skills/ fallback was never read (issue #5242). See
	// hermes_home.go.
	if params.Provider == "hermes" && len(params.Task.AgentSkills) > 0 {
		hermesHome := filepath.Join(envRoot, "hermes-home")
		if err := prepareHermesHome(hermesHome, params.HermesSourceHome, params.HermesSourceMustExist, params.Task.AgentSkills, params.HermesEnv, logger); err != nil {
			return nil, fmt.Errorf("execenv: prepare hermes-home: %w", err)
		}
		env.HermesHome = hermesHome
	}

	// For Cursor, materialize managed MCP into project-local config and use
	// an isolated CURSOR_DATA_DIR for the per-workdir approval sidecar. Cursor
	// still reads ~/.cursor/mcp.json, but only servers with approval entries in
	// this per-task data dir can load, so user-global MCP servers do not leak
	// into managed-MCP runs.
	if params.Provider == "cursor" {
		cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, params.McpConfig, params.CursorMcpAuthSource, manifest)
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare cursor mcp config: %w", err)
		}
		env.CursorDataDir = cursorDataDir
	}

	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		logger.Warn("execenv: write sidecar manifest failed (non-fatal)", "error", err)
	}

	// For OpenClaw, synthesize a per-task config that pins workspace to
	// workDir. The skill scanner then reads {workDir}/skills/ (written by
	// writeContextFiles above). Fail closed on errors: a malformed user
	// config that the openclaw CLI can't read is a real problem and
	// silently degrading to a minimal config would mask it by booting
	// OpenClaw without the agents / providers / API keys it expects.
	if params.Provider == "openclaw" {
		result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
			OpenclawBin: params.OpenclawBin,
			McpConfig:   params.McpConfig,
			Gateway:     params.OpenclawGateway,
		})
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare openclaw config: %w", err)
		}
		env.OpenclawConfigPath = result.ConfigPath
		env.OpenclawIncludeRoot = result.IncludeRoot
	}

	logger.Info("execenv: prepared env", "root", envRoot, "repos_available", len(params.Task.Repos))
	return env, nil
}

// ReuseParams describes the inputs to Reuse. It mirrors PrepareParams for
// the per-provider knobs (CodexVersion, OpenclawBin) so callers can pass
// the same resolved binary path on both first-run and reuse paths.
type ReuseParams struct {
	// WorkspacesRoot is the daemon-owned root under which all task envs live.
	// Passed on reuse so the root-level fail-closed marker is self-healed here
	// too — a marker removed while the daemon runs is restored before a reused
	// task spawns, not only on the fresh-Prepare path.
	WorkspacesRoot string
	WorkDir        string
	Provider       string
	CodexVersion   string // only used when Provider == "codex"
	// ResumeSessionID is the prior Codex thread/session ID this reused task
	// intends to resume, when any. Only consulted when Provider == "codex" and
	// only used while migrating a legacy per-task home whose sessions/ still
	// symlinks the shared ~/.codex/sessions — the single rollout for this ID is
	// exposed into the new task-local sessions dir so thread/resume still finds
	// it. Empty means a fresh thread. See prepareCodexSessionsDir (MUL-4424).
	ResumeSessionID string
	OpenclawBin     string // only used when Provider == "openclaw"; empty = PATH lookup
	// McpConfig is the agent's saved `mcp_config` JSON. Reused on reuse so a
	// freshly-saved managed set re-materialises into the wrapper before the
	// task starts — without this a stale wrapper from a prior run would keep
	// the old MCP set in play.
	McpConfig json.RawMessage
	// CursorMcpAuthSource mirrors PrepareParams.CursorMcpAuthSource on reuse.
	CursorMcpAuthSource string
	// OpenclawGateway is the per-task Gateway pin re-applied on reuse so the
	// agent picks up any runtime_config changes saved since the prior run.
	OpenclawGateway OpenclawGatewayPin
	// Profile is the daemon's profile name (empty = default), mirroring
	// PrepareParams.Profile so a reused task keys its per-issue Codex session
	// store into the same profile namespace (MUL-4424).
	Profile string
	// LocalDirectory is true when the reused WorkDir is a user-supplied
	// directory (the local_directory flow). The flag is propagated into
	// the returned Environment so downstream callers (notably the GC
	// loop) keep the "never delete the user's directory" invariant on
	// reuse paths.
	LocalDirectory bool
	// HermesSourceHome and HermesEnv mirror PrepareParams on reuse so the Hermes
	// overlay re-derives against the agent's current source home / profile and
	// external_dirs vars.
	HermesSourceHome      string
	HermesSourceMustExist bool
	HermesEnv             map[string]string
	Task                  TaskContextForEnv // refreshed context files / skills
}

// Reuse wraps an existing workdir into an Environment and refreshes context files.
// Returns nil if the workdir does not exist (caller should fall back to Prepare).
func Reuse(params ReuseParams, logger *slog.Logger) *Environment {
	if _, err := os.Stat(params.WorkDir); err != nil {
		return nil
	}

	// Self-heal the root-level daemon marker on the reuse path too, so a marker
	// removed while the daemon runs is restored before a reused task spawns —
	// otherwise reuse could run without the fail-closed guard until the next
	// fresh Prepare. Non-fatal: the per-workdir marker still protects the common
	// case, and an empty WorkspacesRoot (legacy callers) simply skips this.
	if params.WorkspacesRoot != "" {
		if err := EnsureWorkspacesRootMarker(params.WorkspacesRoot); err != nil && logger != nil {
			logger.Warn("execenv: workspaces root marker not written on reuse; fail-closed guard limited to the task workdir", "error", err)
		}
	}

	rootDir := filepath.Dir(params.WorkDir)
	if params.LocalDirectory {
		// For local_directory tasks the user's WorkDir is unrelated to
		// envRoot (envRoot still lives under workspacesRoot/{wsID}/...),
		// so reading it from filepath.Dir(WorkDir) would point at the
		// parent of the user's directory. Callers that need a real
		// RootDir on the reuse path should arrange to pass it in
		// explicitly; for v1 the daemon only ever reuses local_directory
		// workdirs after a fresh Prepare in the same task lifetime, so
		// the empty RootDir on reuse is fine for the current callers
		// (GC writes meta from Prepare's result, not Reuse's).
		rootDir = ""
	}
	env := &Environment{
		RootDir:        rootDir,
		WorkDir:        params.WorkDir,
		LocalDirectory: params.LocalDirectory,
		logger:         logger,
	}

	// Roll back the previous dispatch's sidecar writes before refreshing.
	// On reuse the workdir still holds the prior run's issue_context.md and
	// skill directories; without clearing them first, writeSkillFiles sees
	// its own earlier output occupying the canonical slug and falls back to
	// a collision-free sibling (issue-review, issue-review-multica,
	// issue-review-multica-2, …), accumulating a fresh duplicate on every
	// re-dispatch to the same issue. allocateCollisionFreeSkillDir exists to
	// dodge *user*-owned skill dirs (the local_directory flow), not our own
	// prior writes, so we undo them via the prior manifest first and let the
	// refresh below re-create each skill at its natural slug. This also brings
	// the standard providers in line with the Codex path, where
	// hydrateCodexSkills already wipes its skills dir before re-hydrating.
	//
	// Two steps, in order:
	//   1. removeReusedManagedSkillDirs reclaims the platform's own skill
	//      directories even when a prior-run agent left a file inside one.
	//      CleanupSidecars alone can't do this — it preserves any recorded dir
	//      the agent populated (correct on the local_directory teardown path),
	//      which would otherwise keep the canonical slug occupied and push the
	//      refresh back to issue-review-multica.
	//   2. CleanupSidecars rolls back the remaining sidecar files
	//      (issue_context.md, project resources) and the manifest itself.
	//
	// No-op when RootDir is empty (legacy local_directory reuse, which the
	// daemon skips anyway) or when no prior manifest exists (older build).
	if env.RootDir != "" {
		if err := removeReusedManagedSkillDirs(env.RootDir, skillsDirPath(params.WorkDir, params.Provider)); err != nil {
			logger.Warn("execenv: reclaim managed skill dirs on reuse failed", "error", err)
		}
		if err := CleanupSidecars(env.RootDir); err != nil {
			logger.Warn("execenv: roll back prior sidecars on reuse failed", "error", err)
		}
	}

	// Refresh context files (issue_context.md, skills). Reuse tracks a
	// fresh manifest under env.RootDir so a later CleanupSidecars sees
	// the up-to-date list of writes (an old manifest from a prior run
	// would otherwise reference files this Reuse no longer creates). For
	// local_directory tasks the daemon skips Reuse entirely (see
	// daemon.runTask), but writing the manifest unconditionally keeps
	// Prepare/Reuse symmetric so a future caller can rely on the
	// manifest being current after either path. RootDir is empty on the
	// legacy local_directory Reuse fallback — skip the persist in that
	// case to avoid creating a stray manifest at the filesystem root.
	manifest := &sidecarManifest{}
	if err := writeContextFiles(params.WorkDir, params.Provider, params.Task, manifest); err != nil {
		logger.Warn("execenv: refresh context files failed", "error", err)
	}

	// Restore CodexHome for Codex provider — the per-task codex-home directory
	// lives alongside the workdir. Re-run prepareCodexHomeWithOpts to ensure
	// config (especially sandbox/network access) is up to date.
	if params.Provider == "codex" {
		codexHome := filepath.Join(env.RootDir, "codex-home")
		// Refresh the per-task writable HOME (re-seed credential symlinks in
		// case the user's real home changed) and recompute the sandbox
		// writable_roots on reuse, mirroring the fresh Prepare path (MUL-4856).
		taskHome, writableRoots, err := prepareCodexSandboxHome(env.RootDir, "", params.CodexVersion, logger)
		if err != nil {
			logger.Warn("execenv: refresh task home failed", "error", err)
		}
		env.TaskHome = taskHome
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: params.CodexVersion, ResumeSessionID: params.ResumeSessionID, IsLocalDirectory: params.LocalDirectory, SessionStoreKey: codexSessionStoreKey(params.Profile, params.Task.AgentID, params.Task.IssueID), WritableRoots: writableRoots}, logger); err != nil {
			logger.Warn("execenv: refresh codex-home failed", "error", err)
		} else {
			env.CodexHome = codexHome
			if err := hydrateCodexSkills(codexHome, params.Task.AgentSkills, logger); err != nil {
				logger.Warn("execenv: refresh codex skills failed", "error", err)
			}
		}
	}

	// Refresh (or tear down) the per-task HERMES_HOME on reuse. With skills
	// bound, rebuild the overlay so an added/removed/edited skill and the
	// mirrored home/config track the user's current ~/.hermes/ before the next
	// hermes process starts. With no skills bound, drop the redirect entirely so
	// the task reverts to the user's real home — matching a fresh Prepare for a
	// skill-less agent.
	if params.Provider == "hermes" && env.RootDir != "" {
		hermesHome := filepath.Join(env.RootDir, "hermes-home")
		if len(params.Task.AgentSkills) > 0 {
			if err := prepareHermesHome(hermesHome, params.HermesSourceHome, params.HermesSourceMustExist, params.Task.AgentSkills, params.HermesEnv, logger); err != nil {
				// Fail closed: a half-built overlay must not run. Returning nil
				// makes the daemon fall back to a fresh Prepare, whose error
				// then blocks dispatch rather than silently dropping the bound
				// skill.
				logger.Warn("execenv: refresh hermes-home failed; forcing fresh prepare", "error", err)
				return nil
			}
			env.HermesHome = hermesHome
		} else {
			env.HermesHome = ""
			if err := os.RemoveAll(hermesHome); err != nil {
				logger.Warn("execenv: remove stale hermes-home failed", "error", err)
			}
		}
	}

	// Refresh Cursor's managed MCP sidecars on reuse. A newly saved agent
	// mcp_config must replace the prior run's .cursor/mcp.json and isolated
	// approvals before the next cursor-agent process starts.
	if params.Provider == "cursor" && env.RootDir != "" {
		cursorDataDir, err := prepareCursorMcpConfig(env.RootDir, params.WorkDir, params.McpConfig, params.CursorMcpAuthSource, manifest)
		if err != nil {
			logger.Warn("execenv: refresh cursor mcp config failed", "error", err)
			return nil
		}
		env.CursorDataDir = cursorDataDir
	}

	if env.RootDir != "" {
		if err := writeSidecarManifest(env.RootDir, manifest); err != nil {
			logger.Warn("execenv: refresh sidecar manifest failed", "error", err)
		}
	}

	// Refresh the per-task OpenClaw config on reuse — the user may have
	// added/removed agents or rotated providers since the prior task ran,
	// and the workspace override always re-targets the current workDir.
	// Fail closed: a user config that can no longer be parsed should block
	// reuse rather than degrade to a minimal config that boots OpenClaw
	// without the registered agents.
	if params.Provider == "openclaw" {
		result, err := prepareOpenclawConfig(env.RootDir, params.WorkDir, OpenclawConfigPrep{
			OpenclawBin: params.OpenclawBin,
			McpConfig:   params.McpConfig,
			Gateway:     params.OpenclawGateway,
		})
		if err != nil {
			logger.Warn("execenv: refresh openclaw config failed", "error", err)
			return nil
		}
		env.OpenclawConfigPath = result.ConfigPath
		env.OpenclawIncludeRoot = result.IncludeRoot
	}

	logger.Info("execenv: reusing env", "workdir", params.WorkDir)
	return env
}

// hydrateCodexSkills populates the per-task CODEX_HOME/skills directory with
// both user-installed skills (from the shared ~/.codex/skills/) and
// workspace-assigned skills. Workspace skills win on name conflict — they are
// written last and seedUserCodexSkills already pre-filters their names.
//
// The skills directory is wiped first so two stale-state classes that the
// Reuse path would otherwise leak are gone:
//
//   - A name now claimed by a workspace skill that previously held only a
//     user-seeded copy — support files from the user version would otherwise
//     linger under the workspace skill's directory.
//   - A user skill removed from the shared ~/.codex/skills/ since the last
//     run — its old contents would otherwise remain visible to the codex
//     CLI.
//
// Codex is the only runtime that needs this two-stage hydration because the
// daemon sets CODEX_HOME to a per-task directory, isolating the CLI from the
// user's real ~/.codex/. Other runtimes leave HOME untouched and discover
// user-level skills natively (see context.go for the workdir-local paths
// they use for workspace skills).
func hydrateCodexSkills(codexHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	skillsDir := filepath.Join(codexHome, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear codex skills dir: %w", err)
	}
	if err := seedUserCodexSkills(codexHome, workspaceSkills, logger); err != nil {
		logger.Warn("execenv: seed user codex skills failed", "error", err)
	}
	if len(workspaceSkills) == 0 {
		return nil
	}
	// Codex skills live under env.RootDir/codex-home, which the GC loop
	// (cloud) or env teardown (local_directory) wipes wholesale — they
	// don't sit inside the user's workdir and don't need sidecar manifest
	// tracking.
	return writeSkillFiles(skillsDir, workspaceSkills, nil)
}

// GCMetaKind identifies which kind of parent record a task workdir belongs to.
// The GC loop dispatches its decision tree on this value so chat / autopilot /
// quick-create tasks are no longer forced through the issue-centric path.
type GCMetaKind string

const (
	GCKindIssue        GCMetaKind = "issue"
	GCKindChat         GCMetaKind = "chat"
	GCKindAutopilotRun GCMetaKind = "autopilot_run"
	GCKindQuickCreate  GCMetaKind = "quick_create"
)

// GCMeta is persisted to .gc_meta.json inside the env root so the GC loop
// can decide whether the directory is reclaimable. It is a discriminated
// union keyed on Kind: only the ID field matching Kind is meaningful.
//
// Older meta files (pre-v2) lack the Kind field; readers must default empty
// Kind to GCKindIssue for backward compatibility — only IssueID was written
// before, and only issue-centric tasks ever produced a meta file.
type GCMeta struct {
	Kind           GCMetaKind `json:"kind,omitempty"`
	IssueID        string     `json:"issue_id,omitempty"`
	ChatSessionID  string     `json:"chat_session_id,omitempty"`
	AutopilotRunID string     `json:"autopilot_run_id,omitempty"`
	TaskID         string     `json:"task_id,omitempty"`
	WorkspaceID    string     `json:"workspace_id"`
	CompletedAt    time.Time  `json:"completed_at"`
	// LocalDirectory marks tasks whose WorkDir pointed at a user-owned
	// path rather than the synthesised envRoot/workdir. The GC loop honours
	// this by never falling into the gcActionClean branch (which would
	// RemoveAll envRoot — safe by structure, but we still want to keep the
	// envRoot's output/ and logs/ around longer so users can inspect what
	// the agent did in their own tree). Pattern-based artifact cleanup is
	// still allowed.
	LocalDirectory bool `json:"local_directory,omitempty"`
}

const gcMetaFile = ".gc_meta.json"

// WriteGCMeta writes GC metadata into the given directory. The caller is
// responsible for choosing Kind and populating the matching ID field;
// CompletedAt is stamped here so callers don't have to think about clocks.
func WriteGCMeta(envRoot string, meta GCMeta, logger *slog.Logger) error {
	if envRoot == "" {
		return nil
	}
	if meta.Kind == "" {
		// Defensive: a task that doesn't fit any known kind would write a
		// meta file the GC loop can't dispatch on. Skip silently — the
		// directory falls back to the orphan-by-mtime path.
		logger.Debug("execenv: skipping .gc_meta.json write: kind is empty", "envRoot", envRoot)
		return nil
	}
	meta.CompletedAt = time.Now().UTC()
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal gc meta: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, gcMetaFile), data, 0o644)
}

// ReadGCMeta reads GC metadata from a task directory root. Pre-v2 meta files
// (no kind field) are normalized to GCKindIssue so the legacy issue path
// keeps working without a migration.
func ReadGCMeta(envRoot string) (*GCMeta, error) {
	data, err := os.ReadFile(filepath.Join(envRoot, gcMetaFile))
	if err != nil {
		return nil, err
	}
	var meta GCMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.Kind == "" {
		meta.Kind = GCKindIssue
	}
	return &meta, nil
}

const managedEnvProvenanceFile = ".managed_env.json"

// ManagedEnvProvenanceManagedBy discriminates a managed-env provenance file
// the daemon wrote from any lookalike JSON that happens to share the path.
const ManagedEnvProvenanceManagedBy = "multica-daemon-managed-env"

// ManagedEnvProvenance is persisted to .managed_env.json inside the env root at
// Prepare time (NOT on completion, unlike .gc_meta.json). It records that this
// env root is a daemon-managed, non-local_directory issue env owned by a
// specific workspace/issue/agent.
//
// Its whole reason to exist is timing. A squad-leader follow-up on the same
// issue can be claimed the instant the prior task completes — the server's
// task-complete handler reconciles the follow-up and wakes the runtime before
// the prior task's daemon handler writes .gc_meta.json. Keying reuse
// eligibility off .gc_meta.json therefore raced: the successor read a
// not-yet-written file and started a fresh session (MUL-4886). This marker is
// on disk from the moment the env is created, so the successor can prove reuse
// safety inside that window. It is written ONLY for non-local managed issue
// envs, so its presence is itself the "safe to reuse, not a user
// local_directory" assertion; see shouldReusePriorWorkdir.
type ManagedEnvProvenance struct {
	ManagedBy   string `json:"managed_by"`
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	AgentID     string `json:"agent_id"`
}

// WriteManagedEnvProvenance persists the reuse-eligibility marker at the env
// root. Callers must only invoke it for non-local_directory issue envs, since
// the file's presence is the non-local assertion. ManagedBy is stamped here so
// callers cannot forget the discriminator.
func WriteManagedEnvProvenance(envRoot string, p ManagedEnvProvenance) error {
	if envRoot == "" {
		return nil
	}
	p.ManagedBy = ManagedEnvProvenanceManagedBy
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal managed env provenance: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, managedEnvProvenanceFile), data, 0o644)
}

// ReadManagedEnvProvenance reads the Prepare-time reuse-eligibility marker from
// an env root. A missing or malformed file returns an error; callers fail
// closed (no reuse) on any error.
func ReadManagedEnvProvenance(envRoot string) (*ManagedEnvProvenance, error) {
	data, err := os.ReadFile(filepath.Join(envRoot, managedEnvProvenanceFile))
	if err != nil {
		return nil, err
	}
	var p ManagedEnvProvenance
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// Cleanup tears down the execution environment.
// If removeAll is true, the entire env root is deleted. Otherwise, workdir is
// removed but output/ and logs/ are preserved for debugging.
//
// For local_directory tasks (env.LocalDirectory==true) WorkDir is the
// user's own path — Cleanup MUST NEVER delete it, regardless of removeAll.
// In that mode we only ever delete the envRoot scratch directory.
func (env *Environment) Cleanup(removeAll bool) error {
	if env == nil {
		return nil
	}

	if env.LocalDirectory {
		// Never touch the user's directory. RootDir is the daemon's own
		// scratch; safe to remove when the caller asked for a full
		// teardown.
		if removeAll && env.RootDir != "" {
			if err := os.RemoveAll(env.RootDir); err != nil {
				env.logger.Warn("execenv: cleanup local_directory envRoot failed", "error", err)
				return err
			}
		}
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
