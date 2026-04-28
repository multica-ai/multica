package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type PinnedItemResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	ItemType    string  `json:"item_type"`
	ItemID      string  `json:"item_id"`
	Position    float64 `json:"position"`
	CreatedAt   string  `json:"created_at"`
	// Enriched fields (set by list endpoint)
	Title      string  `json:"title"`
	Identifier *string `json:"identifier,omitempty"`
	Icon       *string `json:"icon,omitempty"`
	Status     string  `json:"status,omitempty"`
}

func pinnedItemToResponse(p db.PinnedItem) PinnedItemResponse {
	return PinnedItemResponse{
		ID:          uuidToString(p.ID),
		WorkspaceID: uuidToString(p.WorkspaceID),
		UserID:      uuidToString(p.UserID),
		ItemType:    p.ItemType,
		ItemID:      uuidToString(p.ItemID),
		Position:    p.Position,
		CreatedAt:   timestampToString(p.CreatedAt),
	}
}

type CreatePinRequest struct {
	ItemType string `json:"item_type"`
	ItemID   string `json:"item_id"`
}

type ReorderPinsRequest struct {
	Items []ReorderItem `json:"items"`
}

type ReorderItem struct {
	ID       string  `json:"id"`
	Position float64 `json:"position"`
}

func (h *Handler) ListPins(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	pins, err := h.Queries.ListPinnedItems(r.Context(), db.ListPinnedItemsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pins")
		return
	}

	// Enrich with item details
	resp := make([]PinnedItemResponse, 0, len(pins))
	for _, p := range pins {
		pr := pinnedItemToResponse(p)
		switch p.ItemType {
		case "issue":
			issue, err := h.Queries.GetIssue(r.Context(), p.ItemID)
			if err != nil {
				continue // Skip deleted items
			}
			pr.Title = issue.Title
			prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
			identifier := formatIdentifier(prefix, issue.Number)
			pr.Identifier = &identifier
			pr.Status = issue.Status
		case "project":
			project, err := h.Queries.GetProject(r.Context(), p.ItemID)
			if err != nil {
				continue // Skip deleted items
			}
			pr.Title = project.Title
			pr.Icon = textToPtr(project.Icon)
			pr.Status = project.Status
		}
		resp = append(resp, pr)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreatePin(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	var req CreatePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ItemType != "issue" && req.ItemType != "project" {
		writeError(w, http.StatusBadRequest, "item_type must be 'issue' or 'project'")
		return
	}
	if req.ItemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return
	}

	// Verify the item exists in this workspace
	switch req.ItemType {
	case "issue":
		if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: parseUUID(req.ItemID), WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
	case "project":
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: parseUUID(req.ItemID), WorkspaceID: parseUUID(workspaceID),
		}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}

	// Get max position to append at end
	maxPos, err := h.Queries.GetMaxPinnedItemPosition(r.Context(), db.GetMaxPinnedItemPositionParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	pin, err := h.Queries.CreatePinnedItem(r.Context(), db.CreatePinnedItemParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		ItemType:    req.ItemType,
		ItemID:      parseUUID(req.ItemID),
		Position:    maxPos + 1,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "item already pinned")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create pin")
		return
	}

	resp := pinnedItemToResponse(pin)
	h.publish(protocol.EventPinCreated, workspaceID, "member", userID, map[string]any{"pin": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) DeletePin(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	itemType := chi.URLParam(r, "itemType")
	itemID := chi.URLParam(r, "itemId")

	err := h.Queries.DeletePinnedItem(r.Context(), db.DeletePinnedItemParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		ItemType:    itemType,
		ItemID:      parseUUID(itemID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete pin")
		return
	}

	h.publish(protocol.EventPinDeleted, workspaceID, "member", userID, map[string]any{
		"item_type": itemType,
		"item_id":   itemID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ReorderPins(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)

	var req ReorderPinsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	for _, item := range req.Items {
		if err := h.Queries.UpdatePinnedItemPosition(r.Context(), db.UpdatePinnedItemPositionParams{
			Position:    item.Position,
			ID:          parseUUID(item.ID),
			WorkspaceID: parseUUID(workspaceID),
			UserID:      parseUUID(userID),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder pins")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func formatIdentifier(prefix string, number int32) string {
	if prefix == "" {
		prefix = "ISS"
	}
	return prefix + "-" + strconv.Itoa(int(number))
}
