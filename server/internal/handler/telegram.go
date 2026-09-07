package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TelegramInstallationResponse is the wire shape for a Telegram installation
// row. The encrypted bot token in config is INTENTIONALLY absent — it is
// server-internal. WS lease columns are runtime state, not API surface.
type TelegramInstallationResponse struct {
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

func telegramInstallationToResponse(row db.ChannelInstallation) TelegramInstallationResponse {
	info := telegram.DecodePublicConfig(row.Config)
	return TelegramInstallationResponse{
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

// ListTelegramInstallations (GET /api/workspaces/{id}/telegram/installations)
// is member-visible so the Integrations tab renders for non-admins. Response
// flags mirror Slack: configured = at-rest key set; install_supported is true
// whenever configured (paste-a-token needs no hosted credential).
func (h *Handler) ListTelegramInstallations(w http.ResponseWriter, r *http.Request) {
	if h.TelegramInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []TelegramInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.TelegramInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list telegram installations")
		return
	}
	out := make([]TelegramInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, telegramInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterTelegramRequest is the body for a bot install: the token the user
// pasted from @BotFather.
type RegisterTelegramRequest struct {
	BotToken string `json:"bot_token"`
}

// RegisterTelegramBot (POST /api/workspaces/{id}/telegram/install?agent_id=…)
// installs a user-supplied Telegram bot for an agent. Admin-only at the
// router. Mirrors RegisterSlackBYO.
func (h *Handler) RegisterTelegramBot(w http.ResponseWriter, r *http.Request) {
	if h.TelegramInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "telegram integration not enabled")
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
	var body RegisterTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.TelegramInstall.Register(r.Context(), telegram.RegisterParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		BotToken:    body.BotToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, telegram.ErrInvalidBotToken):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, telegram.ErrCredentialsRejected):
			writeError(w, http.StatusBadRequest, "Telegram rejected this bot token — generate a current token in @BotFather and try again")
		case errors.Is(err, telegram.ErrCredentialsUnverifiable):
			writeError(w, http.StatusServiceUnavailable, "could not reach Telegram to verify this bot — check the server network or proxy and try again; the token was not saved")
		case errors.Is(err, telegram.ErrBotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this Telegram bot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, telegram.ErrBotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this Telegram bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, telegram.ErrBotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this Telegram bot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		case errors.Is(err, telegram.ErrWebhookConfigured):
			writeError(w, http.StatusBadRequest, "this Telegram bot has a webhook configured — remove the webhook before connecting it with long polling")
		default:
			writeError(w, http.StatusInternalServerError, "could not save this Telegram bot — something went wrong on the server; the token was not saved")
		}
		return
	}
	// Broadcast so every open client invalidates its installations query and
	// shows the new bot — matching the Slack install semantics.
	h.publish(protocol.EventTelegramInstallationCreated, uuidToString(row.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(row.ID),
	})
	writeJSON(w, http.StatusOK, telegramInstallationToResponse(row))
}

// RevokeTelegramInstallation (DELETE /api/workspaces/{id}/telegram/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved
// for audit and chat history stays in Multica; a re-install (re-pasting the
// bot's token) flips status back to 'active'.
func (h *Handler) RevokeTelegramInstallation(w http.ResponseWriter, r *http.Request) {
	if h.TelegramInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "telegram integration not configured")
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
	if _, err := h.TelegramInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, telegram.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "telegram installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.TelegramInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventTelegramInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemTelegramBindingTokenRequest carries the raw token from the bot's
// "link your account" prompt.
type RedeemTelegramBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemTelegramBindingTokenResponse echoes the bound identifiers so the
// frontend can confirm without a second fetch.
type RedeemTelegramBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	TelegramUserID string `json:"telegram_user_id"`
}

// RedeemTelegramBindingToken (POST /api/telegram/binding/redeem) binds the
// Telegram user id carried by the token to the logged-in Multica user. The
// redeemer's identity comes from the session, not the token. Status codes
// mirror the Slack redeem handler.
func (h *Handler) RedeemTelegramBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.TelegramBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "telegram integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemTelegramBindingTokenRequest
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
	redeemed, err := h.TelegramBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, telegram.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, telegram.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Telegram account is already bound to a different Multica user")
		case errors.Is(err, telegram.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemTelegramBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		TelegramUserID: redeemed.TelegramUserID,
	})
}
