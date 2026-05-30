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

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/multica-ai/multica/server/internal/cloudruntime"
	"github.com/multica-ai/multica/server/internal/logger"
)

const maxCloudRuntimeRequestBodySize = 1 << 20

type cloudRuntimeProxyOptions struct {
	withUserID bool
	withQuery  bool
	withBody   bool
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
	// Cloud now mints a node-scoped mcn_ PAT itself during /api/v1/nodes
	// and injects it into the EC2 instance via SSM bootstrap (see
	// multica-cloud docs/api/node-pat.md). We no longer forward the
	// caller's mul_ PAT — Fleet doesn't need it, and propagating a
	// long-lived user PAT into a remote machine widened the blast
	// radius of any node compromise. Hence the handler now mirrors
	// the other write endpoints: just the body, no PAT plumbing.
	h.proxyCloudRuntime(w, r, http.MethodPost, "/api/v1/nodes", cloudRuntimeProxyOptions{
		withUserID: true,
		withBody:   true,
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
		RequestID: cloudRuntimeRequestID(r),
	})
	if err != nil {
		writeCloudRuntimeError(w, r, err)
		return
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
