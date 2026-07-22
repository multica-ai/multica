package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	"github.com/multica-ai/multica/server/internal/cerebro/browsertester"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/internalbrowserqa"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type InternalBrowserQARunner interface {
	Verify(context.Context, string, internalbrowserqa.Credential) (internalbrowserqa.Result, error)
}

type internalBrowserVerifyRequest struct {
	App string `json:"app"`
}

func (h *Handler) VerifyInternalAgentBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "internal browser verification is agent-only")
		return
	}
	var req internalBrowserVerifyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	target, err := internalbrowserqa.TargetFor(req.App)
	if err != nil {
		writeError(w, http.StatusBadRequest, "internal browser target is not allowed")
		return
	}
	wsID, agentID, ownerID, ok := h.authorizeInternalAgentBrowser(w, r, target.Vault)
	if !ok {
		return
	}
	if h.InternalBrowserQA == nil {
		writeError(w, http.StatusServiceUnavailable, "internal browser runner is not configured")
		return
	}

	credential := internalbrowserqa.Credential{}
	if target.SessionCookie {
		owner, userErr := h.Queries.GetUser(r.Context(), ownerID)
		if userErr != nil {
			writeError(w, http.StatusInternalServerError, "browser session owner is unavailable")
			return
		}
		credential.SessionToken, err = h.issueJWT(owner)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "browser session could not be created")
			return
		}
	}
	if target.Vault != "" {
		if h.PersonalBrowserSecrets == nil {
			writeError(w, http.StatusServiceUnavailable, "Agent Vault provisioning is not configured")
			return
		}
		credential.Username, err = h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, target.Vault, "USERNAME")
		// A code-login target has no password: it signs in with the staging
		// master code instead, so that key replaces PASSWORD entirely.
		if err == nil && target.CodeSelector != "" {
			credential.LoginCode, err = h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, target.Vault, "LOGIN_CODE")
		} else if err == nil {
			credential.Password, err = h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, target.Vault, "PASSWORD")
		}
		if err == nil && target.AccessHeaders {
			credential.AccessClientID, err = h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, target.Vault, "CF_ACCESS_CLIENT_ID")
			if err == nil {
				credential.AccessClientSecret, err = h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, target.Vault, "CF_ACCESS_CLIENT_SECRET")
			}
		}
		if err != nil {
			h.auditPersonalBrowser(wsID, agentID, target.Host(), "internal-agent-browser-verify", "deny", "credential unavailable")
			writeError(w, http.StatusBadGateway, "credential unavailable")
			return
		}
	}
	result, err := h.InternalBrowserQA.Verify(r.Context(), target.Name, credential)
	if err != nil {
		h.auditPersonalBrowser(wsID, agentID, target.Host(), "internal-agent-browser-verify", "deny", "browser verification failed")
		writeInternalBrowserVerificationError(w, result, err)
		return
	}
	h.auditPersonalBrowser(wsID, agentID, target.Host(), "internal-agent-browser-verify", "allow", "internal target verified")
	writeJSON(w, http.StatusOK, result)
}

func writeInternalBrowserVerificationError(w http.ResponseWriter, result internalbrowserqa.Result, err error) {
	// A 502 body is replaced by the outer gateway. 422 preserves the allowlisted
	// stage while still making the failed verification unambiguously non-successful.
	// The body also carries the attempted address, the stage and cause, and a
	// screenshot when one exists, so a caller can act without guessing.
	writeJSON(w, http.StatusUnprocessableEntity, internalbrowserqa.FailureBody(result, err))
}

func (h *Handler) authorizeInternalAgentBrowser(w http.ResponseWriter, r *http.Request, vault string) (pgtype.UUID, pgtype.UUID, pgtype.UUID, bool) {
	agentID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Agent-ID"), "agent_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	wsID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-Workspace-ID"), "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	ownerID := agent.OwnerID
	if rawOwner := strings.TrimSpace(r.Header.Get("X-User-ID")); rawOwner != "" {
		parsedOwner, parseErr := util.ParseUUID(rawOwner)
		if parseErr != nil || parsedOwner != ownerID {
			writeError(w, http.StatusForbidden, "agent owner does not match task token")
			return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
		}
	}
	groups, err := h.CerebroQueries.ListCerebroGroupsForUser(r.Context(), cerebrodb.ListCerebroGroupsForUserParams{
		WorkspaceID: wsID, UserID: ownerID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	browserTesterGroups, err := browsertester.AgentGroupIDs(r.Context(), groups, agentID, h.CerebroQueries.ListCerebroGroupAgents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	store := toolpolicy.NewStoreFromQueries(h.CerebroQueries)
	groupIDs := make([]pgtype.UUID, 0, len(groups))
	browserTester := false
	vaultGranted := vault == ""
	resource := agentvault.VaultResourcePrefix + vault
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
		if !strings.EqualFold(strings.TrimSpace(group.Name), "browser-testers") {
			continue
		}
		if _, allowed := browserTesterGroups[group.ID]; !allowed {
			continue
		}
		browserTester = true
		if vault == "" {
			continue
		}
		rows, listErr := store.ListForSubject(r.Context(), wsID, toolpolicy.LayerGroup, group.ID)
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "permission check failed")
			return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
		}
		for _, row := range rows {
			if row.ToolKey == "credential.reveal" && row.ResourcePattern == resource && row.Setting == toolpolicy.SettingAllow {
				vaultGranted = true
			}
		}
	}
	if !browserTester || !vaultGranted {
		writeError(w, http.StatusForbidden, "internal browser verification is not granted")
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if vault != "" {
		effective, resolveErr := store.Resolve(r.Context(), toolpolicy.Query{
			WorkspaceID: wsID, ToolKey: "credential.reveal", ResourcePattern: resource,
			RuntimeID: agent.RuntimeID, AgentID: agentID, UserID: ownerID, GroupIDs: groupIDs,
			RequestContext: toolpolicy.RequestContext{Host: "internal", Action: "reveal"}, Base: toolpolicy.SettingAllow,
		})
		if resolveErr != nil || !effective.Allowed() {
			writeError(w, http.StatusForbidden, "internal browser verification is not allowed")
			return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, false
		}
	}
	return wsID, agentID, ownerID, true
}
