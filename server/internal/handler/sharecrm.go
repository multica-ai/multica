package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/sharecrm"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ShareCRMInstallationResponse is the wire shape for a ShareCRM installation
// row. The encrypted App Secret in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it). SSE lease columns are
// runtime state, not API surface, so they are omitted too.
type ShareCRMInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func sharecrmInstallationToResponse(row db.ChannelInstallation) ShareCRMInstallationResponse {
	return ShareCRMInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListShareCRMInstallations (GET /api/workspaces/{id}/sharecrm/installations) is
// member-visible so the Integrations tab renders for non-admins. Response flags
// mirror Slack/DingTalk:
//   - configured: at-rest encryption key is set (ShareCRMInstall != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a BYO install needs only the at-rest key.
func (h *Handler) ListShareCRMInstallations(w http.ResponseWriter, r *http.Request) {
	if h.ShareCRMInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []ShareCRMInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.ShareCRMInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sharecrm installations")
		return
	}
	out := make([]ShareCRMInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, sharecrmInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterShareCRMBYORequest is the body for a bring-your-own-app install: the
// credentials the user pasted from their own ShareCRM open-platform bot.
// gateway_base_url is optional and defaults to the public cloud host.
type RegisterShareCRMBYORequest struct {
	AppID          string `json:"app_id"`
	AppSecret      string `json:"app_secret"`
	GatewayBaseURL string `json:"gateway_base_url,omitempty"`
}

// RegisterShareCRMBYO (POST /api/workspaces/{id}/sharecrm/install/byo?agent_id=…)
// installs a user-supplied ("bring your own") ShareCRM bot for an agent, so
// several agents can each have their own bot identity. Admin-only at the
// router. Like Slack/DingTalk's BYO path this needs only the at-rest key
// configured (ShareCRMInstall != nil).
func (h *Handler) RegisterShareCRMBYO(w http.ResponseWriter, r *http.Request) {
	if h.ShareCRMInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "sharecrm integration not enabled")
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
	// Ownership pre-check at the boundary so a wrong agent_id is a clear 404.
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	initiatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var body RegisterShareCRMBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.ShareCRMInstall.RegisterBYO(r.Context(), sharecrm.RegisterBYOParams{
		WorkspaceID:    wsUUID,
		AgentID:        agentUUID,
		InitiatorID:    initiatorUUID,
		AppID:          body.AppID,
		AppSecret:      body.AppSecret,
		GatewayBaseURL: body.GatewayBaseURL,
	})
	if err != nil {
		switch {
		case errors.Is(err, sharecrm.ErrInvalidAppID), errors.Is(err, sharecrm.ErrInvalidAppSecret):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, sharecrm.ErrRobotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this ShareCRM bot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, sharecrm.ErrRobotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this ShareCRM bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, sharecrm.ErrRobotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this ShareCRM bot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		case errors.Is(err, sharecrm.ErrCredentialValidation):
			// The access-token mint rejected the pasted credentials (a user error),
			// so guide the user to recheck them.
			writeError(w, http.StatusBadRequest, "could not verify the ShareCRM credentials — check the App ID and App Secret, and that the API base URL is reachable")
		default:
			// Encrypt / persist / unexpected failures are server-side, not the
			// user's credentials — surface a 500 instead of misreporting them as bad
			// credentials.
			writeError(w, http.StatusInternalServerError, "could not connect the ShareCRM bot")
		}
		return
	}
	// Broadcast so every open client (Settings, Agent Integrations, other tabs)
	// invalidates its installations query and shows the new bot — matching the
	// revoke event and Slack/DingTalk install semantics.
	h.publishShareCRMInstallationCreated(row, userID)
	writeJSON(w, http.StatusOK, sharecrmInstallationToResponse(row))
}

// publishShareCRMInstallationCreated emits sharecrm_installation:created for a
// newly connected bot. The realtime layer fans it out to the workspace; the web
// app listens on sharecrm_installation:* to invalidate the installations query.
func (h *Handler) publishShareCRMInstallationCreated(row db.ChannelInstallation, actorID string) {
	h.publish(protocol.EventShareCRMInstallationCreated, uuidToString(row.WorkspaceID), "user", actorID, map[string]any{
		"id": uuidToString(row.ID),
	})
}

// RevokeShareCRMInstallation (DELETE /api/workspaces/{id}/sharecrm/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved for
// audit; a re-install (re-pasting the bot's credentials) flips status back to
// 'active'.
func (h *Handler) RevokeShareCRMInstallation(w http.ResponseWriter, r *http.Request) {
	if h.ShareCRMInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "sharecrm integration not configured")
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
	if _, err := h.ShareCRMInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, sharecrm.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "sharecrm installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.ShareCRMInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventShareCRMInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

type RedeemShareCRMBindingTokenRequest struct {
	Token string `json:"token"`
}

type RedeemShareCRMBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	ShareCRMUserID string `json:"sharecrm_user_id"`
}

// RedeemShareCRMBindingToken POST /api/sharecrm/binding/redeem
func (h *Handler) RedeemShareCRMBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.ShareCRMBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "sharecrm integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemShareCRMBindingTokenRequest
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

	redeemed, err := h.ShareCRMBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, sharecrm.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, sharecrm.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this ShareCRM account is already bound to a different Multica user")
		case errors.Is(err, sharecrm.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemShareCRMBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		ShareCRMUserID: redeemed.ShareCRMUserID,
	})
}
