package handler

// wecom_web.go — the Web-UI-facing management endpoints for the WeChat Work
// smart-bot ("智能机器人" / aibot) integration. Parallel of slack.go: it
// exposes list / install / revoke over JSON so the Settings page and per-
// agent Integrations tab can drive wecom.InstallationService.Upsert /
// Revoke. There is no callback surface — the smart-bot is a client-initiated
// WebSocket long connection, so a public callback URL is not required.

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// WecomInstallationResponse is the wire shape for a wecom installation
// row. The secret is NEVER included — it remains sealed on the row. BotID
// is surfaced because operators need to see which bot is bound.
type WecomInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	BotID           string `json:"bot_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
}

func wecomInstallationToResponse(inst wecom.Installation) WecomInstallationResponse {
	return WecomInstallationResponse{
		ID:              uuidToString(inst.ID),
		WorkspaceID:     uuidToString(inst.WorkspaceID),
		AgentID:         uuidToString(inst.AgentID),
		BotID:           inst.BotID,
		InstallerUserID: uuidToString(inst.InstallerUserID),
		Status:          string(inst.Status),
	}
}

// wecomIntegrationConfigured reports whether the wecom integration is
// wired on this deployment. Both surfaces (list + register) short-circuit
// on it: list degrades to an empty response, register returns 503.
func (h *Handler) wecomIntegrationConfigured() bool {
	return h.WecomStore != nil && h.WecomCredentials != nil && h.ChannelRouter != nil
}

// ListWecomInstallations (GET /api/workspaces/{id}/wecom/installations) is
// member-visible so the Integrations tab renders for non-admins. Same
// response envelope Slack / Lark use.
func (h *Handler) ListWecomInstallations(w http.ResponseWriter, r *http.Request) {
	if !h.wecomIntegrationConfigured() {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []WecomInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	svc := h.wecomInstallService()
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not enabled")
		return
	}
	rows, err := svc.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wecom installations")
		return
	}
	out := make([]WecomInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, wecomInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterWecomBYORequest is the body an admin submits from the Web UI's
// BYO dialog. Two fields: the bot's stable identifier (BotID) and its
// long-connection secret. The secret is written straight through the
// secretbox so plaintext never lives on this file's stack past the
// wecom.InstallationService.Upsert call.
type RegisterWecomBYORequest struct {
	BotID  string `json:"bot_id"`
	Secret string `json:"secret"`
}

// RegisterWecomBYO (POST /api/workspaces/{id}/wecom/install/byo?agent_id=…)
// installs a user-supplied ("bring your own") wecom smart-bot for an agent.
// Admin-only at the router.
func (h *Handler) RegisterWecomBYO(w http.ResponseWriter, r *http.Request) {
	if !h.wecomIntegrationConfigured() {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not enabled")
		return
	}
	svc := h.wecomInstallService()
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not enabled")
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
	var body RegisterWecomBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	inst, err := svc.Upsert(r.Context(), wecom.InstallationParams{
		WorkspaceID:     wsUUID,
		AgentID:         agentUUID,
		InstallerUserID: initiatorUUID,
		BotID:           strings.TrimSpace(body.BotID),
		Secret:          strings.TrimSpace(body.Secret),
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.publish(protocol.EventWecomInstallationCreated, uuidToString(inst.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(inst.ID),
	})
	writeJSON(w, http.StatusOK, wecomInstallationToResponse(inst))
}

// RevokeWecomInstallation (DELETE /api/workspaces/{id}/wecom/installations/{installationId})
// flips status to 'revoked'. Admin-only at the router. Row-preserving so a
// re-install through Upsert flips it back to 'active' atomically.
func (h *Handler) RevokeWecomInstallation(w http.ResponseWriter, r *http.Request) {
	if !h.wecomIntegrationConfigured() {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not enabled")
		return
	}
	svc := h.wecomInstallService()
	if svc == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not enabled")
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
	if _, err := svc.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, wecom.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "wecom installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := svc.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventWecomInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// wecomInstallService builds an InstallationService on demand from the
// pieces the handler was wired with. It is cheap (a struct wrap over
// *db.Queries + the shared secretbox.Box behind the CredentialsResolver),
// and this lazy pattern avoids adding a fourth wecom field on the Handler
// struct just for the Web-UI path.
//
// SecretboxCredentialsResolver is the only production implementation of
// wecom.CredentialsResolver, so a type-assert is safe. If a test supplies
// a fake resolver, the assert falls through and both endpoints report 503,
// which is exactly the "no encryption key wired" behaviour we want.
func (h *Handler) wecomInstallService() *wecom.InstallationService {
	res, ok := h.WecomCredentials.(*wecom.SecretboxCredentialsResolver)
	if !ok || res == nil || res.Box == nil {
		return nil
	}
	svc, err := wecom.NewInstallationService(h.Queries, res.Box)
	if err != nil {
		return nil
	}
	return svc
}

// RedeemWecomBindingTokenRequest carries the raw token the user clicked
// through from the bot's "link your Multica account" prompt.
type RedeemWecomBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemWecomBindingTokenResponse echoes the bound workspace / installation
// / wecom user so the frontend can confirm without a second fetch.
type RedeemWecomBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	WecomUserID    string `json:"wecom_user_id"`
}

// RedeemWecomBindingToken (POST /api/wecom/binding/redeem) binds the WeCom
// aibot userid carried by the token to the logged-in Multica user. The
// redeemer's identity comes from the session, not the token, so a stolen
// token cannot bind a WeCom id to an attacker's account. Failure modes map
// to distinct status codes:
//
//   - 410 Gone:      token unknown / consumed / expired
//   - 409 Conflict:  this WeCom id is already bound to a different user
//   - 403 Forbidden: redeemer is not a workspace member
func (h *Handler) RedeemWecomBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.WecomBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemWecomBindingTokenRequest
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
	redeemed, err := h.WecomBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, wecom.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, wecom.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this WeCom user is already bound to a different Multica user")
		case errors.Is(err, wecom.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemWecomBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		WecomUserID:    redeemed.WecomUserID,
	})
}
