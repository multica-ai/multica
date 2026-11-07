package handler

import (
	"encoding/json"
	"errors"
	"net/http"
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
	UserID          string `json:"user_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
}

func weixinInstallationResponse(row db.ChannelInstallation) WeixinInstallationResponse {
	info := weixin.DecodePublicConfig(row.Config)
	return WeixinInstallationResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), AgentID: uuidToString(row.AgentID), BotID: info.BotID, UserID: info.UserID, InstallerUserID: uuidToString(row.InstallerUserID), Status: row.Status, InstalledAt: row.InstalledAt.Time.UTC().Format(time.RFC3339)}
}

func (h *Handler) ListWeixinInstallations(w http.ResponseWriter, r *http.Request) {
	workspace, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	out := []WeixinInstallationResponse{}
	if h.WeixinInstall != nil {
		rows, err := h.WeixinInstall.List(r.Context(), workspace)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list WeChat connections")
			return
		}
		for _, row := range rows {
			out = append(out, weixinInstallationResponse(row))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": out, "configured": h.WeixinInstall != nil, "install_supported": h.WeixinInstall != nil})
}

func (h *Handler) BeginWeixinLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WeixinInstall == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "WeChat integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspace, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	initiator, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	agent, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("agent_id"), "agent_id")
	if !ok {
		return
	}
	row, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agent, WorkspaceID: workspace})
	if err != nil || row.ArchivedAt.Valid {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	view, err := h.WeixinInstall.Begin(r.Context(), workspace, agent, initiator)
	if err != nil {
		writeWeixinError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// POST because a confirmed provider response commits the binding. Both the
// session and its QR view are scoped to the initiating signed-in admin.
func (h *Handler) PollWeixinLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WeixinInstall == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "WeChat integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspace, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	initiator, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	session, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "loginId"), "login id")
	if !ok {
		return
	}
	var body struct {
		VerifyCode string `json:"verify_code"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	view, err := h.WeixinInstall.Poll(r.Context(), session, workspace, initiator, body.VerifyCode)
	if err != nil {
		writeWeixinError(w, err)
		return
	}
	if view.Status == "confirmed" {
		h.publish(protocol.EventWeixinInstallationCreated, uuidToString(workspace), "user", userID, map[string]any{"id": view.InstallationID})
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) CancelWeixinLogin(w http.ResponseWriter, r *http.Request) {
	if h.WeixinInstall == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "WeChat integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspace, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	initiator, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	session, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "loginId"), "login id")
	if !ok {
		return
	}
	if err := h.WeixinInstall.Cancel(r.Context(), session, workspace, initiator); err != nil {
		writeWeixinError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) RevokeWeixinInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WeixinInstall == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "WeChat integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspace, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installation, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	if err := h.WeixinInstall.Revoke(r.Context(), installation, workspace); err != nil {
		writeWeixinError(w, err)
		return
	}
	h.publish(protocol.EventWeixinInstallationRevoked, uuidToString(workspace), "user", userID, map[string]any{"id": uuidToString(installation)})
	w.WriteHeader(http.StatusNoContent)
}

func writeWeixinError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, weixin.ErrNotFound):
		writeError(w, http.StatusNotFound, "WeChat login or connection not found")
	case errors.Is(err, weixin.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "enter the numeric verification code shown in WeChat")
	case errors.Is(err, weixin.ErrConflict):
		writeError(w, http.StatusConflict, "this WeChat bot is already connected; disconnect it before changing its agent")
	case errors.Is(err, weixin.ErrBusy):
		writeError(w, http.StatusConflict, "this login is being checked; try again shortly")
	case errors.Is(err, weixin.ErrAccess):
		writeError(w, http.StatusForbidden, "workspace or agent access changed")
	default:
		writeError(w, http.StatusServiceUnavailable, "could not complete WeChat login; check the server connection and try again")
	}
}
