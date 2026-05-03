package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PinnedItemResponse carries pin metadata only. Title / status / identifier /
// icon are intentionally NOT included — clients derive them from their own
// issue / project query cache so that an `issue:updated` event flows naturally
// into the sidebar without needing a cross-entity invalidate on `pinKeys`.
type PinnedItemResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	ItemType    string  `json:"item_type"`
	ItemID      string  `json:"item_id"`
	Position    float64 `json:"position"`
	CreatedAt   string  `json:"created_at"`
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

	resp := make([]PinnedItemResponse, 0, len(pins))
	for _, p := range pins {
		exists, err := h.pinnedItemExists(r, p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list pins")
			return
		}
		if !exists {
			if err := h.Queries.DeletePinnedItemByID(r.Context(), db.DeletePinnedItemByIDParams{
				WorkspaceID: p.WorkspaceID,
				UserID:      p.UserID,
				ID:          p.ID,
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to list pins")
				return
			}
			h.publish(protocol.EventPinDeleted, workspaceID, "member", userID, map[string]any{
				"item_type": p.ItemType,
				"item_id":   uuidToString(p.ItemID),
			})
			continue
		}
		resp = append(resp, pinnedItemToResponse(p))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) pinnedItemExists(r *http.Request, p db.PinnedItem) (bool, error) {
	switch p.ItemType {
	case "issue":
		_, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: p.ItemID, WorkspaceID: p.WorkspaceID,
		})
		if err == nil {
			return true, nil
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	case "project":
		_, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: p.ItemID, WorkspaceID: p.WorkspaceID,
		})
		if err == nil {
			return true, nil
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	default:
		return false, nil
	}
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

	itemUUID, ok := parseUUIDOrBadRequest(w, req.ItemID, "item_id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	// Verify the item exists in this workspace
	switch req.ItemType {
	case "issue":
		if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
	case "project":
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}

	// Get max position to append at end
	maxPos, err := h.Queries.GetMaxPinnedItemPosition(r.Context(), db.GetMaxPinnedItemPositionParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	pin, err := h.Queries.CreatePinnedItem(r.Context(), db.CreatePinnedItemParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		ItemType:    req.ItemType,
		ItemID:      itemUUID,
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

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	itemUUID, ok := parseUUIDOrBadRequest(w, itemID, "item id")
	if !ok {
		return
	}

	err := h.Queries.DeletePinnedItem(r.Context(), db.DeletePinnedItemParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		ItemType:    itemType,
		ItemID:      itemUUID,
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

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	for _, item := range req.Items {
		itemUUID, ok := parseUUIDOrBadRequest(w, item.ID, "items[].id")
		if !ok {
			return
		}
		if err := h.Queries.UpdatePinnedItemPosition(r.Context(), db.UpdatePinnedItemPositionParams{
			Position:    item.Position,
			ID:          itemUUID,
			WorkspaceID: wsUUID,
			UserID:      parseUUID(userID),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reorder pins")
			return
		}
	}

	// Fan out so other sessions (web/desktop, or a second tab) refetch
	// the pin list and pick up the new order. Without this, reorder is
	// only consistent on the originating client until a hard refresh.
	h.publish(protocol.EventPinReordered, workspaceID, "member", userID, map[string]any{"items": req.Items})

	w.WriteHeader(http.StatusNoContent)
}
