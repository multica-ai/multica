package service

import (
	"context"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/skill"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Skill resources.
//
// A plugin's `skill` resource becomes an ordinary row in the existing skill
// table. Not a plugin-owned copy, not a bundle, not an artifact with a digest:
// the previous plugin system built all of that across fourteen tables and, at
// the end of it, delivered one SKILL.md that this table could already hold.
//
// The only thing the platform has to remember is which installation contributed
// which skill, so uninstall removes exactly those and nothing a person wrote.
// That is one nullable column.
//
// A resource is not a hook — nothing calls anything. The file is fetched once,
// at install, from the same origin as the manifest, and after that it is just a
// skill.

// maxSkillBytes bounds one fetched SKILL.md. Generous for prose, small enough
// that a source URL cannot use the install path to push a large body into the
// database.
const maxSkillBytes = 256 * 1024

// InstallSkillResources writes the manifest's skill resources and prunes the
// ones this installation no longer declares.
//
// Called inside the install transaction: a plugin that half-installs its skills
// is worse than one that fails, because the missing half is invisible.
func (s *PluginService) InstallSkillResources(ctx context.Context, queries *db.Queries, installation db.PluginInstallation, manifest plugincontract.Manifest, sourceURL string, userID pgtype.UUID) error {
	resources := skillResources(manifest)

	// Prune first, so a rename frees its old name before the new one is
	// written. The reverse order would collide with the table's unique
	// (workspace_id, name) on any rename that only changes case or spacing.
	keep := make([]string, 0, len(resources))
	for _, resource := range resources {
		keep = append(keep, resource.Key)
	}
	if err := queries.DeletePluginSkillsNotIn(ctx, db.DeletePluginSkillsNotInParams{
		PluginInstallationID: installation.ID,
		KeepNames:            keep,
	}); err != nil {
		return &PluginError{Kind: PluginErrorUnavailable, Message: "prune plugin skills", Err: err}
	}
	if len(resources) == 0 {
		return nil
	}

	for _, resource := range resources {
		content, err := s.fetchSkillResource(ctx, sourceURL, resource.Entry)
		if err != nil {
			return err
		}
		name, description := skill.ParseSkillFrontmatter(content)
		// The manifest key is authoritative for the name, not the frontmatter.
		// The consent screen listed the key, the tool namespace uses it, and a
		// file that disagrees must not silently install under another name.
		name = resource.Key
		if strings.TrimSpace(description) == "" {
			description = "Provided by the " + manifest.Name + " Plugin."
		}

		if _, err := queries.UpsertPluginSkill(ctx, db.UpsertPluginSkillParams{
			WorkspaceID:          installation.WorkspaceID,
			Name:                 name,
			Description:          description,
			Content:              content,
			CreatedBy:            userID,
			PluginInstallationID: installation.ID,
		}); err != nil {
			if isUniqueViolation(err) {
				return pluginErrf(PluginErrorConflict,
					"a skill named %q already exists in this workspace", name)
			}
			return &PluginError{Kind: PluginErrorUnavailable, Message: "install plugin skill", Err: err}
		}
	}
	return nil
}

func skillResources(manifest plugincontract.Manifest) []plugincontract.Resource {
	resources := make([]plugincontract.Resource, 0, len(manifest.Contributes.Resources))
	for _, resource := range manifest.Contributes.Resources {
		if resource.Type == plugincontract.ResourceSkill {
			resources = append(resources, resource)
		}
	}
	return resources
}

// fetchSkillResource reads one SKILL.md from beside the manifest.
//
// Resolved relative to the source URL rather than taken as a URL of its own:
// the manifest already passed the origin checks, and letting a resource name an
// arbitrary address would hand the install path a second, unreviewed fetch
// target. `entry` is validated at parse time to be a relative path with no
// traversal, which is what makes this safe to join.
func (s *PluginService) fetchSkillResource(ctx context.Context, sourceURL, entry string) (string, error) {
	if strings.HasPrefix(sourceURL, LocalSourcePrefix) {
		raw, err := s.readLocalFile(strings.TrimPrefix(sourceURL, LocalSourcePrefix), entry)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}

	base, err := url.Parse(sourceURL)
	if err != nil {
		return "", pluginErrf(PluginErrorInvalid, "source_url is not a valid URL")
	}
	resolved := base.JoinPath("..", entry).String()

	var raw []byte
	if s.isDevOrigin(resolved) {
		raw, err = fetchDevManifest(ctx, resolved)
	} else {
		raw, err = fetchRemoteManifest(ctx, resolved)
	}
	if err != nil {
		return "", &PluginError{Kind: PluginErrorInvalid, Message: "fetch plugin skill " + entry, Err: err}
	}
	if len(raw) > maxSkillBytes {
		return "", pluginErrf(PluginErrorInvalid, "plugin skill %s exceeds %d bytes", entry, maxSkillBytes)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return "", pluginErrf(PluginErrorInvalid, "plugin skill %s is empty", entry)
	}
	return string(raw), nil
}
