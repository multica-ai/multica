package handler

// CEREBRO-PATCH(agent-infisical-secrets): per-agent Infisical secret references.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
)

var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type AgentInfisicalSecretResponse struct {
	ID          string `json:"id,omitempty"`
	EnvVarName  string `json:"env_var_name"`
	SecretName  string `json:"secret_name"`
	Environment string `json:"environment"`
	SecretPath  string `json:"secret_path"`
}

type replaceAgentInfisicalSecretsRequest struct {
	Secrets []AgentInfisicalSecretResponse `json:"secrets"`
}

func (h *Handler) ListAgentInfisicalSecrets(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}
	rows, err := h.listAgentInfisicalSecrets(r, agent.ID)
	if err != nil {
		slog.Error("failed to list agent infisical secrets", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list infisical secrets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": rows})
}

func (h *Handler) ReplaceAgentInfisicalSecrets(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}
	if h.CerebroQueries == nil || h.TxStarter == nil {
		writeError(w, http.StatusServiceUnavailable, "infisical secrets store is not configured")
		return
	}

	var req replaceAgentInfisicalSecretsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	normalized, err := normalizeAgentInfisicalSecrets(req.Secrets)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("failed to begin infisical secrets replace", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical secrets")
		return
	}
	defer tx.Rollback(r.Context())

	q := h.CerebroQueries.WithTx(tx)
	if err := q.DeleteAgentInfisicalSecrets(r.Context(), agent.ID); err != nil {
		slog.Error("failed to clear infisical secrets", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical secrets")
		return
	}
	out := make([]AgentInfisicalSecretResponse, 0, len(normalized))
	for _, secret := range normalized {
		row, err := q.UpsertAgentInfisicalSecret(r.Context(), cerebrodb.UpsertAgentInfisicalSecretParams{
			AgentID:     agent.ID,
			EnvVarName:  secret.EnvVarName,
			SecretName:  secret.SecretName,
			Environment: secret.Environment,
			SecretPath:  secret.SecretPath,
		})
		if err != nil {
			slog.Error("failed to upsert infisical secret", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "env_var_name", secret.EnvVarName, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to save infisical secrets")
			return
		}
		out = append(out, agentInfisicalSecretResponse(row))
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("failed to commit infisical secrets replace", append(logger.RequestAttrs(r), "agent_id", uuidToString(agent.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to save infisical secrets")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

func (h *Handler) agentInfisicalSecretsForClaim(r *http.Request, agentID pgtype.UUID) []AgentInfisicalSecretResponse {
	rows, err := h.listAgentInfisicalSecrets(r, agentID)
	if err != nil {
		slog.Warn("failed to load agent infisical secrets for claim", append(logger.RequestAttrs(r), "agent_id", uuidToString(agentID), "error", err)...)
		return nil
	}
	return rows
}

func (h *Handler) listAgentInfisicalSecrets(r *http.Request, agentID pgtype.UUID) ([]AgentInfisicalSecretResponse, error) {
	if h.CerebroQueries == nil {
		return []AgentInfisicalSecretResponse{}, nil
	}
	rows, err := h.CerebroQueries.ListAgentInfisicalSecrets(r.Context(), agentID)
	if err != nil {
		return nil, err
	}
	out := make([]AgentInfisicalSecretResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentInfisicalSecretResponse(row))
	}
	return out, nil
}

func agentInfisicalSecretResponse(row cerebrodb.CerebroAgentInfisicalSecret) AgentInfisicalSecretResponse {
	return AgentInfisicalSecretResponse{
		ID:          uuidToString(row.ID),
		EnvVarName:  row.EnvVarName,
		SecretName:  row.SecretName,
		Environment: row.Environment,
		SecretPath:  row.SecretPath,
	}
}

func normalizeAgentInfisicalSecrets(in []AgentInfisicalSecretResponse) ([]AgentInfisicalSecretResponse, error) {
	out := make([]AgentInfisicalSecretResponse, 0, len(in))
	seen := map[string]struct{}{}
	for _, row := range in {
		envName := strings.TrimSpace(row.EnvVarName)
		if envName == "" && strings.TrimSpace(row.SecretName) == "" && strings.TrimSpace(row.Environment) == "" {
			continue
		}
		if !envVarNamePattern.MatchString(envName) {
			return nil, fmt.Errorf("env_var_name must be a valid environment variable name")
		}
		key := strings.ToUpper(envName)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate env_var_name %q", envName)
		}
		seen[key] = struct{}{}
		secretName := strings.TrimSpace(row.SecretName)
		if secretName == "" {
			return nil, fmt.Errorf("secret_name is required")
		}
		environment := strings.TrimSpace(row.Environment)
		if environment == "" {
			return nil, fmt.Errorf("environment is required")
		}
		secretPath := strings.TrimSpace(row.SecretPath)
		if secretPath == "" {
			secretPath = "/"
		}
		out = append(out, AgentInfisicalSecretResponse{
			EnvVarName:  envName,
			SecretName:  secretName,
			Environment: environment,
			SecretPath:  secretPath,
		})
	}
	return out, nil
}
