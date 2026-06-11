package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/octo"
	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// OctoInstallationResponse is the wire shape for an octo_installation row.
// bot_token_encrypted and the WS lease columns are intentionally omitted —
// server-internal state, not API surface.
type OctoInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	RobotID         string `json:"robot_id"`
	BotName         string `json:"bot_name"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func octoInstallationToResponse(row db.OctoInstallation) OctoInstallationResponse {
	return OctoInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		RobotID:         row.RobotID,
		BotName:         row.BotName,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListOctoInstallations (GET /api/workspaces/{id}/octo/installations) is
// member-visible. `configured` reports whether the at-rest encryption key is
// set; when false no install can succeed and the UI hides the tab.
func (h *Handler) ListOctoInstallations(w http.ResponseWriter, r *http.Request) {
	if h.OctoInstallations == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations": []OctoInstallationResponse{},
			"configured":    false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.OctoInstallations.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list octo installations")
		return
	}
	out := make([]OctoInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, octoInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"configured":    true,
	})
}

// CreateOctoInstallationRequest configures a bot for an agent: a bf_* bot token
// plus the agent to bind it to. The api_url defaults to MULTICA_OCTO_API_URL
// when omitted.
type CreateOctoInstallationRequest struct {
	AgentID  string `json:"agent_id"`
	BotToken string `json:"bot_token"`
	APIURL   string `json:"api_url,omitempty"`
}

// CreateOctoInstallation (POST /api/workspaces/{id}/octo/installations) registers
// the bot against Octo to validate the token and capture its identity
// (robot_id / api_url / ws_url), then upserts the installation. Admin-gated by
// the route.
func (h *Handler) CreateOctoInstallation(w http.ResponseWriter, r *http.Request) {
	if h.OctoInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "octo integration not configured")
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
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req CreateOctoInstallationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BotToken == "" {
		writeError(w, http.StatusBadRequest, "bot_token is required")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent id")
	if !ok {
		return
	}
	apiURL := req.APIURL
	if apiURL == "" {
		apiURL = h.OctoAPIBaseURL
	}
	if apiURL == "" {
		writeError(w, http.StatusBadRequest, "api_url is required (or set MULTICA_OCTO_API_URL)")
		return
	}

	// Register against Octo to validate the token and capture the bot identity.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	reg, err := transport.NewHTTPClient(apiURL, req.BotToken).Register(ctx, false, "Multica", "")
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to register bot with Octo (check token and api_url)")
		return
	}

	inst, err := h.OctoInstallations.Upsert(r.Context(), octo.InstallationParams{
		WorkspaceID:     wsUUID,
		AgentID:         agentUUID,
		BotToken:        req.BotToken,
		RobotID:         reg.RobotID,
		BotName:         reg.Name,
		OwnerUID:        reg.OwnerUID,
		APIURL:          reg.APIURL,
		WSURL:           reg.WSURL,
		InstallerUserID: userUUID,
	})
	if err != nil {
		if errors.Is(err, octo.ErrRobotAlreadyBound) {
			writeError(w, http.StatusConflict, "this Octo bot is already bound to another agent; disconnect it there first")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save installation")
		return
	}
	h.publish(protocol.EventOctoInstallationCreated, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(inst.ID),
	})
	writeJSON(w, http.StatusOK, octoInstallationToResponse(inst))
}

// RevokeOctoInstallation (DELETE /api/workspaces/{id}/octo/installations/{installationId})
// flips status to 'revoked'; the hub drops the WS on its next sweep.
func (h *Handler) RevokeOctoInstallation(w http.ResponseWriter, r *http.Request) {
	if h.OctoInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "octo integration not configured")
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
	if _, err := h.OctoInstallations.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, octo.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "octo installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.OctoInstallations.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventOctoInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemOctoBindingTokenRequest carries the raw token from the bot's binding
// prompt.
type RedeemOctoBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemOctoBindingTokenResponse echoes the resolved binding.
type RedeemOctoBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	OctoUID        string `json:"octo_uid"`
}

// RedeemOctoBindingToken (POST /api/octo/binding/redeem) links the logged-in
// user to the token's Octo uid. The redeemer identity comes from the session,
// not the token. Failure modes map to distinct status codes.
func (h *Handler) RedeemOctoBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.OctoBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "octo integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemOctoBindingTokenRequest
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
	redeemed, err := h.OctoBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, octo.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, octo.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Octo account is already bound to a different Multica user")
		case errors.Is(err, octo.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemOctoBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		OctoUID:        string(redeemed.OctoUID),
	})
}
