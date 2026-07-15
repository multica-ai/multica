package handler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	skillAvailabilityPrivate        = "private"
	skillAvailabilitySelectedSpaces = "selected_spaces"
	skillAvailabilityWorkspace      = "workspace"
)

type resolvedSkillAvailability struct {
	mode     string
	spaceIDs []pgtype.UUID
}

func (h *Handler) parseAndValidateSkillAvailability(
	ctx context.Context,
	workspaceID, actorID pgtype.UUID,
	mode string,
	rawSpaceIDs []string,
) (resolvedSkillAvailability, error) {
	if mode == "" {
		mode = skillAvailabilityPrivate
	}
	if mode != skillAvailabilityPrivate &&
		mode != skillAvailabilitySelectedSpaces &&
		mode != skillAvailabilityWorkspace {
		return resolvedSkillAvailability{}, fmt.Errorf("availability_mode must be 'private', 'selected_spaces', or 'workspace'")
	}
	if mode != skillAvailabilitySelectedSpaces {
		if len(rawSpaceIDs) > 0 {
			return resolvedSkillAvailability{}, fmt.Errorf("availability_space_ids is only valid when availability_mode is 'selected_spaces'")
		}
		return resolvedSkillAvailability{mode: mode}, nil
	}

	seen := make(map[string]struct{}, len(rawSpaceIDs))
	spaceIDs := make([]pgtype.UUID, 0, len(rawSpaceIDs))
	for _, rawID := range rawSpaceIDs {
		spaceID, err := util.ParseUUID(rawID)
		if err != nil {
			return resolvedSkillAvailability{}, fmt.Errorf("availability_space_ids contains an invalid uuid")
		}
		key := uuidToString(spaceID)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}

		space, err := h.Queries.GetWorkspaceSpace(ctx, db.GetWorkspaceSpaceParams{
			ID:          spaceID,
			WorkspaceID: workspaceID,
		})
		if err != nil || space.ArchivedAt.Valid {
			return resolvedSkillAvailability{}, fmt.Errorf("availability Space must be active and belong to this workspace")
		}
		canView, err := h.Queries.CanViewWorkspaceSpace(ctx, db.CanViewWorkspaceSpaceParams{
			WorkspaceID: workspaceID,
			ID:          spaceID,
			UserID:      actorID,
		})
		if err != nil || !canView {
			return resolvedSkillAvailability{}, fmt.Errorf("you cannot access one of the selected Spaces")
		}
		spaceIDs = append(spaceIDs, spaceID)
	}
	if len(spaceIDs) == 0 {
		return resolvedSkillAvailability{}, fmt.Errorf("availability_space_ids must contain at least one Space for selected_spaces")
	}
	return resolvedSkillAvailability{mode: mode, spaceIDs: spaceIDs}, nil
}

func replaceSkillAvailableSpacesWithQueries(
	ctx context.Context,
	q *db.Queries,
	skillID, workspaceID, createdBy pgtype.UUID,
	spaceIDs []pgtype.UUID,
) error {
	return q.ReplaceSkillAvailableSpaces(ctx, db.ReplaceSkillAvailableSpacesParams{
		SkillID:     skillID,
		WorkspaceID: workspaceID,
		CreatedBy:   createdBy,
		SpaceIds:    spaceIDs,
	})
}

func applySkillAvailabilityToResponse(resp *SkillResponse, rows []db.SkillAvailableSpace) {
	resp.AvailabilitySpaceIDs = skillAvailabilitySpaceIDs(rows)
}

func applySkillAvailabilityToSummary(resp *SkillSummaryResponse, rows []db.SkillAvailableSpace) {
	resp.AvailabilitySpaceIDs = skillAvailabilitySpaceIDs(rows)
}

func skillAvailabilitySpaceIDs(rows []db.SkillAvailableSpace) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, uuidToString(row.SpaceID))
	}
	return ids
}

func (h *Handler) loadAvailabilitySpacesBySkill(
	ctx context.Context,
	skills []db.Skill,
) (map[string][]db.SkillAvailableSpace, bool) {
	ids := make([]pgtype.UUID, 0, len(skills))
	for _, skill := range skills {
		ids = append(ids, skill.ID)
	}
	return h.loadAvailabilitySpacesBySkillIDs(ctx, ids)
}

func (h *Handler) loadAvailabilitySpacesBySkillIDs(
	ctx context.Context,
	ids []pgtype.UUID,
) (map[string][]db.SkillAvailableSpace, bool) {
	out := make(map[string][]db.SkillAvailableSpace, len(ids))
	if len(ids) == 0 {
		return out, true
	}
	rows, err := h.Queries.ListSkillAvailableSpacesBySkillIDs(ctx, ids)
	if err != nil {
		return nil, false
	}
	for _, row := range rows {
		key := uuidToString(row.SkillID)
		out[key] = append(out[key], row)
	}
	return out, true
}

func skillVisibleToMember(
	skill db.Skill,
	rows []db.SkillAvailableSpace,
	visibleSpaceIDs map[string]struct{},
	userID, role string,
) bool {
	if roleAllowed(role, "owner", "admin") || uuidToString(skill.CreatedBy) == userID {
		return true
	}
	switch skill.AvailabilityMode {
	case skillAvailabilityWorkspace:
		return true
	case skillAvailabilitySelectedSpaces:
		return skillAvailabilityIntersectsVisibleSpaces(rows, visibleSpaceIDs)
	case skillAvailabilityPrivate:
		return false
	default:
		return false
	}
}

func skillAvailabilityIntersectsVisibleSpaces(
	rows []db.SkillAvailableSpace,
	visibleSpaceIDs map[string]struct{},
) bool {
	for _, row := range rows {
		if _, ok := visibleSpaceIDs[uuidToString(row.SpaceID)]; ok {
			return true
		}
	}
	return false
}

func filterSkillAvailabilityRows(
	rows []db.SkillAvailableSpace,
	visibleSpaceIDs map[string]struct{},
) []db.SkillAvailableSpace {
	filtered := make([]db.SkillAvailableSpace, 0, len(rows))
	for _, row := range rows {
		if _, ok := visibleSpaceIDs[uuidToString(row.SpaceID)]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (h *Handler) skillVisibleToRequest(
	ctx context.Context,
	skill db.Skill,
	userID string,
) (bool, []db.SkillAvailableSpace) {
	member, err := h.getWorkspaceMember(ctx, userID, uuidToString(skill.WorkspaceID))
	if err != nil {
		return false, nil
	}
	rows, err := h.Queries.ListSkillAvailableSpaces(ctx, skill.ID)
	if err != nil {
		return false, nil
	}
	if roleAllowed(member.Role, "owner", "admin") || uuidToString(skill.CreatedBy) == userID {
		return true, rows
	}
	visible, ok := h.loadActiveVisibleSpaceIDSet(ctx, skill.WorkspaceID, parseUUID(userID))
	if !ok || !skillVisibleToMember(skill, rows, visible, userID, member.Role) {
		return false, nil
	}
	return true, filterSkillAvailabilityRows(rows, visible)
}
