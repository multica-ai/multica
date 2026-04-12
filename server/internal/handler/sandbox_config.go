package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type CreateSandboxConfigRequest struct {
	Name            string  `json:"name"`
	Provider        string  `json:"provider"`
	ProviderAPIKey  string  `json:"provider_api_key"`
	AIGatewayAPIKey *string `json:"ai_gateway_api_key"`
	GitPAT          *string `json:"git_pat"`
	TemplateID      *string `json:"template_id"`
	Metadata        any     `json:"metadata"`
}

type UpdateSandboxConfigRequest struct {
	Name            string  `json:"name"`
	Provider        string  `json:"provider"`
	ProviderAPIKey  string  `json:"provider_api_key"`
	AIGatewayAPIKey *string `json:"ai_gateway_api_key"`
	GitPAT          *string `json:"git_pat"`
	TemplateID      *string `json:"template_id"`
	Metadata        any     `json:"metadata"`
}

type SandboxConfigResponse struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	Name            string  `json:"name"`
	Provider        string  `json:"provider"`
	ProviderAPIKey  string  `json:"provider_api_key"`
	AIGatewayAPIKey *string `json:"ai_gateway_api_key"`
	GitPAT          *string `json:"git_pat"`
	TemplateID      *string `json:"template_id"`
	Metadata        any     `json:"metadata"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func redactKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 12 {
		if len(key) <= 2 {
			return "****"
		}
		return "****" + key[len(key)-2:]
	}
	return "****" + key[len(key)-4:]
}

func (h *Handler) sandboxConfigToResponse(cfg db.WorkspaceSandboxConfig) SandboxConfigResponse {
	encKey := h.EncryptionKey
	providerKey, _ := decryptField(cfg.ProviderApiKey, encKey, "provider-api-key")
	gatewayKey := decryptOptionalField(cfg.AiGatewayApiKey, encKey, "ai-gateway-api-key")
	gitPat := decryptOptionalField(cfg.GitPat, encKey, "git-pat")

	var metadata any
	if cfg.Metadata != nil {
		json.Unmarshal(cfg.Metadata, &metadata)
	}
	if metadata == nil {
		metadata = map[string]any{}
	}

	return SandboxConfigResponse{
		ID:              uuidToString(cfg.ID),
		WorkspaceID:     uuidToString(cfg.WorkspaceID),
		Name:            cfg.Name,
		Provider:        cfg.Provider,
		ProviderAPIKey:  redactKey(providerKey),
		AIGatewayAPIKey: redactPtr(gatewayKey),
		GitPAT:          redactPtr(gitPat),
		TemplateID:      textToPtr(cfg.TemplateID),
		Metadata:        metadata,
		CreatedAt:       timestampToString(cfg.CreatedAt),
		UpdatedAt:       timestampToString(cfg.UpdatedAt),
	}
}

// ---------------------------------------------------------------------------
// Handlers — multi-config CRUD
// ---------------------------------------------------------------------------

// CreateSandboxConfig creates a new sandbox config and auto-creates a linked cloud runtime.
// POST /api/workspaces/{id}/sandbox-configs
func (h *Handler) CreateSandboxConfig(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	encKey := h.EncryptionKey
	if encKey == nil {
		writeError(w, http.StatusServiceUnavailable, "encryption not configured")
		return
	}

	var req CreateSandboxConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if req.ProviderAPIKey == "" {
		writeError(w, http.StatusBadRequest, "provider_api_key is required")
		return
	}
	if req.Name == "" {
		req.Name = fmt.Sprintf("Cloud Runtime (%s)", req.Provider)
	}

	encProviderKey, err := encryptField(req.ProviderAPIKey, encKey, "provider-api-key")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "encryption failed")
		return
	}

	metadataBytes, _ := json.Marshal(req.Metadata)
	if req.Metadata == nil {
		metadataBytes = []byte("{}")
	}

	cfg, err := h.Queries.CreateSandboxConfig(r.Context(), db.CreateSandboxConfigParams{
		WorkspaceID:     parseUUID(wsID),
		Name:            req.Name,
		Provider:        req.Provider,
		ProviderApiKey:  encProviderKey,
		AiGatewayApiKey: encryptOptionalField(req.AIGatewayAPIKey, encKey, "ai-gateway-api-key"),
		GitPat:          encryptOptionalField(req.GitPAT, encKey, "git-pat"),
		TemplateID:      ptrToText(req.TemplateID),
		Metadata:        metadataBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create sandbox config")
		return
	}

	// Auto-create linked cloud runtime.
	configID := uuidToString(cfg.ID)
	var runtimeOwnerID pgtype.UUID
	if uid := requestUserID(r); uid != "" {
		runtimeOwnerID = parseUUID(uid)
	}
	daemonID := fmt.Sprintf("cloud-sandbox:%s", configID)
	runtimeMeta, _ := json.Marshal(map[string]string{
		"sandbox_provider":   req.Provider,
		"sandbox_config_id":  configID,
	})
	rt, rtErr := h.Queries.UpsertAgentRuntime(r.Context(), db.UpsertAgentRuntimeParams{
		WorkspaceID:     parseUUID(wsID),
		DaemonID:        pgtype.Text{String: daemonID, Valid: true},
		Name:            req.Name,
		RuntimeMode:     "cloud",
		Provider:        "opencode",
		Status:          "online",
		DeviceInfo:      "sandbox",
		Metadata:        runtimeMeta,
		OwnerID:         runtimeOwnerID,
		SandboxConfigID: cfg.ID,
	})
	if rtErr != nil {
		slog.Warn("sandbox config: create cloud runtime failed", "error", rtErr)
	} else {
		h.publish(protocol.EventDaemonRegister, wsID, "system", "", map[string]any{
			"runtimes": []map[string]string{{"id": util.UUIDToString(rt.ID)}},
		})
	}

	writeJSON(w, http.StatusCreated, h.sandboxConfigToResponse(cfg))
}

// ListSandboxConfigs returns all sandbox configs for a workspace.
// GET /api/workspaces/{id}/sandbox-configs
func (h *Handler) ListSandboxConfigs(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	if h.EncryptionKey == nil {
		writeError(w, http.StatusServiceUnavailable, "encryption not configured")
		return
	}

	configs, err := h.Queries.ListSandboxConfigsByWorkspace(r.Context(), parseUUID(wsID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sandbox configs")
		return
	}

	resp := make([]SandboxConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		resp = append(resp, h.sandboxConfigToResponse(cfg))
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSandboxConfigByID updates a specific sandbox config.
// PUT /api/workspaces/{id}/sandbox-configs/{configId}
func (h *Handler) UpdateSandboxConfigByID(w http.ResponseWriter, r *http.Request) {
	configID := chi.URLParam(r, "configId")
	encKey := h.EncryptionKey
	if encKey == nil {
		writeError(w, http.StatusServiceUnavailable, "encryption not configured")
		return
	}

	existing, err := h.Queries.GetSandboxConfigByID(r.Context(), parseUUID(configID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sandbox config not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get sandbox config")
		return
	}

	var req UpdateSandboxConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}

	// Keep existing key if not provided (frontend only has the redacted version).
	var encProviderKey string
	if req.ProviderAPIKey != "" {
		encProviderKey, err = encryptField(req.ProviderAPIKey, encKey, "provider-api-key")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encryption failed")
			return
		}
	} else {
		encProviderKey = existing.ProviderApiKey
	}

	name := req.Name
	if name == "" {
		name = existing.Name
	}

	metadataBytes, _ := json.Marshal(req.Metadata)
	if req.Metadata == nil {
		metadataBytes = []byte("{}")
	}

	cfg, err := h.Queries.UpdateSandboxConfig(r.Context(), db.UpdateSandboxConfigParams{
		ID:              parseUUID(configID),
		Name:            name,
		Provider:        req.Provider,
		ProviderApiKey:  encProviderKey,
		AiGatewayApiKey: encryptOptionalField(req.AIGatewayAPIKey, encKey, "ai-gateway-api-key"),
		GitPat:          encryptOptionalField(req.GitPAT, encKey, "git-pat"),
		TemplateID:      ptrToText(req.TemplateID),
		Metadata:        metadataBytes,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update sandbox config")
		return
	}

	writeJSON(w, http.StatusOK, h.sandboxConfigToResponse(cfg))
}

// DeleteSandboxConfigByID deletes a specific sandbox config and cleans up linked runtimes.
// DELETE /api/workspaces/{id}/sandbox-configs/{configId}
func (h *Handler) DeleteSandboxConfigByID(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	configID := chi.URLParam(r, "configId")
	configUUID := parseUUID(configID)

	// Cancel active tasks on linked runtimes and mark them offline.
	runtimes, _ := h.Queries.ListRuntimesBySandboxConfig(r.Context(), pgtype.UUID{Bytes: configUUID.Bytes, Valid: true})
	for _, rt := range runtimes {
		activeTasks, _ := h.Queries.ListActiveTasksByRuntime(r.Context(), rt.ID)
		for _, task := range activeTasks {
			if _, err := h.TaskService.CancelTask(r.Context(), task.ID); err != nil {
				slog.Warn("sandbox config delete: cancel task failed",
					"task_id", util.UUIDToString(task.ID), "error", err)
			}
		}
		h.Queries.SetAgentRuntimeOffline(r.Context(), rt.ID)
		h.publish(protocol.EventRuntimeUpdated, wsID, "system", "", map[string]string{
			"runtime_id": util.UUIDToString(rt.ID),
		})
	}

	if err := h.Queries.DeleteSandboxConfigByID(r.Context(), configUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sandbox config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Legacy compat handlers (used by settings tab)
// ---------------------------------------------------------------------------

// GetSandboxConfig returns the first sandbox config for a workspace.
// GET /api/workspaces/{id}/sandbox-config
func (h *Handler) GetSandboxConfig(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	if h.EncryptionKey == nil {
		writeError(w, http.StatusServiceUnavailable, "encryption not configured")
		return
	}

	cfg, err := h.Queries.GetSandboxConfig(r.Context(), parseUUID(wsID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "sandbox config not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get sandbox config")
		return
	}

	writeJSON(w, http.StatusOK, h.sandboxConfigToResponse(cfg))
}

// DeleteSandboxConfig deletes all sandbox configs for a workspace.
// DELETE /api/workspaces/{id}/sandbox-config
func (h *Handler) DeleteSandboxConfig(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	wsUUID := parseUUID(wsID)

	runtimes, _ := h.Queries.ListAgentRuntimes(r.Context(), wsUUID)
	for _, rt := range runtimes {
		if rt.RuntimeMode != "cloud" {
			continue
		}
		activeTasks, _ := h.Queries.ListActiveTasksByRuntime(r.Context(), rt.ID)
		for _, task := range activeTasks {
			if _, err := h.TaskService.CancelTask(r.Context(), task.ID); err != nil {
				slog.Warn("sandbox config delete: cancel task failed",
					"task_id", util.UUIDToString(task.ID), "error", err)
			}
		}
		h.Queries.SetAgentRuntimeOffline(r.Context(), rt.ID)
		h.publish(protocol.EventRuntimeUpdated, wsID, "system", "", map[string]string{
			"runtime_id": util.UUIDToString(rt.ID),
		})
	}

	if err := h.Queries.DeleteSandboxConfig(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete sandbox config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Encryption helpers
// ---------------------------------------------------------------------------

func encryptField(value string, key []byte, purpose string) (string, error) {
	derived, err := crypto.DeriveKey(key, purpose)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(value, derived)
}

func encryptOptionalField(value *string, key []byte, purpose string) pgtype.Text {
	if value == nil || *value == "" {
		return pgtype.Text{}
	}
	enc, err := encryptField(*value, key, purpose)
	if err != nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: enc, Valid: true}
}

func decryptField(ciphertext string, key []byte, purpose string) (string, error) {
	derived, err := crypto.DeriveKey(key, purpose)
	if err != nil {
		return "", err
	}
	return crypto.Decrypt(ciphertext, derived)
}

func decryptOptionalField(t pgtype.Text, key []byte, purpose string) *string {
	if !t.Valid || t.String == "" {
		return nil
	}
	plain, err := decryptField(t.String, key, purpose)
	if err != nil {
		return nil
	}
	return &plain
}

func redactPtr(s *string) *string {
	if s == nil {
		return nil
	}
	r := redactKey(*s)
	return &r
}
