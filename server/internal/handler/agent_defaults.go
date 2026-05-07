package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GetPersonalAgentDefaults returns the current user's personal agent defaults
// for the workspace. Returns an empty config object when no record exists.
func (h *Handler) GetPersonalAgentDefaults(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	cfg, err := h.Queries.GetMemberAgentConfig(r.Context(), db.GetMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"config": map[string]any{}})
			return
		}
		slog.Warn("get member agent config failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to load agent defaults")
		return
	}

	var config any
	if err := json.Unmarshal(cfg.Config, &config); err != nil {
		config = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(cfg.ID),
		"config":     config,
		"created_at": timestampToString(cfg.CreatedAt),
		"updated_at": timestampToString(cfg.UpdatedAt),
	})
}

type updatePersonalAgentDefaultsRequest struct {
	Config any `json:"config"`
}

// UpdatePersonalAgentDefaults creates or updates the current user's personal
// agent defaults for the workspace.
func (h *Handler) UpdatePersonalAgentDefaults(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())

	var req updatePersonalAgentDefaultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Config == nil {
		writeError(w, http.StatusBadRequest, "config is required")
		return
	}

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid config")
		return
	}

	cfg, err := h.Queries.UpsertMemberAgentConfig(r.Context(), db.UpsertMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
		Config:      configJSON,
	})
	if err != nil {
		slog.Warn("upsert member agent config failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to save agent defaults")
		return
	}

	var updatedConfig any
	if err := json.Unmarshal(cfg.Config, &updatedConfig); err != nil {
		updatedConfig = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(cfg.ID),
		"config":     updatedConfig,
		"created_at": timestampToString(cfg.CreatedAt),
		"updated_at": timestampToString(cfg.UpdatedAt),
	})
}

// ListAllAgentDefaults returns all members' personal agent defaults for the workspace.
func (h *Handler) ListAllAgentDefaults(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())

	rows, err := h.Queries.ListMemberAgentConfigs(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list member agent configs failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to list agent defaults")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var cfg agentDefaultsConfig
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			cfg = agentDefaultsConfig{}
		}
		// Mask custom_env values for security — only expose keys
		maskedEnv := make(map[string]string, len(cfg.CustomEnv))
		for k := range cfg.CustomEnv {
			maskedEnv[k] = "***"
		}
		maskedCfg := agentDefaultsConfig{
			Instructions: cfg.Instructions,
			CustomEnv:    maskedEnv,
			CustomArgs:   cfg.CustomArgs,
			Skills:       cfg.Skills,
		}
		items = append(items, map[string]any{
			"id":              uuidToString(row.ID),
			"config":          maskedCfg,
			"user_id":         uuidToString(row.UserID),
			"user_name":       row.UserName,
			"user_avatar_url": row.UserAvatarUrl.String,
			"created_at":      timestampToString(row.CreatedAt),
			"updated_at":      timestampToString(row.UpdatedAt),
		})
	}

	writeJSON(w, http.StatusOK, items)
}

// agentDefaultsConfig is the typed shape of the JSON config blob.
type agentDefaultsConfig struct {
	Instructions string            `json:"instructions,omitempty"`
	CustomEnv    map[string]string `json:"custom_env,omitempty"`
	CustomArgs   []string          `json:"custom_args,omitempty"`
	Skills       []string          `json:"skills,omitempty"`
}

// DuplicateAgentDefaults merges another member's agent defaults into the
// current user's config using append semantics.
func (h *Handler) DuplicateAgentDefaults(w http.ResponseWriter, r *http.Request) {
	member, ok := ctxMember(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	configID := chi.URLParam(r, "configId")

	sourceRows, err := h.Queries.ListMemberAgentConfigs(r.Context(), parseUUID(workspaceID))
	if err != nil {
		slog.Warn("list member agent configs for duplicate failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load source config")
		return
	}

	var sourceJSON []byte
	found := false
	for _, row := range sourceRows {
		if uuidToString(row.ID) == configID {
			sourceJSON = row.Config
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "source config not found")
		return
	}

	var source agentDefaultsConfig
	if err := json.Unmarshal(sourceJSON, &source); err != nil {
		writeError(w, http.StatusInternalServerError, "invalid source config")
		return
	}

	// Load current user's config (may not exist)
	var mine agentDefaultsConfig
	myCfg, err := h.Queries.GetMemberAgentConfig(r.Context(), db.GetMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err == nil {
		_ = json.Unmarshal(myCfg.Config, &mine)
	}

	// Merge with append semantics
	if source.Instructions != "" {
		if mine.Instructions != "" {
			mine.Instructions = mine.Instructions + "\n\n" + source.Instructions
		} else {
			mine.Instructions = source.Instructions
		}
	}
	// NOTE: custom_env is intentionally NOT merged for security reasons.
	if len(source.CustomArgs) > 0 {
		mine.CustomArgs = append(mine.CustomArgs, source.CustomArgs...)
	}
	if len(source.Skills) > 0 {
		skillSet := make(map[string]bool)
		for _, s := range mine.Skills {
			skillSet[s] = true
		}
		for _, s := range source.Skills {
			if !skillSet[s] {
				mine.Skills = append(mine.Skills, s)
				skillSet[s] = true
			}
		}
	}

	mergedJSON, err := json.Marshal(mine)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to marshal merged config")
		return
	}

	result, err := h.Queries.UpsertMemberAgentConfig(r.Context(), db.UpsertMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
		Config:      mergedJSON,
	})
	if err != nil {
		slog.Warn("upsert merged agent config failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save merged config")
		return
	}

	var mergedConfig any
	if err := json.Unmarshal(result.Config, &mergedConfig); err != nil {
		mergedConfig = map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         uuidToString(result.ID),
		"config":     mergedConfig,
		"created_at": timestampToString(result.CreatedAt),
		"updated_at": timestampToString(result.UpdatedAt),
	})
}
