package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// agentTaskTokensActivityUpdated is the audit action for a change to which
// identities an agent may be issued. There is no "revealed" counterpart: the
// GET returns the server-configured catalog and a list of enabled ids, never
// a token or any key material, so reading it discloses nothing secret.
const agentTaskTokensActivityUpdated = "agent_task_tokens_updated"

// TaskTokenTemplateSummary is the UI-facing description of one catalog entry.
// It deliberately omits claims: the UI picks from the catalog, it does not get
// to see or influence what will be signed.
type TaskTokenTemplateSummary struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Env         string `json:"env"`
}

// AgentTaskTokensResponse is the wire shape for both task-token endpoints.
type AgentTaskTokensResponse struct {
	AgentID   string                     `json:"agent_id"`
	Available []TaskTokenTemplateSummary `json:"available"`
	Enabled   []string                   `json:"enabled"`
}

// UpdateAgentTaskTokensRequest is the wire shape for PUT.
type UpdateAgentTaskTokensRequest struct {
	Enabled []string `json:"enabled"`
}

// unmarshalTaskTokenTemplates decodes an agent's stored template ids. A
// malformed value degrades to "none enabled" rather than failing the caller —
// for the issuing path (which runs inside task claim) no tokens is a correct,
// non-fatal outcome.
func unmarshalTaskTokenTemplates(agent db.Agent) []string {
	if len(agent.TaskTokenTemplates) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(agent.TaskTokenTemplates, &ids); err != nil {
		slog.Warn("agent task_token_templates is not a JSON array; treating as empty",
			"agent_id", uuidToString(agent.ID), "error", err)
		return nil
	}
	return ids
}

// availableTaskTokenTemplates renders the configured catalog for the UI. An
// unconfigured deployment yields an empty list, which the UI uses to hide the
// tab entirely.
func (h *Handler) availableTaskTokenTemplates() []TaskTokenTemplateSummary {
	templates := h.TaskTokenIssuer.Catalog().List()
	out := make([]TaskTokenTemplateSummary, 0, len(templates))
	for _, tpl := range templates {
		out = append(out, TaskTokenTemplateSummary{
			ID:          tpl.ID,
			Label:       tpl.Label,
			Description: tpl.Description,
			Env:         tpl.Env,
		})
	}
	return out
}

// GetAgentTaskTokens returns the catalog plus this agent's enabled ids.
// Authorization matches the env endpoints (authorizeAgentEnv): agent actors
// are rejected outright, and the caller must be a workspace owner/admin or the
// agent's own human owner.
func (h *Handler) GetAgentTaskTokens(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	enabled := unmarshalTaskTokenTemplates(agent)
	if enabled == nil {
		enabled = []string{}
	}
	writeJSON(w, http.StatusOK, AgentTaskTokensResponse{
		AgentID:   uuidToString(agent.ID),
		Available: h.availableTaskTokenTemplates(),
		Enabled:   enabled,
	})
}

// UpdateAgentTaskTokens replaces the enabled set.
//
// Every id must exist in the server-configured catalog. That check is the
// boundary keeping "what may be signed" in server configuration: without it,
// anyone who can edit an agent could name an arbitrary scope and have the
// server sign it.
func (h *Handler) UpdateAgentTaskTokens(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	var req UpdateAgentTaskTokensRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	catalog := h.TaskTokenIssuer.Catalog()
	seen := make(map[string]struct{}, len(req.Enabled))
	enabled := make([]string, 0, len(req.Enabled))
	for _, id := range req.Enabled {
		if _, dup := seen[id]; dup {
			continue
		}
		if _, found := catalog.Get(id); !found {
			writeError(w, http.StatusBadRequest, "unknown task token template: "+id)
			return
		}
		seen[id] = struct{}{}
		enabled = append(enabled, id)
	}

	encoded, err := json.Marshal(enabled)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode task token templates")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("agent_task_tokens update: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update task tokens")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateAgentTaskTokenTemplates(r.Context(), db.UpdateAgentTaskTokenTemplatesParams{
		ID:                 agent.ID,
		TaskTokenTemplates: encoded,
	})
	if err != nil {
		slog.Warn("update agent task_token_templates failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update task tokens")
		return
	}

	details, _ := json.Marshal(map[string]any{
		"agent_id":   uuidToString(agent.ID),
		"agent_name": agent.Name,
		"before":     unmarshalTaskTokenTemplates(agent),
		"after":      enabled,
	})
	// Persist + audit share one transaction: an audit outage must not leave an
	// unaudited change to which identities this agent may be issued.
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{}, // not tied to an issue
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentTaskTokensActivityUpdated,
		Details:     details,
	}); err != nil {
		slog.Error("agent_task_tokens_updated audit write failed; rolling back",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent_task_tokens update: commit failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update task tokens")
		return
	}

	writeJSON(w, http.StatusOK, AgentTaskTokensResponse{
		AgentID:   uuidToString(updated.ID),
		Available: h.availableTaskTokenTemplates(),
		Enabled:   enabled,
	})
}
