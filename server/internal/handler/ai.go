package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/llm"
)

// AISettings holds the workspace-level AI configuration stored inside workspace.settings["ai"].
type AISettings struct {
	Provider   string   `json:"provider"`
	APIKey     string   `json:"api_key,omitempty"`
	BaseURL    string   `json:"base_url,omitempty"`
	Model      string   `json:"model,omitempty"`
	LabelRules []string `json:"label_rules,omitempty"`
}

// AISettingsResponse is returned to clients — API key is masked.
type AISettingsResponse struct {
	Provider      string   `json:"provider"`
	APIKeyMasked  string   `json:"api_key_masked"`
	BaseURL       string   `json:"base_url"`
	Model         string   `json:"model"`
	LabelRules    []string `json:"label_rules"`
	EnvKeyPresent bool     `json:"env_key_present"`
}

// GetAISettings returns the AI configuration for the workspace.
func (h *Handler) GetAISettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	cfg := parseAISettings(ws.Settings)
	writeJSON(w, http.StatusOK, aiSettingsToResponse(cfg))
}

// UpdateAISettings saves the AI configuration for the workspace.
func (h *Handler) UpdateAISettings(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req AISettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	// Merge into existing settings.
	var existing map[string]json.RawMessage
	if ws.Settings != nil {
		json.Unmarshal(ws.Settings, &existing)
	}
	if existing == nil {
		existing = make(map[string]json.RawMessage)
	}

	aiBytes, _ := json.Marshal(req)
	existing["ai"] = aiBytes

	merged, err := json.Marshal(existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode settings")
		return
	}

	ws, err = h.Queries.UpdateWorkspace(r.Context(), buildUpdateWorkspaceParamsSettings(workspaceID, merged))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}

	cfg := parseAISettings(ws.Settings)
	writeJSON(w, http.StatusOK, aiSettingsToResponse(cfg))
}

// SuggestLabelsHandler handles POST /ai/label.
func (h *Handler) SuggestLabelsHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}
	if len(req.IssueIDs) > 20 {
		writeError(w, http.StatusBadRequest, "max 20 issues per request")
		return
	}

	client, cfg, ok := h.buildLLMClient(w, r, workspaceID)
	if !ok {
		return
	}

	svc := service.NewAILabelService(h.Queries, client)
	results, err := svc.SuggestLabels(r.Context(), workspaceID, req.IssueIDs, cfg.LabelRules)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get label suggestions: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// SuggestScheduleHandler handles POST /ai/schedule.
func (h *Handler) SuggestScheduleHandler(w http.ResponseWriter, r *http.Request) {
	workspaceID := resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req struct {
		IssueIDs []string `json:"issue_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids is required")
		return
	}
	if len(req.IssueIDs) > 20 {
		writeError(w, http.StatusBadRequest, "max 20 issues per request")
		return
	}

	client, _, ok := h.buildLLMClient(w, r, workspaceID)
	if !ok {
		return
	}

	svc := service.NewAIScheduleService(h.Queries, client)
	suggestions, err := svc.SuggestSchedule(r.Context(), workspaceID, req.IssueIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get schedule suggestions: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions})
}

// buildLLMClient resolves the AI config and constructs an LLMClient.
// Returns (client, settings, ok). Writes a 402 if no API key is available.
func (h *Handler) buildLLMClient(w http.ResponseWriter, r *http.Request, workspaceID string) (llm.LLMClient, AISettings, bool) {
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return nil, AISettings{}, false
	}

	cfg := parseAISettings(ws.Settings)

	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("DEEPSEEK_API_KEY")
	}
	if apiKey == "" {
		writeError(w, http.StatusPaymentRequired, "AI is not configured: set an API key in workspace AI settings or DEEPSEEK_API_KEY env var")
		return nil, AISettings{}, false
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	model := cfg.Model
	if model == "" {
		model = "deepseek-chat"
	}

	client := llm.NewOpenAICompatClient(llm.Config{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
	})
	return client, cfg, true
}

// parseAISettings extracts the AISettings from workspace.settings JSONB.
func parseAISettings(settingsBytes []byte) AISettings {
	if settingsBytes == nil {
		return AISettings{}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(settingsBytes, &raw); err != nil {
		return AISettings{}
	}
	aiRaw, ok := raw["ai"]
	if !ok {
		return AISettings{}
	}
	var cfg AISettings
	json.Unmarshal(aiRaw, &cfg)
	return cfg
}

func aiSettingsToResponse(cfg AISettings) AISettingsResponse {
	masked := ""
	if cfg.APIKey != "" {
		if len(cfg.APIKey) > 4 {
			masked = "sk-..." + cfg.APIKey[len(cfg.APIKey)-4:]
		} else {
			masked = "****"
		}
	}
	rules := cfg.LabelRules
	if rules == nil {
		rules = []string{}
	}
	return AISettingsResponse{
		Provider:      cfg.Provider,
		APIKeyMasked:  masked,
		BaseURL:       cfg.BaseURL,
		Model:         cfg.Model,
		LabelRules:    rules,
		EnvKeyPresent: os.Getenv("DEEPSEEK_API_KEY") != "",
	}
}

func maskAPIKey(key string) string {
	if len(key) > 4 {
		return strings.Repeat("*", len(key)-4) + key[len(key)-4:]
	}
	return "****"
}

// buildUpdateWorkspaceParamsSettings returns UpdateWorkspaceParams that only changes the settings field.
func buildUpdateWorkspaceParamsSettings(workspaceID string, settings []byte) db.UpdateWorkspaceParams {
	return db.UpdateWorkspaceParams{
		ID:       parseUUID(workspaceID),
		Settings: settings,
		// Leave all other fields as zero-value so COALESCE keeps their current values.
		Name:        pgtype.Text{},
		Description: pgtype.Text{},
		Context:     pgtype.Text{},
		Repos:       nil,
		IssuePrefix: pgtype.Text{},
	}
}
