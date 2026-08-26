package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/tasktoken"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// agentTaskTokensActivityUpdated is the audit action for a change to which
// identities an agent may be issued. There is no "revealed" counterpart: the
// GET returns the server-configured catalog and a list of enabled ids, never
// a token or any key material, so reading it discloses nothing secret.
const agentTaskTokensActivityUpdated = "agent_task_tokens_updated"

// agentTaskTokensActivityIssued records that identity tokens were actually
// minted for a run. One row per issuance batch, tied to the run's issue, so
// both the accountable human and a workspace admin can see, in-product, that
// a credential was signed in that person's name — and for which systems.
const agentTaskTokensActivityIssued = "agent_task_tokens_issued"

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

// taskTokenIdentityUser returns the human whose identity may be signed for a
// run, and whether signing is permitted at all.
//
// The gate reads originator_user_id — the AUTHORIZATION column — and never
// accountable_user_id. The two are not interchangeable here (migration 185):
// accountable_user_id is an audit/visibility output that degrades to *some*
// human for every run, while originator_user_id is NULL exactly when no human
// lent their authority. An autopilot schedule firing at 3am carries
// originator_source = trigger_owner, which attribution.Precise() reports true
// because the audit label is compliance-grade — but its originator is NULL by
// construction, and signing there would hand the trigger's long-departed
// creator's full permissions to a run they did not request and are not
// watching. That is the same refusal owner_fallback already gets.
//
// Precise() is still required on top, so a source that names an authorizing
// human only in hindsight (backfill) does not mint a live credential. It is a
// second condition, never the deciding one.
func taskTokenIdentityUser(task *db.AgentTaskQueue) (pgtype.UUID, bool) {
	if !task.OriginatorUserID.Valid {
		return pgtype.UUID{}, false
	}
	if !attribution.Source(task.OriginatorSource.String).Precise() {
		return pgtype.UUID{}, false
	}
	return task.OriginatorUserID, true
}

// issueTaskTokens signs the identity tokens this run's agent has enabled.
//
// The identity is the human who authorized the run (see taskTokenIdentityUser).
// When originator_user_id is set the attribution invariant makes it equal to
// accountable_user_id, so the token still speaks for the person the activity UI
// shows (MUL-4302) — it just refuses the sources where the two diverge, which
// are precisely the runs nobody authorized.
//
// Returns nil on every degraded path. Failing to obtain a token is an
// "unauthorized" condition for whatever wanted it, never a task failure, so
// this must not propagate errors into the claim.
func (h *Handler) issueTaskTokens(ctx context.Context, task *db.AgentTaskQueue, agent db.Agent, workspaceID string) map[string]string {
	if h.TaskTokenIssuer == nil {
		return nil
	}
	enabled := unmarshalTaskTokenTemplates(agent)
	if len(enabled) == 0 {
		return nil
	}

	src := attribution.Source(task.OriginatorSource.String)
	identityID, authorized := taskTokenIdentityUser(task)
	if !authorized {
		slog.Info("task token: run carries no human authorization; issuing none",
			"task_id", uuidToString(task.ID),
			"originator_source", task.OriginatorSource.String)
		return nil
	}

	user, err := h.Queries.GetUser(ctx, identityID)
	if err != nil {
		slog.Warn("task token: authorizing user lookup failed; issuing none",
			"task_id", uuidToString(task.ID),
			"user_id", uuidToString(identityID), "error", err)
		return nil
	}

	tctx := tasktoken.Context{
		Identity: tasktoken.Identity{
			Email:  user.Email,
			Name:   user.Name,
			UserID: uuidToString(user.ID),
			Source: string(src),
		},
		WorkspaceID: workspaceID,
		AgentID:     uuidToString(agent.ID),
		AgentName:   agent.Name,
		TaskID:      uuidToString(task.ID),
	}
	// Slug is best-effort: a template that does not reference it must not pay
	// for a failed lookup, and one that does gets an empty string rather than
	// a dropped token.
	if ws, wsErr := h.Queries.GetWorkspace(ctx, parseUUID(workspaceID)); wsErr == nil {
		tctx.WorkspaceSlug = ws.Slug
	} else {
		slog.Warn("task token: workspace lookup failed; slug will be empty",
			"workspace_id", workspaceID, "error", wsErr)
	}

	tokens, receipts := h.TaskTokenIssuer.Issue(enabled, tctx, time.Now())
	if len(tokens) == 0 {
		return nil
	}

	// Fail-closed on audit: a credential minted in a person's name must be
	// visible in the product's activity log, not only in server logs. If the
	// audit row cannot be written, the tokens are withheld — the run degrades
	// to "no token", the same non-fatal condition as every other refusal here.
	issued := make([]map[string]string, 0, len(receipts))
	for _, rc := range receipts {
		issued = append(issued, map[string]string{
			"template_id": rc.TemplateID,
			"env":         rc.Env,
			"jti":         rc.JTI,
			"expires_at":  rc.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
	details, _ := json.Marshal(map[string]any{
		"agent_id":        uuidToString(agent.ID),
		"agent_name":      agent.Name,
		"task_id":         uuidToString(task.ID),
		"user_id":         uuidToString(user.ID),
		"identity_source": string(src),
		"issued":          issued,
	})
	if _, err := h.Queries.CreateActivity(ctx, db.CreateActivityParams{
		ID:          dbid.NewV7(),
		WorkspaceID: agent.WorkspaceID,
		IssueID:     task.IssueID,
		ActorType:   pgtype.Text{String: "agent", Valid: true},
		ActorID:     agent.ID,
		Action:      agentTaskTokensActivityIssued,
		Details:     details,
	}); err != nil {
		slog.Error("task token: issuance audit write failed; withholding tokens",
			"task_id", uuidToString(task.ID), "agent_id", uuidToString(agent.ID), "error", err)
		return nil
	}

	// Logged only once the tokens are actually going out, so a jti in the
	// server log always corresponds to a credential someone could have used.
	for _, rc := range receipts {
		slog.Info("task token issued",
			"template_id", rc.TemplateID, "env", rc.Env, "jti", rc.JTI,
			"identity_source", string(src), "user_id", uuidToString(user.ID),
			"task_id", uuidToString(task.ID), "agent_id", uuidToString(agent.ID))
	}
	return tokens
}
