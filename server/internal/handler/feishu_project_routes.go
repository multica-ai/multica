package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Endpoints for the Feishu Project 1:N space→workspace routing setup flow.
//
// UX flow served by these endpoints (matches the design in CLAUDE conversation):
//  1. GET  .../feishu-project-integration/fields            → pick the biz-line field
//  2. GET  .../feishu-project-integration/business-lines    → pick which biz-line nodes to route
//  3. GET  .../feishu-project-integration/routes            → list current routes
//  4. PUT  .../feishu-project-integration/routes            → overwrite the route table
//
// All routes are scoped to one integration (one workspace). 1:N is achieved at the
// space level by letting multiple workspaces' integrations point at the same project_key
// and route different biz-line nodes to their respective Multica projects.

type FeishuProjectFieldsResponse struct {
	Fields []service.FeishuProjectFieldMeta `json:"fields"`
}

type FeishuProjectBusinessLinesResponse struct {
	BusinessLines []service.FeishuProjectFieldOption `json:"business_lines"`
}

type FeishuProjectRouteRequest struct {
	ProjectID              string  `json:"project_id"`
	BusinessLineID         string  `json:"business_line_id"`
	BusinessLineName       string  `json:"business_line_name"`
	ParentBusinessLineID   string  `json:"parent_business_line_id,omitempty"`
	ParentBusinessLineName string  `json:"parent_business_line_name,omitempty"`
	// Optional. When set, work items routed here whose Meego owner does NOT resolve to
	// any workspace member get assigned to this agent. Empty string / null = no fallback.
	FallbackAgentID *string `json:"fallback_agent_id,omitempty"`
}

type FeishuProjectRouteResponse struct {
	ID                     string  `json:"id"`
	ProjectID              string  `json:"project_id"`
	BusinessLineID         string  `json:"business_line_id"`
	BusinessLineName       string  `json:"business_line_name"`
	ParentBusinessLineID   string  `json:"parent_business_line_id,omitempty"`
	ParentBusinessLineName string  `json:"parent_business_line_name,omitempty"`
	FallbackAgentID        *string `json:"fallback_agent_id,omitempty"`
	CreatedAt              string  `json:"created_at,omitempty"`
	UpdatedAt              string  `json:"updated_at,omitempty"`
}

type ReplaceFeishuProjectRoutesRequest struct {
	Routes []FeishuProjectRouteRequest `json:"routes"`
}

type FeishuProjectRoutesResponse struct {
	Routes []FeishuProjectRouteResponse `json:"routes"`
}

func (h *Handler) ListFeishuProjectWorkItemFields(w http.ResponseWriter, r *http.Request) {
	cfg, ok := h.loadFeishuProjectIntegration(w, r)
	if !ok {
		return
	}
	workItemType := strings.TrimSpace(r.URL.Query().Get("work_item_type"))
	if workItemType == "" {
		workItemType = "issue"
	}
	fields, err := service.NewFeishuProjectClient().ListWorkItemFields(r.Context(), cfg, workItemType)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, FeishuProjectFieldsResponse{Fields: fields})
}

// ListFeishuProjectBusinessLines returns the option tree of the work-item field selected
// as the business-line field. Required query params: field_key. Optional: work_item_type
// (defaults to "issue").
func (h *Handler) ListFeishuProjectBusinessLines(w http.ResponseWriter, r *http.Request) {
	cfg, ok := h.loadFeishuProjectIntegration(w, r)
	if !ok {
		return
	}
	fieldKey := strings.TrimSpace(r.URL.Query().Get("field_key"))
	if fieldKey == "" {
		writeError(w, http.StatusBadRequest, "field_key query param is required")
		return
	}
	workItemType := strings.TrimSpace(r.URL.Query().Get("work_item_type"))
	if workItemType == "" {
		workItemType = "issue"
	}
	lines, err := service.NewFeishuProjectClient().ListFieldOptions(r.Context(), cfg, workItemType, fieldKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, FeishuProjectBusinessLinesResponse{BusinessLines: lines})
}

func (h *Handler) ListFeishuProjectRoutes(w http.ResponseWriter, r *http.Request) {
	cfg, ok := h.loadFeishuProjectIntegration(w, r)
	if !ok {
		return
	}
	routes, err := h.Queries.ListFeishuProjectBusinessLineRoutes(r.Context(), cfg.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Feishu Project routes")
		return
	}
	writeJSON(w, http.StatusOK, FeishuProjectRoutesResponse{Routes: feishuProjectRoutesToResponse(routes)})
}

// ReplaceFeishuProjectRoutes overwrites the integration's route table atomically.
// Front-end always sends the full desired set; partial PATCH is intentionally not exposed
// because that splits route invariants across requests (and routes are small — full sync
// is cheap).
//
// Validation:
//   - project_id must reference an existing project in this workspace
//   - business_line_id must be non-empty (the leaf identity routing keys on)
//   - business_line_id is enforced UNIQUE per integration by the DB
func (h *Handler) ReplaceFeishuProjectRoutes(w http.ResponseWriter, r *http.Request) {
	cfg, ok := h.loadFeishuProjectIntegration(w, r)
	if !ok {
		return
	}
	var req ReplaceFeishuProjectRoutesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	type validRoute struct {
		ProjectID              pgtype.UUID
		BusinessLineID         string
		BusinessLineName       string
		ParentBusinessLineID   string
		ParentBusinessLineName string
		FallbackAgentID        pgtype.UUID // invalid → no fallback
	}
	validated := make([]validRoute, 0, len(req.Routes))
	seen := map[string]bool{}
	for i, in := range req.Routes {
		bizID := strings.TrimSpace(in.BusinessLineID)
		if bizID == "" {
			writeError(w, http.StatusBadRequest, "routes["+strconv.Itoa(i)+"].business_line_id is required")
			return
		}
		if seen[bizID] {
			writeError(w, http.StatusBadRequest, "routes["+strconv.Itoa(i)+"].business_line_id must be unique within the request")
			return
		}
		seen[bizID] = true
		projUUID, valid := parseUUIDOrBadRequest(w, in.ProjectID, "routes["+strconv.Itoa(i)+"].project_id")
		if !valid {
			return
		}
		project, err := h.Queries.GetProject(r.Context(), projUUID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "routes["+strconv.Itoa(i)+"].project_id not found")
			return
		}
		if project.WorkspaceID != cfg.WorkspaceID {
			writeError(w, http.StatusBadRequest, "routes["+strconv.Itoa(i)+"].project_id does not belong to this workspace")
			return
		}
		// Fallback agent is optional. Accept nil pointer OR empty string as "no fallback".
		// When provided, the agent must exist AND belong to this workspace (cross-workspace
		// agent reference would let an attacker bind cross-tenant assignees).
		var fallbackAgentID pgtype.UUID
		if in.FallbackAgentID != nil {
			s := strings.TrimSpace(*in.FallbackAgentID)
			if s != "" {
				agentUUID, valid := parseUUIDOrBadRequest(w, s, "routes["+strconv.Itoa(i)+"].fallback_agent_id")
				if !valid {
					return
				}
				if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
					ID:          agentUUID,
					WorkspaceID: cfg.WorkspaceID,
				}); err != nil {
					writeError(w, http.StatusBadRequest, "routes["+strconv.Itoa(i)+"].fallback_agent_id not found in this workspace")
					return
				}
				fallbackAgentID = agentUUID
			}
		}
		validated = append(validated, validRoute{
			ProjectID:              projUUID,
			BusinessLineID:         bizID,
			BusinessLineName:       strings.TrimSpace(in.BusinessLineName),
			ParentBusinessLineID:   strings.TrimSpace(in.ParentBusinessLineID),
			ParentBusinessLineName: strings.TrimSpace(in.ParentBusinessLineName),
			FallbackAgentID:        fallbackAgentID,
		})
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteFeishuProjectBusinessLineRoutesByIntegration(r.Context(), cfg.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear existing routes")
		return
	}
	out := make([]db.FeishuProjectBusinessLineRoute, 0, len(validated))
	for _, vr := range validated {
		route, err := qtx.UpsertFeishuProjectBusinessLineRoute(r.Context(), db.UpsertFeishuProjectBusinessLineRouteParams{
			IntegrationID:          cfg.ID,
			WorkspaceID:            cfg.WorkspaceID,
			ProjectID:              vr.ProjectID,
			BusinessLineID:         vr.BusinessLineID,
			BusinessLineName:       vr.BusinessLineName,
			ParentBusinessLineID:   vr.ParentBusinessLineID,
			ParentBusinessLineName: vr.ParentBusinessLineName,
			FallbackAgentID:        vr.FallbackAgentID,
		})
		if err != nil {
			slog.Warn("Feishu Project route upsert failed",
				"integration_id", uuidToString(cfg.ID),
				"business_line_id", vr.BusinessLineID,
				"error", err,
			)
			writeError(w, http.StatusInternalServerError, "failed to write route")
			return
		}
		out = append(out, route)
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit routes")
		return
	}
	writeJSON(w, http.StatusOK, FeishuProjectRoutesResponse{Routes: feishuProjectRoutesToResponse(out)})
}

// loadFeishuProjectIntegration resolves the workspace from the URL, enforces owner/admin
// access, and returns the integration row. Returns ok=false if the response has already
// been written (auth failure, missing integration, etc.).
func (h *Handler) loadFeishuProjectIntegration(w http.ResponseWriter, r *http.Request) (db.FeishuProjectIntegration, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return db.FeishuProjectIntegration{}, false
	}
	cfg, err := h.Queries.GetFeishuProjectIntegration(r.Context(), parseUUID(workspaceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Feishu Project integration not found")
			return db.FeishuProjectIntegration{}, false
		}
		writeError(w, http.StatusInternalServerError, "failed to load Feishu Project integration")
		return db.FeishuProjectIntegration{}, false
	}
	return cfg, true
}

func feishuProjectRoutesToResponse(in []db.FeishuProjectBusinessLineRoute) []FeishuProjectRouteResponse {
	out := make([]FeishuProjectRouteResponse, 0, len(in))
	for _, r := range in {
		var fallbackAgentID *string
		if r.FallbackAgentID.Valid {
			s := uuidToString(r.FallbackAgentID)
			fallbackAgentID = &s
		}
		out = append(out, FeishuProjectRouteResponse{
			ID:                     uuidToString(r.ID),
			ProjectID:              uuidToString(r.ProjectID),
			BusinessLineID:         r.BusinessLineID,
			BusinessLineName:       r.BusinessLineName,
			ParentBusinessLineID:   r.ParentBusinessLineID,
			ParentBusinessLineName: r.ParentBusinessLineName,
			FallbackAgentID:        fallbackAgentID,
			CreatedAt:              timestampToString(r.CreatedAt),
			UpdatedAt:              timestampToString(r.UpdatedAt),
		})
	}
	return out
}
