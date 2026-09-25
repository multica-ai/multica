package service

import (
	"context"
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
// A resource is not a hook — nothing calls anything. The file was validated and
// stored when the author published the version, so installing it is a read from
// our own database rather than a fetch of whatever the author is serving today.

// InstallSkillResources writes the manifest's skill resources and prunes the
// ones this installation no longer declares.
//
// Called inside the install transaction: a plugin that half-installs its skills
// is worse than one that fails, because the missing half is invisible.
func (s *PluginService) InstallSkillResources(ctx context.Context, queries *db.Queries, installation db.PluginInstallation, manifest plugincontract.Manifest, userID pgtype.UUID) error {
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
		raw, err := s.packageFile(ctx, queries, installation.PackageVersionID, resource.Entry)
		if err != nil {
			return err
		}
		content := string(raw)
		_, description := skill.ParseSkillFrontmatter(content)
		// The manifest key is authoritative for the name, not the frontmatter.
		// The consent screen listed the key, the tool namespace uses it, and a
		// file that disagrees must not silently install under another name.
		name := resource.Key
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
