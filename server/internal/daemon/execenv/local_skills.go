package execenv

import (
	"os"
	"path/filepath"
)

// LocalSkillPaths returns the paths to local skill directories.
// These are symlinked into the workdir instead of copied.
//
// Directories searched:
//   - ~/.claude/skills/
//   - ~/.openclaw/skills/ (OpenClaw)
func LocalSkillPaths() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	paths := []string{
		filepath.Join(homeDir, ".claude", "skills"),
		filepath.Join(homeDir, ".openclaw", "skills"),
	}

	var existing []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}

	return existing
}

// SymlinkLocalSkills creates symlinks from workdir/.claude/skills/ to local skill directories.
// Returns the number of skills linked.
func SymlinkLocalSkills(workDir string) (int, error) {
	localPaths := LocalSkillPaths()
	if len(localPaths) == 0 {
		return 0, nil
	}

	targetDir := filepath.Join(workDir, ".claude", "skills")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return 0, err
	}

	count := 0
	for _, basePath := range localPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			src := filepath.Join(basePath, entry.Name())
			dst := filepath.Join(targetDir, entry.Name())

			// Skip if already exists (from Multica-assigned skills)
			if _, err := os.Lstat(dst); err == nil {
				continue
			}

			// Create symlink
			if err := os.Symlink(src, dst); err != nil {
				continue
			}
			count++
		}
	}

	return count, nil
}
