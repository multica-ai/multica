package handler

// CEREBRO-PATCH(agent-infisical-secrets): per-agent Infisical folder grants.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
)

type AgentInfisicalFolderResponse struct {
	ID          string `json:"id,omitempty"`
	Environment string `json:"environment"`
	SecretPath  string `json:"secret_path"`
}

type replaceAgentInfisicalFoldersRequest struct {
	Folders []AgentInfisicalFolderResponse `json:"folders"`
}

func (h *Handler) ListAgentInfisicalFolders(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}
	rows, err := h.listAgentInfisicalFolders(r, agent.ID)
	if err != nil {
		slog.Error("failed to list agent infisical folders", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list infisical folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": rows})
}

func (h *Handler) ReplaceAgentInfisicalFolders(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}
	if h.CerebroQueries == nil || h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "infisical folders store is not configured")
		return
	}

	var req replaceAgentInfisicalFoldersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := normalizeAgentInfisicalFolders(req.Folders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// CEREBRO-PATCH(user-infisical-folders): gate — an agent may only be granted
	// folders that the agent's owner is allowed to use (admin-managed allow-list).
	// This stops a user handing their agent keys they were never approved for.
	allowed, err := h.userAllowedInfisicalFolderSet(r, agent.WorkspaceID, agent.OwnerID)
	if err != nil {
		slog.Error("failed to load owner infisical allow-list", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	if _, denied := partitionInfisicalFoldersByAllowList(normalized, allowed); len(denied) > 0 {
		writeError(w, http.StatusForbidden, fmt.Sprintf("folder %s%s is not in the owner's allowed Infisical folders", denied[0].Environment, denied[0].SecretPath))
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("failed to begin infisical folders replace", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	defer tx.Rollback(r.Context())

	q := h.CerebroQueries.WithTx(tx)
	if err := q.DeleteAgentInfisicalFolders(r.Context(), agent.ID); err != nil {
		slog.Error("failed to clear infisical folders", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	out := make([]AgentInfisicalFolderResponse, 0, len(normalized))
	for _, folder := range normalized {
		row, err := q.InsertAgentInfisicalFolder(r.Context(), cerebrodb.InsertAgentInfisicalFolderParams{
			AgentID:     agent.ID,
			Environment: folder.Environment,
			SecretPath:  folder.SecretPath,
		})
		if err != nil {
			slog.Error("failed to insert infisical folder", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "environment", folder.Environment, "secret_path", folder.SecretPath, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
			return
		}
		out = append(out, agentInfisicalFolderResponse(row))
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("failed to commit infisical folders replace", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": out})
}

func (h *Handler) agentInfisicalFoldersForClaim(r *http.Request, agentID, workspaceID, ownerID pgtype.UUID) []AgentInfisicalFolderResponse {
	rows, err := h.listAgentInfisicalFolders(r, agentID)
	if err != nil {
		slog.Warn("failed to load agent infisical folders for claim", append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "error", err)...)
		return nil
	}
	if len(rows) == 0 {
		return rows
	}
	// Spawn-time gate: re-check each grant against the owner's current
	// allow-list so a revoked permission stops being injected even when the
	// agent's saved grants are now stale.
	allowed, err := h.userAllowedInfisicalFolderSet(r, workspaceID, ownerID)
	if err != nil {
		slog.Warn("failed to load owner infisical allow-list for claim; injecting no folders", append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "error", err)...)
		return nil
	}
	filtered, denied := partitionInfisicalFoldersByAllowList(rows, allowed)
	for _, row := range denied {
		slog.Warn("infisical folder grant skipped: not in owner allow-list", append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "environment", row.Environment, "secret_path", row.SecretPath)...)
	}
	return filtered
}

// ListAgentAllowedInfisicalFolders returns the folders the agent's owner is
// allowed to grant, so the UI can offer a picker instead of free-text entry.
func (h *Handler) ListAgentAllowedInfisicalFolders(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}
	rows, err := h.listUserInfisicalFolders(r, agent.WorkspaceID, agent.OwnerID)
	if err != nil {
		slog.Error("failed to list agent owner infisical allow-list", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list allowed infisical folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": rows})
}

func (h *Handler) listAgentInfisicalFolders(r *http.Request, agentID pgtype.UUID) ([]AgentInfisicalFolderResponse, error) {
	if h.CerebroQueries == nil {
		return []AgentInfisicalFolderResponse{}, nil
	}
	rows, err := h.CerebroQueries.ListAgentInfisicalFolders(r.Context(), agentID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentInfisicalFolderResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentInfisicalFolderResponse(row))
	}
	return out, nil
}

func agentInfisicalFolderResponse(row cerebrodb.CerebroAgentInfisicalFolder) AgentInfisicalFolderResponse {
	return AgentInfisicalFolderResponse{
		ID:          uuidToString(row.ID),
		Environment: row.Environment,
		SecretPath:  row.SecretPath,
	}
}

func normalizeAgentInfisicalFolders(in []AgentInfisicalFolderResponse) ([]AgentInfisicalFolderResponse, error) {
	out := make([]AgentInfisicalFolderResponse, 0, len(in))
	seen := map[string]struct{}{}
	for _, row := range in {
		environment := strings.TrimSpace(row.Environment)
		secretPath := strings.TrimSpace(row.SecretPath)
		if environment == "" && secretPath == "" {
			continue
		}
		if environment == "" {
			return nil, fmt.Errorf("environment is required")
		}
		if secretPath == "" {
			secretPath = "/"
		}
		key := environment + "\x00" + secretPath
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate folder %s %s", environment, secretPath)
		}
		seen[key] = struct{}{}
		out = append(out, AgentInfisicalFolderResponse{
			Environment: environment,
			SecretPath:  secretPath,
		})
	}
	return out, nil
}
