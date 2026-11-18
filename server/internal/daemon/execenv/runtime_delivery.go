package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Out-of-checkout delivery for local worktree tasks (MUL-9445).
//
// A worktree task's branch is its deliverable, and agents commit to it while
// they run (`git add -A`, `git commit -a`). Anything the daemon writes into
// the checkout can therefore land in those commits: the task marker, project
// resources, skill directories, and the brief appended to a tracked CLAUDE.md
// or AGENTS.md. For runtimes with a verified way to read instructions from
// elsewhere, nothing is written into the checkout at all:
//
//   - the task marker and resources.json go to the task directory that
//     contains the checkout, where the CLI's upward marker search still finds
//     them;
//   - Claude reads CLAUDE.md and .claude/skills from RuntimeDir via --add-dir;
//   - Codex reads the brief as global instructions from the task CODEX_HOME's
//     AGENTS.md; its skills already live in CODEX_HOME.
//
// The repository's own CLAUDE.md / AGENTS.md are left untouched, so the agent
// can stage and commit edits to them like any other file.

func (env *Environment) selectRuntimeDelivery(provider, version string, logger *slog.Logger) error {
	if env.LocalWorktree == nil || (provider != "claude" && provider != "codex") {
		return nil
	}
	if !agent.SupportsExternalRuntimeConfig(provider, version) {
		if logger != nil {
			logger.Warn("execenv: CLI version not verified for out-of-checkout runtime config; writing it into the worktree", "provider", provider, "version", version)
		}
		return nil
	}
	env.RuntimeDir = filepath.Join(env.RootDir, "runtime")
	if err := os.MkdirAll(env.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("execenv: create runtime directory: %w", err)
	}
	return nil
}

func (env *Environment) prepareRuntimeContext(provider string, ctx TaskContextForEnv, manifest *sidecarManifest) ([]string, error) {
	if env.RuntimeDir == "" {
		return writeContextFilesTracked(env.WorkDir, provider, ctx, manifest, env.SkillDirectories)
	}
	if err := writeTaskContextMarker(env.RootDir, ctx, manifest); err != nil {
		return nil, err
	}
	if err := writeProjectResources(env.RootDir, ctx, manifest); err != nil {
		return nil, fmt.Errorf("write project resources: %w", err)
	}
	if provider != "claude" {
		return resolveSkillSlugs(ctx.AgentSkills), nil // Codex skills are written to CODEX_HOME in Prepare.
	}
	if len(ctx.AgentSkills) == 0 {
		return resolveSkillSlugs(ctx.AgentSkills), nil
	}
	// Claude qualifies an --add-dir skill whose name is already taken
	// ("runtime:<name>"), which would no longer match the name in the brief.
	// Reserve every project skill directory Claude discovers for this cwd: the
	// worktree's own, from the cwd up to the worktree root, and the main
	// checkout's, which Claude Code 2.1.277+ loads when the worktree root has
	// no .claude/skills. The latter may hold gitignored skills the worktree
	// never received, and the resource may itself be a linked worktree, so it
	// is resolved from git rather than taken to be GitRoot.
	var roots []string
	for dir := env.WorkDir; ; dir = filepath.Dir(dir) {
		roots = append(roots, skillsDirPath(dir, provider))
		if dir == env.LocalWorktree.Path || dir == filepath.Dir(dir) {
			break
		}
	}
	mainCheckout, err := mainCheckoutRoot(env.LocalWorktree.GitRoot)
	if err != nil {
		return nil, err
	}
	if mainCheckout != "" {
		roots = append(roots, skillsDirPath(mainCheckout, provider))
	}
	return writeSkillFilesWithReservedNames(skillsDirPath(env.RuntimeDir, provider), ctx.AgentSkills, manifest, env.SkillDirectories, roots...)
}

// runtimeConfigTarget is where out-of-checkout delivery writes the brief.
func (env *Environment) runtimeConfigTarget(provider string) string {
	switch provider {
	case "claude":
		return filepath.Join(env.RuntimeDir, "CLAUDE.md")
	case "codex":
		return filepath.Join(env.CodexHome, "AGENTS.md")
	}
	return ""
}

// InjectRuntimeConfig writes the brief where this environment's runtime
// reads it: the checkout for the usual delivery, or outside it for a local
// worktree task with out-of-checkout delivery.
func (env *Environment) InjectRuntimeConfig(provider string, ctx TaskContextForEnv) (string, error) {
	if env.RuntimeDir == "" {
		return InjectRuntimeConfig(env.WorkDir, provider, ctx)
	}
	ctx.ProjectResourcesOutsideCheckout = true
	brief := buildMetaSkillContent(provider, ctx)
	path := env.runtimeConfigTarget(provider)
	if path == "" || (provider == "codex" && env.CodexHome == "") {
		return brief, fmt.Errorf("execenv: no out-of-checkout runtime config target for %s", provider)
	}
	return brief, writeRuntimeConfigFile(path, brief)
}

// CleanupRuntimeConfig undoes InjectRuntimeConfig. Out-of-checkout files are
// task-local and go with the env root, so there is nothing to strip — and the
// repository's own instruction files were never touched.
func (env *Environment) CleanupRuntimeConfig(provider string) error {
	if env.RuntimeDir == "" {
		return CleanupRuntimeConfig(env.WorkDir, provider)
	}
	return nil
}

// ProtectRuntimeConfig hides a tracked instruction file that received the
// brief from the agent's commits (#10787). Out-of-checkout delivery leaves
// that file alone, so it stays fully committable.
func (env *Environment) ProtectRuntimeConfig(provider string) error {
	if env.LocalWorktree == nil || env.RuntimeDir != "" {
		return nil
	}
	return env.LocalWorktree.ProtectRuntimeConfig(provider)
}

// mainCheckoutRoot returns the repository's main working tree — the first
// entry of `git worktree list` — or "" for a bare repository, which has none.
func mainCheckoutRoot(gitRoot string) (string, error) {
	out, err := runGitStdout(gitRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("execenv: list worktrees of %s: %w", gitRoot, err)
	}
	block, _, _ := strings.Cut(out, "\n\n")
	var root string
	for _, line := range strings.Split(block, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			root = path
		}
		if line == "bare" {
			return "", nil
		}
	}
	if root == "" {
		return "", fmt.Errorf("execenv: list worktrees of %s: no main worktree in %q", gitRoot, block)
	}
	return root, nil
}
