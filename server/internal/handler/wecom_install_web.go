package handler

// wecom_install_web.go — the scan-code install HTTP surface: begin a session and
// poll its status. The manual bot_id + secret path is RegisterWecomBYO in
// wecom_web.go.
//
// Both handlers are Cache-Control: no-store. The status response carries a QR URL
// that is a short-lived bearer credential for finishing the bot's creation, and
// the begin response carries a session id; neither belongs in a shared cache.

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// beginWecomInstallResponse tells the client which session to poll and how
// often.
type beginWecomInstallResponse struct {
	SessionID           string `json:"session_id"`
	Status              string `json:"status"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
}

// wecomInstallStatusResponse is one status poll. QRCodeURL and
// ExpiresInSeconds are populated only while the session is pending — there is
// nothing to render before the QR exists, and nothing to render after it is
// spent.
type wecomInstallStatusResponse struct {
	Status              string `json:"status"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	QRCodeURL           string `json:"qr_code_url,omitempty"`
	ExpiresInSeconds    int    `json:"expires_in_seconds,omitempty"`
	InstallationID      string `json:"installation_id,omitempty"`
	// ErrorReason is the machine code the dialog translates. The operator-facing
	// detail stored beside it stays out of the response: it is English, and it
	// can carry an internal error string.
	ErrorReason string `json:"error_reason,omitempty"`
}

// BeginWecomInstall (POST /api/workspaces/{id}/wecom/install/begin?agent_id=)
// admits or resumes a scan-code install session.
//
// 202, not 200: nothing has been created yet. The worker calls WeCom for the QR
// out of band, and the client polls GetWecomInstallStatus for it.
func (h *Handler) BeginWecomInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil || !h.WecomInstall.Configured() {
		writeWecomScanBeginError(w, wecom.ErrInstallNotConfigured)
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
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
		return
	}

	// The route admits workspace owners and admins only; the agent still has
	// to be one of this workspace's.
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

	res, err := h.WecomInstall.BeginInstall(r.Context(), wecom.BeginInstallParams{
		WorkspaceID:    wsUUID,
		AgentID:        agentUUID,
		InitiatorID:    initiatorUUID,
		IdempotencyKey: key,
	})
	if err != nil {
		writeWecomScanBeginError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, beginWecomInstallResponse{
		SessionID:           res.SessionID,
		Status:              res.Status,
		PollIntervalSeconds: pollIntervalForWecomStatus(res.Status),
	})
}

// writeWecomScanBeginError turns a begin failure into its status and, for the
// outcomes the scan dialog explains to the admin, a stable code it translates.
// The sentence is the fallback for a client that does not know the code.
// The rest are a caller bug or a fault on our side, and the dialog shows its
// generic line for those.
func writeWecomScanBeginError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wecom.ErrActiveInstallationExists):
		writeErrorCode(w, http.StatusConflict, "wecom_agent_already_connected",
			"this agent already has a connected WeCom bot — disconnect it first")
	case errors.Is(err, wecom.ErrRateLimited):
		writeErrorCode(w, http.StatusTooManyRequests, "wecom_scan_install_rate_limited",
			"too many install attempts — wait a few minutes and try again")
	case errors.Is(err, wecom.ErrInstallNotConfigured):
		writeErrorCode(w, http.StatusServiceUnavailable, "wecom_scan_install_unavailable",
			"wecom scan install not enabled")
	case errors.Is(err, wecom.ErrAgentMismatch):
		writeError(w, http.StatusConflict, "this Idempotency-Key was already used to start an install for a different agent")
	case errors.Is(err, wecom.ErrIdempotencyKeyRequired):
		writeError(w, http.StatusBadRequest, "Idempotency-Key header is required")
	case errors.Is(err, wecom.ErrIdempotencyKeyTooLong):
		writeError(w, http.StatusBadRequest, "Idempotency-Key is too long")
	default:
		writeError(w, http.StatusInternalServerError, "could not start the WeCom install")
	}
}

// GetWecomInstallStatus (GET /api/workspaces/{id}/wecom/install/{sessionId})
// is the status poll behind the QR dialog.
func (h *Handler) GetWecomInstallStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.WecomInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom scan install not enabled")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	sessionUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(chi.URLParam(r, "sessionId")), "session id")
	if !ok {
		return
	}

	// The route admits workspace owners and admins only, and GetSession reads
	// within this workspace, so a session from another one is not found.
	snap, err := h.WecomInstall.GetSession(r.Context(), wsUUID, sessionUUID, true)
	if err != nil {
		h.writeWecomSessionError(w, err)
		return
	}

	resp := wecomInstallStatusResponse{
		Status:              snap.Status,
		PollIntervalSeconds: pollIntervalForWecomStatus(snap.Status),
		ErrorReason:         snap.ErrorReason,
	}
	if snap.Status == wecom.InstallStatusPending {
		resp.QRCodeURL = snap.QRCodeURL
		if !snap.ExpiresAt.IsZero() {
			if remaining := int(time.Until(snap.ExpiresAt) / time.Second); remaining > 0 {
				resp.ExpiresInSeconds = remaining
			}
		}
	}
	if snap.InstallationID.Valid {
		resp.InstallationID = uuidToString(snap.InstallationID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeWecomSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, wecom.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "install session not found")
		return
	}
	writeError(w, http.StatusInternalServerError, "could not load the install session")
}

// pollIntervalForWecomStatus is the client's next poll delay. creating polls
// fast because it is only waiting on our own generate call; pending polls at
// WeCom's own minimum query_result interval. Terminal states still return a
// value, but the client stops on receipt.
func pollIntervalForWecomStatus(status string) int {
	if status == wecom.InstallStatusCreating {
		return 1
	}
	return 2
}
