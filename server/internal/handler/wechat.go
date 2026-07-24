package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/wechat"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WechatInstallationResponse is the wire shape for a WeChat installation row.
// The encrypted bot_token in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it). WS lease columns are
// runtime state, not API surface, so they are omitted too.
type WechatInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	AppID           string `json:"app_id"`        // the iLink bot id, e.g. "xxxxxx@im.bot"
	IlinkUserID     string `json:"ilink_user_id"` // human-readable id of the scanning account
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func wechatInstallationToResponse(row db.ChannelInstallation) WechatInstallationResponse {
	info := wechat.DecodePublicConfig(row.Config)
	return WechatInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		AppID:           info.AppID,
		IlinkUserID:     info.IlinkUserID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListWechatInstallations (GET /api/workspaces/{id}/wechat/installations) is
// member-visible so the Integrations tab renders for non-admins. Response flags
// mirror Lark/Slack:
//   - configured: at-rest encryption key is set (WechatRegistration != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a QR-scan install needs only the at-rest key.
func (h *Handler) ListWechatInstallations(w http.ResponseWriter, r *http.Request) {
	if h.WechatRegistration == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []WechatInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.WechatRegistration.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wechat installations")
		return
	}
	out := make([]WechatInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, wechatInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// BeginWechatInstallResponse is the payload the QR-code dialog consumes. The
// frontend renders qr_code_url as a QR image and polls
// /wechat/install/{session_id}/status at the supplied cadence.
type BeginWechatInstallResponse struct {
	SessionID           string `json:"session_id"`
	QRCodeURL           string `json:"qr_code_url"`
	ExpiresInSeconds    int    `json:"expires_in_seconds"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// BeginWechatInstall (POST /api/workspaces/{id}/wechat/install/begin?agent_id=…)
// opens a new QR-login session against the WeChat iLink backend. The router only
// requires workspace membership; this handler authorizes per-agent via
// canManageAgent (the agent's owner OR a workspace owner/admin), mirroring Lark's
// device-flow begin. Returns 503 when the integration is not wired.
func (h *Handler) BeginWechatInstall(w http.ResponseWriter, r *http.Request) {
	if h.WechatRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat install not configured")
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
	agentIDStr := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentIDStr == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, agentIDStr, "agent_id")
	if !ok {
		return
	}
	// Ownership pre-check at the HTTP boundary so a malformed agent_id surfaces
	// 404 here.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	// Authorize the initiator against the target agent.
	if !h.canManageAgent(w, r, agent) {
		return
	}
	initiatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	res, err := h.WechatRegistration.BeginInstall(r.Context(), wechat.BeginInstallParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start install: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, BeginWechatInstallResponse{
		SessionID:           res.SessionID,
		QRCodeURL:           res.QRCodeURL,
		ExpiresInSeconds:    res.ExpiresInSeconds,
		PollIntervalSeconds: res.PollIntervalSeconds,
	})
}

// WechatInstallStatusResponse is the polling payload. status is one of "pending"
// | "success" | "error"; on success installation_id is populated, on error
// error_reason is a stable code (see wechat.RegistrationReason*).
type WechatInstallStatusResponse struct {
	Status         string `json:"status"`
	InstallationID string `json:"installation_id,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// GetWechatInstallStatus (GET /api/workspaces/{id}/wechat/install/{sessionId}/status)
// returns the current state of an in-flight QR-login session. The router only
// requires workspace membership; unknown / cross-workspace sessions return 404,
// which the frontend treats as "session lost, please restart".
//
// Unlike Lark, the realtime wechat_installation:created event is published HERE
// on first observation of success (the WeChat RegistrationService has no event
// bus dependency), so other open clients refresh their installations list.
func (h *Handler) GetWechatInstallStatus(w http.ResponseWriter, r *http.Request) {
	if h.WechatRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat install not configured")
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
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionId"))
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}
	state, ok := h.WechatRegistration.GetSession(wsUUID, sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "install session not found")
		return
	}
	resp := WechatInstallStatusResponse{
		Status:      string(state.Status),
		ErrorReason: state.ErrorReason,
		ErrorMessage: state.ErrorMessage,
	}
	// Publish the realtime created event on first observation of success so other
	// open clients invalidate their installations query. This handler is
	// idempotent on re-poll (the event is harmless to re-publish — clients only
	// invalidate).
	if state.Status == wechat.RegistrationStatusSuccess && state.InstallationID.Valid {
		resp.InstallationID = uuidToString(state.InstallationID)
		h.publish(protocol.EventWechatInstallationCreated, uuidToString(wsUUID), "user", userID, map[string]any{
			"id": resp.InstallationID,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// RevokeWechatInstallation (DELETE /api/workspaces/{id}/wechat/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved for
// audit; a re-install flips status back to 'active'.
func (h *Handler) RevokeWechatInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WechatRegistration == nil {
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
	// Workspace-scoped lookup so one workspace cannot revoke another's
	// installation by guessing the UUID.
	if _, err := h.WechatRegistration.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, wechat.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "wechat installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.WechatRegistration.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventWechatInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemWechatBindingTokenRequest carries the raw token the user clicked through
// from the bot's "link your account" prompt.
type RedeemWechatBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemWechatBindingTokenResponse echoes the bound workspace/installation/user
// so the frontend can confirm without a second fetch.
type RedeemWechatBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	WeChatUserID   string `json:"wechat_user_id"`
}

// RedeemWechatBindingToken (POST /api/wechat/binding/redeem) binds the WeChat
// user id carried by the token to the logged-in Multica user. The redeemer's
// identity comes from the session, not the token, so a stolen token cannot bind
// a WeChat id to an attacker's account. Failure modes map to distinct status
// codes:
//   - 410 Gone:      token unknown / consumed / expired
//   - 409 Conflict:  this WeChat id is already bound to a different user
//   - 403 Forbidden: redeemer is not a workspace member
func (h *Handler) RedeemWechatBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.WechatBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "wechat integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemWechatBindingTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	redeemed, err := h.WechatBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, wechat.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, wechat.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this WeChat account is already bound to a different Multica user")
		case errors.Is(err, wechat.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemWechatBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		WeChatUserID:   redeemed.WeChatUserID,
	})
}
