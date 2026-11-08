package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/discord"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DiscordInstallationResponse is the wire shape for a Discord installation
// row. The encrypted bot token in config is INTENTIONALLY absent — it is
// server-internal. WS lease columns are runtime state, not API surface.
type DiscordInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	BotID           string `json:"bot_id"`
	BotUsername     string `json:"bot_username"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func discordInstallationToResponse(row db.ChannelInstallation) DiscordInstallationResponse {
	info := discord.DecodePublicConfig(row.Config)
	return DiscordInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		BotID:           info.BotID,
		BotUsername:     info.BotUsername,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListDiscordInstallations (GET /api/workspaces/{id}/discord/installations) is
// member-visible so the Integrations tab renders for non-admins. Response flags
// mirror Telegram: configured = at-rest key set; install_supported is true
// whenever configured (paste-a-token needs no hosted credential).
func (h *Handler) ListDiscordInstallations(w http.ResponseWriter, r *http.Request) {
	if h.DiscordInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []DiscordInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.DiscordInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list discord installations")
		return
	}
	out := make([]DiscordInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, discordInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterDiscordRequest is the body for a bot install: the token the user
// pasted from the Discord developer portal.
type RegisterDiscordRequest struct {
	BotToken string `json:"bot_token"`
}

// RegisterDiscordBot (POST /api/workspaces/{id}/discord/install?agent_id=…)
// installs a user-supplied Discord bot for an agent. Admin-only at the router.
func (h *Handler) RegisterDiscordBot(w http.ResponseWriter, r *http.Request) {
	if h.DiscordInstall == nil {
		writeFeatureDisabled(w, "discord_not_configured", "discord integration not enabled")
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
	var body RegisterDiscordRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.DiscordInstall.Register(r.Context(), discord.RegisterParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		BotToken:    body.BotToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, discord.ErrInvalidBotToken):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, discord.ErrCredentialsRejected):
			writeError(w, http.StatusBadRequest, "Discord rejected this bot token — reset it in the Discord developer portal and try again")
		case errors.Is(err, discord.ErrCredentialsUnverifiable):
			writeError(w, http.StatusServiceUnavailable, "could not reach Discord to verify this bot — check the server network or proxy and try again; the token was not saved")
		case errors.Is(err, discord.ErrBotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this Discord bot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, discord.ErrBotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this Discord bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, discord.ErrBotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this Discord bot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		default:
			writeError(w, http.StatusInternalServerError, "could not save this Discord bot — something went wrong on the server; the token was not saved")
		}
		return
	}
	// Broadcast so every open client invalidates its installations query and
	// shows the new bot — matching the Slack/Telegram install semantics.
	h.publish(protocol.EventDiscordInstallationCreated, uuidToString(row.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(row.ID),
	})
	writeJSON(w, http.StatusOK, discordInstallationToResponse(row))
}

// RevokeDiscordInstallation (DELETE /api/workspaces/{id}/discord/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved for
// audit and chat history stays in Multica; a re-install flips status back to
// 'active'.
func (h *Handler) RevokeDiscordInstallation(w http.ResponseWriter, r *http.Request) {
	if h.DiscordInstall == nil {
		writeFeatureDisabled(w, "discord_not_configured", "discord integration not configured")
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
	if _, err := h.DiscordInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, discord.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "discord installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.DiscordInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventDiscordInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemDiscordBindingTokenRequest carries the raw token from the bot's "link
// your account" prompt.
type RedeemDiscordBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemDiscordBindingTokenResponse echoes the bound identifiers so the
// frontend can confirm without a second fetch.
type RedeemDiscordBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	DiscordUserID  string `json:"discord_user_id"`
}

// RedeemDiscordBindingToken (POST /api/discord/binding/redeem) binds the
// Discord user id carried by the token to the logged-in Multica user. The
// redeemer's identity comes from the session, not the token. Status codes
// mirror the Slack/Telegram redeem handlers.
func (h *Handler) RedeemDiscordBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.DiscordBindingTokens == nil {
		writeFeatureDisabled(w, "discord_not_configured", "discord integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemDiscordBindingTokenRequest
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
	redeemed, err := h.DiscordBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, discord.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, discord.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Discord account is already bound to a different Multica user")
		case errors.Is(err, discord.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemDiscordBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		DiscordUserID:  redeemed.DiscordUserID,
	})
}
