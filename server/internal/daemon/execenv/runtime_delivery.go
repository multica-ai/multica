package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// InjectRuntimeConfig keeps provider transport decisions with the environment
// that prepared its sidecars, including across the preparation-helper boundary.
func (env *Environment) InjectRuntimeConfig(provider string, ctx TaskContextForEnv) (string, error) {
	if env.RuntimeDir == "" {
		return InjectRuntimeConfig(env.WorkDir, provider, ctx)
	}
	ctx.ProjectResourcesOutsideCheckout = true
	brief := buildMetaSkillContent(provider, ctx)
	if provider == "claude" {
		return brief, writeRuntimeConfigFile(filepath.Join(env.RuntimeDir, "CLAUDE.md"), brief)
	}
	return brief, nil // Codex receives developerInstructions at thread setup.
}

// CleanupRuntimeConfig must use the same target as injection. In particular,
// native Codex runs must never strip markers from user-owned AGENTS.md files.
func (env *Environment) CleanupRuntimeConfig(provider string) error {
	if env.RuntimeDir == "" {
		return CleanupRuntimeConfig(env.WorkDir, provider)
	}
	return nil // Task-local runtime files are reclaimed with envRoot.
}

// File delivery still needs the legacy index protection. External delivery
// leaves repository instruction files fully available to agent-made commits.
func (env *Environment) ProtectRuntimeConfig(provider string) error {
	if env.RuntimeDir != "" {
		return nil
	}
	return env.LocalWorktree.ProtectRuntimeConfig(provider)
}

func (env *Environment) prepareRuntimeContext(provider string, ctx TaskContextForEnv, manifest *sidecarManifest) ([]string, error) {
	if env.RuntimeDir == "" {
		return writeContextFilesTracked(env.WorkDir, provider, ctx, manifest, env.SkillDirectories)
	}
	if err := writeTaskContextMarker(env.RootDir, ctx, manifest); err != nil {
		return nil, err
	}
	if err := writeProjectResources(env.RootDir, ctx, manifest); err != nil {
		return nil, err
	}
	if provider == "claude" {
		var roots []string
		for dir := env.WorkDir; ; dir = filepath.Dir(dir) {
			roots = append(roots, skillsDirPath(dir, provider))
			if dir == env.LocalWorktree.Path || dir == filepath.Dir(dir) {
				break
			}
		}
		// Claude Code 2.1.277+ loads the main checkout's project skills in a
		// linked worktree whose root has no .claude/skills — which, with the
		// runtime skills moved out, includes this one. Those may be untracked,
		// so the worktree cannot show them; reserve their names too.
		roots = append(roots, skillsDirPath(env.LocalWorktree.GitRoot, provider))
		return writeSkillFilesWithReservedNames(skillsDirPath(env.RuntimeDir, provider), ctx.AgentSkills, manifest, env.SkillDirectories, roots...)
	}
	return resolveSkillSlugs(ctx.AgentSkills), nil
}

func (env *Environment) selectRuntimeDelivery(provider, version string, logger *slog.Logger) error {
	if env.LocalWorktree == nil || (provider != "claude" && provider != "codex") {
		return nil
	}
	if !agent.SupportsExternalRuntimeConfig(provider, version) {
		if logger != nil {
			logger.Warn("execenv: CLI version lacks validated external runtime delivery; using worktree instruction files", "provider", provider, "version", version)
		}
		return nil
	}
	env.RuntimeDir = filepath.Join(env.RootDir, "runtime")
	if err := os.MkdirAll(env.RuntimeDir, 0o755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	return nil
}
