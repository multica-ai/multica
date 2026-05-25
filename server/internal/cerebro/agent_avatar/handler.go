package agentavatar

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultAvatarModel = "openai/gpt-5-image-mini"
const gatewayChatCompletionsPath = "/api/ai/proxy/v1/chat/completions"

// Avatar visual identity is derived deterministically from the agent name so
// re-generations stay stable without storing a seed. The background colour is
// the primary way to tell agents apart at a glance (Jesper's requirement:
// "om ikke andet baggrundsfarver"), so backgrounds carry the distinct hue while
// clothing stays professionally neutral and the person (gender/hair) varies
// independently — making every agent read as a different individual.

// backgroundColors are the solid studio backdrops behind each portrait. Twelve
// clearly distinct, muted-professional hues so adjacent agents never share a
// background.
var backgroundColors = []string{
	"deep teal", "warm amber", "dusty rose", "slate blue",
	"muted sage green", "soft plum", "terracotta", "steel grey",
	"ocean blue", "olive gold", "burgundy red", "warm indigo",
}

// clothingColors stay neutral so the background carries the colour identity and
// never clashes with it.
var clothingColors = []string{
	"a navy blazer", "a charcoal blazer", "a dark grey suit",
	"a black turtleneck", "a crisp white shirt", "a deep navy sweater",
}

// portraitGenders and hairStyles vary the subject so two agents never look like
// the same person, while keeping the Scandinavian brief.
var portraitGenders = []string{"man", "woman"}

var hairStyles = []string{
	"short blond", "light brown", "dark brown",
	"auburn", "ash grey", "short black",
}

// workspaceLoader reads a workspace (for its gateway settings). *db.Queries
// satisfies it; tests supply a stub.
type workspaceLoader interface {
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}

// Handler generates photorealistic agent avatars via the Firtal Data Registry AI Gateway.
type Handler struct {
	storage    storage.Storage
	workspaces workspaceLoader
	httpClient *http.Client
}

// New creates a Handler. store persists the generated PNG; workspaces supplies
// the per-workspace gateway credentials (with env fallback).
func New(store storage.Storage, workspaces workspaceLoader) *Handler {
	return &Handler{storage: store, workspaces: workspaces, httpClient: http.DefaultClient}
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

	cfg := h.gatewayConfig(r.Context(), wsID)
	if cfg.baseURL == "" || cfg.apiKey == "" {
		writeError(w, http.StatusServiceUnavailable, "image generation not configured: data registry AI gateway URL/key unset")
		return
	}

	prompt := buildPrompt(req.AgentName, req.CustomPrompt)
	imgBytes, err := callGateway(r.Context(), h.httpClient, cfg, prompt)
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
// otherwise a name-seeded prompt produces a photorealistic Scandinavian
// headshot with a distinct background colour (the per-agent identifier) and a
// person that varies by gender/hair so every agent reads as a real, different
// individual. The closing clause is what keeps the model on photographs rather
// than the flat illustrations it otherwise drifts to.
func buildPrompt(agentName, customPrompt string) string {
	if customPrompt != "" {
		return customPrompt
	}
	background := backgroundColors[nameIndex(agentName, "bg", len(backgroundColors))]
	clothing := clothingColors[nameIndex(agentName, "clothes", len(clothingColors))]
	gender := portraitGenders[nameIndex(agentName, "gender", len(portraitGenders))]
	hair := hairStyles[nameIndex(agentName, "hair", len(hairStyles))]
	return fmt.Sprintf(
		"Photorealistic professional headshot portrait of a %s with Scandinavian features, "+
			"%s hair, wearing %s, against a solid %s studio background, "+
			"soft natural studio lighting, sharp focus, shallow depth of field, "+
			"high-quality DSLR photograph, square 1:1 format, centered, single person. "+
			"A realistic photo of a real person — not an illustration, cartoon, drawing, or 3D render.",
		gender, hair, clothing, background,
	)
}

// nameIndex deterministically maps an agent name into [0,n) for a given trait
// salt. The salt makes background/clothing/gender/hair vary independently, so
// two agents that collide on one trait still differ on the others. FNV-1a gives
// a well-distributed hash — a hand-rolled one bunched names onto the same colour.
func nameIndex(name, salt string, n int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(salt + "\x00" + name))
	return int(h.Sum32() % uint32(n))
}

type gatewayResponse struct {
	Choices []struct {
		Message struct {
			Images  []imageRef `json:"images"`
			Content any        `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// imageRef extracts a base64 data URL from a gateway image entry. OpenRouter
// (which the Data Registry gateway proxies) returns each image as an object
// shaped like {"type":"image_url","image_url":{"url":"data:image/png;base64,…"}}.
// Some providers/tests emit a bare string instead, so accept both forms.
type imageRef struct {
	URL string
}

func (r *imageRef) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		r.URL = s
		return nil
	}
	var obj struct {
		URL      string `json:"url"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	if obj.ImageURL.URL != "" {
		r.URL = obj.ImageURL.URL
	} else {
		r.URL = obj.URL
	}
	return nil
}

type gatewayConfig struct {
	baseURL string
	apiKey  string
	model   string
}

func gatewayConfigFromEnv() gatewayConfig {
	model := strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AVATAR_MODEL"))
	if model == "" {
		model = defaultAvatarModel
	}
	return gatewayConfig{
		baseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL")), "/"),
		apiKey:  strings.TrimSpace(os.Getenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY")),
		model:   model,
	}
}

// workspaceGatewaySettings mirrors the firtal_gateway block of a workspace's
// settings JSON — the same source the gateway runtime reads its credentials
// from. Avatar generation must use this so the key configured in the workspace
// Settings UI is honoured, instead of a separate (and easily stale) server env var.
type workspaceGatewaySettings struct {
	FirtalGateway *struct {
		GatewayURL string `json:"gateway_url"`
		APIKey     string `json:"api_key"`
	} `json:"firtal_gateway"`
}

// gatewayConfig resolves credentials for the workspace: the workspace-settings
// gateway URL/key take precedence, falling back to the server env (which also
// supplies the model default). This mirrors FirtalGatewayConfigFromWorkspaceSettings
// used by the gateway runtime, keeping avatar generation on the same credential.
func (h *Handler) gatewayConfig(ctx context.Context, wsID string) gatewayConfig {
	cfg := gatewayConfigFromEnv()
	if h.workspaces == nil {
		return cfg
	}
	id, err := util.ParseUUID(wsID)
	if err != nil {
		return cfg
	}
	ws, err := h.workspaces.GetWorkspace(ctx, id)
	if err != nil || len(ws.Settings) == 0 {
		return cfg
	}
	var s workspaceGatewaySettings
	if err := json.Unmarshal(ws.Settings, &s); err != nil || s.FirtalGateway == nil {
		return cfg
	}
	if u := strings.TrimRight(strings.TrimSpace(s.FirtalGateway.GatewayURL), "/"); u != "" {
		cfg.baseURL = u
	}
	if k := strings.TrimSpace(s.FirtalGateway.APIKey); k != "" {
		cfg.apiKey = k
	}
	return cfg
}

func callGateway(ctx context.Context, client *http.Client, cfg gatewayConfig, prompt string) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body := map[string]any{
		"model":      cfg.model,
		"modalities": []string{"image"},
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+gatewayChatCompletionsPath, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-trace-name", "agent-avatar-generate")
	req.Header.Set("x-tags", "multica,agent-avatar")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call data registry AI gateway: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("data registry AI gateway returned HTTP %d: %.512s", resp.StatusCode, string(respBody))
	}

	var parsed gatewayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 || len(parsed.Choices[0].Message.Images) == 0 {
		return nil, fmt.Errorf("no image in data registry AI gateway response")
	}

	imgData := parsed.Choices[0].Message.Images[0].URL
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
