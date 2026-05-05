package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type ProjectResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	IssueCount  int64   `json:"issue_count"`
	DoneCount   int64   `json:"done_count"`
}

func projectToResponse(p db.Project) ProjectResponse {
	return ProjectResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		Title:       p.Title,
		Description: textToPtr(p.Description),
		Icon:        textToPtr(p.Icon),
		Status:      p.Status,
		Priority:    p.Priority,
		LeadType:    textToPtr(p.LeadType),
		LeadID:      uuidToPtr(p.LeadID),
		CreatedAt:   timestampToString(p.CreatedAt),
		UpdatedAt:   timestampToString(p.UpdatedAt),
	}
}

func (h *Handler) loadProjectIssueStats(ctx context.Context, projectID pgtype.UUID) (int64, int64) {
	stats, err := h.Queries.GetProjectIssueStats(ctx, []pgtype.UUID{projectID})
	if err != nil || len(stats) == 0 {
		return 0, 0
	}
	return stats[0].TotalCount, stats[0].DoneCount
}

type CreateProjectRequest struct {
	Title       string                                `json:"title"`
	Description *string                               `json:"description"`
	Icon        *string                               `json:"icon"`
	Status      string                                `json:"status"`
	Priority    string                                `json:"priority"`
	LeadType    *string                               `json:"lead_type"`
	LeadID      *string                               `json:"lead_id"`
	Resources   []CreateProjectResourceRequestPayload `json:"resources,omitempty"`
}

// CreateProjectResourceRequestPayload mirrors CreateProjectResourceRequest but
// is embedded inside the project create payload. Kept as a separate type so a
// future change to the standalone request can't silently break this surface.
type CreateProjectResourceRequestPayload struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        *string         `json:"label"`
	Position     *int32          `json:"position"`
}

type UpdateProjectRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      *string `json:"status"`
	Priority    *string `json:"priority"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}
	var priorityFilter pgtype.Text
	if p := r.URL.Query().Get("priority"); p != "" {
		priorityFilter = pgtype.Text{String: p, Valid: true}
	}
	projects, err := h.Queries.ListProjects(r.Context(), db.ListProjectsParams{
		WorkspaceID: wsUUID,
		Status:      statusFilter,
		Priority:    priorityFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list projects")
		return
	}

	// Batch-fetch issue stats for all projects
	statsMap := make(map[string]db.GetProjectIssueStatsRow)
	if len(projects) > 0 {
		projectIDs := make([]pgtype.UUID, len(projects))
		for i, p := range projects {
			projectIDs[i] = p.ID
		}
		stats, err := h.Queries.GetProjectIssueStats(r.Context(), projectIDs)
		if err == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.ProjectID)] = s
			}
		}
	}

	resp := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectToResponse(p)
		if s, ok := statsMap[resp[i].ID]; ok {
			resp[i].IssueCount = s.TotalCount
			resp[i].DoneCount = s.DoneCount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	resp := projectToResponse(project)
	resp.IssueCount, resp.DoneCount = h.loadProjectIssueStats(r.Context(), project.ID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	status := req.Status
	if status == "" {
		status = "planned"
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
		if !ok {
			return
		}
		leadID = id
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Pre-validate every resource payload before opening a transaction so an
	// invalid ref produces a clean 400 with no DB work.
	normalizedRefs := make([]json.RawMessage, len(req.Resources))
	for i, res := range req.Resources {
		res.ResourceType = strings.TrimSpace(res.ResourceType)
		if res.ResourceType == "" {
			writeError(w, http.StatusBadRequest, "resources[].resource_type is required")
			return
		}
		ref, err := validateAndNormalizeResourceRef(res.ResourceType, res.ResourceRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, "resources["+strconv.Itoa(i)+"]: "+err.Error())
			return
		}
		normalizedRefs[i] = ref
	}

	createParams := db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       req.Title,
		Description: ptrToText(req.Description),
		Icon:        ptrToText(req.Icon),
		Status:      status,
		LeadType:    leadType,
		LeadID:      leadID,
		Priority:    priority,
	}

	// Without resources, keep the simple non-tx path.
	if len(req.Resources) == 0 {
		project, err := h.Queries.CreateProject(r.Context(), createParams)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create project")
			return
		}
		resp := projectToResponse(project)
		h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": resp})
		writeJSON(w, http.StatusCreated, resp)
		return
	}

	// Transactional path: project + all resources are atomic.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	project, err := qtx.CreateProject(r.Context(), createParams)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	creator, _ := h.parseUserUUIDOrZero(userID)
	resourceRows := make([]db.ProjectResource, 0, len(req.Resources))
	for i, res := range req.Resources {
		var label pgtype.Text
		if res.Label != nil && strings.TrimSpace(*res.Label) != "" {
			label = pgtype.Text{String: strings.TrimSpace(*res.Label), Valid: true}
		}
		var position int32 = int32(i)
		if res.Position != nil {
			position = *res.Position
		}
		row, err := qtx.CreateProjectResource(r.Context(), db.CreateProjectResourceParams{
			ProjectID:    project.ID,
			WorkspaceID:  project.WorkspaceID,
			ResourceType: res.ResourceType,
			ResourceRef:  normalizedRefs[i],
			Label:        label,
			Position:     position,
			CreatedBy:    creator,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeError(w, http.StatusConflict, "resources["+strconv.Itoa(i)+"]: this resource is already attached")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to attach resource at index "+strconv.Itoa(i))
			return
		}
		resourceRows = append(resourceRows, row)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project create")
		return
	}

	resp := projectToResponse(project)
	resourceResp := make([]ProjectResourceResponse, len(resourceRows))
	for i, row := range resourceRows {
		resourceResp[i] = projectResourceToResponse(row)
	}
	h.publish(protocol.EventProjectCreated, workspaceID, "member", userID, map[string]any{"project": resp})
	for _, rr := range resourceResp {
		h.publish(protocol.EventProjectResourceCreated, workspaceID, "member", userID, map[string]any{
			"resource":   rr,
			"project_id": resp.ID,
		})
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":           resp.ID,
		"workspace_id": resp.WorkspaceID,
		"title":        resp.Title,
		"description":  resp.Description,
		"icon":         resp.Icon,
		"status":       resp.Status,
		"priority":     resp.Priority,
		"lead_type":    resp.LeadType,
		"lead_id":      resp.LeadID,
		"created_at":   resp.CreatedAt,
		"updated_at":   resp.UpdatedAt,
		"issue_count":  resp.IssueCount,
		"done_count":   resp.DoneCount,
		"resources":    resourceResp,
	})
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	prevProject, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	var req UpdateProjectRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var rawFields map[string]json.RawMessage
	json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateProjectParams{
		ID:          prevProject.ID,
		Description: prevProject.Description,
		Icon:        prevProject.Icon,
		LeadType:    prevProject.LeadType,
		LeadID:      prevProject.LeadID,
	}
	if req.Title != nil {
		params.Title = pgtype.Text{String: *req.Title, Valid: true}
	}
	if req.Status != nil {
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if _, ok := rawFields["description"]; ok {
		if req.Description != nil {
			params.Description = pgtype.Text{String: *req.Description, Valid: true}
		} else {
			params.Description = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["icon"]; ok {
		if req.Icon != nil {
			params.Icon = pgtype.Text{String: *req.Icon, Valid: true}
		} else {
			params.Icon = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_type"]; ok {
		if req.LeadType != nil {
			params.LeadType = pgtype.Text{String: *req.LeadType, Valid: true}
		} else {
			params.LeadType = pgtype.Text{Valid: false}
		}
	}
	if _, ok := rawFields["lead_id"]; ok {
		if req.LeadID != nil {
			leadUUID, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
			if !ok {
				return
			}
			params.LeadID = leadUUID
		} else {
			params.LeadID = pgtype.UUID{Valid: false}
		}
	}
	project, err := h.Queries.UpdateProject(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update project")
		return
	}
	resp := projectToResponse(project)
	h.publish(protocol.EventProjectUpdated, workspaceID, "member", userID, map[string]any{"project": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "project id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if err := h.Queries.DeleteProject(r.Context(), project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project")
		return
	}
	h.publish(protocol.EventProjectDeleted, workspaceID, "member", userID, map[string]any{"project_id": uuidToString(project.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// SearchProjectResponse extends ProjectResponse with search metadata.
type SearchProjectResponse struct {
	ProjectResponse
	MatchSource    string  `json:"match_source"`
	MatchedSnippet *string `json:"matched_snippet,omitempty"`
}

// buildProjectSearchQuery builds a dynamic SQL query for project search.
func buildProjectSearchQuery(phrase string, terms []string, includeClosed bool) (string, []any) {
	phrase = strings.ToLower(phrase)
	for i, t := range terms {
		terms[i] = strings.ToLower(t)
	}

	argIdx := 1
	args := []any{}
	nextArg := func(val any) string {
		args = append(args, val)
		s := fmt.Sprintf("$%d", argIdx)
		argIdx++
		return s
	}

	escapedPhrase := escapeLike(phrase)
	phraseParam := nextArg(escapedPhrase)
	phraseContains := "'%' || " + phraseParam + " || '%'"
	phraseStartsWith := phraseParam + " || '%'"

	wsParam := nextArg(nil) // workspace_id placeholder

	var termParams []string
	if len(terms) > 1 {
		for _, t := range terms {
			et := escapeLike(t)
			termParams = append(termParams, nextArg(et))
		}
	}

	// --- WHERE clause ---
	var whereParts []string

	// Full phrase match: title or description
	phraseMatch := fmt.Sprintf(
		"(LOWER(p.title) LIKE %s OR LOWER(COALESCE(p.description, '')) LIKE %s)",
		phraseContains, phraseContains,
	)
	whereParts = append(whereParts, phraseMatch)

	// Multi-word AND match
	if len(termParams) > 1 {
		var termConditions []string
		for _, tp := range termParams {
			tc := "'%' || " + tp + " || '%'"
			termConditions = append(termConditions, fmt.Sprintf(
				"(LOWER(p.title) LIKE %s OR LOWER(COALESCE(p.description, '')) LIKE %s)",
				tc, tc,
			))
		}
		whereParts = append(whereParts, "("+strings.Join(termConditions, " AND ")+")")
	}

	whereClause := "(" + strings.Join(whereParts, " OR ") + ")"

	if !includeClosed {
		whereClause += " AND p.status NOT IN ('completed', 'cancelled')"
	}

	// --- ORDER BY ranking ---
	var rankCases []string

	// Tier 0: Exact title match
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) = %s THEN 0", phraseParam))

	// Tier 1: Title starts with phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) LIKE %s THEN 1", phraseStartsWith))

	// Tier 2: Title contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(p.title) LIKE %s THEN 2", phraseContains))

	// Tier 3: Title matches all words (multi-word only)
	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(p.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		rankCases = append(rankCases, fmt.Sprintf("WHEN (%s) THEN 3", strings.Join(titleTerms, " AND ")))
	}

	// Tier 4: Description contains phrase
	rankCases = append(rankCases, fmt.Sprintf("WHEN LOWER(COALESCE(p.description, '')) LIKE %s THEN 4", phraseContains))

	rankExpr := "CASE " + strings.Join(rankCases, " ") + " ELSE 5 END"

	// --- match_source expression ---
	matchSourceExpr := fmt.Sprintf(`CASE
		WHEN LOWER(p.title) LIKE %s THEN 'title'
		ELSE 'description'
	END`, phraseContains)

	if len(termParams) > 1 {
		var titleTerms []string
		for _, tp := range termParams {
			titleTerms = append(titleTerms, fmt.Sprintf("LOWER(p.title) LIKE '%s' || %s || '%s'", "%", tp, "%"))
		}
		matchSourceExpr = fmt.Sprintf(`CASE
			WHEN LOWER(p.title) LIKE %s THEN 'title'
			WHEN (%s) THEN 'title'
			ELSE 'description'
		END`,
			phraseContains, strings.Join(titleTerms, " AND "),
		)
	}

	limitParam := nextArg(nil)
	offsetParam := nextArg(nil)

	query := fmt.Sprintf(`SELECT p.id, p.workspace_id, p.title, p.description, p.icon,
		p.status, p.priority, p.lead_type, p.lead_id,
		p.created_at, p.updated_at,
		COUNT(*) OVER() AS total_count,
		%s AS match_source
	FROM project p
	WHERE p.workspace_id = %s AND %s
	ORDER BY %s, p.updated_at DESC
	LIMIT %s OFFSET %s`,
		matchSourceExpr,
		wsParam,
		whereClause,
		rankExpr,
		limitParam,
		offsetParam,
	)

	return query, args
}

func (h *Handler) SearchProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			limit = v
		}
	}
	if limit > 50 {
		limit = 50
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	includeClosed := r.URL.Query().Get("include_closed") == "true"

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	terms := splitSearchTerms(q)

	sqlQuery, args := buildProjectSearchQuery(q, terms, includeClosed)
	args[1] = wsUUID
	args[len(args)-2] = limit
	args[len(args)-1] = offset

	rows, err := h.DB.Query(ctx, sqlQuery, args...)
	if err != nil {
		slog.Warn("search projects failed", "error", err, "workspace_id", workspaceID, "query", q)
		writeError(w, http.StatusInternalServerError, "failed to search projects")
		return
	}
	defer rows.Close()

	type projectSearchRow struct {
		project     db.Project
		totalCount  int64
		matchSource string
	}

	var results []projectSearchRow
	for rows.Next() {
		var row projectSearchRow
		if err := rows.Scan(
			&row.project.ID,
			&row.project.WorkspaceID,
			&row.project.Title,
			&row.project.Description,
			&row.project.Icon,
			&row.project.Status,
			&row.project.Priority,
			&row.project.LeadType,
			&row.project.LeadID,
			&row.project.CreatedAt,
			&row.project.UpdatedAt,
			&row.totalCount,
			&row.matchSource,
		); err != nil {
			slog.Warn("search projects scan failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to search projects")
			return
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("search projects rows error", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search projects")
		return
	}

	var total int64
	if len(results) > 0 {
		total = results[0].totalCount
	}

	// Batch-fetch issue stats
	statsMap := make(map[string]db.GetProjectIssueStatsRow)
	if len(results) > 0 {
		projectIDs := make([]pgtype.UUID, len(results))
		for i, r := range results {
			projectIDs[i] = r.project.ID
		}
		stats, err := h.Queries.GetProjectIssueStats(ctx, projectIDs)
		if err == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.ProjectID)] = s
			}
		}
	}

	resp := make([]SearchProjectResponse, len(results))
	for i, row := range results {
		pr := projectToResponse(row.project)
		if s, ok := statsMap[pr.ID]; ok {
			pr.IssueCount = s.TotalCount
			pr.DoneCount = s.DoneCount
		}
		spr := SearchProjectResponse{
			ProjectResponse: pr,
			MatchSource:     row.matchSource,
		}
		if row.matchSource == "description" {
			desc := ""
			if row.project.Description.Valid {
				desc = row.project.Description.String
			}
			if desc != "" {
				snippet := extractSnippet(desc, q)
				spr.MatchedSnippet = &snippet
			}
		}
		resp[i] = spr
	}

	w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": resp,
		"total":    total,
	})
}
