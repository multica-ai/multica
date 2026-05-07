package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// PreviewGeneratorClient is a thin HTTP client for the agent-preview-generator
// internal service. It is constructed eagerly in router.go when
// PREVIEW_GENERATOR_URL is set, before the router accepts traffic, so
// concurrent requests never race on initialization.
type PreviewGeneratorClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewPreviewGeneratorClient constructs a PreviewGeneratorClient. Call this in
// router.go after handler.New() so h.PreviewClient is set once, before the
// router accepts traffic.
func NewPreviewGeneratorClient(baseURL string) *PreviewGeneratorClient {
	return &PreviewGeneratorClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// proxyToPreviewGenerator forwards a request to the preview generator and
// copies the response (status + body) back to the caller.
func (h *Handler) proxyToPreviewGenerator(w http.ResponseWriter, method, path string, body []byte) {
	if h.PreviewClient == nil {
		writeError(w, http.StatusServiceUnavailable, "PREVIEW_GENERATOR_URL is not configured")
		return
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, h.PreviewClient.baseURL+path, reqBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build upstream request: "+err.Error())
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := h.PreviewClient.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "preview generator unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read upstream response")
		return
	}

	// Only set Content-Type when there is a body to describe.
	if resp.StatusCode != http.StatusNoContent && len(respBody) > 0 {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// CreatePreview proxies POST /api/previews → POST /api/v1/previews
// ---------------------------------------------------------------------------

// CreatePreviewRequest is the client-facing payload (workspace_id is injected
// from the auth context, not read from the body).
type CreatePreviewRequest struct {
	App      string `json:"app"`
	IssueID  string `json:"issue_id"`
	Repo     string `json:"repo"`
	ImageTag string `json:"image_tag"`
}

func (h *Handler) CreatePreview(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req CreatePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.App == "" {
		writeError(w, http.StatusBadRequest, "app is required")
		return
	}
	if req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	if req.ImageTag == "" {
		writeError(w, http.StatusBadRequest, "image_tag is required")
		return
	}

	// Inject the workspace_id from the auth context into the upstream payload
	// so the preview generator can scope previews to the correct workspace.
	upstream := map[string]any{
		"app":          req.App,
		"workspace_id": workspaceID,
		"issue_id":     req.IssueID,
		"repo":         req.Repo,
		"image_tag":    req.ImageTag,
	}

	body, err := json.Marshal(upstream)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode upstream request")
		return
	}

	h.proxyToPreviewGenerator(w, http.MethodPost, "/api/v1/previews", body)
}

// ---------------------------------------------------------------------------
// ListPreviews proxies GET /api/previews → GET /api/v1/previews
// ---------------------------------------------------------------------------

func (h *Handler) ListPreviews(w http.ResponseWriter, r *http.Request) {
	h.proxyToPreviewGenerator(w, http.MethodGet, "/api/v1/previews", nil)
}

// ---------------------------------------------------------------------------
// GetPreview proxies GET /api/previews/{id} → GET /api/v1/previews/{id}
// ---------------------------------------------------------------------------

func (h *Handler) GetPreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "preview id is required")
		return
	}
	h.proxyToPreviewGenerator(w, http.MethodGet, "/api/v1/previews/"+id, nil)
}

// ---------------------------------------------------------------------------
// DeletePreview proxies DELETE /api/previews/{id} → DELETE /api/v1/previews/{id}
// ---------------------------------------------------------------------------

func (h *Handler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "preview id is required")
		return
	}
	h.proxyToPreviewGenerator(w, http.MethodDelete, "/api/v1/previews/"+id, nil)
}
