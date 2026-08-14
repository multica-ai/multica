package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	qianwenBodyLimit         = 20 * 1024
	qianwenPairingBodyLimit  = 2 * 1024
	qianwenRequestTimeout    = 2500 * time.Millisecond
	qianwenOpenUserIDHeader  = "X-Qianwen-Open-User-Id"
	qianwenOpenUUIDHeader    = "X-Qianwen-Open-Uuid"
	qianwenTimestampHeader   = "X-Qianwen-Timestamp"
	qianwenNonceHeader       = "X-Qianwen-Nonce"
	qianwenSignatureHeader   = "X-Qianwen-Signature"
	qianwenPairingRetryAfter = "600"
	qianwenTaskListLimit     = 10
	qianwenTaskListMaxLimit  = 20
	qianwenTaskCursorMax     = 512
)

// QianwenService is the handler seam implemented by *qianwen.Service. Keeping
// it narrow lets HTTP contract tests inject a fake without a database.
type QianwenService interface {
	PairingSupported() bool
	InstallPersonal(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error)
	ListByWorkspace(context.Context, pgtype.UUID) ([]db.ChannelInstallation, error)
	GetInWorkspace(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error)
	MintPairingCode(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.PairingCodeResult, error)
	RedeemPairingCode(context.Context, string, string, qianwen.PairingRedeemRequest) (qianwen.PairingBindingResult, error)
	Revoke(context.Context, pgtype.UUID, pgtype.UUID) error
	UnbindCurrentUser(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error
	Submit(context.Context, string, string, qianwen.SubmitInvocation) (qianwen.SubmitResult, error)
	Status(context.Context, string, string, qianwen.StatusInvocation) (qianwen.RequestStatus, error)
	ListCurrentTasks(context.Context, string, string, qianwen.TaskListInvocation) (qianwen.CurrentTaskList, error)
}

type QianwenInstallationResponse struct {
	ID               string `json:"id"`
	AgentID          string `json:"agent_id"`
	ConnectionID     string `json:"connection_id"`
	Mode             string `json:"mode"`
	Status           string `json:"status"`
	CurrentUserBound bool   `json:"current_user_bound"`
}

type QianwenInstallResponse struct {
	QianwenInstallationResponse
	AccessToken       string `json:"access_token"`
	TokenVisibleOnce  bool   `json:"token_visible_once"`
	SubmitPath        string `json:"submit_path"`
	StatusPathPattern string `json:"status_path_pattern"`
}

type QianwenPairingCodeResponse struct {
	PairingCode     string    `json:"pairing_code"`
	ExpiresAt       time.Time `json:"expires_at"`
	CodeVisibleOnce bool      `json:"code_visible_once"`
}

func qianwenInstallationResponse(row db.ChannelInstallation) QianwenInstallationResponse {
	public := qianwen.DecodePublicConfig(row.Config)
	return QianwenInstallationResponse{
		ID:           uuidToString(row.ID),
		AgentID:      uuidToString(row.AgentID),
		ConnectionID: public.ConnectionID,
		Mode:         public.Mode,
		Status:       row.Status,
	}
}

// requireQianwenHumanActor is the handler-level backstop behind the router's
// RequireHumanActor middleware. A task or cloud-node credential must never be
// able to mint, list, or revoke a long-lived Skill credential on its
// human owner's behalf.
func requireQianwenHumanActor(w http.ResponseWriter, r *http.Request) bool {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return false
	}
	return true
}

// InstallQianwenPersonal creates an agent's private polling Skill credential or
// reconnects an explicitly revoked installation. The plaintext token is
// returned once; subsequent list calls only expose the public connection id.
func (h *Handler) InstallQianwenPersonal(w http.ResponseWriter, r *http.Request) {
	if !requireQianwenHumanActor(w, r) {
		return
	}
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("agent_id")), "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	installerID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	result, err := h.Qianwen.InstallPersonal(r.Context(), workspaceID, agentID, installerID)
	if errors.Is(err, qianwen.ErrPairingUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "qianwen pairing is not enabled")
		return
	}
	if errors.Is(err, qianwen.ErrInstallationAlreadyActive) {
		writeError(w, http.StatusConflict, "qianwen installation is already active")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create qianwen installation")
		return
	}
	base := "/api/channels/qianwen/" + result.ConnectionID + "/requests"
	writeJSON(w, http.StatusCreated, QianwenInstallResponse{
		QianwenInstallationResponse: qianwenInstallationResponse(result.Installation),
		AccessToken:                 result.AccessToken,
		TokenVisibleOnce:            true,
		SubmitPath:                  base,
		StatusPathPattern:           base + "/{request_id}",
	})
}

func (h *Handler) ListQianwenInstallations(w http.ResponseWriter, r *http.Request) {
	if !requireQianwenHumanActor(w, r) {
		return
	}
	if h.Qianwen == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []QianwenInstallationResponse{},
			"configured":        false,
			"pairing_supported": false,
		})
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Qianwen.ListByWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list qianwen installations")
		return
	}
	currentUserID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	boundInstallationIDs, err := h.Queries.ListQianwenBoundInstallationIDsForUser(r.Context(), db.ListQianwenBoundInstallationIDsForUserParams{
		WorkspaceID:   workspaceID,
		MulticaUserID: currentUserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load qianwen binding state")
		return
	}
	bound := make(map[pgtype.UUID]struct{}, len(boundInstallationIDs))
	for _, installationID := range boundInstallationIDs {
		bound[installationID] = struct{}{}
	}
	out := make([]QianwenInstallationResponse, 0, len(rows))
	for _, row := range rows {
		response := qianwenInstallationResponse(row)
		_, response.CurrentUserBound = bound[row.ID]
		out = append(out, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"pairing_supported": h.Qianwen.PairingSupported(),
		"mode":              "personal_polling",
	})
}

// UnbindQianwenCurrentUser severs only the authenticated member's own opaque
// Qianwen identity. It never accepts a user id from the route or body and does
// not revoke the shared installation credential.
func (h *Handler) UnbindQianwenCurrentUser(w http.ResponseWriter, r *http.Request) {
	if !requireQianwenHumanActor(w, r) {
		return
	}
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	currentUserID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	if err := h.Qianwen.UnbindCurrentUser(r.Context(), workspaceID, installationID, currentUserID); err != nil {
		if errors.Is(err, qianwen.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "qianwen installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to unbind qianwen identity")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MintQianwenPairingCode returns an eight-digit code once for the currently
// authenticated Multica user. The target identity is never accepted from the
// request body.
func (h *Handler) MintQianwenPairingCode(w http.ResponseWriter, r *http.Request) {
	if !requireQianwenHumanActor(w, r) {
		return
	}
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	installation, err := h.Qianwen.GetInWorkspace(r.Context(), installationID, workspaceID)
	if errors.Is(err, qianwen.ErrInstallationNotFound) {
		writeError(w, http.StatusNotFound, "active qianwen installation not found")
		return
	}
	if err != nil || installation.Status != "active" {
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load qianwen installation")
		} else {
			writeError(w, http.StatusNotFound, "active qianwen installation not found")
		}
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          installation.AgentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	if !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, uuidToString(workspaceID)) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	targetUserID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	result, err := h.Qianwen.MintPairingCode(r.Context(), workspaceID, installationID, targetUserID)
	if errors.Is(err, qianwen.ErrInstallationNotFound) {
		writeError(w, http.StatusNotFound, "active qianwen installation not found")
		return
	}
	if errors.Is(err, qianwen.ErrPairingUnavailable) {
		writeError(w, http.StatusServiceUnavailable, "qianwen pairing is not enabled")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create qianwen pairing code")
		return
	}
	writeJSON(w, http.StatusCreated, QianwenPairingCodeResponse{
		PairingCode:     result.Code,
		ExpiresAt:       result.ExpiresAt,
		CodeVisibleOnce: true,
	})
}

// RedeemQianwenPairingCode is the Qianwen-side half of account pairing. The
// body carries only the spoken one-time code; opaque user/device identity and
// replay metadata must arrive in fixed system-derived headers so model output
// can never choose the Multica actor being bound.
func (h *Handler) RedeemQianwenPairingCode(w http.ResponseWriter, r *http.Request) {
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	connectionID, token, ok := h.qianwenPublicCredentials(w, r)
	if !ok {
		return
	}
	identity, status, ok := qianwenInvocationMetadataFromHeaders(r)
	if !ok {
		if status == http.StatusForbidden {
			writeError(w, status, "qianwen identity is unavailable")
		} else {
			writeError(w, status, "invalid qianwen invocation metadata")
		}
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, qianwenPairingBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body struct {
		PairingCode string `json:"pairing_code"`
	}
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), qianwenRequestTimeout)
	defer cancel()
	_, err := h.Qianwen.RedeemPairingCode(ctx, connectionID, token, qianwen.PairingRedeemRequest{
		Code:     body.PairingCode,
		Identity: identity,
	})
	if h.writeQianwenPairingError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "paired"})
}

func qianwenInvocationMetadataFromHeaders(r *http.Request) (qianwen.InvocationMetadata, int, bool) {
	read := func(name string) (string, bool) {
		values := r.Header.Values(name)
		returnValue := ""
		if len(values) == 1 {
			returnValue = values[0]
		}
		return returnValue, len(values) == 1 && returnValue != ""
	}
	openUserID, userOK := read(qianwenOpenUserIDHeader)
	openUUID, uuidOK := read(qianwenOpenUUIDHeader)
	if !userOK || !uuidOK {
		if len(r.Header.Values(qianwenOpenUserIDHeader)) > 1 || len(r.Header.Values(qianwenOpenUUIDHeader)) > 1 {
			return qianwen.InvocationMetadata{}, http.StatusUnauthorized, false
		}
		return qianwen.InvocationMetadata{}, http.StatusForbidden, false
	}
	timestamp, timestampOK := read(qianwenTimestampHeader)
	nonce, nonceOK := read(qianwenNonceHeader)
	signature, signatureOK := read(qianwenSignatureHeader)
	if !timestampOK || !nonceOK || !signatureOK {
		return qianwen.InvocationMetadata{}, http.StatusUnauthorized, false
	}
	return qianwen.InvocationMetadata{
		OpenUserID: openUserID,
		OpenUUID:   openUUID,
		Timestamp:  timestamp,
		Nonce:      nonce,
		Signature:  signature,
	}, 0, true
}

func (h *Handler) writeQianwenPairingError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, qianwen.ErrUnauthorized):
		h.chargeQianwenBadCredential(r)
		writeError(w, http.StatusUnauthorized, "invalid qianwen credentials")
	case errors.Is(err, qianwen.ErrInvalidInvocation), errors.Is(err, qianwen.ErrStaleInvocation):
		writeError(w, http.StatusUnauthorized, "invalid qianwen invocation")
	case errors.Is(err, qianwen.ErrIdentityUnavailable), errors.Is(err, qianwen.ErrPairingAccessDenied):
		writeError(w, http.StatusForbidden, "qianwen identity cannot be paired")
	case errors.Is(err, qianwen.ErrPairingCodeInvalid):
		writeError(w, http.StatusGone, "qianwen pairing code is invalid or expired")
	case errors.Is(err, qianwen.ErrBindingAlreadyAssigned), errors.Is(err, qianwen.ErrInvocationReplay):
		writeError(w, http.StatusConflict, "qianwen pairing request conflicts with existing state")
	case errors.Is(err, qianwen.ErrPairingRateLimited):
		w.Header().Set("Retry-After", qianwenPairingRetryAfter)
		writeError(w, http.StatusTooManyRequests, "too many qianwen pairing attempts")
	case errors.Is(err, qianwen.ErrPairingUnavailable):
		writeError(w, http.StatusServiceUnavailable, "qianwen pairing is not enabled")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		writeError(w, http.StatusGatewayTimeout, "qianwen pairing request timed out")
	default:
		writeError(w, http.StatusInternalServerError, "qianwen pairing request failed")
	}
	return true
}

func (h *Handler) RevokeQianwenInstallation(w http.ResponseWriter, r *http.Request) {
	if !requireQianwenHumanActor(w, r) {
		return
	}
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	installationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	installation, err := h.Qianwen.GetInWorkspace(r.Context(), installationID, workspaceID)
	if errors.Is(err, qianwen.ErrInstallationNotFound) {
		writeError(w, http.StatusNotFound, "qianwen installation not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load qianwen installation")
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: installation.AgentID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	if err := h.Qianwen.Revoke(r.Context(), workspaceID, installationID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke qianwen installation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SubmitQianwenRequest is public in the HTTP-auth sense: the installation
// bearer token is the credential, so a Multica browser session is neither
// required nor accepted as a substitute.
func (h *Handler) SubmitQianwenRequest(w http.ResponseWriter, r *http.Request) {
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	connectionID, token, ok := h.qianwenPublicCredentials(w, r)
	if !ok {
		return
	}
	identity, identityStatus, ok := qianwenInvocationMetadataFromHeaders(r)
	if !ok {
		if identityStatus == http.StatusForbidden {
			writeError(w, identityStatus, "qianwen identity is unavailable")
		} else {
			writeError(w, identityStatus, "invalid qianwen invocation metadata")
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, qianwenBodyLimit)
	var req qianwen.SubmitRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request body")
		}
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), qianwenRequestTimeout)
	defer cancel()
	result, err := h.Qianwen.Submit(ctx, connectionID, token, qianwen.SubmitInvocation{
		Request:  req,
		Identity: identity,
	})
	if h.writeQianwenServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"request_id":    result.RequestID,
		"status":        result.Status,
		"poll_after_ms": 2000,
	})
}

func (h *Handler) GetQianwenRequestStatus(w http.ResponseWriter, r *http.Request) {
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	connectionID, token, ok := h.qianwenPublicCredentials(w, r)
	if !ok {
		return
	}
	identity, identityStatus, ok := qianwenInvocationMetadataFromHeaders(r)
	if !ok {
		if identityStatus == http.StatusForbidden {
			writeError(w, identityStatus, "qianwen identity is unavailable")
		} else {
			writeError(w, identityStatus, "invalid qianwen invocation metadata")
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), qianwenRequestTimeout)
	defer cancel()
	status, err := h.Qianwen.Status(ctx, connectionID, token, qianwen.StatusInvocation{
		RequestID: chi.URLParam(r, "requestId"),
		Identity:  identity,
	})
	if h.writeQianwenServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// ListQianwenCurrentTasks returns the caller-relative active task projection
// for a bound Qianwen identity. Like submit and status, the installation
// bearer plus signed opaque identity are the only authentication inputs.
func (h *Handler) ListQianwenCurrentTasks(w http.ResponseWriter, r *http.Request) {
	if h.Qianwen == nil {
		writeError(w, http.StatusServiceUnavailable, "qianwen integration is not enabled")
		return
	}
	connectionID, token, ok := h.qianwenPublicCredentials(w, r)
	if !ok {
		return
	}
	identity, identityStatus, ok := qianwenInvocationMetadataFromHeaders(r)
	if !ok {
		if identityStatus == http.StatusForbidden {
			writeError(w, identityStatus, "qianwen identity is unavailable")
		} else {
			writeError(w, identityStatus, "invalid qianwen invocation metadata")
		}
		return
	}
	taskListRequest, err := parseQianwenTaskListQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid qianwen task list query")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), qianwenRequestTimeout)
	defer cancel()
	result, err := h.Qianwen.ListCurrentTasks(ctx, connectionID, token, qianwen.TaskListInvocation{
		Request:  taskListRequest,
		Identity: identity,
	})
	if h.writeQianwenServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseQianwenTaskListQuery(rawQuery string) (qianwen.TaskListRequest, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return qianwen.TaskListRequest{}, err
	}
	for key, value := range values {
		if (key != "limit" && key != "cursor") || len(value) != 1 {
			return qianwen.TaskListRequest{}, qianwen.ErrInvalidRequest
		}
	}

	request := qianwen.TaskListRequest{Limit: qianwenTaskListLimit}
	if values.Has("limit") {
		rawLimit := values.Get("limit")
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || rawLimit == "" || strconv.Itoa(limit) != rawLimit || limit < 1 || limit > qianwenTaskListMaxLimit {
			return qianwen.TaskListRequest{}, qianwen.ErrInvalidRequest
		}
		request.Limit = limit
	}
	if values.Has("cursor") {
		request.Cursor = values.Get("cursor")
		if len(request.Cursor) > qianwenTaskCursorMax || strings.ContainsAny(request.Cursor, "\r\n\x00") {
			return qianwen.TaskListRequest{}, qianwen.ErrInvalidRequest
		}
	}
	return request, nil
}

// qianwenPublicCredentials applies the public-ingress gates in security order:
// an absolute IP ceiling and existing bad-credential debt before parsing any
// credential, strict bearer/credential-shape validation before a database call,
// then the credential-scoped budget. Invalid credentials never consume a key
// derived from attacker-controlled route text.
func (h *Handler) qianwenPublicCredentials(w http.ResponseWriter, r *http.Request) (connectionID, token string, ok bool) {
	if !h.allowQianwenIPRequest(w, r) {
		return "", "", false
	}
	connectionID = chi.URLParam(r, "connectionId")
	token, ok = qianwenBearerToken(r)
	if !ok || !qianwen.ValidCredentialShape(connectionID, token) {
		h.chargeQianwenBadCredential(r)
		writeError(w, http.StatusUnauthorized, "invalid qianwen credentials")
		return "", "", false
	}
	if !h.allowQianwenCredentialRequest(w, r, connectionID, token) {
		return "", "", false
	}
	return connectionID, token, true
}

func (h *Handler) allowQianwenIPRequest(w http.ResponseWriter, r *http.Request) bool {
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), "qianwen:"+ip) {
		writeQianwenRateLimit(w, h.WebhookAbsoluteIPRateLimiter, r.Context(), "qianwen:"+ip)
		return false
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !webhookLimiterCheck(r.Context(), h.WebhookIPRateLimiter, "qianwen:"+ip) {
		writeQianwenRateLimit(w, h.WebhookIPRateLimiter, r.Context(), "qianwen:"+ip)
		return false
	}
	return true
}

func (h *Handler) allowQianwenCredentialRequest(w http.ResponseWriter, r *http.Request, connectionID, token string) bool {
	key := qianwenCredentialRateLimitKey(connectionID, token)
	if h.WebhookRateLimiter != nil && !h.WebhookRateLimiter.Allow(r.Context(), key) {
		writeQianwenRateLimit(w, h.WebhookRateLimiter, r.Context(), key)
		return false
	}
	return true
}

// qianwenCredentialRateLimitKey keeps both the public connection id and the
// bearer secret out of in-memory/Redis limiter keys. The NUL delimiter makes
// the tuple encoding unambiguous before hashing; the result is always 64 lower-
// case hexadecimal characters.
func qianwenCredentialRateLimitKey(connectionID, token string) string {
	sum := sha256.Sum256([]byte(connectionID + "\x00" + token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) chargeQianwenBadCredential(r *http.Request) {
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookIPRateLimiter != nil {
		h.WebhookIPRateLimiter.Allow(r.Context(), "qianwen:"+ip)
	}
}

func (h *Handler) writeQianwenServiceError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, qianwen.ErrUnauthorized):
		h.chargeQianwenBadCredential(r)
		writeError(w, http.StatusUnauthorized, "invalid qianwen credentials")
	case errors.Is(err, qianwen.ErrIdentityUnavailable), errors.Is(err, qianwen.ErrPairingAccessDenied):
		writeError(w, http.StatusForbidden, "qianwen identity is not bound")
	case errors.Is(err, qianwen.ErrInvalidInvocation), errors.Is(err, qianwen.ErrStaleInvocation):
		writeError(w, http.StatusUnauthorized, "invalid qianwen invocation")
	case errors.Is(err, qianwen.ErrInvalidRequest), errors.Is(err, qianwen.ErrUnsupportedCommand):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, qianwen.ErrRequestConflict):
		writeError(w, http.StatusConflict, "request_id was already used with a different query")
	case errors.Is(err, qianwen.ErrRequestNotFound):
		writeError(w, http.StatusNotFound, "qianwen request not found")
	case errors.Is(err, qianwen.ErrTaskNotQueued):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "qianwen request is stored but no task was queued; retry with the same request_id")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "qianwen request timed out; retry with the same request_id")
	default:
		writeError(w, http.StatusInternalServerError, "qianwen request failed")
	}
	return true
}

func qianwenBearerToken(r *http.Request) (string, bool) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func writeQianwenRateLimit(w http.ResponseWriter, limiter WebhookRateLimiter, ctx context.Context, key string) {
	retry := webhookLimiterRetryAfter(ctx, limiter, key)
	seconds := int(retry.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
}
