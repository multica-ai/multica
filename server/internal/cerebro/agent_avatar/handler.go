package agentavatar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/storage"
)

const openRouterModel = "openai/gpt-5-image-mini"
const openRouterEndpoint = "https://openrouter.ai/api/v1/chat/completions"

// clothingColors drives visual diversity across agents. The color for each
// agent is derived deterministically from its name so the result is stable
// across re-generations without a stored seed.
var clothingColors = []string{
	"navy blue", "forest green", "burgundy", "warm grey",
	"mustard yellow", "terracotta", "sage green", "slate blue",
	"plum", "rust orange", "teal", "cream white",
}

// Handler generates photorealistic agent avatars via OpenRouter gpt-5-image-mini.
type Handler struct {
	storage storage.Storage
}

// New creates a Handler. store is used to persist the generated PNG.
func New(store storage.Storage) *Handler {
	return &Handler{storage: store}
}

type generateRequest struct {
	AgentName    string `json:"agent_name"`
	CustomPrompt string `json:"custom_prompt,omitempty"`
}

type generateResponse struct {
	URL string `json:"url"`
}

// Generate handles POST /api/agents/generate-avatar.
// Accepts agent_name (for auto-prompt colour selection) and an optional
// custom_prompt. Returns the CDN URL of the uploaded PNG.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req generateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "image generation not configured: OPENROUTER_API_KEY unset")
		return
	}

	prompt := buildPrompt(req.AgentName, req.CustomPrompt)
	imgBytes, err := callOpenRouter(r.Context(), apiKey, prompt)
	if err != nil {
		slog.Error("avatar generation failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "image generation failed")
		return
	}

	id, _ := uuid.NewV7()
	key := fmt.Sprintf("workspaces/%s/avatars/%s.png", wsID, id)
	url, err := h.storage.Upload(r.Context(), key, imgBytes, "image/png", id.String()+".png")
	if err != nil {
		slog.Error("avatar upload failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}

	writeJSON(w, http.StatusOK, generateResponse{URL: url})
}

// buildPrompt returns the generation prompt. A custom prompt is used as-is;
// otherwise a Scandinavian-appearance prompt with a name-derived clothing
// colour ensures every agent looks distinct without user effort.
func buildPrompt(agentName, customPrompt string) string {
	if customPrompt != "" {
		return customPrompt
	}
	color := clothingColors[nameColorIndex(agentName)]
	return fmt.Sprintf(
		"Photorealistic portrait of a person with Scandinavian appearance, "+
			"%s clothing, professional headshot, soft neutral background, "+
			"natural light, high quality photo, square format, solo subject",
		color,
	)
}

func nameColorIndex(name string) int {
	h := 0
	for _, c := range name {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return h % len(clothingColors)
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Images  []string `json:"images"`
			Content any      `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func callOpenRouter(ctx context.Context, apiKey, prompt string) ([]byte, error) {
	body := map[string]any{
		"model":      openRouterModel,
		"modalities": []string{"image"},
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterEndpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://multica.io")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openrouter: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openrouter returned HTTP %d: %.512s", resp.StatusCode, string(respBody))
	}

	var parsed openRouterResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 || len(parsed.Choices[0].Message.Images) == 0 {
		return nil, fmt.Errorf("no image in OpenRouter response")
	}

	imgData := parsed.Choices[0].Message.Images[0]
	// Strip "data:image/png;base64," prefix when present.
	if idx := strings.Index(imgData, ","); idx != -1 {
		imgData = imgData[idx+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(imgData)
	if err != nil {
		// Fall back to URL-safe encoding (some providers use it).
		decoded, err = base64.URLEncoding.DecodeString(imgData)
		if err != nil {
			return nil, fmt.Errorf("decode base64 image: %w", err)
		}
	}
	return decoded, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
