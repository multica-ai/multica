package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// wecomInstallationResponse is the wire shape for a WeCom installation row.
// The encrypted bot secret, WS lease columns, and raw bot_info are
// deliberately absent — spec §7.3.1 requires the response to expose only
// management-relevant fields.
type wecomInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	BotID           string `json:"bot_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

func wecomInstallationToResponse(row db.ChannelInstallation) wecomInstallationResponse {
	resp := wecomInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if cfg, err := wecom.UnmarshalInstallationConfig(row.Config); err == nil {
		resp.BotID = cfg.AppID
	}
	return resp
}

// ListWecomInstallations (GET /api/workspaces/{id}/wecom/installations) is
// member-visible so the Integrations tab renders for non-admins too. When
// the deployment has not opted into WeCom (no MULTICA_WECOM_SECRET_KEY),
// the response is still 200 with empty installations + configured=false,
// so the UI can hide the tab without probing a 503.
func (h *Handler) ListWecomInstallations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []wecomInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListChannelInstallationsByWorkspace(r.Context(), db.ListChannelInstallationsByWorkspaceParams{
		WorkspaceID: wsUUID,
		ChannelType: string(wecom.TypeWecom),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wecom installations")
		return
	}
	out := make([]wecomInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, wecomInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": h.WecomInstall.Configured(),
	})
}

// beginWecomInstallResponse mirrors spec §7.3.1: session_id, status, and a
// fixed 1-second poll interval. The frontend polls status for the QR / final
// outcome — begin never returns qr_code_url.
type beginWecomInstallResponse struct {
	SessionID           string `json:"session_id"`
	Status              string `json:"status"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// BeginWecomInstall (POST /api/workspaces/{id}/wecom/install/begin?agent_id=)
// opens or resumes a scan-code session. The response is always 202 Accepted
// so the frontend flow is uniform regardless of whether begin created a
// fresh row or joined an in-flight one (spec §7.3.1).
//
// Authorization is per-agent via canManageAgent (agent owner OR workspace
// owner/admin); the router only checks workspace membership so an agent
// owner can bind their own agent without being a workspace admin.
func (h *Handler) BeginWecomInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil || !h.WecomInstall.Configured() {
		writeError(w, http.StatusServiceUnavailable, "wecom install not configured")
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
	key := r.Header.Get("Idempotency-Key")
	if strings.TrimSpace(key) == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}
	if len(key) > 128 {
		writeError(w, http.StatusBadRequest, "Idempotency-Key must not exceed 128 bytes")
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	initiatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	callerIsAdmin := false
	if member, mErr := h.getWorkspaceMember(r.Context(), userID, uuidToString(wsUUID)); mErr == nil {
		callerIsAdmin = roleAllowed(member.Role, "owner", "admin")
	}

	res, err := h.WecomInstall.BeginInstall(r.Context(), wecom.BeginInstallParams{
		WorkspaceID:            wsUUID,
		AgentID:                agentUUID,
		InitiatorID:            initiatorUUID,
		IdempotencyKey:         key,
		CallerIsWorkspaceAdmin: callerIsAdmin,
	})
	if err != nil {
		switch {
		case errors.Is(err, wecom.ErrAgentMismatch):
			writeError(w, http.StatusConflict, "agent_mismatch")
		case errors.Is(err, wecom.ErrActiveInstallationExists):
			writeError(w, http.StatusConflict, "installation_conflict")
		case errors.Is(err, wecom.ErrInstallInProgress):
			writeError(w, http.StatusConflict, "install_in_progress")
		case errors.Is(err, wecom.ErrRateLimited):
			writeError(w, http.StatusTooManyRequests, "rate_limited")
		case errors.Is(err, wecom.ErrIdempotencyKeyRequired):
			writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		case errors.Is(err, wecom.ErrIdempotencyKeyTooLong):
			writeError(w, http.StatusBadRequest, "Idempotency-Key must not exceed 128 bytes")
		default:
			writeError(w, http.StatusInternalServerError, "failed to start wecom install")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, beginWecomInstallResponse{
		SessionID:           res.SessionID,
		Status:              res.Status,
		PollIntervalSeconds: 1,
	})
}

// wecomInstallStatusResponse mirrors spec §7.3.1 exactly. QR-URL and
// error fields are optional; the frontend switches on status + error_reason.
type wecomInstallStatusResponse struct {
	Status              string `json:"status"`
	QRCodeURL           string `json:"qr_code_url,omitempty"`
	ExpiresInSeconds    int    `json:"expires_in_seconds,omitempty"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	InstallationID      string `json:"installation_id,omitempty"`
	ErrorReason         string `json:"error_reason,omitempty"`
	ErrorMessage        string `json:"error_message,omitempty"`
}

// GetWecomInstallStatus (GET /api/workspaces/{id}/wecom/install/{sessionId}/status)
// is the polling endpoint. Only the session initiator or a workspace owner /
// admin may read the session; everyone else gets a uniform 404 to prevent
// session-id enumeration.
//
// creating returns a 1-second poll interval and no QR. pending returns a
// 2-second interval, the decrypted QR URL, and remaining seconds.
// success / error are terminal.
func (h *Handler) GetWecomInstallStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil || !h.WecomInstall.Configured() {
		writeError(w, http.StatusServiceUnavailable, "wecom install not configured")
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
	sessionIDStr := strings.TrimSpace(chi.URLParam(r, "sessionId"))
	if sessionIDStr == "" {
		writeError(w, http.StatusBadRequest, "session id is required")
		return
	}
	sessionUUID, ok := parseUUIDOrBadRequest(w, sessionIDStr, "session id")
	if !ok {
		return
	}
	// Two-phase auth: load the session snapshot without decrypting the URL
	// first, decide whether the caller may see it, then re-request with
	// decryptQR=true. This keeps the ciphertext on the DB path a single
	// authorized viewer touches, and lets unauthorized viewers 404 without
	// running the crypto.
	preview, err := h.WecomInstall.GetSession(r.Context(), wsUUID, sessionUUID, false)
	if err != nil {
		if errors.Is(err, wecom.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "install session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load install session")
		return
	}
	isInitiator := uuidToString(preview.InitiatorUserID) == userID
	callerIsAdmin := false
	if !isInitiator {
		if member, mErr := h.getWorkspaceMember(r.Context(), userID, uuidToString(wsUUID)); mErr == nil {
			callerIsAdmin = roleAllowed(member.Role, "owner", "admin")
		}
		if !callerIsAdmin {
			writeError(w, http.StatusNotFound, "install session not found")
			return
		}
	}
	snap := preview
	if isInitiator || callerIsAdmin {
		snap, err = h.WecomInstall.GetSession(r.Context(), wsUUID, sessionUUID, true)
		if err != nil {
			if errors.Is(err, wecom.ErrSessionNotFound) {
				writeError(w, http.StatusNotFound, "install session not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to load install session")
			return
		}
	}
	resp := wecomInstallStatusResponse{
		Status:              snap.Status,
		PollIntervalSeconds: pollIntervalForStatus(snap.Status),
		ErrorReason:         snap.ErrorReason,
		ErrorMessage:        snap.ErrorMessage,
	}
	if snap.Status == wecom.InstallStatusPending {
		resp.QRCodeURL = snap.QRCodeURL
		if !snap.ExpiresAt.IsZero() {
			remaining := time.Until(snap.ExpiresAt) / time.Second
			if remaining < 0 {
				remaining = 0
			}
			resp.ExpiresInSeconds = int(remaining)
		}
	}
	if snap.InstallationID.Valid {
		resp.InstallationID = uuidToString(snap.InstallationID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// pollIntervalForStatus maps a status to the frontend's next poll cadence.
// creating uses 1s (waiting for generate); pending uses 2s per WeCom's own
// rate limit; terminal states return 2s but the frontend stops polling
// on receipt.
func pollIntervalForStatus(status string) int {
	switch status {
	case wecom.InstallStatusCreating:
		return 1
	default:
		return 2
	}
}

// RevokeWecomInstallation (DELETE /api/workspaces/{id}/wecom/installations/{installationId})
// flips a wecom installation to revoked. Symmetric with the Lark revoke:
// the bound agent's owner OR a workspace owner/admin may revoke; a
// hard-deleted-agent orphan falls back to workspace owner/admin only.
//
// The remote bot on WeCom's side is NOT deleted — spec §10 documents this,
// and the wire status matrix returns 204 regardless.
func (h *Handler) RevokeWecomInstallation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not configured")
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
	inst, err := h.Queries.GetChannelInstallationInWorkspace(r.Context(), db.GetChannelInstallationInWorkspaceParams{
		ID:          instUUID,
		WorkspaceID: wsUUID,
		ChannelType: string(wecom.TypeWecom),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "wecom installation not found")
		return
	}
	// Authorize against the bound agent. When the agent has been
	// hard-deleted (orphan), fall back to workspace owner/admin so cleanup
	// still has an entry point.
	agent, agentErr := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          inst.AgentID,
		WorkspaceID: wsUUID,
	})
	if agentErr != nil {
		if _, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "wecom installation not found", "owner", "admin"); !ok {
			return
		}
	} else if !h.canManageAgent(w, r, agent) {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if err := qtx.SetChannelInstallationStatus(r.Context(), db.SetChannelInstallationStatusParams{
		ID:     instUUID,
		Status: "revoked",
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	if err := qtx.FailChannelOutboundByInstallation(r.Context(), db.FailChannelOutboundByInstallationParams{
		InstallationID: instUUID,
		LastError:      pgtype.Text{String: "installation_revoked", Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to fail outbound queue")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit revoke")
		return
	}

	h.publish(protocol.EventWecomInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id":       uuidToString(instUUID),
		"agent_id": uuidToString(inst.AgentID),
	})
	// Wake the ChannelSupervisor so it stops driving the connection now
	// instead of waiting the next sweep.
	if h.ChannelSupervisor != nil {
		h.ChannelSupervisor.Notify()
	}
	w.WriteHeader(http.StatusNoContent)
}

// RedeemWecomBindingTokenRequest carries the raw token from the bot's DM link.
type RedeemWecomBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemWecomBindingTokenResponse echoes the bound workspace/installation.
type RedeemWecomBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
}

// RedeemWecomBindingToken (POST /api/wecom/binding/redeem) binds the WeCom
// userid carried by the token to the logged-in Multica user. The redeemer's
// identity comes from the session, not the token.
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
			writeError(w, http.StatusConflict, "this WeCom account is already bound to a different Multica user")
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
	})
}
