package execenv

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// prepareQwenpawWorkspace creates a per-task QwenPaw workspace directory and
// populates it with the bound skills. The daemon passes --workspace <dir> to
// `qwenpaw acp` so the QwenPaw agent discovers the skills natively through
// QwenPaw's workspace skill discovery mechanism.
//
// Skills are written into <workspace>/skills/<slug>/SKILL.md and a
// pre-populated skill.json manifest is created with enabled: true for each
// bound skill, so QwenPaw's reconcile_workspace_manifest (registry.py) resolves
// them as effective without the user having to enable them manually.
//
// The function is idempotent: it always removes the existing skills dir and
// manifest before rebuilding, so an added/removed/edited skill is reflected
// without accumulating stale state. This includes the empty-skills case — when
// all skills are unbound, any previously written files are cleaned up, matching
// the user's expectation that unbinding a skill revokes it.
//
// This mirrors the pattern used by writeHermesBoundSkills (hermes_home.go:738).
func prepareQwenpawWorkspace(qwenpawWorkspace string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	// Create the workspace directory.
	if err := os.MkdirAll(qwenpawWorkspace, 0o700); err != nil {
		return fmt.Errorf("create qwenpaw workspace dir: %w", err)
	}

	// Remove existing skills dir and manifest so an added/removed/edited
	// skill is reflected without accumulating stale state. This mirrors the
	// pattern used by writeHermesBoundSkills (hermes_home.go:738).
	skillsDir := filepath.Join(qwenpawWorkspace, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("remove existing skills dir: %w", err)
	}
	manifestPath := filepath.Join(qwenpawWorkspace, "skill.json")
	if err := os.RemoveAll(manifestPath); err != nil {
		return fmt.Errorf("remove existing manifest: %w", err)
	}

	if len(workspaceSkills) == 0 {
		// No skills to write. Existing files have been cleaned up above,
		// so unbinding every skill properly revokes them.
		return nil
	}

	// Write skills into <workspace>/skills/.
	// QwenPaw's store.py reads workspace skills from the "skills" directory
	// via get_workspace_skills_dir(); see qwenpaw/agents/skill_system/store.py.
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("create qwenpaw skills dir: %w", err)
	}

	skillNames := make([]string, 0, len(workspaceSkills))
	slugs := resolveSkillSlugs(workspaceSkills)
	for i, skill := range workspaceSkills {
		slug := slugs[i]
		skillDir := filepath.Join(skillsDir, slug)

		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf("create skill dir for %q: %w", skill.Name, err)
		}

		body := ensureSkillFrontmatter(skill.Content, slug, skill.Description)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
			return fmt.Errorf("write SKILL.md for %q: %w", skill.Name, err)
		}

		// Write supporting files.
		for _, f := range skill.Files {
			fpath := filepath.Join(skillDir, f.Path)
			if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
				return fmt.Errorf("create dir for skill file %q: %w", f.Path, err)
			}
			if err := os.WriteFile(fpath, []byte(f.Content), 0o644); err != nil {
				return fmt.Errorf("write skill file %q: %w", f.Path, err)
			}
		}

		skillNames = append(skillNames, slug)
	}

	// Write the skill.json manifest with enabled: true for every bound skill.
	// This matches the format QwenPaw's reconcile_workspace_manifest expects
	// (see store.py get_workspace_skill_manifest_path / registry.py
	// resolve_effective_skills), so the skills are immediately effective
	// without requiring the user to enable them via the UI or CLI.
	now := time.Now().UTC().Format(time.RFC3339)
	skills := make(map[string]any, len(skillNames))
	for _, slug := range skillNames {
		skills[slug] = map[string]any{
			"enabled":  true,
			"channels": []string{"all"},
			"source":   "customized",
			"metadata": map[string]any{
				"name":        slug,
				"description": "",
				"source":      "customized",
				"protected":   false,
				"updated_at":  now,
			},
			"updated_at": now,
		}
	}

	manifest := map[string]any{
		"schema_version": "workspace-skill-manifest.v1",
		"version":        0,
		"skills":         skills,
	}

	manifestPath = filepath.Join(qwenpawWorkspace, "skill.json")
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal skill manifest: %w", err)
	}
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("write skill manifest: %w", err)
	}

	logger.Info("qwenpaw workspace prepared", "workspace", qwenpawWorkspace, "skills", len(skillNames))
	return nil
}
