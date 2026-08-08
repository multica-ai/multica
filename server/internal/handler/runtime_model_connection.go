package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/piagent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	runtimeModelConnectionUpdated = "runtime_model_connection_updated"
	runtimeModelConnectionDeleted = "runtime_model_connection_deleted"
)

type RuntimeModelConnectionResponse struct {
	RuntimeID  string         `json:"runtime_id"`
	Config     piagent.Config `json:"config"`
	HasAPIKey  bool           `json:"has_api_key"`
	Configured bool           `json:"configured"`
}

type UpdateRuntimeModelConnectionRequest struct {
	Provider string `json:"provider"`
	API      string `json:"api"`
	BaseURL  string `json:"base_url"`
	Model    string `json:"model"`
	// Nil preserves the saved key. A non-empty value creates or rotates it;
	// clearing the connection uses DELETE so a valid config can never be left
	// behind without the secret it requires.
	APIKey *string `json:"api_key,omitempty"`
}

func runtimeModelConnectionResponse(rt db.AgentRuntime) RuntimeModelConnectionResponse {
	cfg, valid, _ := piagent.Decode(json.RawMessage(rt.DefaultModelConfig))
	hasAPIKey := rt.DefaultModelApiKey.Valid && strings.TrimSpace(rt.DefaultModelApiKey.String) != ""
	return RuntimeModelConnectionResponse{
		RuntimeID:  uuidToString(rt.ID),
		Config:     cfg,
		HasAPIKey:  hasAPIKey,
		Configured: valid && hasAPIKey,
	}
}

func (h *Handler) loadRuntimeModelConnectionTarget(w http.ResponseWriter, r *http.Request, requireEdit bool) (db.AgentRuntime, db.Member, bool) {
	runtimeID := chi.URLParam(r, "runtimeId")
	runtimeUUID, ok := parseUUIDOrBadRequest(w, runtimeID, "runtime_id")
	if !ok {
		return db.AgentRuntime{}, db.Member{}, false
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), runtimeUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return db.AgentRuntime{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceMember(w, r, uuidToString(rt.WorkspaceID), "runtime not found")
	if !ok {
		return db.AgentRuntime{}, db.Member{}, false
	}
	if rt.Provider != "pi" {
		writeError(w, http.StatusBadRequest, "model connections are currently supported only for Pi runtimes")
		return db.AgentRuntime{}, db.Member{}, false
	}
	if !requireEdit {
		return rt, member, true
	}
	actorType, _ := h.resolveActor(r, requestUserID(r), uuidToString(rt.WorkspaceID))
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not manage runtime model connections")
		return db.AgentRuntime{}, db.Member{}, false
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "you can only edit your own runtimes")
		return db.AgentRuntime{}, db.Member{}, false
	}
	return rt, member, true
}

func (h *Handler) GetRuntimeModelConnection(w http.ResponseWriter, r *http.Request) {
	rt, _, ok := h.loadRuntimeModelConnectionTarget(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, runtimeModelConnectionResponse(rt))
}

func (h *Handler) UpdateRuntimeModelConnection(w http.ResponseWriter, r *http.Request) {
	rt, member, ok := h.loadRuntimeModelConnectionTarget(w, r, true)
	if !ok {
		return
	}
	var req UpdateRuntimeModelConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := piagent.Normalize(piagent.Config{
		Provider: req.Provider,
		API:      req.API,
		BaseURL:  req.BaseURL,
		Model:    req.Model,
	})
	if err := piagent.Validate(cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode model connection")
		return
	}
	var nextAPIKey pgtype.Text
	if req.APIKey != nil {
		trimmed := strings.TrimSpace(*req.APIKey)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "api_key must not be empty; delete the connection to clear it")
			return
		}
		nextAPIKey = pgtype.Text{String: trimmed, Valid: true}
	}
	if !nextAPIKey.Valid && (!rt.DefaultModelApiKey.Valid || strings.TrimSpace(rt.DefaultModelApiKey.String) == "") {
		writeError(w, http.StatusBadRequest, "api_key is required for a new model connection")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update model connection")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	updated, err := qtx.UpdateAgentRuntimeDefaultModelConnection(r.Context(), db.UpdateAgentRuntimeDefaultModelConnectionParams{
		ID:                 rt.ID,
		DefaultModelConfig: configJSON,
		DefaultModelApiKey: nextAPIKey,
	})
	if err != nil {
		slog.Warn("update runtime model connection failed", append(logger.RequestAttrs(r), "error", err, "runtime_id", uuidToString(rt.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update model connection")
		return
	}
	if err := createRuntimeModelConnectionActivity(r, qtx, member, updated, runtimeModelConnectionUpdated, nextAPIKey.Valid); err != nil {
		slog.Error("runtime model connection audit write failed", append(logger.RequestAttrs(r), "error", err, "runtime_id", uuidToString(rt.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to record model connection update")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update model connection")
		return
	}
	h.invalidateRuntimeModelCatalog(r, uuidToString(updated.ID))
	h.publishRuntimeModelConnectionChange(r, updated, member)
	writeJSON(w, http.StatusOK, runtimeModelConnectionResponse(updated))
}

func (h *Handler) DeleteRuntimeModelConnection(w http.ResponseWriter, r *http.Request) {
	rt, member, ok := h.loadRuntimeModelConnectionTarget(w, r, true)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete model connection")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	updated, err := qtx.ClearAgentRuntimeDefaultModelConnection(r.Context(), rt.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete model connection")
		return
	}
	if err := createRuntimeModelConnectionActivity(r, qtx, member, rt, runtimeModelConnectionDeleted, false); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record model connection deletion")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete model connection")
		return
	}
	h.invalidateRuntimeModelCatalog(r, uuidToString(updated.ID))
	h.publishRuntimeModelConnectionChange(r, updated, member)
	writeJSON(w, http.StatusOK, runtimeModelConnectionResponse(updated))
}

func (h *Handler) invalidateRuntimeModelCatalog(r *http.Request, runtimeID string) {
	if h.ModelCatalogCache == nil {
		return
	}
	if err := h.ModelCatalogCache.Invalidate(r.Context(), runtimeID); err != nil {
		slog.Warn("runtime model catalog cache invalidate failed", append(logger.RequestAttrs(r), "error", err, "runtime_id", runtimeID)...)
	}
}

func createRuntimeModelConnectionActivity(r *http.Request, q *db.Queries, member db.Member, rt db.AgentRuntime, action string, keyRotated bool) error {
	cfg, _, _ := piagent.Decode(json.RawMessage(rt.DefaultModelConfig))
	details, _ := json.Marshal(map[string]any{
		"runtime_id":      uuidToString(rt.ID),
		"provider":        cfg.Provider,
		"model":           cfg.Model,
		"api_key_rotated": keyRotated,
	})
	_, err := q.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: rt.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      action,
		Details:     details,
	})
	return err
}

func (h *Handler) publishRuntimeModelConnectionChange(r *http.Request, rt db.AgentRuntime, member db.Member) {
	h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{
		"action": "update",
	})
}
