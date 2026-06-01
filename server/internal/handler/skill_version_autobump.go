package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CEREBRO-PATCH(skill-version-autobump): cerebro-only skill versioning (migration
// 9013) tracks current_version + an append-only skill_version log. The change-request
// flow bumps it on merge, but a direct edit by the owner/admin (the common path, and
// the one the `multica skill update` CLI hits) left current_version frozen at 1.0.0.
// This file advances the version on every direct content edit so the field reflects
// reality. Whole-file cerebro feature; lives in the handler package next to
// skill_create.go / skill_ownership.go.

// autobumpSkillVersionOnEdit advances current_version and records a skill_version
// snapshot when a manager edits a skill's content directly. The new version
// follows the SKILL.md frontmatter `version:` when the author bumped it; otherwise
// the patch component is incremented so an edit is never invisible. current_version
// only ever moves forward, which preserves the change-request semver invariant
// (a proposed version must be strictly greater than current).
func (h *Handler) autobumpSkillVersionOnEdit(ctx context.Context, qtx *db.Queries, skill db.Skill, newContent string, files []SkillFileResponse, userID string) (db.Skill, error) {
	next := nextSkillVersion(skill.CurrentVersion, newContent)
	if next == "" || next == skill.CurrentVersion {
		return skill, nil
	}

	updated, err := qtx.UpdateSkillCurrentVersion(ctx, db.UpdateSkillCurrentVersionParams{
		ID:             skill.ID,
		CurrentVersion: next,
		Content:        newContent,
	})
	if err != nil {
		return skill, err
	}

	if _, err := qtx.CreateSkillVersion(ctx, db.CreateSkillVersionParams{
		SkillID:     updated.ID,
		Version:     next,
		Content:     newContent,
		Files:       skillFilesToVersionJSON(files),
		Description: updated.Description,
		CreatedBy:   parseUUID(userID),
	}); err != nil {
		return skill, err
	}

	return updated, nil
}

// nextSkillVersion decides the version a direct content edit should land on.
// Preference order: adopt the frontmatter version when it is a valid semver
// greater than current; otherwise increment the current patch number. Returns
// current unchanged only when no sensible advance exists (it never should, since
// patch-increment always produces a higher version).
func nextSkillVersion(current, content string) string {
	fm := parseSkillFrontmatterVersion(content)
	if validSemver(fm) && (!validSemver(current) || semverGT(fm, current)) {
		return fm
	}
	if validSemver(current) {
		return bumpPatch(current)
	}
	// Legacy skill with a non-semver current_version and no usable frontmatter
	// version: seed a baseline so the field becomes meaningful from here on.
	return "1.0.1"
}

// bumpPatch returns v with its patch component incremented. v must be valid
// semver; callers guard with validSemver.
func bumpPatch(v string) string {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return "1.0.1"
	}
	patch, _ := strconv.Atoi(m[3])
	return fmt.Sprintf("%s.%s.%d", m[1], m[2], patch+1)
}

// parseSkillFrontmatterVersion extracts the `version:` field from SKILL.md YAML
// frontmatter. Mirrors parseSkillFrontmatter's lightweight line scan rather than
// pulling in a YAML parser for one field.
func parseSkillFrontmatterVersion(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "version:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "version:"))
			return strings.Trim(v, "\"'")
		}
	}
	return ""
}
