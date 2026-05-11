package handler

// CEREBRO-PATCH(project-nesting-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fork-specific: nested projects (parent/child + show_descendants).
// Kept in a dedicated file so upstream-merges don't conflict on project.go.

type ProjectTreeItemResponse struct {
	ProjectResponse
	ParentProjectID *string `json:"parent_project_id"`
	ShowDescendants bool    `json:"show_descendants"`
	Depth           int16   `json:"depth"`
}

type setProjectParentRequest struct {
	ParentProjectID *string `json:"parent_project_id"`
}

type setProjectShowDescendantsRequest struct {
	ShowDescendants bool `json:"show_descendants"`
}

type projectRollupStatsResponse struct {
	IssueCount int64 `json:"issue_count"`
	DoneCount  int64 `json:"done_count"`
}

func nestingParentID(row db.ProjectNesting) *string {
	return uuidToPtr(row.ParentProjectID)
}

func defaultProjectNesting(projectID pgtype.UUID) db.ProjectNesting {
	return db.ProjectNesting{
		ProjectID:       projectID,
		ParentProjectID: pgtype.UUID{Valid: false},
		ShowDescendants: true,
		Depth:           0,
	}
}

func nestingByProject(rows []db.ProjectNesting) map[string]db.ProjectNesting {
	out := make(map[string]db.ProjectNesting, len(rows))
	for _, row := range rows {
		out[uuidToString(row.ProjectID)] = row
	}
	return out
}

func childrenByParent(rows []db.ProjectNesting) map[string][]string {
	out := map[string][]string{}
	for _, row := range rows {
		if row.ParentProjectID.Valid {
			parentID := uuidToString(row.ParentProjectID)
			out[parentID] = append(out[parentID], uuidToString(row.ProjectID))
		}
	}
	return out
}

func maxRelativeDepth(projectID string, children map[string][]string) int16 {
	var walk func(string, int16) int16
	walk = func(id string, rel int16) int16 {
		max := rel
		for _, childID := range children[id] {
			if got := walk(childID, rel+1); got > max {
				max = got
			}
		}
		return max
	}
	return walk(projectID, 0)
}

func isDescendant(candidateParentID, projectID string, nesting map[string]db.ProjectNesting) bool {
	for current := candidateParentID; current != ""; {
		if current == projectID {
			return true
		}
		row, ok := nesting[current]
		if !ok || !row.ParentProjectID.Valid {
			return false
		}
		current = uuidToString(row.ParentProjectID)
	}
	return false
}

func (h *Handler) ListProjectTree(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	member, ok := h.resolveMemberFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}

	projects, err := h.Queries.ListProjectsAccessibleToUser(r.Context(), db.ListProjectsAccessibleToUserParams{
		WorkspaceID: wsUUID,
		IsAdmin:     isWorkspaceAdmin(member),
		UserID:      member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}
	nestingRows, err := h.Queries.ListProjectNesting(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project nesting")
		return
	}
	nesting := nestingByProject(nestingRows)

	projectIDs := make([]pgtype.UUID, len(projects))
	for i, project := range projects {
		projectIDs[i] = project.ID
	}
	statsMap := make(map[string]db.GetProjectIssueStatsRow)
	resourceCountMap := make(map[string]int64)
	if len(projectIDs) > 0 {
		if stats, err := h.Queries.GetProjectIssueStats(r.Context(), projectIDs); err == nil {
			for _, row := range stats {
				statsMap[uuidToString(row.ProjectID)] = row
			}
		}
		if counts, err := h.Queries.GetProjectResourceCounts(r.Context(), projectIDs); err == nil {
			for _, row := range counts {
				resourceCountMap[uuidToString(row.ProjectID)] = row.ResourceCount
			}
		}
	}

	resp := make([]ProjectTreeItemResponse, len(projects))
	for i, project := range projects {
		projectID := uuidToString(project.ID)
		row, ok := nesting[projectID]
		if !ok {
			row = defaultProjectNesting(project.ID)
		}
		resp[i] = ProjectTreeItemResponse{
			ProjectResponse: projectToResponse(project),
			ParentProjectID: nestingParentID(row),
			ShowDescendants: row.ShowDescendants,
			Depth:           row.Depth,
		}
		if row.ShowDescendants {
			if stats, err := h.Queries.GetProjectRollupStats(r.Context(), project.ID); err == nil {
				resp[i].IssueCount = stats.TotalCount
				resp[i].DoneCount = stats.DoneCount
			}
		} else if stats, ok := statsMap[projectID]; ok {
			resp[i].IssueCount = stats.TotalCount
			resp[i].DoneCount = stats.DoneCount
		}
		resp[i].ResourceCount = resourceCountMap[projectID]
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}

func (h *Handler) SetProjectParent(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req setProjectParentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	var parentUUID pgtype.UUID
	if req.ParentProjectID != nil && *req.ParentProjectID != "" {
		parentUUID, ok = parseUUIDOrBadRequest(w, *req.ParentProjectID, "parent_project_id")
		if !ok {
			return
		}
		if uuidToString(parentUUID) == projectID {
			writeError(w, http.StatusBadRequest, "project cannot be its own parent")
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: parentUUID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "parent project not found")
			return
		}
	}

	rows, err := h.Queries.ListProjectNesting(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project nesting")
		return
	}
	nesting := nestingByProject(rows)
	projectRow, ok := nesting[projectID]
	if !ok {
		projectRow = defaultProjectNesting(projectUUID)
		rows = append(rows, projectRow)
		nesting[projectID] = projectRow
	}

	newDepth := int16(0)
	if parentUUID.Valid {
		parentID := uuidToString(parentUUID)
		if isDescendant(parentID, projectID, nesting) {
			writeError(w, http.StatusBadRequest, "project nesting cycle detected")
			return
		}
		parentRow, ok := nesting[parentID]
		if !ok {
			parentRow = defaultProjectNesting(parentUUID)
		}
		newDepth = parentRow.Depth + 1
	}
	if newDepth+maxRelativeDepth(projectID, childrenByParent(rows)) > 2 {
		writeError(w, http.StatusBadRequest, "project nesting supports max 3 levels")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpsertProjectNesting(r.Context(), db.UpsertProjectNestingParams{
		ProjectID:       projectUUID,
		ParentProjectID: parentUUID,
		ShowDescendants: projectRow.ShowDescendants,
		Depth:           newDepth,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project parent")
		return
	}
	children := childrenByParent(rows)
	var updateDescendantDepths func(string, int16) error
	updateDescendantDepths = func(id string, depth int16) error {
		for _, childID := range children[id] {
			childUUID := parseUUID(childID)
			childDepth := depth + 1
			if err := qtx.UpdateProjectNestingDepth(r.Context(), db.UpdateProjectNestingDepthParams{
				ProjectID: childUUID,
				Depth:     childDepth,
			}); err != nil {
				return err
			}
			if err := updateDescendantDepths(childID, childDepth); err != nil {
				return err
			}
		}
		return nil
	}
	if err := updateDescendantDepths(projectID, newDepth); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update descendant depths")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project parent")
		return
	}

	userID := requestUserID(r)
	resp := ProjectTreeItemResponse{
		ProjectResponse: projectToResponse(project),
		ParentProjectID: nestingParentID(updated),
		ShowDescendants: updated.ShowDescendants,
		Depth:           updated.Depth,
	}
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetProjectShowDescendants(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var req setProjectShowDescendantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	row, err := h.Queries.GetProjectNesting(r.Context(), projectUUID)
	if err != nil {
		row = defaultProjectNesting(projectUUID)
	}
	row, err = h.Queries.UpsertProjectNesting(r.Context(), db.UpsertProjectNestingParams{
		ProjectID:       projectUUID,
		ParentProjectID: row.ParentProjectID,
		ShowDescendants: req.ShowDescendants,
		Depth:           row.Depth,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project setting")
		return
	}
	resp := ProjectTreeItemResponse{
		ProjectResponse: projectToResponse(project),
		ParentProjectID: nestingParentID(row),
		ShowDescendants: row.ShowDescendants,
		Depth:           row.Depth,
	}
	userID := requestUserID(r)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetProjectRollupStats(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projectUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	stats, err := h.Queries.GetProjectRollupStats(r.Context(), projectUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project rollup stats")
		return
	}
	writeJSON(w, http.StatusOK, projectRollupStatsResponse{
		IssueCount: stats.TotalCount,
		DoneCount:  stats.DoneCount,
	})
}
