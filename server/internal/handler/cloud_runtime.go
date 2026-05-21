package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const maxCloudRuntimeRequestBodySize = 1 << 20

type cloudRuntimeProxyOptions struct {
	withUserID  bool
	withQuery   bool
	withBody    bool
	withUserPAT bool
}

func (h *Handler) GetCloudRuntimeService(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/", cloudRuntimeProxyOptions{
		withUserID: true,
	})
}

func (h *Handler) GetCloudRuntimeHealth(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/healthz", cloudRuntimeProxyOptions{})
}

func (h *Handler) GetCloudRuntimeReady(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/readyz", cloudRuntimeProxyOptions{})
}

func (h *Handler) ListCloudRuntimeNodes(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodGet, "/api/v1/nodes", cloudRuntimeProxyOptions{
		withUserID: true,
		withQuery:  true,
	})
}

func (h *Handler) CreateCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes", cloudRuntimeProxyOptions{
		withUserID:  true,
		withBody:    true,
		withUserPAT: true,
	})
}

func (h *Handler) DeleteCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodDelete, "/api/v1/nodes", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) StartCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes/start", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) StopCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes/stop", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) RebootCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes/reboot", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) GetCloudRuntimeNodeStatus(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes/status", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) ExecCloudRuntimeNode(w http.ResponseWriter, r *http.Request) {
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes/exec", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
	})
}

func (h *Handler) proxyCloudRuntime(w http.ResponseWriter, r *http.Request, method, path string, opts cloudRuntimeProxyOptions) {
	if h.CloudRuntime == nil || !h.CloudRuntime.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "cloud runtime is not configured")
		return
	}

	var userID string
	if opts.withUserID {
		var ok bool
		userID, ok = requireUserID(w, r)
		if !ok {
			return
		}
	}

	var body []byte
	if opts.withBody {
		var ok bool
		body, ok = readCloudRuntimeJSONBody(w, r)
		if !ok {
			return
		}
	}

	userPAT, patGenerated, ok := h.cloudRuntimeUserPAT(w, r, userID, opts.withUserPAT)
	if !ok {
		return
	}

	var query url.Values
	if opts.withQuery {
		query = r.URL.Query()
	}

	resp, err := h.CloudRuntime.Do(r.Context(), cloudruntime.Request{
		Method:    method,
		Path:      path,
		Query:     query,
		Body:      body,
		UserID:    userID,
		UserPAT:   userPAT,
		RequestID: cloudRuntimeRequestID(r),
	})
	if err != nil {
		if patGenerated {
			h.revokeGeneratedPAT(r.Context(), userPAT)
		}
		writeCloudRuntimeError(w, r, err)
		return
	}
	if patGenerated && resp.StatusCode >= 300 {
		h.revokeGeneratedPAT(r.Context(), userPAT)
	}
	writeCloudRuntimeResponse(w, resp)
}

func readCloudRuntimeJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCloudRuntimeRequestBodySize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	if len(bytes.TrimSpace(data)) == 0 {
		writeError(w, http.StatusBadRequest, "request body is required")
		return nil, false
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	return data, true
}

func cloudRuntimeRequestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return chimw.GetReqID(r.Context())
}

func (h *Handler) cloudRuntimeUserPAT(w http.ResponseWriter, r *http.Request, userID string, enabled bool) (pat string, generated bool, ok bool) {
	if !enabled {
		return "", false, true
	}
	if p := strings.TrimSpace(r.Header.Get("X-User-PAT")); p != "" {
		if !strings.HasPrefix(p, "mul_") {
			writeError(w, http.StatusBadRequest, "invalid X-User-PAT")
			return "", false, false
		}
		if h.Queries == nil {
			writeError(w, http.StatusInternalServerError, "failed to validate X-User-PAT")
			return "", false, false
		}
		token, err := h.Queries.GetPersonalAccessTokenByHash(r.Context(), auth.HashToken(p))
		if err != nil || uuidToString(token.UserID) != userID {
			writeError(w, http.StatusForbidden, "invalid X-User-PAT")
			return "", false, false
		}
		return p, false, true
	}

	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if strings.HasPrefix(authHeader, prefix+"mul_") {
		return strings.TrimPrefix(authHeader, prefix), false, true
	}

	// Auto-generate a PAT for cloud runtime bootstrap.
	p, err := h.generateCloudRuntimePAT(r.Context(), userID)
	if err != nil {
		slog.Error("failed to generate cloud runtime PAT", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to generate cloud runtime PAT")
		return "", false, false
	}
	return p, true, true
}

func (h *Handler) generateCloudRuntimePAT(ctx context.Context, userID string) (string, error) {
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		return "", err
	}
	prefix := rawToken[:12]
	_, err = h.Queries.CreatePersonalAccessToken(ctx, db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(userID),
		Name:        "Cloud Runtime (auto)",
		TokenHash:   auth.HashToken(rawToken),
		TokenPrefix: prefix,
	})
	if err != nil {
		return "", err
	}
	return rawToken, nil
}

func (h *Handler) revokeGeneratedPAT(ctx context.Context, rawToken string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	hash := auth.HashToken(rawToken)
	if _, err := h.DB.Exec(ctx, `UPDATE personal_access_token SET revoked = TRUE WHERE token_hash = $1`, hash); err != nil {
		slog.Warn("failed to revoke auto-generated cloud runtime PAT", "error", err)
	}
	h.PATCache.Invalidate(ctx, hash)
}

func writeCloudRuntimeResponse(w http.ResponseWriter, resp *cloudruntime.Response) {
	if requestID := resp.Header.Get("X-Request-ID"); requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}
	body := bytes.TrimSpace(resp.Body)
	if len(body) == 0 {
		w.WriteHeader(resp.StatusCode)
		return
	}
	if json.Valid(body) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}
	writeJSON(w, resp.StatusCode, map[string]string{"error": string(body)})
}

func writeCloudRuntimeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, cloudruntime.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "cloud runtime is not configured")
	case errors.Is(err, cloudruntime.ErrInvalidBaseURL):
		writeError(w, http.StatusServiceUnavailable, "cloud runtime is misconfigured")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "cloud runtime request timed out")
	default:
		slog.Warn("cloud runtime request failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusBadGateway, "cloud runtime request failed")
	}
}
