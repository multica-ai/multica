package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CEREBRO-PATCH(pin-enriched-response): cerebro hydrates Title/Icon/Identifier/Status
// for channel/dm/project pins (where the client doesn't otherwise have a cached
// label source for the underlying issue kind). Issues still resolve from the
// per-issue query cache when those fields are nil so `issue:updated` events
// flow naturally into the sidebar.
type PinnedItemResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	ItemType    string  `json:"item_type"`
	ItemID      string  `json:"item_id"`
	Position    float64 `json:"position"`
	CreatedAt   string  `json:"created_at"`
	Title       string  `json:"title,omitempty"`
	Identifier  *string `json:"identifier,omitempty"`
	Status      string  `json:"status,omitempty"`
	Icon        *string `json:"icon,omitempty"`
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
		// CEREBRO-PATCH(pin-enriched-response): hydrate pinned items with
		// title/identifier/status/icon and filter out deleted or kind-shifted
		// rows (channels, DMs, projects).
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
		case "channel":
			issue, err := h.Queries.GetIssue(r.Context(), p.ItemID)
			if err != nil || issue.Kind != ChannelKindChannel {
				continue // Skip deleted or kind-changed items
			}
			pr.Title = issue.Title
			channelIcon := "#"
			pr.Icon = &channelIcon
		case "dm":
			issue, err := h.Queries.GetIssue(r.Context(), p.ItemID)
			if err != nil || issue.Kind != ChannelKindDM {
				continue // Skip deleted or kind-changed items
			}
			peerName, ok := h.dmPeerName(r.Context(), issue.ID, userID)
			if !ok {
				continue // Skip orphaned DMs (no peer found — viewer not a participant or peer deleted)
			}
			pr.Title = peerName
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
	switch req.ItemType {
	case "issue", "project", "channel", "dm":
		// ok
	default:
		writeError(w, http.StatusBadRequest, "item_type must be 'issue', 'project', 'channel' or 'dm'")
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
		// CEREBRO-PATCH(pin-issue-kind-filter): pinning an "issue" must reject
		// channel/dm kinds — those have separate item_type values.
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: itemUUID, WorkspaceID: wsUUID,
		})
		if err != nil || issue.Kind != "issue" {
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
	case "channel", "dm":
		// Channels/DMs are issues underneath. Require the requester to be a
		// participant (subscriber) — non-participants must not be able to pin.
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID: parseUUID(req.ItemID), WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, req.ItemType+" not found")
			return
		}
		if (req.ItemType == "channel" && issue.Kind != ChannelKindChannel) ||
			(req.ItemType == "dm" && issue.Kind != ChannelKindDM) {
			writeError(w, http.StatusNotFound, req.ItemType+" not found")
			return
		}
		subscribed, err := h.Queries.IsIssueSubscriber(r.Context(), db.IsIssueSubscriberParams{
			IssueID:  parseUUID(req.ItemID),
			UserType: "member",
			UserID:   parseUUID(userID),
		})
		if err != nil || !subscribed {
			writeError(w, http.StatusForbidden, "not a participant of this "+req.ItemType)
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

// CEREBRO-PATCH(pin-helpers): formatIdentifier + dmPeerName power the enriched
// pin list response above.
func formatIdentifier(prefix string, number int32) string {
	if prefix == "" {
		prefix = "ISS"
	}
	return prefix + "-" + strconv.Itoa(int(number))
}

// dmPeerName returns the display name of the other member in a 1:1 DM.
// Returns ok=false if the viewer isn't a participant or the peer can't be
// resolved — the pin should be skipped in that case.
func (h *Handler) dmPeerName(ctx context.Context, issueID pgtype.UUID, viewerUserID string) (string, bool) {
	subs, err := h.Queries.ListIssueSubscribers(ctx, issueID)
	if err != nil {
		return "", false
	}
	for _, s := range subs {
		if s.UserType != "member" {
			continue
		}
		if uuidToString(s.UserID) == viewerUserID {
			continue
		}
		user, err := h.Queries.GetUser(ctx, s.UserID)
		if err != nil {
			continue
		}
		return user.Name, true
	}
	return "", false
}
