package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/qianwen"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	qianwenBodyLimit      = 20 * 1024
	qianwenRequestTimeout = 2500 * time.Millisecond
)

// QianwenService is the handler seam implemented by *qianwen.Service. Keeping
// it narrow lets HTTP contract tests inject a fake without a database.
type QianwenService interface {
	InstallPersonal(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) (qianwen.InstallationResult, error)
	ListByWorkspace(context.Context, pgtype.UUID) ([]db.ChannelInstallation, error)
	GetInWorkspace(context.Context, pgtype.UUID, pgtype.UUID) (db.ChannelInstallation, error)
	Revoke(context.Context, pgtype.UUID) error
	Submit(context.Context, string, string, qianwen.SubmitRequest) (qianwen.SubmitResult, error)
	Status(context.Context, string, string, string) (qianwen.RequestStatus, error)
}

type QianwenInstallationResponse struct {
	ID           string `json:"id"`
	AgentID      string `json:"agent_id"`
	ConnectionID string `json:"connection_id"`
	Mode         string `json:"mode"`
	Status       string `json:"status"`
}

type QianwenInstallResponse struct {
	QianwenInstallationResponse
	AccessToken       string `json:"access_token"`
	TokenVisibleOnce  bool   `json:"token_visible_once"`
	SubmitPath        string `json:"submit_path"`
	StatusPathPattern string `json:"status_path_pattern"`
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
// able to mint, rotate, list, or revoke a long-lived Skill credential on its
// human owner's behalf.
func requireQianwenHumanActor(w http.ResponseWriter, r *http.Request) bool {
	if isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "this endpoint is only available to human actors")
		return false
	}
	return true
}

// InstallQianwenPersonal creates or rotates an agent's private polling Skill
// credential. The plaintext token is returned once; subsequent list calls only
// expose the public connection id.
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
		writeJSON(w, http.StatusOK, map[string]any{"installations": []QianwenInstallationResponse{}, "configured": false})
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
	out := make([]QianwenInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, qianwenInstallationResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"configured":    true,
		"mode":          "personal_polling",
	})
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
	if err := h.Qianwen.Revoke(r.Context(), installationID); err != nil {
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
	result, err := h.Qianwen.Submit(ctx, connectionID, token, req)
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
	ctx, cancel := context.WithTimeout(r.Context(), qianwenRequestTimeout)
	defer cancel()
	status, err := h.Qianwen.Status(ctx, connectionID, token, chi.URLParam(r, "requestId"))
	if h.writeQianwenServiceError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, status)
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
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
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
