// CEREBRO-PATCH(cerebro-agent-credential-grant-write): FIR-1479 admin-only write
// path — grant or revoke an actor's access to a credential (one Agent Vault box)
// from one agent-centric endpoint, mirroring the tool-grant write path
// (agent_tool_grant_cerebro.go). This is the API behind the credentials column
// Jesper asked for in the permissions interface: a ticked box = access to exactly
// that one vault box for that actor, least-privilege and deny-by-default.
//
// The whole endpoint is gated by the cerebro_credentials_per_actor feature flag
// (registry.ts, default OFF): until an admin turns it on per workspace, every
// verb here returns 404 and nothing in the live permission path changes. The
// resolver (credentialpolicy/chain.go) and persistence (migration 9096) already
// exist; this slice is the authoring API. The capabilities card and the UI land
// in follow-up commits.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/credentialpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

// flagCredentialsPerActor gates the credential-grant write path. It must match
// the key registered in packages/cerebro-feature-flags/registry.ts.
const flagCredentialsPerActor = "cerebro_credentials_per_actor"

// agentCredentialGrantRequest is the wire body for POST/DELETE
// /api/agents/{id}/credential-grants.
//
// credential is required (the Agent Vault box name, e.g. "bigquery"). The target
// is exactly one of: nothing (the agent itself, the default tick-this-box UX),
// group_id, or user. setting is optional on grant ("allow" default, or "deny" to
// pin a deny at this layer); it is ignored on revoke, which clears the row.
type agentCredentialGrantRequest struct {
	Credential string `json:"credential"`
	GroupID    string `json:"group_id"`
	User       string `json:"user"`    // UUID or email
	Setting    string `json:"setting"` // optional: "allow" | "deny"; default "allow"
}

// agentCredentialGrantResult summarises what changed so the CLI can print a
// confirmation.
type agentCredentialGrantResult struct {
	AgentID    string `json:"agent_id"`
	Action     string `json:"action"` // "grant" | "revoke"
	Credential string `json:"credential"`
	Layer      string `json:"layer"`
	TargetType string `json:"target_type"` // "agent" | "group" | "user"
	TargetID   string `json:"target_id"`
	Setting    string `json:"setting,omitempty"`
}

// AddAgentCredentialGrant handles POST /api/agents/{id}/credential-grants — grant
// (or deny) an actor access to a credential.
func (h *Handler) AddAgentCredentialGrant(w http.ResponseWriter, r *http.Request) {
	h.writeAgentCredentialGrant(w, r, "grant")
}

// RemoveAgentCredentialGrant handles DELETE /api/agents/{id}/credential-grants —
// clear an actor's explicit setting for a credential at this layer.
func (h *Handler) RemoveAgentCredentialGrant(w http.ResponseWriter, r *http.Request) {
	h.writeAgentCredentialGrant(w, r, "revoke")
}

func (h *Handler) writeAgentCredentialGrant(w http.ResponseWriter, r *http.Request, action string) {
	if !h.requireToolsAdmin(w) {
		return
	}

	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	// Credential grants govern who can reach a shared secret box, so require the
	// workspace owner/admin role — the same threat model as the tool-grant
	// endpoint — not merely "can manage this agent".
	wsID := uuidToString(agent.WorkspaceID)
	member, ok := h.requireWorkspaceMember(w, r, wsID, "agent not found")
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "only workspace owners and admins can manage credential grants")
		return
	}

	// Gate the whole endpoint behind the default-off flag: until an admin turns
	// cerebro_credentials_per_actor on for this workspace, credentials are not yet
	// a live permission type and the endpoint behaves as if it does not exist.
	if !h.credentialsPerActorEnabled(r, agent.WorkspaceID) {
		writeError(w, http.StatusNotFound, "credentials-per-actor is not enabled for this workspace")
		return
	}

	var body agentCredentialGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Credential == "" {
		writeError(w, http.StatusBadRequest, "credential is required")
		return
	}
	if body.GroupID != "" && body.User != "" {
		writeError(w, http.StatusBadRequest, "specify at most one of group_id or user")
		return
	}

	// Resolve the target actor → (layer, subject). No group/user means the agent
	// itself: the tick-this-box-for-this-agent default.
	layer := credentialpolicy.LayerAgent
	targetType := "agent"
	targetID := agent.ID
	switch {
	case body.GroupID != "":
		gid, err := util.ParseUUID(body.GroupID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid group_id")
			return
		}
		layer, targetType, targetID = credentialpolicy.LayerGroup, "group", gid
	case body.User != "":
		uid, err := util.ParseUUID(body.User)
		if err != nil {
			u, uerr := h.Queries.GetUserByEmail(r.Context(), body.User)
			if uerr != nil {
				writeError(w, http.StatusNotFound, "user not found: "+body.User)
				return
			}
			uid = u.ID
		}
		layer, targetType, targetID = credentialpolicy.LayerUser, "user", uid
	}

	store := credentialpolicy.NewStoreFromQueries(h.CerebroQueries)

	if action == "revoke" {
		if err := store.Clear(r.Context(), agent.WorkspaceID, body.Credential, layer, targetID); err != nil {
			writeError(w, http.StatusInternalServerError, "revoke credential grant: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, agentCredentialGrantResult{
			AgentID:    uuidToString(agent.ID),
			Action:     action,
			Credential: body.Credential,
			Layer:      string(layer),
			TargetType: targetType,
			TargetID:   uuidToString(targetID),
		})
		return
	}

	// grant: default to Allow; permit an explicit Deny so an admin can pin a deny
	// at one layer (the resolver folds nearest-to-ceiling-wins).
	setting := credentialpolicy.SettingAllow
	if body.Setting != "" {
		setting = credentialpolicy.Setting(body.Setting)
	}
	row, err := store.Set(r.Context(), credentialpolicy.SetParams{
		WorkspaceID:   agent.WorkspaceID,
		CredentialKey: body.Credential,
		Layer:         layer,
		SubjectID:     targetID,
		Setting:       setting,
		UpdatedBy:     member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "grant credential: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, agentCredentialGrantResult{
		AgentID:    uuidToString(agent.ID),
		Action:     action,
		Credential: row.CredentialKey,
		Layer:      row.Layer,
		TargetType: targetType,
		TargetID:   uuidToString(targetID),
		Setting:    row.Setting,
	})
}

// agentCredentialGrantListItem is one explicit credential setting on the agent
// layer, for GET /api/agents/{id}/credential-grants.
type agentCredentialGrantListItem struct {
	Credential string `json:"credential"`
	Setting    string `json:"setting"`
}

// ListAgentCredentialGrants handles GET /api/agents/{id}/credential-grants — the
// explicit per-credential settings pinned directly on this agent (the agent
// layer). The full resolved verdict across all layers is a separate read; this
// is the authoring view the credentials column edits.
func (h *Handler) ListAgentCredentialGrants(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, uuidToString(agent.WorkspaceID), "agent not found"); !ok {
		return
	}
	if !h.credentialsPerActorEnabled(r, agent.WorkspaceID) {
		writeError(w, http.StatusNotFound, "credentials-per-actor is not enabled for this workspace")
		return
	}

	store := credentialpolicy.NewStoreFromQueries(h.CerebroQueries)
	settings, err := store.ListForSubject(r.Context(), agent.WorkspaceID, credentialpolicy.LayerAgent, agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list credential grants: "+err.Error())
		return
	}
	items := make([]agentCredentialGrantListItem, 0, len(settings))
	for _, s := range settings {
		items = append(items, agentCredentialGrantListItem{
			Credential: s.CredentialKey,
			Setting:    string(s.Setting),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": uuidToString(agent.ID),
		"grants":   items,
	})
}

// credentialsPerActorEnabled reports whether the cerebro_credentials_per_actor
// flag is turned on for the workspace. A missing override row or a DB error means
// the TypeScript default (false) — fail-closed, so the endpoint stays dark until
// an admin deliberately enables it.
func (h *Handler) credentialsPerActorEnabled(r *http.Request, workspaceID pgtype.UUID) bool {
	if h.CerebroQueries == nil || !workspaceID.Valid {
		return false
	}
	rows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(r.Context(), workspaceID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.FlagKey == flagCredentialsPerActor {
			return row.Enabled
		}
	}
	return false
}
