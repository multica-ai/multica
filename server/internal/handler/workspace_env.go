package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	workspaceEnvActivityRevealed = "workspace_env_revealed"
	workspaceEnvActivityUpdated  = "workspace_env_updated"
)

type WorkspaceEnvResponse struct {
	WorkspaceID string            `json:"workspace_id"`
	GlobalEnv   map[string]string `json:"global_env"`
}

type UpdateWorkspaceEnvRequest struct {
	GlobalEnv map[string]string `json:"global_env"`
}

func unmarshalWorkspaceSettings(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil || settings == nil {
		return map[string]any{}
	}
	return settings
}

func unmarshalWorkspaceGlobalEnv(raw []byte) map[string]string {
	settings := unmarshalWorkspaceSettings(raw)
	rawEnv, ok := settings["global_env"]
	if !ok {
		return map[string]string{}
	}
	envBytes, err := json.Marshal(rawEnv)
	if err != nil {
		return map[string]string{}
	}
	var env map[string]string
	if err := json.Unmarshal(envBytes, &env); err != nil || env == nil {
		return map[string]string{}
	}
	return env
}

func encodeWorkspaceSettingsWithGlobalEnv(raw []byte, env map[string]string) ([]byte, error) {
	settings := unmarshalWorkspaceSettings(raw)
	if len(env) == 0 {
		delete(settings, "global_env")
	} else {
		settings["global_env"] = env
	}
	return json.Marshal(settings)
}

func redactWorkspaceSettings(raw []byte) map[string]any {
	settings := unmarshalWorkspaceSettings(raw)
	env := unmarshalWorkspaceGlobalEnv(raw)
	if len(env) > 0 {
		settings["global_env"] = map[string]any{
			"has_values": true,
			"key_count":  len(env),
		}
	} else {
		delete(settings, "global_env")
	}
	return settings
}

func (h *Handler) authorizeWorkspaceEnv(w http.ResponseWriter, r *http.Request) (db.Workspace, db.Member, bool) {
	workspaceID := chi.URLParam(r, "id")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return db.Workspace{}, db.Member{}, false
	}
	actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access workspace env management endpoints")
		return db.Workspace{}, db.Member{}, false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return db.Workspace{}, db.Member{}, false
	}
	return ws, member, true
}

func (h *Handler) GetWorkspaceEnv(w http.ResponseWriter, r *http.Request) {
	ws, member, ok := h.authorizeWorkspaceEnv(w, r)
	if !ok {
		return
	}

	env := unmarshalWorkspaceGlobalEnv(ws.Settings)
	revealedKeys := sortedKeys(env)
	details, _ := json.Marshal(map[string]any{
		"revealed_keys": revealedKeys,
		"key_count":     len(revealedKeys),
	})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: ws.ID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      workspaceEnvActivityRevealed,
		Details:     details,
	}); err != nil {
		slog.Error("workspace_env_revealed audit write failed; refusing to serve plaintext",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(ws.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; refusing to serve env without a recorded reveal")
		return
	}

	writeJSON(w, http.StatusOK, WorkspaceEnvResponse{
		WorkspaceID: uuidToString(ws.ID),
		GlobalEnv:   env,
	})
}

func (h *Handler) UpdateWorkspaceEnv(w http.ResponseWriter, r *http.Request) {
	ws, member, ok := h.authorizeWorkspaceEnv(w, r)
	if !ok {
		return
	}

	var req UpdateWorkspaceEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.GlobalEnv == nil {
		req.GlobalEnv = map[string]string{}
	}

	existing := unmarshalWorkspaceGlobalEnv(ws.Settings)
	merged, audit := mergeAgentEnv(existing, req.GlobalEnv)
	settingsBytes, err := encodeWorkspaceSettingsWithGlobalEnv(ws.Settings, merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode workspace env")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("workspace_env update: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(ws.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{
		ID:       ws.ID,
		Settings: settingsBytes,
	})
	if err != nil {
		slog.Warn("update workspace global_env failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(ws.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	details, _ := json.Marshal(map[string]any{
		"added_keys":     audit.added,
		"removed_keys":   audit.removed,
		"changed_keys":   audit.changed,
		"preserved_keys": audit.preserved,
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: ws.ID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      workspaceEnvActivityUpdated,
		Details:     details,
	}); err != nil {
		slog.Error("workspace_env_updated audit write failed; rolling back update",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(ws.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; env update rolled back")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("workspace_env update: tx commit failed",
			append(logger.RequestAttrs(r), "error", err, "workspace_id", uuidToString(ws.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	userID := requestUserID(r)
	h.publish(protocol.EventWorkspaceUpdated, uuidToString(updated.ID), "member", userID, map[string]any{"workspace": workspaceToResponse(updated)})
	writeJSON(w, http.StatusOK, WorkspaceEnvResponse{
		WorkspaceID: uuidToString(updated.ID),
		GlobalEnv:   merged,
	})
}
