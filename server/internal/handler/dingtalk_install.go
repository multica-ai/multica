package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DingTalk bot installation endpoints — the scan-to-create device flow
// ("一键创建钉钉应用扫码接入"). Mirrors the Lark install surface in
// lark.go: member-visible listing, admin-gated begin/status/revoke.

// DingTalkInstallationResponse is the wire shape for an installation
// row. The encrypted client_secret is INTENTIONALLY absent — the only
// consumer that needs the plaintext is the (future) inbound transport,
// which decrypts server-side.
type DingTalkInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	ClientID        string `json:"client_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func dingTalkInstallationToResponse(row dingtalk.Installation) DingTalkInstallationResponse {
	return DingTalkInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		ClientID:        row.ClientID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListDingTalkInstallations (GET /api/workspaces/{id}/dingtalk/installations)
// is member-visible — the Integrations tab should not render blank for
// non-admins.
//
// Response fields:
//   - configured: at-rest encryption key is set
//     (`DingTalkInstallations != nil`). When false, no install flow can
//     succeed at all; the UI hides the panel's actions.
//   - install_supported: the device-flow install path is wired
//     end-to-end (a RegistrationService exists). When false, the
//     agent-detail "Bind" button stays hidden and the Settings tab
//     surfaces a "coming soon" notice; already-installed bots still
//     appear and remain manageable.
func (h *Handler) ListDingTalkInstallations(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstallations == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []DingTalkInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.DingTalkInstallations.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dingtalk installations")
		return
	}
	out := make([]DingTalkInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, dingTalkInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": h.DingTalkRegistration != nil,
	})
}

// RevokeDingTalkInstallation (DELETE /api/workspaces/{id}/dingtalk/installations/{installationId})
// flips status to 'revoked'. The row itself is preserved for audit; a
// re-install via the device-flow path flips status back to 'active'.
func (h *Handler) RevokeDingTalkInstallation(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk integration not configured")
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
	// Workspace-scoped lookup ensures one workspace cannot revoke
	// another's installation by guessing the UUID.
	if _, err := h.DingTalkInstallations.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, dingtalk.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "dingtalk installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.DingTalkInstallations.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventDingTalkInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// BeginDingTalkInstallResponse is the payload the QR-code dialog
// consumes. The frontend renders `qr_code_url` as a QR image (and as a
// tap-to-open link fallback) and starts polling
// /dingtalk/install/{session_id}/status at the supplied cadence.
type BeginDingTalkInstallResponse struct {
	SessionID           string `json:"session_id"`
	QRCodeURL           string `json:"qr_code_url"`
	ExpiresInSeconds    int    `json:"expires_in_seconds"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// BeginDingTalkInstall (POST /api/workspaces/{id}/dingtalk/install/begin)
// opens a new device-flow registration session against DingTalk.
// Admin-only at the router. The agent_id query param picks which
// Multica Agent the new app will be bound to; the agent must belong to
// this workspace (RegistrationService re-checks that defense-in-depth).
//
// Returns 503 when the integration is not wired; the UI hides the bind
// button in that case so this should not be reached through the normal
// flow.
func (h *Handler) BeginDingTalkInstall(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk install not configured")
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
	// Ownership pre-check at the HTTP boundary so a malformed agent_id
	// surfaces 404 here (not an opaque service error from inside the
	// service's own re-check).
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

	res, err := h.DingTalkRegistration.BeginInstall(r.Context(), dingtalk.BeginInstallParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start install: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, BeginDingTalkInstallResponse{
		SessionID:           res.SessionID,
		QRCodeURL:           res.QRCodeURL,
		ExpiresInSeconds:    res.ExpiresInSeconds,
		PollIntervalSeconds: res.PollIntervalSeconds,
	})
}

// DingTalkInstallStatusResponse is the polling payload. `status` is one
// of "pending" | "success" | "error"; on success `installation_id` is
// populated, on error `error_reason` is a stable code (see
// dingtalk.RegistrationReason*).
type DingTalkInstallStatusResponse struct {
	Status         string `json:"status"`
	InstallationID string `json:"installation_id,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// GetDingTalkInstallStatus (GET /api/workspaces/{id}/dingtalk/install/{sessionId}/status)
// returns the current state of an in-flight install session.
// Admin-only at the router. Unknown / cross-workspace / GC'd sessions
// return 404 — the frontend treats it as "session lost, please restart".
func (h *Handler) GetDingTalkInstallStatus(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk install not configured")
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
	state, err := h.DingTalkRegistration.GetSession(wsUUID, sessionID)
	if err != nil {
		if errors.Is(err, dingtalk.ErrRegistrationSessionNotFound) {
			writeError(w, http.StatusNotFound, "install session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load install session")
		return
	}
	resp := DingTalkInstallStatusResponse{
		Status:       string(state.Status),
		ErrorReason:  state.ErrorReason,
		ErrorMessage: state.ErrorMessage,
	}
	if state.InstallationID.Valid {
		resp.InstallationID = uuidToString(state.InstallationID)
		// The dingtalk_installation:created event is published by the
		// RegistrationService at the row-commit point, not here.
	}
	writeJSON(w, http.StatusOK, resp)
}

// RedeemDingTalkBindingTokenRequest carries the raw token the user clicked
// through from the bot's "link your account" prompt.
type RedeemDingTalkBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemDingTalkBindingTokenResponse echoes the bound workspace/installation/
// user so the frontend can confirm without a second fetch.
type RedeemDingTalkBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	DingTalkUserID string `json:"dingtalk_user_id"`
}

// RedeemDingTalkBindingToken (POST /api/dingtalk/binding/redeem) binds the
// DingTalk user id carried by the token to the logged-in Multica user. The
// redeemer's identity comes from the session, not the token, so a stolen
// token cannot bind a DingTalk id to an attacker's account. Failure modes
// map to distinct status codes (mirrors the Slack/Lark redeem endpoints):
//   - 410 Gone:      token unknown / consumed / expired
//   - 409 Conflict:  this DingTalk id is already bound to a different user
//   - 403 Forbidden: redeemer is not a workspace member
func (h *Handler) RedeemDingTalkBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemDingTalkBindingTokenRequest
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

	redeemed, err := h.DingTalkBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, dingtalk.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, dingtalk.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this DingTalk account is already bound to a different Multica user")
		case errors.Is(err, dingtalk.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemDingTalkBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		DingTalkUserID: redeemed.DingTalkUserID,
	})
}
