package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/sandbox"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Known cloud agent providers
// ---------------------------------------------------------------------------

type cloudAgentProvider struct {
	Name       string   `json:"name"`
	Display    string   `json:"display"`
	DetectCmd  []string `json:"-"`
	InstallCmd string   `json:"-"`
}

var knownCloudProviders = []cloudAgentProvider{
	{Name: "opencode", Display: "OpenCode", DetectCmd: []string{"sh", "-lc", "command -v opencode || which opencode || ls /root/.local/bin/opencode 2>/dev/null || ls /usr/local/bin/opencode 2>/dev/null"}, InstallCmd: "curl -fsSL https://opencode.ai/install.sh | sh"},
	{Name: "claude", Display: "Claude Code", DetectCmd: []string{"sh", "-lc", "command -v claude || which claude || ls /root/.local/bin/claude 2>/dev/null || ls /usr/local/bin/claude 2>/dev/null"}, InstallCmd: ""},
	{Name: "codex", Display: "Codex", DetectCmd: []string{"sh", "-lc", "command -v codex || which codex || ls /root/.local/bin/codex 2>/dev/null || ls /usr/local/bin/codex 2>/dev/null"}, InstallCmd: ""},
}

func findCloudProvider(name string) *cloudAgentProvider {
	for i := range knownCloudProviders {
		if knownCloudProviders[i].Name == name {
			return &knownCloudProviders[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

type ProviderDetectionResult struct {
	Name      string `json:"name"`
	Display   string `json:"display"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	CanInstall bool  `json:"can_install"`
}

type ProviderInstallResult struct {
	Name      string `json:"name"`
	Success   bool   `json:"success"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	DurationMs int64 `json:"duration_ms"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// DetectProviders spins up a sandbox and checks which agent tools are installed.
// POST /api/runtimes/{runtimeId}/detect-providers
func (h *Handler) DetectProviders(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}
	if rt.RuntimeMode != "cloud" {
		writeError(w, http.StatusBadRequest, "provider detection is only available for cloud runtimes")
		return
	}
	if !rt.SandboxConfigID.Valid {
		writeError(w, http.StatusBadRequest, "runtime has no linked sandbox config")
		return
	}

	// Load sandbox config.
	cfg, err := h.Queries.GetSandboxConfigByID(r.Context(), rt.SandboxConfigID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sandbox config")
		return
	}
	providerKey, err := h.decryptSandboxKey(cfg.ProviderApiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt provider key")
		return
	}

	// Create sandbox.
	provider := sandbox.NewE2BProvider(providerKey)
	sb, err := provider.CreateOrConnect(r.Context(), "", sandbox.CreateOpts{
		TemplateID: pgTextToString(cfg.TemplateID),
		Timeout:    2 * time.Minute,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to create sandbox: %v", err))
		return
	}
	defer func() {
		if destroyErr := provider.Destroy(r.Context(), sb.ID); destroyErr != nil {
			slog.Warn("detect-providers: sandbox cleanup failed", "error", destroyErr)
		}
	}()

	// Detect each provider.
	results := make([]ProviderDetectionResult, 0, len(knownCloudProviders))
	for _, kp := range knownCloudProviders {
		stdout, execErr := provider.Exec(r.Context(), sb, kp.DetectCmd)
		installed := execErr == nil && strings.TrimSpace(stdout) != ""
		results = append(results, ProviderDetectionResult{
			Name:       kp.Name,
			Display:    kp.Display,
			Installed:  installed,
			Path:       strings.TrimSpace(stdout),
			CanInstall: kp.InstallCmd != "",
		})
	}

	// Persist detection results in sandbox config metadata (keyed by template_id).
	templateID := pgTextToString(cfg.TemplateID)
	if templateID == "" {
		templateID = "base"
	}
	h.persistDetectionResults(r.Context(), cfg, templateID, results)

	writeJSON(w, http.StatusOK, results)
}

// InstallProvider installs an agent tool in a sandbox and verifies it.
// POST /api/runtimes/{runtimeId}/install-provider
func (h *Handler) InstallProvider(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}
	if rt.RuntimeMode != "cloud" {
		writeError(w, http.StatusBadRequest, "install is only available for cloud runtimes")
		return
	}
	if !rt.SandboxConfigID.Valid {
		writeError(w, http.StatusBadRequest, "runtime has no linked sandbox config")
		return
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kp := findCloudProvider(req.Provider)
	if kp == nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown provider: %s", req.Provider))
		return
	}
	if kp.InstallCmd == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("install not yet supported for %s", kp.Display))
		return
	}

	// Load sandbox config.
	cfg, err := h.Queries.GetSandboxConfigByID(r.Context(), rt.SandboxConfigID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sandbox config")
		return
	}
	providerKey, err := h.decryptSandboxKey(cfg.ProviderApiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt provider key")
		return
	}

	start := time.Now()
	sbProvider := sandbox.NewE2BProvider(providerKey)
	sb, err := sbProvider.CreateOrConnect(r.Context(), "", sandbox.CreateOpts{
		TemplateID: pgTextToString(cfg.TemplateID),
		Timeout:    5 * time.Minute,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, ProviderInstallResult{
			Name: kp.Name, Success: false,
			Error: fmt.Sprintf("sandbox creation failed: %v", err), DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}
	defer func() {
		if destroyErr := sbProvider.Destroy(r.Context(), sb.ID); destroyErr != nil {
			slog.Warn("install-provider: sandbox cleanup failed", "error", destroyErr)
		}
	}()

	// Run install command.
	slog.Info("install-provider: running install", "provider", kp.Name, "runtime_id", util.UUIDToString(rt.ID))
	installOut, installErr := sbProvider.Exec(r.Context(), sb, []string{"sh", "-c", kp.InstallCmd})
	if installErr != nil {
		writeJSON(w, http.StatusOK, ProviderInstallResult{
			Name: kp.Name, Success: false,
			Output: installOut, Error: fmt.Sprintf("install failed: %v", installErr),
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	// Verify installation.
	verifyOut, verifyErr := sbProvider.Exec(r.Context(), sb, kp.DetectCmd)
	if verifyErr != nil || strings.TrimSpace(verifyOut) == "" {
		writeJSON(w, http.StatusOK, ProviderInstallResult{
			Name: kp.Name, Success: false,
			Output: installOut, Error: "install completed but binary not found in PATH",
			DurationMs: time.Since(start).Milliseconds(),
		})
		return
	}

	writeJSON(w, http.StatusOK, ProviderInstallResult{
		Name: kp.Name, Success: true,
		Output: fmt.Sprintf("installed at %s", strings.TrimSpace(verifyOut)),
		DurationMs: time.Since(start).Milliseconds(),
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) persistDetectionResults(ctx context.Context, cfg db.WorkspaceSandboxConfig, templateID string, results []ProviderDetectionResult) {
	var metadata map[string]any
	if cfg.Metadata != nil {
		json.Unmarshal(cfg.Metadata, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	detected, _ := metadata["detected_providers"].(map[string]any)
	if detected == nil {
		detected = map[string]any{}
	}
	detected[templateID] = map[string]any{
		"providers":   results,
		"detected_at": time.Now().UTC().Format(time.RFC3339),
	}
	metadata["detected_providers"] = detected

	metaBytes, _ := json.Marshal(metadata)
	h.Queries.UpdateSandboxConfig(ctx, db.UpdateSandboxConfigParams{
		ID:              cfg.ID,
		Name:            cfg.Name,
		Provider:        cfg.Provider,
		ProviderApiKey:  cfg.ProviderApiKey,
		AiGatewayApiKey: cfg.AiGatewayApiKey,
		GitPat:          cfg.GitPat,
		TemplateID:      cfg.TemplateID,
		Metadata:        metaBytes,
	})
}

func (h *Handler) decryptSandboxKey(ciphertext string) (string, error) {
	if h.EncryptionKey == nil {
		return "", fmt.Errorf("encryption not configured")
	}
	derived, err := crypto.DeriveKey(h.EncryptionKey, "provider-api-key")
	if err != nil {
		return "", err
	}
	return crypto.Decrypt(ciphertext, derived)
}

func pgTextToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

// ---------------------------------------------------------------------------
// List E2B templates
// ---------------------------------------------------------------------------

type SandboxTemplateResponse struct {
	TemplateID string   `json:"template_id"`
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
	CPUCount   int      `json:"cpu_count"`
	MemoryMB   int      `json:"memory_mb"`
}

// ListTemplates proxies the E2B templates API using the runtime's sandbox config API key.
// GET /api/runtimes/{runtimeId}/templates
func (h *Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")

	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found"); !ok {
		return
	}
	if !rt.SandboxConfigID.Valid {
		writeError(w, http.StatusBadRequest, "runtime has no linked sandbox config")
		return
	}

	cfg, err := h.Queries.GetSandboxConfigByID(r.Context(), rt.SandboxConfigID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load sandbox config")
		return
	}
	apiKey, err := h.decryptSandboxKey(cfg.ProviderApiKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt provider key")
		return
	}

	templates, err := fetchE2BTemplates(r.Context(), apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch templates: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

// ListTemplatesByKey fetches E2B templates using a raw API key (for use before runtime exists).
// POST /api/sandbox/templates
func (h *Handler) ListTemplatesByKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderAPIKey string `json:"provider_api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ProviderAPIKey == "" {
		writeError(w, http.StatusBadRequest, "provider_api_key is required")
		return
	}

	templates, err := fetchE2BTemplates(r.Context(), req.ProviderAPIKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("failed to fetch templates: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, templates)
}

func fetchE2BTemplates(ctx context.Context, apiKey string) ([]SandboxTemplateResponse, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.e2b.app/templates", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("E2B API returned %d: %s", resp.StatusCode, string(body))
	}

	var raw []struct {
		TemplateID  string   `json:"templateID"`
		Aliases     []string `json:"aliases"`
		CPUCount    int      `json:"cpuCount"`
		MemoryMB    int      `json:"memoryMB"`
		BuildStatus string   `json:"buildStatus"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	result := make([]SandboxTemplateResponse, 0, len(raw))
	for _, t := range raw {
		if t.BuildStatus != "ready" {
			continue
		}
		name := t.TemplateID
		if len(t.Aliases) > 0 {
			name = t.Aliases[0]
		}
		result = append(result, SandboxTemplateResponse{
			TemplateID: t.TemplateID,
			Name:       name,
			Aliases:    t.Aliases,
			CPUCount:   t.CPUCount,
			MemoryMB:   t.MemoryMB,
		})
	}
	return result, nil
}
