package handler

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Per-project usage on the workspace dashboard.
//
//   GET /api/dashboard/usage/by-project     per-project token usage + run count
//   GET /api/dashboard/usage/project-tasks  one project's most recent runs
//
// A run's project is its issue's project, the project dimension of the hourly
// rollup. Both endpoints take the same ?days= / ?squad_id= / ?tz= as
// usage/by-member, on the same window, history limit and raw task_usage
// filters, so the project rows add up to the Cost and Tokens KPIs. by-project
// also takes the page's ?project_id= filter; project-tasks requires
// ?project_id=, a project id or noProjectID.
//
// Access control follows usage/by-member:
//   - owner/admin see every run; anyone else sees only the runs they are the
//     accountable member of (only_user_id / user_id in SQL);
//   - private projects (MUL-8472): by-project folds the usage of projects the
//     caller cannot see onto restrictedProjectsID, which names none of them;
//     project-tasks answers a hidden project like a missing one (200 []).
//     The dashboard refuses guests, but should one get here, its hidden set
//     holds projectaccess.NoProject and the no-project bucket folds into
//     restrictedProjectsID as well;
//   - project-tasks applies member-tasks' per-run rules (writeDashboardTasks).

// noProjectID marks usage of runs whose issue has no project, or that have no
// issue at all (chats, autopilot runs). Not a UUID, so it cannot collide with
// a project id. Same sentinel as the issue list's "no project" group.
const noProjectID = "__no_project__"

// restrictedProjectsID is the one bucket for usage of private projects the
// caller cannot see.
const restrictedProjectsID = "__restricted_projects__"

// DashboardUsageByProjectResponse is one project's usage in the window.
// ProjectID is a project id, noProjectID or restrictedProjectsID; ProjectTitle
// is empty for the two buckets. TaskCount is the number of distinct runs with
// usage; a run that used two models counts once.
type DashboardUsageByProjectResponse struct {
	ProjectID    string                 `json:"project_id"`
	ProjectTitle string                 `json:"project_title"`
	TaskCount    int32                  `json:"task_count"`
	Usage        []DashboardMemberUsage `json:"usage"`
}

// GetDashboardUsageByProject returns per-project token aggregates and run counts.
func (h *Handler) GetDashboardUsageByProject(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	squadScope, squadID, ok := parseSquadScopeParam(w, r)
	if !ok {
		return
	}
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	if !hidden.CanSeeProject(projectID) {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	tz := h.resolveViewingTZ(r)
	window := h.dashboardCutoff(w, r, workspaceID, tz, 1)
	onlyUser, _ := dashboardMemberScope(member)

	rows, err := h.Queries.ListDashboardUsageByProject(r.Context(), db.ListDashboardUsageByProjectParams{
		WorkspaceID:      parseUUID(workspaceID),
		HiddenProjectIds: hidden.Param(),
		SinceHour:        usageHourCutoff(window.since),
		ProjectID:        projectID,
		SquadScope:       squadScope,
		SquadID:          squadID,
		OnlyUserID:       onlyUser,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by project")
		return
	}

	resp := make([]DashboardUsageByProjectResponse, 0)
	index := make(map[string]int)
	for _, row := range rows {
		i, seen := index[row.ProjectKey]
		if !seen {
			i = len(resp)
			index[row.ProjectKey] = i
			resp = append(resp, DashboardUsageByProjectResponse{
				ProjectID:    row.ProjectKey,
				ProjectTitle: row.ProjectTitle,
				TaskCount:    row.TaskCount,
				Usage:        []DashboardMemberUsage{},
			})
		}
		resp[i].Usage = append(resp[i].Usage, DashboardMemberUsage{
			Provider:                 row.Provider,
			Model:                    row.Model,
			InputTokens:              row.InputTokens,
			OutputTokens:             row.OutputTokens,
			CacheReadTokens:          row.CacheReadTokens,
			CacheWriteTokens:         row.CacheWriteTokens,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetDashboardProjectTasks returns one project's most recent runs with usage
// in the window, or with ?project_id=__no_project__ those outside any project.
func (h *Handler) GetDashboardProjectTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	rawProject := r.URL.Query().Get("project_id")
	if rawProject == "" {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	withoutProject := rawProject == noProjectID
	var projectID pgtype.UUID
	if !withoutProject {
		if projectID, ok = parseUUIDOrBadRequest(w, rawProject, "project_id"); !ok {
			return
		}
	}
	squadScope, squadID, ok := parseSquadScopeParam(w, r)
	if !ok {
		return
	}

	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}
	// An invalid projectID is "no project", which only a guest cannot see.
	if !hidden.CanSeeProject(projectID) {
		writeJSON(w, http.StatusOK, []struct{}{})
		return
	}
	onlyUser, privileged := dashboardMemberScope(member)
	h.writeDashboardTasks(w, r, workspaceID, member, hidden, db.ListDashboardMemberTaskUsageParams{
		ProjectID:      projectID,
		WithoutProject: withoutProject,
		SquadScope:     squadScope,
		SquadID:        squadID,
		AllUsers:       privileged,
		UserID:         onlyUser,
	})
}
