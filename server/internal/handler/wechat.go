package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/wechat"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WechatInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	BotID           string `json:"bot_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func wechatInstallationToResponse(row db.WechatInstallation) WechatInstallationResponse {
	return WechatInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		BotID:           row.BotID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListWechatInstallations (GET /api/workspaces/{id}/wechat/installations)
func (h *Handler) ListWechatInstallations(w http.ResponseWriter, r *http.Request) {
	if h.WechatInstallations == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations": []WechatInstallationResponse{},
			"configured":    false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.WechatInstallations.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wechat installations")
		return
	}
	out := make([]WechatInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, wechatInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"configured":    true,
	})
}

type CreateWechatInstallationRequest struct {
	AgentID string `json:"agent_id"`
	BotID   string `json:"bot_id"`
	Secret  string `json:"secret"`
}

// CreateWechatInstallation (POST /api/workspaces/{id}/wechat/installations)
func (h *Handler) CreateWechatInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WechatInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}

	var req CreateWechatInstallationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BotID == "" || req.Secret == "" || req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "bot_id, secret, and agent_id are required")
		return
	}

	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	inst, err := h.WechatInstallations.Create(r.Context(), wechat.CreateInstallationParams{
		WorkspaceID:     wsUUID,
		AgentID:         agentUUID,
		BotID:           req.BotID,
		Secret:          req.Secret,
		InstallerUserID: userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create wechat installation")
		return
	}

	h.publish(protocol.EventWechatInstallationCreated, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(inst.ID),
	})
	writeJSON(w, http.StatusCreated, wechatInstallationToResponse(inst))
}

// RevokeWechatInstallation (DELETE /api/workspaces/{id}/wechat/installations/{installationId})
func (h *Handler) RevokeWechatInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WechatInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	instUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}

	// Workspace-scoped check: verify the installation belongs to this workspace
	inst, err := h.WechatInstallations.Get(r.Context(), instUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "wechat installation not found")
		return
	}
	if uuidToString(inst.WorkspaceID) != uuidToString(wsUUID) {
		writeError(w, http.StatusNotFound, "wechat installation not found")
		return
	}

	if err := h.WechatInstallations.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke wechat installation")
		return
	}

	h.publish(protocol.EventWechatInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

type CreateWechatUserBindingRequest struct {
	WechatUserID string `json:"wechat_userid"`
	UserID       string `json:"user_id"`
}

// CreateWechatUserBinding (POST /api/workspaces/{id}/wechat/installations/{installationId}/bindings)
func (h *Handler) CreateWechatUserBinding(w http.ResponseWriter, r *http.Request) {
	if h.WechatInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat integration not configured")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	instUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}

	var req CreateWechatUserBindingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WechatUserID == "" || req.UserID == "" {
		writeError(w, http.StatusBadRequest, "wechat_userid and user_id are required")
		return
	}

	userUUID, ok := parseUUIDOrBadRequest(w, req.UserID, "user_id")
	if !ok {
		return
	}

	_, err := h.Queries.CreateWechatUserBinding(r.Context(), db.CreateWechatUserBindingParams{
		WorkspaceID:    wsUUID,
		MulticaUserID:  userUUID,
		InstallationID: instUUID,
		WechatUserid:   req.WechatUserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create binding")
		return
	}
	w.WriteHeader(http.StatusCreated)
}
