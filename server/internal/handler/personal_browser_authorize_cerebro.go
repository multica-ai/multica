package handler

// FIR-2037 — per-action authoritative gate for the PERSONAL BROWSER.
//
// The claim-time gate (daemon_personal_browser_cerebro.go) decides ONCE, when an
// agent starts, whether the personal-browser transport is available at all. That
// is too early to honour conditions: at claim time we do not know which site the
// agent will open. This endpoint moves the real decision to PER ACTION — every
// time the agent opens or acts on a site, the desktop control server calls here
// with the target HOST, and we resolve the full tool-policy chain
// (workspace → runtime → agent → group → user) with Base=Deny AND the host in the
// request context, so a host-allowlist condition ("only these domains") actually
// bites (FIR-1609 conditions). This is what makes the personal browser:
//   - settable on ALL layers (the chain already does this),
//   - conditional / site-limited (RequestContext.Host below), and
//   - authoritative: the caller is the AGENT (mat_ token), resolved server-side
//     from the auth middleware's server-set X-Agent-ID — an ungranted agent gets
//     Deny here even if it found the desktop loopback sidecar, closing the
//     same-user bypass the Fase 3 security review flagged.
//
// This is a cerebro-zone file (`_cerebro.go`); the only upstream touch is the
// single marked route line in router.go.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
)

type PersonalBrowserSecretReader interface {
	RevealCredential(ctx context.Context, workspaceID pgtype.UUID, vault, key string) (string, error)
}

type personalBrowserSecureFillRequest struct {
	Host       string `json:"host"`
	Vault      string `json:"vault"`
	Key        string `json:"key"`
	Action     string `json:"action"`
	AgentToken string `json:"agentToken"`
}

// SecureFillPersonalBrowser returns one secret exclusively to the trusted
// desktop bridge. The bridge injects it into Chromium and reduces its own
// response to success + non-sensitive audit metadata before the CLI sees it.
func (h *Handler) SecureFillPersonalBrowser(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") != "" {
		writeError(w, http.StatusForbidden, "secure fill requires the signed-in desktop app")
		return
	}
	memberID, ok := parseUUIDOrBadRequest(w, r.Header.Get("X-User-ID"), "user_id")
	if !ok {
		return
	}
	var req personalBrowserSecureFillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action != "secure-fill" {
		writeError(w, http.StatusBadRequest, "invalid action")
		return
	}
	if !strings.HasPrefix(req.AgentToken, "mat_") {
		writeError(w, http.StatusForbidden, "invalid agent token")
		return
	}
	taskToken, err := h.Queries.GetTaskTokenByHash(r.Context(), auth.HashToken(req.AgentToken))
	if err != nil || taskToken.UserID != memberID {
		writeError(w, http.StatusForbidden, "invalid agent token")
		return
	}
	agentID, wsID := taskToken.AgentID, taskToken.WorkspaceID
	agent, err := h.Queries.GetAgent(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != wsID {
		writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
		return
	}
	groups, err := h.CerebroQueries.ListCerebroGroupsForUser(r.Context(), cerebrodb.ListCerebroGroupsForUserParams{WorkspaceID: wsID, UserID: agent.OwnerID})
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
		writeError(w, http.StatusForbidden, "secure fill is not granted for this vault and group")
		return
	}
	eff, err := store.Resolve(r.Context(), toolpolicy.Query{WorkspaceID: wsID, ToolKey: "credential.reveal", ResourcePattern: resource, RuntimeID: agent.RuntimeID, AgentID: agentID, UserID: agent.OwnerID, GroupIDs: groupIDs, RequestContext: toolpolicy.RequestContext{Host: toolpolicy.HostOf(req.Host), Action: "reveal"}, Base: toolpolicy.SettingAllow})
	if err != nil || !eff.Allowed() {
		writeError(w, http.StatusForbidden, "secure fill is not allowed")
		return
	}
	if h.PersonalBrowserSecrets == nil {
		writeError(w, http.StatusServiceUnavailable, "secure fill is not configured")
		return
	}
	value, err := h.PersonalBrowserSecrets.RevealCredential(r.Context(), wsID, req.Vault, req.Key)
	if err != nil {
		h.auditPersonalBrowser(wsID, agentID, toolpolicy.HostOf(req.Host), "secure-fill", "deny", "credential unavailable")
		writeError(w, http.StatusBadGateway, "credential unavailable")
		return
	}
	h.auditPersonalBrowser(wsID, agentID, toolpolicy.HostOf(req.Host), "secure-fill", "allow", "vault and group grant matched")
	writeJSON(w, http.StatusOK, map[string]any{"value": value, "vault": req.Vault, "key": req.Key})
}

type personalBrowserAuthorizeRequest struct {
	// Host is the target host of the action (the site the agent is opening or
	// acting on). The desktop control server derives it from the navigate target
	// or the current page URL and sends it here; conditions match against it.
	Host string `json:"host"`
	// Action is informational context for the audit line (navigate/click/fill/…),
	// never part of the decision.
	Action string `json:"action"`
}

// AuthorizePersonalBrowser resolves whether the CALLING AGENT may drive the
// personal browser against a given host, right now.
//
// POST /api/cerebro/personal-browser/authorize  (agent mat_ token)
// Body:     { "host": "app.example.com", "action": "navigate" }
// Response: { "allowed": bool, "decision": "allow|ask|deny", "reason": string }
//
// The chain is resolved with Base=Deny: an unconfigured workspace keeps the
// browser sealed. Only an explicit allow opens it; ask (human approval) and deny
// both return allowed=false here — the personal-browser surface acts inside the
// user's real, logged-in session, so it never proceeds on an unresolved ask.
func (h *Handler) AuthorizePersonalBrowser(w http.ResponseWriter, r *http.Request) {
	// Authoritative identity: the auth middleware validated the mat_ token and
	// SERVER-SET these headers from the token row (overriding anything the client
	// sent). Only a task token carries an agent identity; reject everything else
	// so a human PAT or a forged header can never reach this agent-only gate.
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "personal browser is agent-only")
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
	// Owner (user-ceiling layer). Present on a task token; tolerate absence.
	var ownerID pgtype.UUID
	if uid := strings.TrimSpace(r.Header.Get("X-User-ID")); uid != "" {
		if parsed, err := util.ParseUUID(uid); err == nil {
			ownerID = parsed
		}
	}

	var req personalBrowserAuthorizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	host := toolpolicy.HostOf(req.Host)

	// Resolve the agent's runtime (the runtime layer keys on it). A lookup
	// failure fails closed — a missing runtime ceiling must not under-enforce a
	// Deny-base gate. Cross-check the token's workspace against the agent row so
	// a token can never resolve an agent in another workspace.
	var runtimeID pgtype.UUID
	if agent, err := h.Queries.GetAgent(r.Context(), agentID); err == nil {
		if agent.WorkspaceID != wsID {
			writeError(w, http.StatusForbidden, "agent does not belong to this workspace")
			return
		}
		runtimeID = agent.RuntimeID
		if !ownerID.Valid {
			ownerID = agent.OwnerID
		}
	} else {
		slog.Warn("personal-browser authorize: agent lookup failed — failing closed",
			"workspace_id", util.UUIDToString(wsID), "agent_id", util.UUIDToString(agentID), "error", err)
		h.auditPersonalBrowser(wsID, agentID, host, req.Action, "deny", "agent lookup failed")
		writeJSON(w, http.StatusOK, map[string]any{
			"allowed": false, "decision": "deny", "reason": "agent lookup failed",
		})
		return
	}

	store := toolpolicy.NewStoreFromQueries(h.CerebroQueries)
	eff, err := store.Resolve(r.Context(), toolpolicy.Query{
		WorkspaceID: wsID,
		ToolKey:     personalBrowserToolKey,
		RuntimeID:   runtimeID,
		AgentID:     agentID,
		UserID:      ownerID,
		// The host is the whole point: a rule with a host-allowlist condition only
		// bites when the call's host is on the list (FIR-1609). Rows without a
		// condition (a plain Allow/Deny) ignore the host and resolve as before.
		RequestContext: toolpolicy.RequestContext{Host: host},
		Base:           toolpolicy.SettingDeny,
	})
	if err != nil {
		slog.Error("personal-browser authorize: resolve failed",
			"workspace_id", util.UUIDToString(wsID), "agent_id", util.UUIDToString(agentID), "host", host, "error", err)
		h.auditPersonalBrowser(wsID, agentID, host, req.Action, "deny", "resolve failed")
		writeError(w, http.StatusInternalServerError, "permission check failed")
		return
	}

	decision := "deny"
	allowed := false
	switch eff.Setting {
	case toolpolicy.SettingAllow:
		decision, allowed = "allow", true
	case toolpolicy.SettingAsk:
		// Ask means a human must approve. There is no inbox round-trip on this
		// surface yet, so an unresolved ask does NOT proceed — it acts inside the
		// user's real session. Surfaced distinctly so the desktop can say why.
		decision = "ask"
	default:
		decision = "deny"
	}

	h.auditPersonalBrowser(wsID, agentID, host, req.Action, decision, eff.Reason)
	writeJSON(w, http.StatusOK, map[string]any{
		"allowed":  allowed,
		"decision": decision,
		"reason":   eff.Reason,
	})
}

// auditPersonalBrowser records every per-action verdict on the user's logged-in
// browser. The desktop side already appends a local audit line per action; this
// is the central, server-side trail — the security-sensitive decision (which
// agent, which host, allowed or not) lives in the server log, not only on the
// machine. slog is used (not the issue timeline) because the action has no issue
// context.
func (h *Handler) auditPersonalBrowser(wsID, agentID pgtype.UUID, host, action, decision, reason string) {
	slog.Info("personal-browser authorize",
		"workspace_id", util.UUIDToString(wsID),
		"agent_id", util.UUIDToString(agentID),
		"host", host,
		"action", action,
		"decision", decision,
		"reason", reason,
	)
}
