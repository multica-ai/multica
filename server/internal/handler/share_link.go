package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ShareLinkResponse is the JSON shape returned for a workspace share link.
type ShareLinkResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Code        string  `json:"code"`
	CreatedBy   string  `json:"created_by"`
	Role        string  `json:"role"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	MaxUses     *int32  `json:"max_uses,omitempty"`
	UseCount    int32   `json:"use_count"`
	IsActive    bool    `json:"is_active"`
	CreatedAt   string  `json:"created_at"`
	// Enriched
	CreatorName  string `json:"creator_name,omitempty"`
	CreatorEmail string `json:"creator_email,omitempty"`
}

type CreateShareLinkRequest struct {
	Role      string `json:"role"`
	ExpiresIn *int   `json:"expires_in,omitempty"` // hours from now
	MaxUses   *int   `json:"max_uses,omitempty"`
}

type JoinByShareLinkRequest struct {
	Code string `json:"code"`
}

func generateShareCode() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateShareLink — admin creates a shareable invite link.
// POST /api/workspaces/{id}/share-links
func (h *Handler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	var req CreateShareLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	role := "member"
	if req.Role != "" {
		r := strings.ToLower(strings.TrimSpace(req.Role))
		if r == "admin" {
			role = "admin"
		} else if r != "member" {
			writeError(w, http.StatusBadRequest, "invalid role")
			return
		}
	}

	code, err := generateShareCode()
	if err != nil {
		slog.Warn("generate share code failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}

	var expiresAt pgtype.Timestamptz
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiresAt = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(*req.ExpiresIn) * time.Hour), Valid: true}
	}

	var maxUses pgtype.Int4
	if req.MaxUses != nil && *req.MaxUses > 0 {
		maxUses = pgtype.Int4{Int32: int32(*req.MaxUses), Valid: true}
	}

	link, err := h.Queries.CreateShareLink(r.Context(), db.CreateShareLinkParams{
		WorkspaceID: requester.WorkspaceID,
		Code:        code,
		CreatedBy:   requester.UserID,
		Role:        role,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
	})
	if err != nil {
		slog.Warn("create share link failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create share link")
		return
	}

	slog.Info("share link created", append(logger.RequestAttrs(r), "share_link_id", uuidToString(link.ID), "workspace_id", workspaceID)...)

	resp := shareLinkToResponse(link)
	writeJSON(w, http.StatusCreated, resp)
}

// ListShareLinks — list active share links for a workspace (admin view).
// GET /api/workspaces/{id}/share-links
func (h *Handler) ListShareLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	rows, err := h.Queries.ListShareLinksByWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list share links")
		return
	}

	resp := make([]ShareLinkResponse, len(rows))
	for i, row := range rows {
		resp[i] = shareLinkListToResponse(row)
	}
	if resp == nil {
		resp = []ShareLinkResponse{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// RevokeShareLink — admin revokes a share link.
// DELETE /api/workspaces/{id}/share-links/{linkId}
func (h *Handler) RevokeShareLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	linkID := chi.URLParam(r, "linkId")
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	linkUUID, ok := parseUUIDOrBadRequest(w, linkID, "link id")
	if !ok {
		return
	}

	if err := h.Queries.RevokeShareLink(r.Context(), db.RevokeShareLinkParams{
		ID:          linkUUID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke share link")
		return
	}

	slog.Info("share link revoked", "share_link_id", linkID, "workspace_id", workspaceID)
	w.WriteHeader(http.StatusNoContent)
}

// JoinByShareLink — authenticated user joins a workspace via a share link.
// POST /api/share-links/join
func (h *Handler) JoinByShareLink(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req JoinByShareLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}

	link, err := h.Queries.GetShareLinkByCode(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusNotFound, "share link not found or expired")
		return
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	// Check if already a member.
	_, memberErr := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      user.ID,
		WorkspaceID: link.WorkspaceID,
	})
	if memberErr == nil {
		writeError(w, http.StatusConflict, "you are already a member of this workspace")
		return
	}

	// Use a transaction: increment use count + create member.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}
	defer tx.Rollback(r.Context())

	qtx := h.Queries.WithTx(tx)

	if err := qtx.IncrementShareLinkUseCount(r.Context(), link.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}

	member, err := qtx.CreateMember(r.Context(), db.CreateMemberParams{
		WorkspaceID: link.WorkspaceID,
		UserID:      user.ID,
		Role:        link.Role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "you are already a member of this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}

	// Mark onboarded.
	qtx.MarkUserOnboarded(r.Context(), user.ID)

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join workspace")
		return
	}

	wsID := uuidToString(link.WorkspaceID)
	slog.Info("user joined via share link", "user_id", userID, "workspace_id", wsID)

	memberResp := memberWithUserResponse(member, user)
	h.publish(protocol.EventMemberAdded, wsID, "member", userID, map[string]any{
		"member": memberResp,
	})
	h.notifyDaemonWorkspacesChanged(userID)

	writeJSON(w, http.StatusOK, map[string]any{
		"member":      memberResp,
		"workspace_id": wsID,
	})
}

func shareLinkToResponse(link db.WorkspaceShareLink) ShareLinkResponse {
	var expiresAt *string
	if link.ExpiresAt.Valid {
		s := timestampToString(link.ExpiresAt)
		expiresAt = &s
	}
	var maxUses *int32
	if link.MaxUses.Valid {
		maxUses = &link.MaxUses.Int32
	}
	return ShareLinkResponse{
		ID:          uuidToString(link.ID),
		WorkspaceID: uuidToString(link.WorkspaceID),
		Code:        link.Code,
		CreatedBy:   uuidToString(link.CreatedBy),
		Role:        link.Role,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
		UseCount:    link.UseCount,
		IsActive:    link.IsActive,
		CreatedAt:   timestampToString(link.CreatedAt),
	}
}

func shareLinkListToResponse(row db.ListShareLinksByWorkspaceRow) ShareLinkResponse {
	var expiresAt *string
	if row.ExpiresAt.Valid {
		s := timestampToString(row.ExpiresAt)
		expiresAt = &s
	}
	var maxUses *int32
	if row.MaxUses.Valid {
		maxUses = &row.MaxUses.Int32
	}
	return ShareLinkResponse{
		ID:          uuidToString(row.ID),
		WorkspaceID: uuidToString(row.WorkspaceID),
		Code:        row.Code,
		CreatedBy:   uuidToString(row.CreatedBy),
		Role:        row.Role,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
		UseCount:    row.UseCount,
		IsActive:    row.IsActive,
		CreatedAt:   timestampToString(row.CreatedAt),
		CreatorName:  row.CreatorName,
		CreatorEmail: row.CreatorEmail,
	}
}
