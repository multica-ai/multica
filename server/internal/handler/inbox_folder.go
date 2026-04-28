package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type InboxFolderResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	Name        string  `json:"name"`
	Position    float64 `json:"position"`
	CreatedAt   string  `json:"created_at"`
	ParentID    *string `json:"parent_id"`
}

func inboxFolderToResponse(f db.InboxFolder) InboxFolderResponse {
	return InboxFolderResponse{
		ID:          uuidToString(f.ID),
		WorkspaceID: uuidToString(f.WorkspaceID),
		UserID:      uuidToString(f.UserID),
		Name:        f.Name,
		Position:    f.Position,
		CreatedAt:   timestampToString(f.CreatedAt),
		ParentID:    uuidToPtr(f.ParentID),
	}
}

type CreateInboxFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

type UpdateInboxFolderRequest struct {
	Name *string `json:"name"`
}

type SetInboxFolderParentRequest struct {
	// Empty string = top-level (no parent).
	ParentID string `json:"parent_id"`
}

type AddInboxFolderItemRequest struct {
	ItemType string `json:"item_type"`
	ItemID   string `json:"item_id"`
}

type InboxFolderMembershipResponse struct {
	FolderID string `json:"folder_id"`
	ItemType string `json:"item_type"`
	ItemID   string `json:"item_id"`
	AddedAt  string `json:"added_at"`
}

func validInboxFolderItemType(t string) bool {
	return t == "chat_session" || t == "notification"
}

func (h *Handler) ListInboxFolders(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	folders, err := h.Queries.ListInboxFolders(r.Context(), db.ListInboxFoldersParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}

	resp := make([]InboxFolderResponse, len(folders))
	for i, f := range folders {
		resp[i] = inboxFolderToResponse(f)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateInboxFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req CreateInboxFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	maxPos, err := h.Queries.GetMaxInboxFolderPosition(r.Context(), db.GetMaxInboxFolderPositionParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get position")
		return
	}

	parentID := pgtype.UUID{}
	if req.ParentID != nil && *req.ParentID != "" {
		if _, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
			ID:          parseUUID(*req.ParentID),
			WorkspaceID: parseUUID(workspaceID),
			UserID:      parseUUID(userID),
		}); err != nil {
			writeError(w, http.StatusBadRequest, "parent folder not found")
			return
		}
		parentID = parseUUID(*req.ParentID)
	}

	folder, err := h.Queries.CreateInboxFolder(r.Context(), db.CreateInboxFolderParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Name:        name,
		Position:    maxPos + 1,
		ParentID:    parentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}

	resp := inboxFolderToResponse(folder)
	h.publish(protocol.EventInboxFolderCreated, workspaceID, "member", userID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateInboxFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	folderID := chi.URLParam(r, "folderId")

	var req UpdateInboxFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == nil {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	name := strings.TrimSpace(*req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name cannot be empty")
		return
	}

	folder, err := h.Queries.RenameInboxFolder(r.Context(), db.RenameInboxFolderParams{
		Name:        name,
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	resp := inboxFolderToResponse(folder)
	h.publish(protocol.EventInboxFolderUpdated, workspaceID, "member", userID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) SetInboxFolderParent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	folderID := chi.URLParam(r, "folderId")

	var req SetInboxFolderParentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if _, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	parentID := pgtype.UUID{}
	if req.ParentID != "" {
		if req.ParentID == folderID {
			writeError(w, http.StatusBadRequest, "folder cannot be its own parent")
			return
		}
		// Verify the proposed parent exists and belongs to the same user.
		parent, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
			ID:          parseUUID(req.ParentID),
			WorkspaceID: parseUUID(workspaceID),
			UserID:      parseUUID(userID),
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "parent folder not found")
			return
		}
		// Walk up from the proposed parent. If we ever reach folderID, the move
		// would create a cycle (i.e. parent is a descendant of folderID).
		// Bounded loop guards against pre-existing data corruption.
		current := parent
		for i := 0; i < 100 && current.ParentID.Valid; i++ {
			if uuidToString(current.ParentID) == folderID {
				writeError(w, http.StatusBadRequest, "cannot move folder into its own descendant")
				return
			}
			next, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
				ID:          current.ParentID,
				WorkspaceID: parseUUID(workspaceID),
				UserID:      parseUUID(userID),
			})
			if err != nil {
				break
			}
			current = next
		}
		parentID = parseUUID(req.ParentID)
	}

	folder, err := h.Queries.UpdateInboxFolderParent(r.Context(), db.UpdateInboxFolderParentParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		ParentID:    parentID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update folder parent")
		return
	}

	resp := inboxFolderToResponse(folder)
	h.publish(protocol.EventInboxFolderUpdated, workspaceID, "member", userID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteInboxFolder(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	folderID := chi.URLParam(r, "folderId")

	if _, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	if err := h.Queries.DeleteInboxFolder(r.Context(), db.DeleteInboxFolderParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}

	h.publish(protocol.EventInboxFolderDeleted, workspaceID, "member", userID, map[string]any{"folder_id": folderID})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AddInboxFolderItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	folderID := chi.URLParam(r, "folderId")

	if _, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	var req AddInboxFolderItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validInboxFolderItemType(req.ItemType) {
		writeError(w, http.StatusBadRequest, "item_type must be 'chat_session' or 'notification'")
		return
	}
	if req.ItemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required")
		return
	}

	// "Move" semantics: remove from any other folder this user owns first.
	if err := h.Queries.RemoveItemFromAllFolders(r.Context(), db.RemoveItemFromAllFoldersParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		ItemType:    req.ItemType,
		ItemID:      parseUUID(req.ItemID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear existing memberships")
		return
	}

	if err := h.Queries.AddItemToFolder(r.Context(), db.AddItemToFolderParams{
		FolderID: parseUUID(folderID),
		ItemType: req.ItemType,
		ItemID:   parseUUID(req.ItemID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add item to folder")
		return
	}

	h.publish(protocol.EventInboxFolderItemAdded, workspaceID, "member", userID, map[string]any{
		"folder_id": folderID,
		"item_type": req.ItemType,
		"item_id":   req.ItemID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RemoveInboxFolderItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	folderID := chi.URLParam(r, "folderId")
	itemType := chi.URLParam(r, "itemType")
	itemID := chi.URLParam(r, "itemId")

	if _, err := h.Queries.GetInboxFolder(r.Context(), db.GetInboxFolderParams{
		ID:          parseUUID(folderID),
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusNotFound, "folder not found")
		return
	}

	if err := h.Queries.RemoveItemFromFolder(r.Context(), db.RemoveItemFromFolderParams{
		FolderID: parseUUID(folderID),
		ItemType: itemType,
		ItemID:   parseUUID(itemID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove item from folder")
		return
	}

	h.publish(protocol.EventInboxFolderItemRemoved, workspaceID, "member", userID, map[string]any{
		"folder_id": folderID,
		"item_type": itemType,
		"item_id":   itemID,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListInboxFolderMemberships(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	rows, err := h.Queries.ListInboxFolderMemberships(r.Context(), db.ListInboxFolderMembershipsParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list memberships")
		return
	}

	resp := make([]InboxFolderMembershipResponse, len(rows))
	for i, m := range rows {
		resp[i] = InboxFolderMembershipResponse{
			FolderID: uuidToString(m.FolderID),
			ItemType: m.ItemType,
			ItemID:   uuidToString(m.ItemID),
			AddedAt:  timestampToString(m.AddedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
