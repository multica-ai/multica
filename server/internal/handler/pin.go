package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/projectaccess"
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

	// Capability opt-in: clients built before saved views classified every
	// non-issue pin as a project pin, fetched its detail, got 404, and
	// permanently auto-unpinned it. Exposing item_type=view through the old
	// contract would let an old Desktop DESTROY the user's view pins just by
	// opening the sidebar — so view rows only ship to clients that declare
	// they understand them (?include=view).
	includeViews := strings.Contains(r.URL.Query().Get("include"), "view")

	// Pins on private projects (or their issues) the caller cannot see are
	// left out rather than deleted, so they come back if access does.
	hidden, ok := h.requestHiddenProjects(w, r, workspaceID)
	if !ok {
		return
	}

	resp := make([]PinnedItemResponse, 0, len(pins))
	for _, p := range pins {
		if p.ItemType == "view" && !includeViews {
			continue
		}
		if !hidden.Empty() && !h.pinVisible(r, hidden, p) {
			continue
		}
		resp = append(resp, pinnedItemToResponse(p))
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
	if req.ItemType != "issue" && req.ItemType != "project" && req.ItemType != "view" {
		writeError(w, http.StatusBadRequest, "item_type must be 'issue', 'project' or 'view'")
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
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		if !h.requireProjectVisible(w, r, workspaceID, issue.ProjectID, http.StatusNotFound, "issue not found") {
			return
		}
	case "project":
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		if !h.requireProjectVisible(w, r, workspaceID, itemUUID, http.StatusNotFound, "project not found") {
			return
		}
	case "view":
		// Same read rule as the view endpoints: your own views, or views
		// shared to the workspace. Foreign private views 404 — a pin must
		// never confirm their existence.
		view, err := h.Queries.GetIssueView(r.Context(), db.GetIssueViewParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		})
		if err != nil || !canReadIssueView(view, parseUUID(userID)) {
			writeError(w, http.StatusNotFound, "view not found")
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
	h.publishPin(protocol.EventPinCreated, workspaceID, userID, map[string]any{"pin": resp})
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

	h.publishPin(protocol.EventPinDeleted, workspaceID, userID, map[string]any{
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
	h.publishPin(protocol.EventPinReordered, workspaceID, userID, map[string]any{"items": req.Items})

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) pinVisible(r *http.Request, hidden projectaccess.Hidden, pin db.PinnedItem) bool {
	switch pin.ItemType {
	case "project":
		return hidden.CanSeeProject(pin.ItemID)
	case "issue":
		projectID, err := h.Queries.GetIssueProjectID(r.Context(), pin.ItemID)
		if err != nil {
			// A missing issue keeps today's behavior (the client prunes it);
			// a read failure hides the pin rather than risk showing it.
			return errors.Is(err, pgx.ErrNoRows)
		}
		return hidden.CanSeeProject(projectID)
	default:
		return true
	}
}

// publishPin sends a pin event to its member's own sessions only: a pin is
// that member's, and its item id may name a private project or issue.
func (h *Handler) publishPin(eventType, workspaceID, userID string, payload any) {
	h.Bus.Publish(events.Event{
		Type:                eventType,
		WorkspaceID:         workspaceID,
		ActorType:           "member",
		ActorID:             userID,
		MemberPrivateUserID: userID,
		Payload:             payload,
	})
}
