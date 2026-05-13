package execenv

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// seedUserCodexSkills copies user-installed skill directories from the shared
// ~/.codex/skills/ into the per-task CODEX_HOME so the codex CLI discovers
// them natively. Codex is the only runtime whose HOME is redirected to a
// per-task directory (via the CODEX_HOME env var), so without this step the
// CLI never sees the user's `~/.codex/skills/` content.
//
// Workspace-assigned skills take precedence on name conflict: any user skill
// whose sanitized name matches a workspace skill's sanitized name is skipped
// here, and writeSkillFiles then writes the workspace version into a clean
// slot.
//
// Per-skill failures are logged and skipped — a single broken user skill
// must not prevent the task from running. Returning an error is reserved for
// failures that prevent listing the shared skills directory at all.
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
		// shared ~/.agents/skills/<name>/ directory. Resolve symlinks so we
		// copy the real content into the per-task home.
		resolved, err := filepath.EvalSymlinks(src)
		if err != nil {
			logger.Warn("execenv: codex user-skill resolve failed", "name", name, "error", err)
			continue
		}
		fi, err := os.Stat(resolved)
		if err != nil || !fi.IsDir() {
			continue
		}
		dst := filepath.Join(targetSkillsDir, name)
		if err := os.RemoveAll(dst); err != nil {
			logger.Warn("execenv: codex user-skill clean dst failed", "name", name, "error", err)
			continue
		}
		if err := copyDirTree(resolved, dst); err != nil {
			logger.Warn("execenv: codex user-skill copy failed", "name", name, "error", err)
			continue
		}
	}
	return nil
}

// copyDirTree walks src recursively and copies every regular file under it
// to the matching path under dst. Nested symlinks are ignored to keep the
// per-task home self-contained; the caller is expected to resolve the root
// before calling.
func copyDirTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}
