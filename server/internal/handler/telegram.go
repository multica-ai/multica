package handler

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/telegram"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TelegramInstallationResponse is the wire shape for a Telegram installation
// row. The encrypted bot token in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it).
type TelegramInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
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
// flags mirror Slack/Lark:
//   - configured: at-rest encryption key is set (TelegramInstall != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a BYO install needs only the at-rest key (no hosted OAuth creds).
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

// RegisterTelegramBYORequest is the body for a bring-your-own-bot install:
// the single bot token pasted from BotFather. Unlike Slack, Telegram has no
// separate app-level token.
type RegisterTelegramBYORequest struct {
	BotToken string `json:"bot_token"`
}

// RegisterTelegramBYO (POST /api/workspaces/{id}/telegram/install/byo?agent_id=…)
// installs a user-supplied Telegram bot for an agent, so several agents can
// each have their own bot identity. Admin-only at the router. This needs only
// the at-rest key configured (TelegramInstall != nil) — there is no hosted
// OAuth path for Telegram.
func (h *Handler) RegisterTelegramBYO(w http.ResponseWriter, r *http.Request) {
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
	var body RegisterTelegramBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.TelegramInstall.RegisterBYO(r.Context(), telegram.RegisterBYOParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		BotToken:    body.BotToken,
	})
	if err != nil {
		switch {
		case errors.Is(err, telegram.ErrInvalidBotToken):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, telegram.ErrBotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this bot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, telegram.ErrBotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this bot is connected to an archived agent in this workspace — restore that agent, or disconnect its bot, before connecting it here")
		case errors.Is(err, telegram.ErrBotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this bot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		default:
			// The dominant non-sentinel failure here is getMe rejecting the
			// pasted bot token (a user error), so guide the user to recheck it
			// rather than surfacing an opaque 500.
			writeError(w, http.StatusBadRequest, "could not verify the bot token — check that it is correct and the bot is not deleted")
		}
		return
	}
	// Broadcast so every open client (Settings, Agent Integrations, other tabs)
	// invalidates its installations query and shows the new bot — matching the
	// revoke event and Slack's install semantics. The installer's own tab also
	// invalidates locally, but other clients rely on this event.
	h.publishTelegramInstallationCreated(row, userID)
	writeJSON(w, http.StatusOK, telegramInstallationToResponse(row))
}

// publishTelegramInstallationCreated emits telegram_installation:created for a
// newly connected bot. The realtime layer fans it out to the workspace; the
// web app listens on telegram_installation:* to invalidate the installations
// query.
func (h *Handler) publishTelegramInstallationCreated(row db.ChannelInstallation, actorID string) {
	h.publish(protocol.EventTelegramInstallationCreated, uuidToString(row.WorkspaceID), "user", actorID, map[string]any{
		"id": uuidToString(row.ID),
	})
}

// RevokeTelegramInstallation (DELETE /api/workspaces/{id}/telegram/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. The row is preserved
// for audit; a re-install (re-pasting the bot's token) flips status back to
// 'active'.
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
	row, err := h.TelegramInstall.GetInWorkspace(r.Context(), instUUID, wsUUID)
	if err != nil {
		if errors.Is(err, telegram.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "telegram installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.TelegramInstall.Revoke(r.Context(), row); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventTelegramInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// channelHandler is the inbound seam the Telegram webhook routes through.
// *engine.Router satisfies it; wired in production and swapped in tests.
type channelHandler interface {
	Handle(ctx context.Context, msg channel.InboundMessage) error
}

// SetWebhookChannelHandler wires the inbound seam the Telegram webhook routes
// through. router.go calls this with the shared *engine.Router (the same
// instance assigned to ChannelRouter) once Telegram is enabled
// (MULTICA_TELEGRAM_SECRET_KEY set). The field itself stays unexported so
// only this package's webhook handler and its tests touch it directly.
func (h *Handler) SetWebhookChannelHandler(handler channelHandler) {
	h.webhookChannelHandler = handler
}

// TelegramWebhook (POST /api/webhooks/telegram/{botId}) is the PUBLIC,
// unauthenticated endpoint Telegram POSTs updates to. botId is the opaque
// per-bot routing key (the numeric bot id from the token prefix, stored at
// config->>'app_id') — not a UUID, so it is read as a raw path param, never
// parsed/loaded through the UUID resolvers.
//
// This handler always ACKs with 200 OK, even for drops: Telegram retries
// non-2xx responses aggressively (indefinitely, by webhook contract), and a
// 401/404 here would also reveal whether a given bot id is installed to an
// unauthenticated caller. Every early-return path below is a deliberate
// silent drop, not an error: unknown/inactive installation, secret
// mismatch, malformed body, and updates InboundFromUpdate filters out
// (non-message updates, the bot's own messages, channel posts, etc).
func (h *Handler) TelegramWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	botID := chi.URLParam(r, "botId")

	inst, err := h.Queries.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(telegram.TypeTelegram),
		AppID:       botID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.ErrorContext(ctx, "telegram webhook: load installation failed", "bot_id", botID, "error", err)
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if inst.Status != "active" {
		w.WriteHeader(http.StatusOK)
		return
	}

	want := telegram.WebhookSecret(inst.Config)
	got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		w.WriteHeader(http.StatusOK)
		return
	}

	var update telegram.Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		w.WriteHeader(http.StatusOK)
		return
	}

	msg, ok := telegram.InboundFromUpdate(update, botID, telegram.DecodePublicConfig(inst.Config).BotUsername)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Product outcomes (unbound sender, no membership, etc.) are not errors
	// here — the Router's resolver pipeline owns those decisions and any
	// user-facing reply. Only an infra failure is worth logging.
	if h.webhookChannelHandler == nil {
		// Defensive guard against a production wiring bug (router.go not
		// wiring webhookChannelHandler) — not a test seam. Telegram must
		// not retry-storm, so still ACK 200.
		slog.Error("telegram webhook: channel handler not wired")
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := h.webhookChannelHandler.Handle(ctx, msg); err != nil {
		slog.ErrorContext(ctx, "telegram webhook: router handle failed", "bot_id", botID, "error", err)
	}
	w.WriteHeader(http.StatusOK)
}
