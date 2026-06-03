package agentavatar

// Workspace-logo AI generation (FIR-2580). Reuses the same Data Registry AI
// gateway plumbing as agent avatars (gatewayConfig + callGateway + storage),
// but produces a small set of square app-icon options from a free-text prompt
// and persists nothing — the renderer saves the chosen URL via UpdateWorkspace,
// which is owner/admin gated.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
)

// maxLogoCount caps how many icon variants one request may generate. Five gives
// the user a real choice without fanning out an unbounded number of paid image
// calls per click.
const maxLogoCount = 5

type generateLogosRequest struct {
	Prompt string `json:"prompt"`
	Count  int    `json:"count,omitempty"`
}

type generateLogosResponse struct {
	URLs []string `json:"urls"`
}

// GenerateLogos handles POST /api/workspaces/{id}/generate-logo. It generates up
// to maxLogoCount square app-icon images from the caller's prompt, uploads each
// to workspace storage, and returns their URLs for the user to choose from. It
// does NOT write avatar_url — the renderer persists the chosen URL through
// UpdateWorkspace. The route is mounted under the owner/admin role middleware,
// so a forged member call is rejected with 403 before reaching this handler.
func (h *Handler) GenerateLogos(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req generateLogosRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	count := req.Count
	if count <= 0 {
		count = maxLogoCount
	}
	if count > maxLogoCount {
		count = maxLogoCount
	}

	cfg := h.gatewayConfig(r.Context(), wsID)
	if cfg.baseURL == "" || cfg.apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "image generation not configured: data registry AI gateway URL/key unset")
		return
	}

	urls := h.generateLogoSet(r.Context(), wsID, cfg, prompt, count)
	if len(urls) == 0 {
		slog.Error("workspace logo generation produced no images", logger.RequestAttrs(r)...)
		writeError(w, http.StatusInternalServerError, "image generation failed")
		return
	}

	writeJSON(w, http.StatusOK, generateLogosResponse{URLs: urls})
}

// generateLogoSet generates `count` icon variants from one prompt concurrently,
// uploads each PNG, and returns the successful URLs. Partial failures are
// tolerated: as long as one variant succeeds the user still gets choices; an
// empty slice means every variant failed.
func (h *Handler) generateLogoSet(ctx context.Context, wsID string, cfg gatewayConfig, prompt string, count int) []string {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		urls = make([]string, 0, count)
	)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(variant int) {
			defer wg.Done()
			img, err := callGateway(ctx, h.httpClient, cfg, buildLogoPrompt(prompt, variant))
			if err != nil {
				slog.Warn("workspace logo variant generation failed", "workspace_id", wsID, "variant", variant, "error", err)
				return
			}
			id, _ := uuid.NewV7()
			key := fmt.Sprintf("workspaces/%s/avatars/%s.png", wsID, id)
			url, err := h.storage.Upload(ctx, key, img, "image/png", id.String()+".png")
			if err != nil {
				slog.Warn("workspace logo variant upload failed", "workspace_id", wsID, "variant", variant, "error", err)
				return
			}
			mu.Lock()
			urls = append(urls, url)
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return urls
}

// buildLogoPrompt wraps the user's idea in an app-icon template that steers the
// model toward a clean, high-end square logo, and appends a per-variant nudge so
// the generated options come out visibly different from one another.
func buildLogoPrompt(userPrompt string, variant int) string {
	return fmt.Sprintf(
		"A high-end, premium app icon: %s. "+
			"Clean modern minimal flat vector logo, bold and instantly recognisable even at small sizes, "+
			"centered with balanced composition on a simple solid or subtle-gradient background, crisp edges, "+
			"no text, no words, no letters, no watermark. Square 1:1 app-icon format. "+
			"Distinct design concept, variation %d.",
		strings.TrimSpace(userPrompt), variant+1,
	)
}
