package execenv

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// seedUserCodexSkills links user-installed skill directories from the shared
// ~/.codex/skills/ into the per-task CODEX_HOME so the codex CLI discovers
// them natively. Codex is the only runtime whose HOME is redirected to a
// per-task directory (via the CODEX_HOME env var), so without this step the
// CLI never sees the user's `~/.codex/skills/` content.
//
// Links, never copies. A copy charges the whole skill tree to every task
// directory — 100 MB for a user with a handful of npm-backed skills — and
// hydrateCodexSkills re-does it on every task start, on the critical path,
// while no GC path ever reclaims it as long as the issue stays open. The two
// other shared resources in the same per-task home already link for the same
// reason: sessions (linkCodexSessionsToStore) and the plugin cache
// (exposeSharedCodexPluginCache). A skill directory is read-only input to the
// CLI, so the write isolation a copy incidentally bought has no requester.
//
// Workspace-assigned skills take precedence on name conflict: any user skill
// whose sanitized name matches a workspace skill's sanitized name is skipped
// here, and writeSkillFiles then writes the workspace version into a clean
// slot. writeSkillFiles never writes through a link it did not create:
// allocateCollisionFreeSkillDir probes with os.Lstat, so a link left here
// counts as occupied and the workspace skill lands in a `-multica` sibling.
//
// Per-skill failures are logged and skipped — a single broken user skill
// must not prevent the task from running. Returning an error is reserved for
// failures that would affect every skill: listing the shared skills
// directory, or creating the per-task skills directory.
func seedUserCodexSkills(codexHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	sharedSkillsDir := filepath.Join(resolveSharedCodexHome(), "skills")

	info, err := os.Stat(sharedSkillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat shared skills dir: %w", err)
	}
	if !info.IsDir() {
		return nil
	}

	reserved := make(map[string]struct{}, len(workspaceSkills))
	for _, s := range workspaceSkills {
		reserved[sanitizeSkillName(s.Name)] = struct{}{}
	}

	entries, err := os.ReadDir(sharedSkillsDir)
	if err != nil {
		return fmt.Errorf("read shared skills dir: %w", err)
	}

	targetSkillsDir := filepath.Join(codexHome, "skills")
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || strings.HasPrefix(name, ".") {
			continue
		}
		if _, claimed := reserved[sanitizeSkillName(name)]; claimed {
			logger.Info("execenv: codex user-skill yields to workspace skill", "name", name)
			continue
		}
		src := filepath.Join(sharedSkillsDir, name)
		// Installers like lark-cli ship each skill as a symlink into a
		// shared ~/.agents/skills/<name>/ directory. Resolve it so the
		// per-task link points at the real directory instead of at another
		// link, and so the IsDir check below sees the actual target.
		resolved, err := filepath.EvalSymlinks(src)
		if err != nil {
			logger.Warn("execenv: codex user-skill resolve failed", "name", name, "error", err)
			continue
		}
		fi, err := os.Stat(resolved)
		if err != nil || !fi.IsDir() {
			continue
		}
		// hydrateCodexSkills wipes the skills dir before every seed, so the
		// parent is normally absent here; createDirLink needs it to exist.
		// Created lazily so a task with no eligible user skills still gets no
		// skills directory at all.
		if err := os.MkdirAll(targetSkillsDir, 0o755); err != nil {
			return fmt.Errorf("create codex skills dir %s: %w", targetSkillsDir, err)
		}
		dst := filepath.Join(targetSkillsDir, name)
		// Removes the link, never the link target: os.RemoveAll unlinks a
		// symlink, and on Windows RemoveDirectory drops a junction while
		// leaving the directory it points at intact.
		if err := os.RemoveAll(dst); err != nil {
			logger.Warn("execenv: codex user-skill clean dst failed", "name", name, "error", err)
			continue
		}
		if err := createDirLink(resolved, dst); err != nil {
			logger.Warn("execenv: codex user-skill link failed", "name", name, "error", err)
			continue
		}
	}
	return nil
}
