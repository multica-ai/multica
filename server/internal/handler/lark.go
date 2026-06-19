package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/lark"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// LarkInstallationResponse is the wire shape for an installation row.
// `app_secret_encrypted` is INTENTIONALLY absent — the encrypted blob
// is server-internal and there is no product reason to expose it (the
// only consumer that needs the plaintext is the WS hub, which calls
// InstallationService.DecryptAppSecret server-side). Likewise, the WS
// lease columns are omitted; they are runtime state, not API surface.
type LarkInstallationResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	AgentID         string  `json:"agent_id"`
	AppID           string  `json:"app_id"`
	TenantKey       *string `json:"tenant_key,omitempty"`
	BotOpenID       string  `json:"bot_open_id"`
	InstallerUserID string  `json:"installer_user_id"`
	Status          string  `json:"status"`
	// Region is the Lark cloud this installation lives on: "feishu"
	// (mainland) or "lark" (international). The UI uses it to render a
	// badge and to build the correct "Manage in Lark" dev-console host.
	Region      string `json:"region"`
	InstalledAt string `json:"installed_at"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func larkInstallationToResponse(row db.LarkInstallation) LarkInstallationResponse {
	resp := LarkInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		AppID:           row.AppID,
		BotOpenID:       row.BotOpenID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		Region:          row.Region,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if row.TenantKey.Valid {
		tk := row.TenantKey.String
		resp.TenantKey = &tk
	}
	return resp
}

// ListLarkInstallations (GET /api/workspaces/{id}/lark/installations)
// is member-visible — the Integrations tab should not render blank
// for non-admins. Unlike the GitHub list, we do not strip any field
// here because no API surface column doubles as a management handle:
// revocation goes by the UUID id, which is meaningless without the
// admin route's authorization, so exposing it is harmless.
//
// Response fields:
//   - configured: at-rest encryption key is set (`LarkInstallations
//     != nil`). When false, no install flow can succeed at all; the
//     UI hides the tab.
//   - install_supported: the device-flow install path is wired
//     end-to-end: a RegistrationService exists (deployment supplied
//     MULTICA_LARK_SECRET_KEY) AND the APIClient.IsConfigured signal
//     is true (the real Lark HTTP client is in place — the stub
//     cannot complete the post-poll GetBotInfo call). When false,
//     the agent-detail "Bind" button stays hidden and the Settings
//     tab surfaces a "coming soon" notice; already-installed bots
//     still appear and remain manageable.
func (h *Handler) ListLarkInstallations(w http.ResponseWriter, r *http.Request) {
	if h.LarkInstallations == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []LarkInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.LarkInstallations.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list lark installations")
		return
	}
	out := make([]LarkInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, larkInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": h.LarkRegistration != nil && h.LarkAPIClient != nil && h.LarkAPIClient.IsConfigured(),
	})
}

// RevokeLarkInstallation (DELETE /api/workspaces/{id}/lark/installations/{installationId})
// flips status to 'revoked' so the WS hub drops the connection on its
// next sweep. The row itself is preserved for audit; a re-install via
// the device-flow path flips status back to 'active' atomically.
func (h *Handler) RevokeLarkInstallation(w http.ResponseWriter, r *http.Request) {
	if h.LarkInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "lark integration not configured")
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
	if _, err := h.LarkInstallations.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, lark.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "lark installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.LarkInstallations.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventLarkInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemLarkBindingTokenRequest carries the raw token the user
// clicked through from the Bot's "you need to bind" reply card.
type RedeemLarkBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemLarkBindingTokenResponse is the post-redemption shape. We
// echo the workspace/installation/open_id so the frontend can render
// "you are now bound to <workspace> via <agent>" without a second
// fetch.
type RedeemLarkBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	LarkOpenID     string `json:"lark_open_id"`
}

// RedeemLarkBindingToken (POST /api/lark/binding/redeem) is the only
// path that writes a lark_user_binding row from user-driven action.
// The redeemer's identity is taken from the session, not the token,
// so a stolen token cannot bind a Lark open_id to an attacker's
// Multica account. The token only proves "this open_id requested
// binding" — combining it with the logged-in user is what creates
// the (open_id ↔ user) mapping.
//
// Consume + bind happen inside a single DB transaction (see
// lark.BindingTokenService.RedeemAndBind). The three failure modes
// each map to a distinct status code so the frontend can render the
// appropriate copy without a separate probe:
//   - 410 Gone:       token unknown / consumed / expired
//   - 409 Conflict:   open_id is already bound to a different user
//   - 403 Forbidden:  redeemer is not a workspace member
func (h *Handler) RedeemLarkBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.LarkBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "lark integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemLarkBindingTokenRequest
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

	redeemed, err := h.LarkBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, lark.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, lark.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this Lark account is already bound to a different Multica user")
		case errors.Is(err, lark.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}

	writeJSON(w, http.StatusOK, RedeemLarkBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		LarkOpenID:     string(redeemed.LarkOpenID),
	})
}

// BeginLarkInstallResponse is the payload the QR-code dialog consumes.
// The frontend renders `qr_code_url` as a QR image (and as a tap-to-
// open link fallback) and starts polling
// /lark/install/{session_id}/status at the supplied cadence.
type BeginLarkInstallResponse struct {
	SessionID           string `json:"session_id"`
	QRCodeURL           string `json:"qr_code_url"`
	ExpiresInSeconds    int    `json:"expires_in_seconds"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// BeginLarkInstall (POST /api/workspaces/{id}/lark/install/begin)
// opens a new device-flow registration session against Lark. Admin-only
// at the router. The agent_id query param picks which Multica Agent
// the new Bot will be bound to; the agent must belong to this
// workspace (RegistrationService re-checks that defense-in-depth).
//
// Returns 503 when the integration is not wired (no at-rest key, no
// HTTP client, no RegistrationService); the UI hides the bind button
// in that case so this should not be reached through the normal flow.
func (h *Handler) BeginLarkInstall(w http.ResponseWriter, r *http.Request) {
	if h.LarkRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "lark install not configured")
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
	// region is the cloud the user explicitly chose to bind against —
	// "feishu" (mainland, accounts.feishu.cn) or "lark" (international,
	// accounts.larksuite.com). The frontend now exposes two CTAs ("Bind
	// to Feishu" / "Bind to Lark") so the QR is rendered against the
	// right cloud up front rather than relying on the mid-poll
	// tenant-brand auto-switch from a Feishu-first begin. We accept
	// "feishu", "lark", and the empty string (for back-compat with
	// callers that pre-date the split CTA, which RegionOrDefault inside
	// the service maps to Feishu); any other value is a 400 — the
	// service would normalize an unknown value to Feishu silently and
	// that would mask a frontend regression where a typo'd region
	// landed users on the wrong cloud without telling them.
	regionParam := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("region")))
	switch regionParam {
	case "", "feishu", "lark":
		// ok — empty defaults to feishu downstream.
	default:
		writeError(w, http.StatusBadRequest, "region must be 'feishu' or 'lark'")
		return
	}
	// Ownership pre-check at the HTTP boundary so a malformed
	// agent_id surfaces 404 here (not an opaque service error from
	// inside the service's own re-check).
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

	res, err := h.LarkRegistration.BeginInstall(r.Context(), lark.BeginInstallParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		Region:      lark.Region(regionParam),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to start install: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, BeginLarkInstallResponse{
		SessionID:           res.SessionID,
		QRCodeURL:           res.QRCodeURL,
		ExpiresInSeconds:    res.ExpiresInSeconds,
		PollIntervalSeconds: res.PollIntervalSeconds,
	})
}

// LarkInstallStatusResponse is the polling payload. `status` is one
// of "pending" | "success" | "error"; on success `installation_id`
// is populated, on error `error_reason` is a stable code (see
// lark.RegistrationReason*).
type LarkInstallStatusResponse struct {
	Status         string `json:"status"`
	InstallationID string `json:"installation_id,omitempty"`
	ErrorReason    string `json:"error_reason,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// GetLarkInstallStatus (GET /api/workspaces/{id}/lark/install/{sessionId}/status)
// returns the current state of an in-flight install session. Admin-
// only at the router. Unknown / cross-workspace / GC'd sessions return
// 404 — the frontend treats it as "session lost, please restart".
//
// On success this handler does NOT clean up the session — the
// frontend may poll once more after the dialog closes to confirm
// before the in-process GC sweep retires the entry; reading is
// idempotent.
func (h *Handler) GetLarkInstallStatus(w http.ResponseWriter, r *http.Request) {
	if h.LarkRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "lark install not configured")
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
	state, err := h.LarkRegistration.GetSession(wsUUID, sessionID)
	if err != nil {
		if errors.Is(err, lark.ErrRegistrationSessionNotFound) {
			writeError(w, http.StatusNotFound, "install session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load install session")
		return
	}
	resp := LarkInstallStatusResponse{
		Status:       string(state.Status),
		ErrorReason:  state.ErrorReason,
		ErrorMessage: state.ErrorMessage,
	}
	if state.InstallationID.Valid {
		resp.InstallationID = uuidToString(state.InstallationID)
		// The lark_installation:created event is published by the
		// RegistrationService at the row-commit point (see
		// registration_service.go finishSuccess), not here — that keeps
		// the connection-badge refresh independent of whether any browser
		// polls this status endpoint to success.
	}
	writeJSON(w, http.StatusOK, resp)
}
