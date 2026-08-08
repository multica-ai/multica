package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/weixin"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WeixinInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	BotID           string `json:"bot_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
}

func weixinInstallationResponse(inst weixin.Installation) WeixinInstallationResponse {
	return WeixinInstallationResponse{
		ID: uuidToString(inst.ID), WorkspaceID: uuidToString(inst.WorkspaceID), AgentID: uuidToString(inst.AgentID),
		BotID: inst.BotID, InstallerUserID: uuidToString(inst.InstallerUserID), Status: string(inst.Status),
	}
}

func (h *Handler) ListWeixinInstallations(w http.ResponseWriter, r *http.Request) {
	if h.WeixinInstall == nil || h.WeixinLogin == nil {
		writeJSON(w, http.StatusOK, map[string]any{"installations": []WeixinInstallationResponse{}, "configured": false, "install_supported": false})
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installations, err := h.WeixinInstall.ListByWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list Weixin installations")
		return
	}
	out := make([]WeixinInstallationResponse, 0, len(installations))
	for _, inst := range installations {
		out = append(out, weixinInstallationResponse(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": out, "configured": true, "install_supported": true})
}

type BeginWeixinInstallResponse struct {
	SessionID string `json:"session_id"`
	QRCodeURL string `json:"qr_code_url"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) BeginWeixinInstall(w http.ResponseWriter, r *http.Request) {
	if h.WeixinLogin == nil {
		writeError(w, http.StatusServiceUnavailable, "Weixin integration not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("agent_id")), "agent_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	installerID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	session, err := h.WeixinLogin.Begin(r.Context(), workspaceID, agentID, installerID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start Weixin QR login")
		return
	}
	writeJSON(w, http.StatusOK, BeginWeixinInstallResponse{SessionID: session.ID, QRCodeURL: session.QRCodeURL, ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339)})
}

type WeixinInstallStatusResponse struct {
	Status       string                      `json:"status"`
	Installation *WeixinInstallationResponse `json:"installation,omitempty"`
}

func (h *Handler) GetWeixinInstallStatus(w http.ResponseWriter, r *http.Request) {
	if h.WeixinLogin == nil {
		writeError(w, http.StatusServiceUnavailable, "Weixin integration not enabled")
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	session, err := h.WeixinLogin.Status(r.Context(), chi.URLParam(r, "sessionId"), workspaceID)
	if err != nil {
		switch {
		case errors.Is(err, weixin.ErrLoginSessionNotFound):
			writeError(w, http.StatusNotFound, "Weixin login session not found")
		case errors.Is(err, weixin.ErrLoginSessionExpired):
			writeError(w, http.StatusGone, "Weixin login session expired")
		default:
			writeError(w, http.StatusBadGateway, "failed to check Weixin QR status")
		}
		return
	}
	response := WeixinInstallStatusResponse{Status: session.Status}
	if session.Installation != nil {
		inst := weixinInstallationResponse(*session.Installation)
		response.Installation = &inst
		if session.JustConfirmed {
			h.publish(protocol.EventWeixinInstallationCreated, uuidToString(session.Installation.WorkspaceID), "user", uuidToString(session.Installation.InstallerUserID), map[string]any{"id": inst.ID})
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) RevokeWeixinInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WeixinInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "Weixin integration not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	if _, err := h.WeixinInstall.GetInWorkspace(r.Context(), installationID, workspaceID); err != nil {
		writeError(w, http.StatusNotFound, "Weixin installation not found")
		return
	}
	if err := h.WeixinInstall.Revoke(r.Context(), installationID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disconnect Weixin")
		return
	}
	h.publish(protocol.EventWeixinInstallationRevoked, uuidToString(workspaceID), "user", userID, map[string]any{"id": uuidToString(installationID)})
	w.WriteHeader(http.StatusNoContent)
}
