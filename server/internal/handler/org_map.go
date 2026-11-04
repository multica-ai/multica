package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetCompanyOrgMap returns the org map of the workspace. Any member may read it.
// An optional floor_id narrows the floors and squads to one floor.
//
//	GET /api/company/org-map[?floor_id=<uuid>]
func (h *Handler) GetCompanyOrgMap(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var floorID pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("floor_id")); raw != "" {
		floorID, ok = parseUUIDOrBadRequest(w, raw, "floor_id")
		if !ok {
			return
		}
	}
	out, err := service.LoadOrgMap(r.Context(), h.Queries, wsUUID, floorID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the org map")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// GetCompanyRequestUsage returns the runs and tokens spent on a request issue
// and its direct children, so the Secretary can state what a request cost.
// Any member may read it.
//
//	GET /api/company/requests/{id}/usage
func (h *Handler) GetCompanyRequestUsage(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	row, err := h.Queries.GetRequestUsageSummary(r.Context(), db.GetRequestUsageSummaryParams{
		WorkspaceID: issue.WorkspaceID,
		ID:          issue.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read request usage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":         uuidToString(issue.ID),
		"run_count":          row.RunCount,
		"metered_run_count":  row.MeteredRunCount,
		"input_tokens":       row.InputTokens,
		"output_tokens":      row.OutputTokens,
		"cache_read_tokens":  row.CacheReadTokens,
		"cache_write_tokens": row.CacheWriteTokens,
		"cost_usd_ticks":     row.CostUsdTicks,
	})
}
