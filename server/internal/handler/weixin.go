package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/weixin"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Weixin (WeChat) integration. The Weixin host (apps/weixin-host) runs the
// official openclaw-weixin plugin and relays each bound Weixin account to an
// agent through the Chat API, using a PAT of the member who connected it —
// so the Chats are theirs, exactly as if they had chatted in Multica. This
// handler is the access-control layer in front of the host: any member who
// may chat with an agent can connect their own Weixin to it; the installer
// or a workspace owner/admin can disconnect it.

// WeixinInstallationResponse is the wire shape of a Weixin installation.
type WeixinInstallationResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	AgentID         string  `json:"agent_id"`
	AccountID       string  `json:"account_id"`
	InstallerUserID string  `json:"installer_user_id"`
	Status          string  `json:"status"`
	LastError       *string `json:"last_error"`
	CreatedAt       string  `json:"created_at"`
}

func weixinInstallationToResponse(inst weixin.Installation) WeixinInstallationResponse {
	status := "active"
	if inst.LastError != nil && *inst.LastError != "" {
		status = "error"
	}
	return WeixinInstallationResponse{
		ID:              inst.ID,
		WorkspaceID:     inst.WorkspaceID,
		AgentID:         inst.AgentID,
		AccountID:       inst.AccountID,
		InstallerUserID: inst.InstallerUserID,
		Status:          status,
		LastError:       inst.LastError,
		CreatedAt:       inst.CreatedAt,
	}
}

// WeixinLoginResponse is the wire shape of a QR login. The owner fields the
// host tracks stay server-side.
type WeixinLoginResponse struct {
	ID                string `json:"id"`
	State             string `json:"state"`
	QRContent         string `json:"qr_content"`
	Message           string `json:"message"`
	VerifyCodeInvalid bool   `json:"verify_code_invalid"`
}

func weixinLoginToResponse(login weixin.Login) WeixinLoginResponse {
	return WeixinLoginResponse{
		ID:                login.ID,
		State:             login.State,
		QRContent:         login.QRContent,
		Message:           login.Message,
		VerifyCodeInvalid: login.VerifyCodeInvalid,
	}
}

// ListWeixinInstallations (GET /api/workspaces/{id}/weixin/installations)
// is member-visible. configured = a Weixin host is set up; install_supported
// = it answered, so a new connection can start.
func (h *Handler) ListWeixinInstallations(w http.ResponseWriter, r *http.Request) {
	empty := []WeixinInstallationResponse{}
	if h.WeixinHost == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations": empty, "configured": false, "install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.WeixinHost.ListInstallations(r.Context(), uuidToString(wsUUID))
	if err != nil {
		slog.Warn("weixin: list installations failed", "error", err)
		writeJSON(w, http.StatusOK, map[string]any{
			"installations": empty, "configured": true, "install_supported": false,
		})
		return
	}
	out := make([]WeixinInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, weixinInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out, "configured": true, "install_supported": true,
	})
}

// StartWeixinLogin (POST /api/workspaces/{id}/weixin/logins?agent_id=…)
// returns a QR code that connects the caller's Weixin to the agent.
func (h *Handler) StartWeixinLogin(w http.ResponseWriter, r *http.Request) {
	if h.WeixinHost == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "weixin integration not enabled")
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
	agent, ok := h.loadWeixinTargetAgent(w, r, userID, wsUUID, r.URL.Query().Get("agent_id"))
	if !ok {
		return
	}
	login, err := h.WeixinHost.StartLogin(r.Context(), weixin.LoginOwner{
		WorkspaceID: uuidToString(wsUUID),
		AgentID:     uuidToString(agent.ID),
		UserID:      userID,
	})
	if err != nil {
		slog.Warn("weixin: start login failed", "error", err)
		writeError(w, http.StatusBadGateway, "could not get a Weixin QR code")
		return
	}
	writeJSON(w, http.StatusCreated, weixinLoginToResponse(login))
}

// GetWeixinLogin (GET /api/workspaces/{id}/weixin/logins/{loginId}) reports
// scan progress to the member who started the login.
func (h *Handler) GetWeixinLogin(w http.ResponseWriter, r *http.Request) {
	login, _, ok := h.loadOwnWeixinLogin(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, weixinLoginToResponse(login))
}

// SubmitWeixinVerifyCodeRequest carries the number the phone shows when
// Weixin asks for one during login.
type SubmitWeixinVerifyCodeRequest struct {
	Code string `json:"code"`
}

// SubmitWeixinVerifyCode (POST …/weixin/logins/{loginId}/verify-code).
func (h *Handler) SubmitWeixinVerifyCode(w http.ResponseWriter, r *http.Request) {
	login, _, ok := h.loadOwnWeixinLogin(w, r)
	if !ok {
		return
	}
	var req SubmitWeixinVerifyCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	updated, err := h.WeixinHost.SubmitVerifyCode(r.Context(), login.ID, strings.TrimSpace(req.Code))
	if err != nil {
		if errors.Is(err, weixin.ErrConflict) {
			writeError(w, http.StatusConflict, "this login is not waiting for a verify code")
			return
		}
		writeError(w, http.StatusBadGateway, "could not reach the Weixin host")
		return
	}
	writeJSON(w, http.StatusOK, weixinLoginToResponse(updated))
}

// CompleteWeixinLogin (POST …/weixin/logins/{loginId}/complete) turns a
// scanned login into an installation: it mints the caller a PAT the host
// relays with, and revokes the tokens of installations the new one replaces.
func (h *Handler) CompleteWeixinLogin(w http.ResponseWriter, r *http.Request) {
	login, userID, ok := h.loadOwnWeixinLogin(w, r)
	if !ok {
		return
	}
	if login.State != "connected" || login.Consumed {
		writeError(w, http.StatusConflict, "this Weixin login is not ready to complete")
		return
	}
	wsUUID := parseUUID(login.WorkspaceID)
	// Access may have changed since the QR was issued.
	agent, ok := h.loadWeixinTargetAgent(w, r, userID, wsUUID, login.AgentID)
	if !ok {
		return
	}

	pat, rawToken, err := h.mintPersonalAccessToken(r.Context(), userID, "Weixin · "+agent.Name, pgtype.Timestamptz{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create relay token")
		return
	}
	created, err := h.WeixinHost.CreateInstallation(r.Context(), weixin.CreateInstallationRequest{
		LoginID:         login.ID,
		WorkspaceID:     login.WorkspaceID,
		AgentID:         login.AgentID,
		InstallerUserID: userID,
		Token:           rawToken,
		TokenID:         uuidToString(pat.ID),
	})
	if err != nil {
		if rerr := h.revokePersonalAccessToken(r.Context(), pat.ID, pat.UserID); rerr != nil {
			slog.Error("weixin: revoke unused relay token failed", "error", rerr)
		}
		if errors.Is(err, weixin.ErrConflict) {
			writeError(w, http.StatusConflict, "this Weixin login is not ready to complete")
			return
		}
		slog.Warn("weixin: create installation failed", "error", err)
		writeError(w, http.StatusBadGateway, "could not reach the Weixin host")
		return
	}
	for _, old := range created.Superseded {
		h.revokeWeixinRelayToken(r, old.TokenID, old.InstallerUserID)
	}
	writeJSON(w, http.StatusCreated, weixinInstallationToResponse(created.Installation))
}

// RevokeWeixinInstallation (DELETE …/weixin/installations/{installationId})
// disconnects a Weixin account. The installer or a workspace owner/admin may.
func (h *Handler) RevokeWeixinInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WeixinHost == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "weixin integration not enabled")
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
	installationID := chi.URLParam(r, "installationId")
	rows, err := h.WeixinHost.ListInstallations(r.Context(), uuidToString(wsUUID))
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach the Weixin host")
		return
	}
	var target *weixin.Installation
	for i := range rows {
		if rows[i].ID == installationID {
			target = &rows[i]
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "weixin installation not found")
		return
	}
	if target.InstallerUserID != userID {
		member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(wsUUID))
		if err != nil || !roleAllowed(member.Role, "owner", "admin") {
			writeError(w, http.StatusForbidden, "only the member who connected it or an admin can disconnect it")
			return
		}
	}
	if err := h.WeixinHost.DeleteInstallation(r.Context(), target.ID); err != nil && !errors.Is(err, weixin.ErrNotFound) {
		writeError(w, http.StatusBadGateway, "could not reach the Weixin host")
		return
	}
	h.revokeWeixinRelayToken(r, target.TokenID, target.InstallerUserID)
	w.WriteHeader(http.StatusNoContent)
}

// loadWeixinTargetAgent resolves the agent a Weixin account would relay to
// and checks the caller may chat with it — the relay only ever does what
// the caller could do in Multica's Chat.
func (h *Handler) loadWeixinTargetAgent(w http.ResponseWriter, r *http.Request, userID string, wsUUID pgtype.UUID, agentIDParam string) (db.Agent, bool) {
	agentID := strings.TrimSpace(agentIDParam)
	if agentID == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return db.Agent{}, false
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent_id")
	if !ok {
		return db.Agent{}, false
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return db.Agent{}, false
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return db.Agent{}, false
	}
	workspaceID := uuidToString(wsUUID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return db.Agent{}, false
	}
	return agent, true
}

// loadOwnWeixinLogin fetches a login and hides it from anyone but the member
// who started it, in the workspace it was started in.
func (h *Handler) loadOwnWeixinLogin(w http.ResponseWriter, r *http.Request) (weixin.Login, string, bool) {
	if h.WeixinHost == nil {
		writeFeatureDisabled(w, "weixin_not_configured", "weixin integration not enabled")
		return weixin.Login{}, "", false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return weixin.Login{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return weixin.Login{}, "", false
	}
	login, err := h.WeixinHost.GetLogin(r.Context(), chi.URLParam(r, "loginId"))
	if errors.Is(err, weixin.ErrNotFound) ||
		(err == nil && (login.UserID != userID || login.WorkspaceID != uuidToString(wsUUID))) {
		writeError(w, http.StatusNotFound, "weixin login not found")
		return weixin.Login{}, "", false
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach the Weixin host")
		return weixin.Login{}, "", false
	}
	return login, userID, true
}

func (h *Handler) revokeWeixinRelayToken(r *http.Request, tokenID, userID string) {
	tokenUUID, err := util.ParseUUID(tokenID)
	if err != nil {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return
	}
	if err := h.revokePersonalAccessToken(r.Context(), tokenUUID, userUUID); err != nil {
		slog.Error("weixin: revoke relay token failed", "token_id", tokenID, "error", err)
	}
}
