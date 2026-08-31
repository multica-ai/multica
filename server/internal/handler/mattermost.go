package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/mattermost"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MattermostInstallationResponse is the wire shape for a Mattermost
// installation row. The encrypted access token in config is INTENTIONALLY
// absent — it is server-internal. WS lease columns are runtime state, not API
// surface. The server URL IS included: unlike Slack or Telegram, it is the
// only thing that tells an admin which deployment a row belongs to.
type MattermostInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	ServerURL       string `json:"server_url"`
	BotUserID       string `json:"bot_user_id"`
	BotUsername     string `json:"bot_username"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func mattermostInstallationToResponse(row db.ChannelInstallation) MattermostInstallationResponse {
	info := mattermost.DecodePublicConfig(row.Config)
	return MattermostInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		ServerURL:       info.ServerURL,
		BotUserID:       info.BotUserID,
		BotUsername:     info.BotUsername,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListMattermostInstallations
// (GET /api/workspaces/{id}/mattermost/installations) is member-visible so the
// Integrations tab renders for non-admins. Response flags mirror Telegram:
// configured = at-rest key set; install_supported is true whenever configured
// (paste-a-URL-and-token needs no hosted credential).
func (h *Handler) ListMattermostInstallations(w http.ResponseWriter, r *http.Request) {
	if h.MattermostInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []MattermostInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.MattermostInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list mattermost installations")
		return
	}
	out := make([]MattermostInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, mattermostInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterMattermostRequest is the body for a bot install: the server URL and
// the access token the admin generated for a Mattermost bot account.
type RegisterMattermostRequest struct {
	ServerURL   string `json:"server_url"`
	AccessToken string `json:"access_token"`
}

// RegisterMattermostBot
// (POST /api/workspaces/{id}/mattermost/install?agent_id=…) installs a
// user-supplied Mattermost bot for an agent. Admin-only at the router.
// Mirrors RegisterTelegramBot.
func (h *Handler) RegisterMattermostBot(w http.ResponseWriter, r *http.Request) {
	if h.MattermostInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "mattermost integration not enabled")
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
	var body RegisterMattermostRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.MattermostInstall.Register(r.Context(), mattermost.RegisterParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		ServerURL:   body.ServerURL,
		AccessToken: body.AccessToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, mattermost.ErrInvalidServerURL):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, mattermost.ErrInvalidAccessToken):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, mattermost.ErrNotABotAccount):
			writeError(w, http.StatusBadRequest, "this token belongs to a user account — create a bot account in the Mattermost System Console and use its access token")
		case errors.Is(err, mattermost.ErrCredentialsRejected):
			writeError(w, http.StatusBadRequest, "Mattermost rejected this access token — generate a current token for the bot account and try again")
		case errors.Is(err, mattermost.ErrCredentialsUnverifiable):
			writeError(w, http.StatusServiceUnavailable, "could not reach this Mattermost server to verify the bot — check the server URL, network or proxy and try again; nothing was saved")
		case errors.Is(err, mattermost.ErrBotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this Mattermost bot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, mattermost.ErrBotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this Mattermost bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, mattermost.ErrBotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this Mattermost bot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		default:
			writeError(w, http.StatusInternalServerError, "could not save this Mattermost bot — something went wrong on the server; nothing was saved")
		}
		return
	}
	// Broadcast so every open client invalidates its installations query and
	// shows the new bot — matching the Slack and Telegram install semantics.
	h.publish(protocol.EventMattermostInstallationCreated, uuidToString(row.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(row.ID),
	})
	writeJSON(w, http.StatusOK, mattermostInstallationToResponse(row))
}

// RevokeMattermostInstallation
// (DELETE /api/workspaces/{id}/mattermost/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved
// for audit and chat history stays in Multica; a re-install (re-pasting the
// server URL and token) flips status back to 'active'.
func (h *Handler) RevokeMattermostInstallation(w http.ResponseWriter, r *http.Request) {
	if h.MattermostInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "mattermost integration not configured")
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
	if _, err := h.MattermostInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, mattermost.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "mattermost installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.MattermostInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventMattermostInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemMattermostBindingTokenRequest carries the raw token from the bot's
// "link your account" prompt.
type RedeemMattermostBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemMattermostBindingTokenResponse echoes the bound identifiers so the
// frontend can confirm without a second fetch.
type RedeemMattermostBindingTokenResponse struct {
	WorkspaceID      string `json:"workspace_id"`
	InstallationID   string `json:"installation_id"`
	MattermostUserID string `json:"mattermost_user_id"`
}

// RedeemMattermostBindingToken (POST /api/mattermost/binding/redeem) binds the
// Mattermost user id carried by the token to the logged-in Multica user. The
// redeemer's identity comes from the session, not the token. Status codes
// mirror the Slack and Telegram redeem handlers.
func (h *Handler) RedeemMattermostBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.MattermostBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "mattermost integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemMattermostBindingTokenRequest
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
	redeemed, err := h.MattermostBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, mattermost.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, mattermost.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Mattermost account is already bound to a different Multica user")
		case errors.Is(err, mattermost.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemMattermostBindingTokenResponse{
		WorkspaceID:      uuidToString(redeemed.WorkspaceID),
		InstallationID:   uuidToString(redeemed.InstallationID),
		MattermostUserID: redeemed.MattermostUserID,
	})
}
