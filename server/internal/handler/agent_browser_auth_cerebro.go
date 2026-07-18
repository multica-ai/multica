package handler

// FIR-3006 — narrowly broker Agent Vault browser credentials to the local
// `multica agent-browser provision-auth` bridge. The bridge immediately writes
// the password to agent-browser's encrypted auth vault over stdin and emits only
// non-sensitive result metadata.

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	"github.com/multica-ai/multica/server/internal/cerebro/browsertester"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type agentBrowserProvisionAuthRequest struct {
	Vault       string `json:"vault"`
	UsernameKey string `json:"username_key"`
	PasswordKey string `json:"password_key"`
	Host        string `json:"host"`
}

func (h *Handler) ProvisionAgentBrowserAuth(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "agent-browser provisioning is agent-only")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Agent-ID"), "agent_id")
	if !ok {
		return
	}
	wsID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Workspace-ID"), "workspace_id")
	if !ok {
		return
	}

	var req agentBrowserProvisionAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Vault) == "" || strings.TrimSpace(req.UsernameKey) == "" || strings.TrimSpace(req.PasswordKey) == "" {
		writeError(w, http.StatusBadRequest, "vault and credential keys are required")
		return
	}

	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
		return
	}
	ownerID := agent.OwnerID
	if rawOwner := strings.TrimSpace(r.Header.Get("X-User-ID")); rawOwner != "" {
		parsedOwner, parseErr := util.ParseUUID(rawOwner)
		if parseErr != nil || parsedOwner != ownerID {
			writeError(w, http.StatusForbidden, "agent owner does not match task token")
			return
		}
	}

	groups, err := h.CerebroQueries.ListCerebroGroupsForUser(r.Context(), cerebrodb.ListCerebroGroupsForUserParams{
		WorkspaceID: wsID,
		UserID:      ownerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	browserTesterGroups, err := browsertester.AgentGroupIDs(r.Context(), groups, agentID, h.CerebroQueries.ListCerebroGroupAgents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}
	resource := agentvault.VaultResourcePrefix + strings.TrimSpace(req.Vault)
	store := toolpolicy.NewStoreFromQueries(h.CerebroQueries)
	groupIDs := make([]pgtype.UUID, 0, len(groups))
	groupGranted := false
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		if !strings.EqualFold(strings.TrimSpace(group.Name), "browser-testers") {
			continue
		}
		if _, allowed := browserTesterGroups[group.ID]; !allowed {
			continue
		}
		rows, listErr := store.ListForSubject(r.Context(), wsID, toolpolicy.LayerGroup, group.ID)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return
		}
		for _, row := range rows {
			if row.ToolKey == "credential.reveal" && row.ResourcePattern == resource && row.Setting == toolpolicy.SettingAllow {
				groupGranted = true
			}
		}
	}
	if !groupGranted {
		writeError(w, http.StatusForbidden, "agent-browser provisioning is not granted for this vault and group")
		return
	}
	effective, err := store.Resolve(r.Context(), toolpolicy.Query{
		WorkspaceID:     wsID,
		ToolKey:         "credential.reveal",
		ResourcePattern: resource,
		RuntimeID:       agent.RuntimeID,
		AgentID:         agentID,
		UserID:          ownerID,
		GroupIDs:        groupIDs,
		RequestContext:  toolpolicy.RequestContext{Host: toolpolicy.HostOf(req.Host), Action: "reveal"},
		Base:            toolpolicy.SettingAllow,
	})
	if err != nil || !effective.Allowed() {
		writeError(w, http.StatusForbidden, "agent-browser provisioning is not allowed")
		return
	}
	if h.PersonalBrowserSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "Agent Vault provisioning is not configured")
		return
	}
	username, err := h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, req.Vault, req.UsernameKey)
	if err != nil {
		h.auditPersonalBrowser(wsID, agentID, toolpolicy.HostOf(req.Host), "agent-browser-provision-auth", "deny", "credential unavailable")
		writeError(w, http.StatusBadGateway, "credential unavailable")
		return
	}
	password, err := h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, req.Vault, req.PasswordKey)
	if err != nil {
		h.auditPersonalBrowser(wsID, agentID, toolpolicy.HostOf(req.Host), "agent-browser-provision-auth", "deny", "credential unavailable")
		writeError(w, http.StatusBadGateway, "credential unavailable")
		return
	}
	h.auditPersonalBrowser(wsID, agentID, toolpolicy.HostOf(req.Host), "agent-browser-provision-auth", "allow", "vault, group, and agent grant matched")
	writeJSON(w, http.StatusOK, map[string]any{
		"username": username,
		"password": password,
		"audit": map[string]string{
			"vault": req.Vault, "username_key": req.UsernameKey, "password_key": req.PasswordKey,
		},
	})
}
