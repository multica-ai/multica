package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	instructionsHistoryScopePersonal = "personal"
	instructionsHistoryScopeSystem   = "system"
	defaultInstructionsHistoryLimit  = int32(50)
	maxInstructionsHistoryLimit      = int32(100)
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

	nextInstructions, hasInstructions := instructionsFromConfigJSON(configJSON)
	q := h.Queries
	commit := func() error { return nil }
	if h.TxStarter != nil {
		realTx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save agent defaults")
			return
		}
		defer realTx.Rollback(r.Context())
		q = h.Queries.WithTx(realTx)
		commit = func() error { return realTx.Commit(r.Context()) }
	}

	previousInstructions := ""
	if existing, err := q.GetMemberAgentConfig(r.Context(), db.GetMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
	}); err == nil {
		previousInstructions, _ = instructionsFromConfigJSON(existing.Config)
	} else if !isNotFound(err) {
		slog.Warn("get member agent config before update failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to save agent defaults")
		return
	}

	cfg, err := q.UpsertMemberAgentConfig(r.Context(), db.UpsertMemberAgentConfigParams{
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

	if hasInstructions && nextInstructions != previousInstructions {
		if _, err := q.InsertInstructionsHistory(r.Context(), db.InsertInstructionsHistoryParams{
			WorkspaceID: parseUUID(workspaceID),
			Scope:       instructionsHistoryScopePersonal,
			MemberID:    member.ID,
			Content:     nextInstructions,
			ActorID:     member.ID,
		}); err != nil {
			slog.Warn("insert personal instructions history failed",
				append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to save agent defaults")
			return
		}
	}

	if err := commit(); err != nil {
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

// ListInstructionsHistory returns recent instructions versions for the current
// user's personal defaults or the workspace system defaults.
func (h *Handler) ListInstructionsHistory(w http.ResponseWriter, r *http.Request) {
	member, scope, ok := h.instructionsHistoryAccess(w, r)
	if !ok {
		return
	}
	workspaceID := workspaceIDFromURL(r, "id")

	rows, err := h.Queries.ListInstructionsHistory(r.Context(), db.ListInstructionsHistoryParams{
		WorkspaceID: parseUUID(workspaceID),
		Scope:       scope,
		MemberID:    historyMemberID(scope, member.ID),
		Limit:       instructionsHistoryLimit(r),
	})
	if err != nil {
		slog.Warn("list instructions history failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "scope", scope)...)
		writeError(w, http.StatusInternalServerError, "failed to list instructions history")
		return
	}

	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, instructionsHistoryRowToResponse(row, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": len(items),
	})
}

// GetInstructionsHistory returns a single instructions history version,
// including the full content snapshot.
func (h *Handler) GetInstructionsHistory(w http.ResponseWriter, r *http.Request) {
	member, scope, ok := h.instructionsHistoryAccess(w, r)
	if !ok {
		return
	}
	workspaceID := workspaceIDFromURL(r, "id")
	versionID := chi.URLParam(r, "versionId")
	versionUUID, ok := parseUUIDOrBadRequest(w, versionID, "version id")
	if !ok {
		return
	}

	row, err := h.Queries.GetInstructionsHistory(r.Context(), db.GetInstructionsHistoryParams{
		ID:          versionUUID,
		WorkspaceID: parseUUID(workspaceID),
		Scope:       scope,
		MemberID:    historyMemberID(scope, member.ID),
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "instructions history version not found")
			return
		}
		slog.Warn("get instructions history failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID, "scope", scope, "version_id", versionID)...)
		writeError(w, http.StatusInternalServerError, "failed to load instructions history")
		return
	}

	writeJSON(w, http.StatusOK, instructionsHistoryDetailToResponse(row))
}

// agentDefaultsConfig is the typed shape of the JSON config blob.
type agentDefaultsConfig struct {
	Instructions string            `json:"instructions,omitempty"`
	CustomEnv    map[string]string `json:"custom_env,omitempty"`
	CustomArgs   []string          `json:"custom_args,omitempty"`
	Skills       []string          `json:"skills,omitempty"`
}

func instructionsFromConfigJSON(raw []byte) (string, bool) {
	var cfg agentDefaultsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", false
	}
	var rawMap map[string]any
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return cfg.Instructions, cfg.Instructions != ""
	}
	_, ok := rawMap["instructions"]
	return cfg.Instructions, ok
}

func historyMemberID(scope string, memberID pgtype.UUID) pgtype.UUID {
	if scope == instructionsHistoryScopePersonal {
		return memberID
	}
	return pgtype.UUID{}
}

func instructionsHistoryLimit(r *http.Request) int32 {
	limit := defaultInstructionsHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = int32(parsed)
		}
	}
	if limit > maxInstructionsHistoryLimit {
		return maxInstructionsHistoryLimit
	}
	return limit
}

func (h *Handler) instructionsHistoryAccess(w http.ResponseWriter, r *http.Request) (db.Member, string, bool) {
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = instructionsHistoryScopePersonal
	}
	if scope != instructionsHistoryScopePersonal && scope != instructionsHistoryScopeSystem {
		writeError(w, http.StatusBadRequest, "invalid scope")
		return db.Member{}, "", false
	}

	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, "", false
	}
	if scope == instructionsHistoryScopeSystem && !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.Member{}, "", false
	}
	return member, scope, true
}

func instructionsHistoryRowToResponse(row db.ListInstructionsHistoryRow, includeContent bool) map[string]any {
	resp := map[string]any{
		"id":               uuidToString(row.ID),
		"workspace_id":     uuidToString(row.WorkspaceID),
		"scope":            row.Scope,
		"member_id":        uuidToPtr(row.MemberID),
		"actor_id":         uuidToPtr(row.ActorID),
		"actor_user_id":    uuidToPtr(row.ActorUserID),
		"actor_name":       textToPtr(row.ActorName),
		"actor_avatar_url": textToPtr(row.ActorAvatarUrl),
		"restored_from":    uuidToPtr(row.RestoredFrom),
		"created_at":       timestampToString(row.CreatedAt),
		"content_preview":  instructionsHistoryPreview(row.Content),
	}
	if includeContent {
		resp["content"] = row.Content
	}
	return resp
}

func instructionsHistoryDetailToResponse(row db.GetInstructionsHistoryRow) map[string]any {
	return map[string]any{
		"id":               uuidToString(row.ID),
		"workspace_id":     uuidToString(row.WorkspaceID),
		"scope":            row.Scope,
		"member_id":        uuidToPtr(row.MemberID),
		"actor_id":         uuidToPtr(row.ActorID),
		"actor_user_id":    uuidToPtr(row.ActorUserID),
		"actor_name":       textToPtr(row.ActorName),
		"actor_avatar_url": textToPtr(row.ActorAvatarUrl),
		"restored_from":    uuidToPtr(row.RestoredFrom),
		"created_at":       timestampToString(row.CreatedAt),
		"content_preview":  instructionsHistoryPreview(row.Content),
		"content":          row.Content,
	}
}

func instructionsHistoryPreview(content string) string {
	const maxRunes = 80
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes])
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
	q := h.Queries
	commit := func() error { return nil }
	if h.TxStarter != nil {
		tx, err := h.TxStarter.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save merged config")
			return
		}
		defer tx.Rollback(r.Context())
		q = h.Queries.WithTx(tx)
		commit = func() error { return tx.Commit(r.Context()) }
	}

	myCfg, err := q.GetMemberAgentConfig(r.Context(), db.GetMemberAgentConfigParams{
		MemberID:    member.ID,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err == nil {
		_ = json.Unmarshal(myCfg.Config, &mine)
	}
	previousInstructions := mine.Instructions

	// Merge with append semantics
	if source.Instructions != "" {
		if mine.Instructions != "" {
			mine.Instructions = mine.Instructions + "\n\n" + source.Instructions
		} else {
			mine.Instructions = source.Instructions
		}
	}
	// Copy env keys with empty values as a template — values are secrets and
	// must be filled in by the user, but knowing which keys exist is safe
	// (ListAllAgentDefaults already exposes keys with "***" values).
	if len(source.CustomEnv) > 0 {
		if mine.CustomEnv == nil {
			mine.CustomEnv = make(map[string]string)
		}
		for k := range source.CustomEnv {
			if _, exists := mine.CustomEnv[k]; !exists {
				mine.CustomEnv[k] = "" // key only, user fills in value
			}
		}
	}
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

	result, err := q.UpsertMemberAgentConfig(r.Context(), db.UpsertMemberAgentConfigParams{
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

	if mine.Instructions != previousInstructions {
		if _, err := q.InsertInstructionsHistory(r.Context(), db.InsertInstructionsHistoryParams{
			WorkspaceID: parseUUID(workspaceID),
			Scope:       instructionsHistoryScopePersonal,
			MemberID:    member.ID,
			Content:     mine.Instructions,
			ActorID:     member.ID,
		}); err != nil {
			slog.Warn("insert duplicated instructions history failed",
				append(logger.RequestAttrs(r), "error", err, "workspace_id", workspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to save merged config")
			return
		}
	}

	if err := commit(); err != nil {
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
