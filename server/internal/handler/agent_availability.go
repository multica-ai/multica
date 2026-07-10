package handler

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	agentAvailabilityPrivate        = "private"
	agentAvailabilitySelectedSpaces = "selected_spaces"
	agentAvailabilityWorkspace      = "workspace"
)

// resolvedAgentAvailability is the normalized location gate stored across
// agent.availability_mode and agent_available_space. It grants no data,
// integration, or resource access; invocation still has to pass the existing
// permission_mode / invocation_targets audience gate as well.
type resolvedAgentAvailability struct {
	mode     string
	spaceIDs []pgtype.UUID
}

func availabilityModeFromLegacyPermission(permissionMode string) string {
	if permissionMode == permissionModePublicTo {
		return agentAvailabilityWorkspace
	}
	return agentAvailabilityPrivate
}

// parseAndValidateAgentAvailability validates both identity and access for
// selected Spaces. Merely knowing a Private Space UUID must never let an
// owner publish an agent into it: the owner must be able to view every target.
func (h *Handler) parseAndValidateAgentAvailability(
	ctx context.Context,
	workspaceID, ownerID pgtype.UUID,
	mode string,
	rawSpaceIDs []string,
) (resolvedAgentAvailability, error) {
	if mode == "" {
		mode = agentAvailabilityPrivate
	}
	if mode != agentAvailabilityPrivate &&
		mode != agentAvailabilitySelectedSpaces &&
		mode != agentAvailabilityWorkspace {
		return resolvedAgentAvailability{}, fmt.Errorf("availability_mode must be 'private', 'selected_spaces', or 'workspace'")
	}

	if mode != agentAvailabilitySelectedSpaces {
		if len(rawSpaceIDs) > 0 {
			return resolvedAgentAvailability{}, fmt.Errorf("availability_space_ids is only valid when availability_mode is 'selected_spaces'")
		}
		return resolvedAgentAvailability{mode: mode}, nil
	}

	seen := make(map[string]struct{}, len(rawSpaceIDs))
	spaceIDs := make([]pgtype.UUID, 0, len(rawSpaceIDs))
	for _, rawID := range rawSpaceIDs {
		spaceID, err := util.ParseUUID(rawID)
		if err != nil {
			return resolvedAgentAvailability{}, fmt.Errorf("availability_space_ids contains an invalid uuid")
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
			return resolvedAgentAvailability{}, fmt.Errorf("availability Space must be active and belong to this workspace")
		}
		canView, err := h.Queries.CanViewWorkspaceSpace(ctx, db.CanViewWorkspaceSpaceParams{
			WorkspaceID: workspaceID,
			ID:          spaceID,
			UserID:      ownerID,
		})
		if err != nil || !canView {
			return resolvedAgentAvailability{}, fmt.Errorf("agent owner cannot access one of the selected Spaces")
		}
		spaceIDs = append(spaceIDs, spaceID)
	}
	if len(spaceIDs) == 0 {
		return resolvedAgentAvailability{}, fmt.Errorf("availability_space_ids must contain at least one Space for selected_spaces")
	}
	return resolvedAgentAvailability{mode: mode, spaceIDs: spaceIDs}, nil
}

func (h *Handler) replaceAgentAvailableSpaces(
	ctx context.Context,
	agentID, workspaceID, createdBy pgtype.UUID,
	spaceIDs []pgtype.UUID,
) error {
	return h.Queries.ReplaceAgentAvailableSpaces(ctx, db.ReplaceAgentAvailableSpacesParams{
		AgentID:     agentID,
		WorkspaceID: workspaceID,
		CreatedBy:   createdBy,
		SpaceIds:    spaceIDs,
	})
}

func replaceAgentAvailableSpacesWithQueries(
	ctx context.Context,
	q *db.Queries,
	agentID, workspaceID, createdBy pgtype.UUID,
	spaceIDs []pgtype.UUID,
) error {
	return q.ReplaceAgentAvailableSpaces(ctx, db.ReplaceAgentAvailableSpacesParams{
		AgentID:     agentID,
		WorkspaceID: workspaceID,
		CreatedBy:   createdBy,
		SpaceIds:    spaceIDs,
	})
}

func applyAgentAvailabilityToResponse(resp *AgentResponse, rows []db.AgentAvailableSpace) {
	spaceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		spaceIDs = append(spaceIDs, uuidToString(row.SpaceID))
	}
	resp.AvailabilitySpaceIDs = spaceIDs
}

func (h *Handler) loadAvailabilitySpacesByAgent(
	ctx context.Context,
	agents []db.Agent,
) (map[string][]db.AgentAvailableSpace, bool) {
	ids := make([]pgtype.UUID, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}
	out := make(map[string][]db.AgentAvailableSpace, len(agents))
	if len(ids) == 0 {
		return out, true
	}
	rows, err := h.Queries.ListAgentAvailableSpacesByAgentIDs(ctx, ids)
	if err != nil {
		return nil, false
	}
	for _, row := range rows {
		key := uuidToString(row.AgentID)
		out[key] = append(out[key], row)
	}
	return out, true
}

func (h *Handler) loadActiveVisibleSpaceIDSet(
	ctx context.Context,
	workspaceID, userID pgtype.UUID,
) (map[string]struct{}, bool) {
	ids, err := h.Queries.ListActiveVisibleSpaceIDsForUser(ctx, db.ListActiveVisibleSpaceIDsForUserParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		return nil, false
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		out[uuidToString(id)] = struct{}{}
	}
	return out, true
}

func availabilityIntersectsVisibleSpaces(rows []db.AgentAvailableSpace, visibleSpaceIDs map[string]struct{}) bool {
	for _, row := range rows {
		if _, ok := visibleSpaceIDs[uuidToString(row.SpaceID)]; ok {
			return true
		}
	}
	return false
}

func filterAgentAvailabilityRows(
	rows []db.AgentAvailableSpace,
	visibleSpaceIDs map[string]struct{},
) []db.AgentAvailableSpace {
	filtered := make([]db.AgentAvailableSpace, 0, len(rows))
	for _, row := range rows {
		if _, ok := visibleSpaceIDs[uuidToString(row.SpaceID)]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func filterAvailabilitySpaceIDs(ids []string, visibleSpaceIDs map[string]struct{}) []string {
	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := visibleSpaceIDs[id]; ok {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

// agentLocationAllowsInvocation is the independent location gate. Callers
// must still apply the invocation-audience gate afterwards.
func (h *Handler) agentLocationAllowsInvocation(
	ctx context.Context,
	agent db.Agent,
	effectiveUser string,
	targetSpaceID pgtype.UUID,
) bool {
	// Any supplied target context must itself be an active Space in the
	// agent's workspace. This prevents stale/foreign UUIDs from being used to
	// satisfy a Selected Spaces row.
	if targetSpaceID.Valid {
		space, err := h.Queries.GetWorkspaceSpace(ctx, db.GetWorkspaceSpaceParams{
			ID:          targetSpaceID,
			WorkspaceID: agent.WorkspaceID,
		})
		if err != nil || space.ArchivedAt.Valid {
			return false
		}
	}

	switch agent.AvailabilityMode {
	case agentAvailabilityPrivate:
		if effectiveUser == "" || uuidToString(agent.OwnerID) != effectiveUser {
			return false
		}
		if !targetSpaceID.Valid {
			return true
		}
		allowed, err := h.Queries.CanViewWorkspaceSpace(ctx, db.CanViewWorkspaceSpaceParams{
			WorkspaceID: agent.WorkspaceID,
			ID:          targetSpaceID,
			UserID:      parseUUID(effectiveUser),
		})
		return err == nil && allowed

	case agentAvailabilitySelectedSpaces:
		// Context-free Chat must not turn membership in any selected Space into
		// an implicit global context. A concrete Issue/Autopilot Space is
		// required and must match exactly, including for the agent owner.
		if !targetSpaceID.Valid {
			return false
		}
		allowed, err := h.Queries.IsAgentAvailableInActiveSpace(ctx, db.IsAgentAvailableInActiveSpaceParams{
			AgentID:     agent.ID,
			WorkspaceID: agent.WorkspaceID,
			SpaceID:     targetSpaceID,
		})
		return err == nil && allowed

	case agentAvailabilityWorkspace:
		// Context-free Chat is valid for workspace availability. A concrete
		// target was already checked active + same-workspace above.
		return true
	default:
		return false
	}
}
