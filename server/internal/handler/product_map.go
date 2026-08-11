package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProductRefResponse is a polymorphic traceability link from a product node to
// a Multica project or issue.
type ProductRefResponse struct {
	RefType string `json:"ref_type"`
	RefID   string `json:"ref_id"`
}

// ProductEditorResponse identifies a product-level editor by user id.
type ProductEditorResponse struct {
	UserID string `json:"user_id"`
}

// ProductNodeResponse is the read-only product-map node shape. Evidence is
// rendered as a raw object so the frontend can show source + freshness without
// the server needing to understand every evidence schema.
type ProductNodeResponse struct {
	ID              string                  `json:"id"`
	WorkspaceID     string                  `json:"workspace_id"`
	ParentID        string                  `json:"parent_id,omitempty"`
	Name            string                  `json:"name"`
	Slug            string                  `json:"slug"`
	Description     string                  `json:"description"`
	SortOrder       int32                   `json:"sort_order"`
	Status          string                  `json:"status"`
	StatusSource    string                  `json:"status_source"`
	Evidence        map[string]any          `json:"evidence"`
	CreatedAt       string                  `json:"created_at"`
	UpdatedAt       string                  `json:"updated_at"`
	Refs            []ProductRefResponse    `json:"refs"`
	Editors         []ProductEditorResponse `json:"editors"`
	Children        []*ProductNodeResponse  `json:"children,omitempty"`
	HasLiveEvidence bool                    `json:"has_live_evidence"`
}

func productNodeToResponse(n db.ProductNode, refs []db.ProductRef, editors []db.ProductEditor, children []*ProductNodeResponse) *ProductNodeResponse {
	evidence := map[string]any{}
	if len(n.Evidence) > 0 {
		_ = json.Unmarshal(n.Evidence, &evidence)
	}
	parentID := ""
	if n.ParentID.Valid {
		parentID = uuidToString(n.ParentID)
	}
	refResp := make([]ProductRefResponse, 0, len(refs))
	for _, r := range refs {
		refResp = append(refResp, ProductRefResponse{RefType: r.RefType, RefID: uuidToString(r.RefID)})
	}
	editorResp := make([]ProductEditorResponse, 0, len(editors))
	for _, e := range editors {
		editorResp = append(editorResp, ProductEditorResponse{UserID: uuidToString(e.UserID)})
	}
	return &ProductNodeResponse{
		ID:              uuidToString(n.ID),
		WorkspaceID:     uuidToString(n.WorkspaceID),
		ParentID:        parentID,
		Name:            n.Name,
		Slug:            n.Slug,
		Description:     n.Description,
		SortOrder:       n.SortOrder,
		Status:          n.Status,
		StatusSource:    n.StatusSource,
		Evidence:        evidence,
		CreatedAt:       timestampToString(n.CreatedAt),
		UpdatedAt:       timestampToString(n.UpdatedAt),
		Refs:            refResp,
		Editors:         editorResp,
		Children:        children,
		HasLiveEvidence: n.Status == "released" && len(evidence) > 0,
	}
}

// ListProductMap returns the full product tree for the requesting workspace.
// Read-only: any workspace member may view it (acceptance criterion 1).
func (h *Handler) ListProductMap(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	nodes, err := h.Queries.ListProductNodesByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list product map")
		return
	}

	// Load refs and editors once per workspace, then group in memory — the MVP
	// tree is small; two extra queries beat N+1 per node.
	editors, err := h.Queries.ListProductEditorsByWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list product editors")
		return
	}
	refsByProduct := map[string][]db.ProductRef{}
	for _, n := range nodes {
		refs, err := h.Queries.ListProductRefsByProduct(r.Context(), n.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list product refs")
			return
		}
		refsByProduct[uuidToString(n.ID)] = refs
	}
	editorsByProduct := map[string][]db.ProductEditor{}
	for _, e := range editors {
		key := uuidToString(e.ProductID)
		editorsByProduct[key] = append(editorsByProduct[key], e)
	}

	// Build the tree: nodes whose parent is present become children; orphans
	// (missing parent) degrade to roots so a partially-deleted tree still
	// renders.
	byID := map[string]*ProductNodeResponse{}
	for _, n := range nodes {
		byID[uuidToString(n.ID)] = productNodeToResponse(n, refsByProduct[uuidToString(n.ID)], editorsByProduct[uuidToString(n.ID)], nil)
	}
	roots := []*ProductNodeResponse{}
	for _, n := range nodes {
		node := byID[uuidToString(n.ID)]
		if n.ParentID.Valid {
			if parent, ok := byID[uuidToString(n.ParentID)]; ok {
				parent.Children = append(parent.Children, node)
				continue
			}
		}
		roots = append(roots, node)
	}

	if roots == nil {
		roots = []*ProductNodeResponse{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": roots})
}

// GetProductMapNode returns one node with its refs and editors. The editor
// list is the ACL data basis (acceptance criterion 4): the frontend can render
// "editors" without a separate endpoint, and future edit handlers enforce it
// server-side via IsProductEditor.
func (h *Handler) GetProductMapNode(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	rawID := chi.URLParam(r, "id")
	nodeID, ok := parseUUIDOrBadRequest(w, rawID, "id")
	if !ok {
		return
	}

	node, err := h.Queries.GetProductNode(r.Context(), db.GetProductNodeParams{ID: nodeID, WorkspaceID: parseUUID(workspaceID)})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "product node not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get product node")
		return
	}

	refs, err := h.Queries.ListProductRefsByProduct(r.Context(), node.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list product refs")
		return
	}
	editors, err := h.Queries.ListProductEditorsByProduct(r.Context(), node.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list product editors")
		return
	}

	writeJSON(w, http.StatusOK, productNodeToResponse(node, refs, editors, nil))
}

// IsProductEditor reports whether userID is a registered editor for the
// product. MVP registers editors but does not open the edit workflow; this is
// the server-side ACL guard future edit endpoints must call, and the
// acceptance test proves an un-authorized member is rejected.
func (h *Handler) IsProductEditor(ctx context.Context, productID, userID pgtype.UUID) (bool, error) {
	return h.Queries.IsProductEditor(ctx, db.IsProductEditorParams{ProductID: productID, UserID: userID})
}
