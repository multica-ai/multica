package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/integrations/composio"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Connected Apps v2 (MUL-8516). Team accounts belong to the workspace and are
// managed by admins; personal accounts belong to a member. Agents get apps
// with an account scope and a level; every call goes through the gateway in
// internal/connectedapps.

// AppConnectionResponse is one team or personal account.
type AppConnectionResponse struct {
	ID          string  `json:"id"`
	ToolkitSlug string  `json:"toolkit_slug"`
	Status      string  `json:"status"`
	ConnectedBy *string `json:"connected_by,omitempty"`
	ConnectedAt string  `json:"connected_at"`
	LastUsedAt  *string `json:"last_used_at"`
}

// AgentAppResponse is one app attached to an agent.
type AgentAppResponse struct {
	AgentID      string `json:"agent_id"`
	ToolkitSlug  string `json:"toolkit_slug"`
	AccountScope string `json:"account_scope"`
	Level        string `json:"level"`
}

// AppsOverviewResponse backs Settings › Apps.
type AppsOverviewResponse struct {
	CatalogMode string                  `json:"catalog_mode"`
	CanManage   bool                    `json:"can_manage"`
	Team        []AppConnectionResponse `json:"team"`
	Personal    []AppConnectionResponse `json:"personal"`
	AgentApps   []AgentAppResponse      `json:"agent_apps"`
}

// AppCatalogEntry is one app in the add-app dialog.
type AppCatalogEntry struct {
	Slug             string `json:"slug"`
	Name             string `json:"name"`
	Logo             string `json:"logo,omitempty"`
	Category         string `json:"category,omitempty"`
	Curated          bool   `json:"curated"`
	RecommendedScope string `json:"recommended_scope,omitempty"`
}

// AppRuleResponse is the policy of one action class for one account.
type AppRuleResponse struct {
	ActionClass   string `json:"action_class"`
	Policy        string `json:"policy"`
	DefaultPolicy string `json:"default_policy"`
	Custom        bool   `json:"custom"`
}

// AppToolCallResponse is one row of an app's activity.
type AppToolCallResponse struct {
	ID            string  `json:"id"`
	AgentID       string  `json:"agent_id"`
	IssueID       *string `json:"issue_id"`
	ChatSessionID *string `json:"chat_session_id"`
	TriggeredBy   *string `json:"triggered_by"`
	ToolSlug      string  `json:"tool_slug"`
	ActionClass   string  `json:"action_class"`
	Outcome       string  `json:"outcome"`
	Error         *string `json:"error"`
	CreatedAt     string  `json:"created_at"`
}

// AppRequestResponse is one confirmation or connection request. Arguments and
// results are shown only to the approver: a personal account's draft email is
// not for everyone who can open the issue.
type AppRequestResponse struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	Status            string          `json:"status"`
	AgentID           string          `json:"agent_id"`
	IssueID           *string         `json:"issue_id"`
	ChatSessionID     *string         `json:"chat_session_id"`
	ApproverUserID    string          `json:"approver_user_id"`
	AccountScope      string          `json:"account_scope"`
	ToolkitSlug       string          `json:"toolkit_slug"`
	ToolSlug          string          `json:"tool_slug"`
	ActionClass       string          `json:"action_class"`
	Summary           string          `json:"summary"`
	Arguments         json.RawMessage `json:"arguments,omitempty"`
	ApprovedArguments json.RawMessage `json:"approved_arguments,omitempty"`
	Result            json.RawMessage `json:"result,omitempty"`
	Error             *string         `json:"error"`
	Reason            *string         `json:"reason"`
	Redacted          bool            `json:"redacted"`
	ExpiresAt         string          `json:"expires_at"`
	DecidedBy         *string         `json:"decided_by"`
	DecidedAt         *string         `json:"decided_at"`
	CreatedAt         string          `json:"created_at"`
}

func (h *Handler) appsReady(w http.ResponseWriter, r *http.Request) bool {
	if h.Composio == nil || h.ConnectedApps == nil || !h.composioMCPAppsEnabled(r.Context()) {
		writeFeatureDisabled(w, "apps_not_configured", "connected apps are not configured")
		return false
	}
	return true
}

// appsMember returns the caller's membership and whether they administer the
// workspace.
func (h *Handler) appsMember(w http.ResponseWriter, r *http.Request) (db.Member, pgtype.UUID, bool, bool) {
	workspaceID := ctxWorkspaceID(r.Context())
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Member{}, pgtype.UUID{}, false, false
	}
	return member, parseUUID(workspaceID), roleAllowed(member.Role, "owner", "admin"), true
}

func connectionFromTeam(row db.WorkspaceComposioConnection) AppConnectionResponse {
	return AppConnectionResponse{
		ID:          uuidToString(row.ID),
		ToolkitSlug: row.ToolkitSlug,
		Status:      row.Status,
		ConnectedBy: uuidToPtr(row.ConnectedBy),
		ConnectedAt: timestampToString(row.ConnectedAt),
		LastUsedAt:  timestampToPtr(row.LastUsedAt),
	}
}

func connectionFromPersonal(row db.UserComposioConnection) AppConnectionResponse {
	return AppConnectionResponse{
		ID:          uuidToString(row.ID),
		ToolkitSlug: row.ToolkitSlug,
		Status:      row.Status,
		ConnectedAt: timestampToString(row.ConnectedAt),
		LastUsedAt:  timestampToPtr(row.LastUsedAt),
	}
}

func agentAppResponse(row db.AgentApp) AgentAppResponse {
	return AgentAppResponse{
		AgentID:      uuidToString(row.AgentID),
		ToolkitSlug:  row.ToolkitSlug,
		AccountScope: row.AccountScope,
		Level:        row.Level,
	}
}

// GetAppsOverview (GET /api/apps) returns the workspace's team accounts, the
// caller's personal accounts, and which agents use which apps.
func (h *Handler) GetAppsOverview(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	team, err := h.Queries.ListWorkspaceComposioConnections(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list team accounts")
		return
	}
	personal, err := h.Queries.ListUserComposioConnectionsAnyStatus(ctx, member.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list personal accounts")
		return
	}
	agentApps, err := h.Queries.ListAgentAppsInWorkspace(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent apps")
		return
	}
	resp := AppsOverviewResponse{
		CatalogMode: h.ConnectedApps.CatalogMode(ctx, workspaceID),
		CanManage:   isAdmin,
		Team:        make([]AppConnectionResponse, 0, len(team)),
		Personal:    make([]AppConnectionResponse, 0, len(personal)),
		AgentApps:   make([]AgentAppResponse, 0, len(agentApps)),
	}
	for _, row := range team {
		resp.Team = append(resp.Team, connectionFromTeam(row))
	}
	for _, row := range personal {
		resp.Personal = append(resp.Personal, connectionFromPersonal(row))
	}
	for _, row := range agentApps {
		resp.AgentApps = append(resp.AgentApps, agentAppResponse(row))
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListAppCatalog (GET /api/apps/catalog) is every app the workspace may add:
// all connectable apps, or only curated ones in curated mode.
func (h *Handler) ListAppCatalog(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	_, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	toolkits, err := h.Composio.ListToolkits(r.Context())
	if err != nil {
		slog.Warn("apps catalog failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusBadGateway, "failed to load apps")
		return
	}
	mode := h.ConnectedApps.CatalogMode(r.Context(), workspaceID)
	out := make([]AppCatalogEntry, 0, len(toolkits))
	for _, tk := range toolkits {
		slug := connectedapps.NormalizeSlug(tk.Slug)
		if !connectedapps.AppAllowed(mode, slug) {
			continue
		}
		entry := AppCatalogEntry{Slug: slug, Name: tk.Name, Logo: tk.LogoURL, Category: tk.Category}
		if app, ok := connectedapps.Curated(slug); ok {
			entry.Curated, entry.RecommendedScope = true, app.Scope
		}
		out = append(out, entry)
	}
	// Curated apps first, each group keeping Composio's usage order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Curated && !out[j].Curated })
	writeJSON(w, http.StatusOK, map[string]any{"catalog_mode": mode, "apps": out})
}

type appSlugRequest struct {
	ToolkitSlug string `json:"toolkit_slug"`
}

func (h *Handler) decodeAppSlug(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID) (string, bool) {
	var req appSlugRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	slug := connectedapps.NormalizeSlug(req.ToolkitSlug)
	if slug == "" {
		writeError(w, http.StatusBadRequest, "toolkit_slug is required")
		return "", false
	}
	if !connectedapps.AppAllowed(h.ConnectedApps.CatalogMode(r.Context(), workspaceID), slug) {
		writeError(w, http.StatusForbidden, "this workspace only allows curated apps")
		return "", false
	}
	return slug, true
}

func writeConnectError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, composio.ErrToolkitNotSupported) {
		writeError(w, http.StatusBadRequest, "app not supported")
		return
	}
	slog.Warn("apps connect failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusBadGateway, "failed to start connect")
}

// ConnectTeamApp (POST /api/apps/team/connect) starts connecting the
// workspace's team account for an app. Admins only.
func (h *Handler) ConnectTeamApp(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "only workspace admins can connect team accounts")
		return
	}
	slug, ok := h.decodeAppSlug(w, r, workspaceID)
	if !ok {
		return
	}
	url, err := h.Composio.BeginTeamConnect(r.Context(), workspaceID, member.UserID, slug)
	if err != nil {
		writeConnectError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ComposioConnectInitResponse{RedirectURL: url})
}

// ConnectPersonalApp (POST /api/apps/personal/connect) starts connecting the
// caller's own account, within the workspace's catalog.
func (h *Handler) ConnectPersonalApp(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	slug, ok := h.decodeAppSlug(w, r, workspaceID)
	if !ok {
		return
	}
	url, err := h.Composio.BeginConnect(r.Context(), member.UserID, slug)
	if err != nil {
		writeConnectError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ComposioConnectInitResponse{RedirectURL: url})
}

// DisconnectTeamApp (DELETE /api/apps/team/connections/{id}) revokes a team
// account and cancels the requests waiting on it. Admins only.
func (h *Handler) DisconnectTeamApp(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	_, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "only workspace admins can disconnect team accounts")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "connection id")
	if !ok {
		return
	}
	row, err := h.Composio.DisconnectTeam(r.Context(), workspaceID, id)
	if errors.Is(err, composio.ErrConnectionNotFound) {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}
	if err != nil {
		slog.Warn("apps team disconnect failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusBadGateway, "failed to disconnect")
		return
	}
	h.ConnectedApps.CancelTeamRequests(r.Context(), workspaceID, row.ToolkitSlug)
	w.WriteHeader(http.StatusNoContent)
}

// GetTeamAppImpact (GET /api/apps/team/impact?toolkit_slug=) is what
// disconnecting a team account would stop. Admins only.
func (h *Handler) GetTeamAppImpact(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	_, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	slug := connectedapps.NormalizeSlug(r.URL.Query().Get("toolkit_slug"))
	ctx := r.Context()
	apps, err := h.Queries.ListAgentAppsInWorkspace(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent apps")
		return
	}
	agentIDs := []pgtype.UUID{}
	agents := []map[string]string{}
	for _, app := range apps {
		if app.ToolkitSlug != slug || app.AccountScope != connectedapps.ScopeTeam {
			continue
		}
		agentIDs = append(agentIDs, app.AgentID)
		name := ""
		if agent, err := h.Queries.GetAgent(ctx, app.AgentID); err == nil {
			name = agent.Name
		}
		agents = append(agents, map[string]string{"id": uuidToString(app.AgentID), "name": name})
	}
	var tasks, autopilots int64
	if len(agentIDs) > 0 {
		tasks, _ = h.Queries.CountActiveTasksForAgents(ctx, agentIDs)
		autopilots, _ = h.Queries.CountActiveAutopilotsForAgents(ctx, db.CountActiveAutopilotsForAgentsParams{WorkspaceID: workspaceID, AgentIds: agentIDs})
	}
	pending, _ := h.Queries.CountPendingTeamAppActionRequests(ctx, db.CountPendingTeamAppActionRequestsParams{WorkspaceID: workspaceID, ToolkitSlug: slug})
	writeJSON(w, http.StatusOK, map[string]any{
		"agents":            agents,
		"active_tasks":      tasks,
		"active_autopilots": autopilots,
		"pending_requests":  pending,
	})
}

// UpdateAppSettings (PUT /api/apps/settings) sets the catalog mode. Admins only.
func (h *Handler) UpdateAppSettings(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	if !isAdmin {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req struct {
		CatalogMode string `json:"catalog_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.CatalogMode != connectedapps.CatalogAll && req.CatalogMode != connectedapps.CatalogCurated) {
		writeError(w, http.StatusBadRequest, "catalog_mode must be all or curated")
		return
	}
	row, err := h.Queries.UpsertWorkspaceAppSetting(r.Context(), db.UpsertWorkspaceAppSettingParams{
		WorkspaceID: workspaceID, CatalogMode: req.CatalogMode, UpdatedBy: member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"catalog_mode": row.CatalogMode})
}

// ruleScope resolves whose rules a request reads or writes: the team account
// (owner NULL) or the caller's personal account.
func ruleScope(r *http.Request, member db.Member) (string, pgtype.UUID, bool) {
	switch r.URL.Query().Get("scope") {
	case connectedapps.ScopeTeam:
		return connectedapps.ScopeTeam, pgtype.UUID{}, true
	case connectedapps.ScopePersonal:
		return connectedapps.ScopePersonal, member.UserID, true
	}
	return "", pgtype.UUID{}, false
}

func (h *Handler) rulesResponse(ctx context.Context, workspaceID pgtype.UUID, slug string, owner pgtype.UUID) ([]AppRuleResponse, error) {
	rows, err := h.Queries.ListAppActionRules(ctx, db.ListAppActionRulesParams{WorkspaceID: workspaceID, ToolkitSlug: slug, OwnerUserID: owner})
	if err != nil {
		return nil, err
	}
	set := map[string]string{}
	for _, row := range rows {
		set[row.ActionClass] = row.Policy
	}
	out := make([]AppRuleResponse, 0, len(connectedapps.Classes))
	for _, class := range connectedapps.Classes {
		def := connectedapps.DefaultPolicy(slug, class)
		policy, custom := set[class]
		if !custom {
			policy = def
		}
		out = append(out, AppRuleResponse{ActionClass: class, Policy: policy, DefaultPolicy: def, Custom: custom})
	}
	return out, nil
}

// GetAppRules (GET /api/apps/rules?toolkit_slug=&scope=team|personal).
func (h *Handler) GetAppRules(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	_, owner, ok := ruleScope(r, member)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be team or personal")
		return
	}
	slug := connectedapps.NormalizeSlug(r.URL.Query().Get("toolkit_slug"))
	rules, err := h.rulesResponse(r.Context(), workspaceID, slug, owner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// UpdateAppRules (PUT /api/apps/rules?toolkit_slug=&scope=) replaces the
// rules of one account. Team rules need an admin; personal rules are the
// caller's own.
func (h *Handler) UpdateAppRules(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	scope, owner, ok := ruleScope(r, member)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be team or personal")
		return
	}
	if scope == connectedapps.ScopeTeam && !isAdmin {
		writeError(w, http.StatusForbidden, "only workspace admins can change team account rules")
		return
	}
	slug := connectedapps.NormalizeSlug(r.URL.Query().Get("toolkit_slug"))
	if slug == "" {
		writeError(w, http.StatusBadRequest, "toolkit_slug is required")
		return
	}
	var req struct {
		Rules []struct {
			ActionClass string `json:"action_class"`
			Policy      string `json:"policy"`
		} `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	for _, rule := range req.Rules {
		if !connectedapps.ValidClass(rule.ActionClass) || !connectedapps.ValidPolicy(rule.Policy) {
			writeError(w, http.StatusBadRequest, "invalid rule")
			return
		}
	}
	for _, rule := range req.Rules {
		if _, err := h.Queries.UpsertAppActionRule(r.Context(), db.UpsertAppActionRuleParams{
			WorkspaceID: workspaceID, OwnerUserID: owner, ToolkitSlug: slug,
			ActionClass: rule.ActionClass, Policy: rule.Policy, UpdatedBy: member.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save rules")
			return
		}
	}
	rules, err := h.rulesResponse(r.Context(), workspaceID, slug, owner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// ListAppActivity (GET /api/apps/activity?toolkit_slug=&scope=) lists recent
// calls through one account. Team activity is for admins; personal activity
// for its owner.
func (h *Handler) ListAppActivity(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	scope, owner, ok := ruleScope(r, member)
	if !ok {
		writeError(w, http.StatusBadRequest, "scope must be team or personal")
		return
	}
	slug := connectedapps.NormalizeSlug(r.URL.Query().Get("toolkit_slug"))
	limit := int32(50)
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = int32(v)
	}
	var rows []db.AppToolCall
	var err error
	if scope == connectedapps.ScopeTeam {
		if !isAdmin {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		rows, err = h.Queries.ListTeamAppToolCalls(r.Context(), db.ListTeamAppToolCallsParams{WorkspaceID: workspaceID, ToolkitSlug: slug, Limit: limit})
	} else {
		rows, err = h.Queries.ListPersonalAppToolCalls(r.Context(), db.ListPersonalAppToolCallsParams{WorkspaceID: workspaceID, ToolkitSlug: slug, AccountOwnerUserID: owner, Limit: limit})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list activity")
		return
	}
	out := make([]AppToolCallResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, AppToolCallResponse{
			ID: uuidToString(row.ID), AgentID: uuidToString(row.AgentID),
			IssueID: uuidToPtr(row.IssueID), ChatSessionID: uuidToPtr(row.ChatSessionID), TriggeredBy: uuidToPtr(row.TriggeredBy),
			ToolSlug: row.ToolSlug, ActionClass: row.ActionClass, Outcome: row.Outcome,
			Error: textToPtr(row.Error), CreatedAt: timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"calls": out})
}

// ListAgentApps (GET /api/agents/{id}/apps).
func (h *Handler) ListAgentApps(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentApps(r.Context(), agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent apps")
		return
	}
	out := make([]AgentAppResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentAppResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out})
}

// UpdateAgentApps (PUT /api/agents/{id}/apps) replaces an agent's apps. The
// agent's manager may attach personal-account apps; team-account apps act as
// the whole workspace, so adding or changing one needs an admin.
func (h *Handler) UpdateAgentApps(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	member, workspaceID, isAdmin, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	var req struct {
		Apps []struct {
			ToolkitSlug  string `json:"toolkit_slug"`
			AccountScope string `json:"account_scope"`
			Level        string `json:"level"`
		} `json:"apps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	existing, err := h.Queries.ListAgentApps(ctx, agent.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent apps")
		return
	}
	current := map[string]db.AgentApp{}
	for _, row := range existing {
		current[row.ToolkitSlug] = row
	}
	mode := h.ConnectedApps.CatalogMode(ctx, workspaceID)
	keep := []string{}
	seen := map[string]bool{}
	type want struct{ slug, scope, level string }
	var wants []want
	for _, app := range req.Apps {
		slug := connectedapps.NormalizeSlug(app.ToolkitSlug)
		if slug == "" || seen[slug] || !connectedapps.ValidScope(app.AccountScope) || !connectedapps.ValidLevel(app.Level) {
			writeError(w, http.StatusBadRequest, "invalid app")
			return
		}
		seen[slug] = true
		prev, had := current[slug]
		changed := !had || prev.AccountScope != app.AccountScope || prev.Level != app.Level
		if changed && !connectedapps.AppAllowed(mode, slug) {
			writeError(w, http.StatusForbidden, "this workspace only allows curated apps")
			return
		}
		touchesTeam := app.AccountScope == connectedapps.ScopeTeam || (had && prev.AccountScope == connectedapps.ScopeTeam)
		if changed && touchesTeam && !isAdmin {
			writeError(w, http.StatusForbidden, "only workspace admins can give agents a team account")
			return
		}
		keep = append(keep, slug)
		wants = append(wants, want{slug, app.AccountScope, app.Level})
	}
	if !isAdmin {
		for slug, row := range current {
			if row.AccountScope == connectedapps.ScopeTeam && !seen[slug] {
				writeError(w, http.StatusForbidden, "only workspace admins can remove a team account from an agent")
				return
			}
		}
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent apps")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteAgentAppsExcept(ctx, db.DeleteAgentAppsExceptParams{AgentID: agent.ID, Keep: keep}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent apps")
		return
	}
	for _, wnt := range wants {
		if _, err := qtx.UpsertAgentApp(ctx, db.UpsertAgentAppParams{
			WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, ToolkitSlug: wnt.slug,
			AccountScope: wnt.scope, Level: wnt.level, CreatedBy: member.UserID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save agent apps")
			return
		}
	}
	if err := qtx.SetAgentComposioToolkitAllowlist(ctx, db.SetAgentComposioToolkitAllowlistParams{ID: agent.ID, Slugs: keep}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent apps")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save agent apps")
		return
	}
	h.ListAgentApps(w, r)
}

// syncAgentAppsFromAllowlist mirrors a write through the legacy
// composio_toolkit_allowlist field into agent_app: new slugs join as personal
// apps at the default level, missing slugs leave, existing ones keep their
// scope and level.
func syncAgentAppsFromAllowlist(ctx context.Context, q *db.Queries, agent db.Agent, slugs []string, actorID pgtype.UUID) error {
	existing, err := q.ListAgentApps(ctx, agent.ID)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, row := range existing {
		have[row.ToolkitSlug] = true
	}
	keep := []string{}
	for _, s := range slugs {
		slug := connectedapps.NormalizeSlug(s)
		if slug == "" {
			continue
		}
		keep = append(keep, slug)
		if have[slug] {
			continue
		}
		if _, err := q.UpsertAgentApp(ctx, db.UpsertAgentAppParams{
			WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, ToolkitSlug: slug,
			AccountScope: connectedapps.ScopePersonal, Level: connectedapps.LevelConfirmWrites, CreatedBy: actorID,
		}); err != nil {
			return err
		}
	}
	return q.DeleteAgentAppsExcept(ctx, db.DeleteAgentAppsExceptParams{AgentID: agent.ID, Keep: keep})
}

func (h *Handler) requestResponse(req db.AppActionRequest, viewer pgtype.UUID) AppRequestResponse {
	out := AppRequestResponse{
		ID: uuidToString(req.ID), Kind: req.Kind, Status: req.Status, AgentID: uuidToString(req.AgentID),
		IssueID: uuidToPtr(req.IssueID), ChatSessionID: uuidToPtr(req.ChatSessionID),
		ApproverUserID: uuidToString(req.ApproverUserID), AccountScope: req.AccountScope,
		ToolkitSlug: req.ToolkitSlug, ToolSlug: req.ToolSlug, ActionClass: req.ActionClass, Summary: req.Summary,
		Error: textToPtr(req.Error), Reason: textToPtr(req.Reason),
		ExpiresAt: timestampToString(req.ExpiresAt), DecidedBy: uuidToPtr(req.DecidedBy),
		DecidedAt: timestampToPtr(req.DecidedAt), CreatedAt: timestampToString(req.CreatedAt),
	}
	if req.ApproverUserID == viewer {
		out.Arguments = json.RawMessage(req.Arguments)
		if len(req.ApprovedArguments) > 0 {
			out.ApprovedArguments = json.RawMessage(req.ApprovedArguments)
		}
		if len(req.Result) > 0 {
			out.Result = json.RawMessage(req.Result)
		}
	} else {
		out.Redacted = true
		out.Error = nil
	}
	return out
}

// ListAppRequests (GET /api/app-requests?issue_id= | chat_session_id= |
// pending=mine). Issue requests follow issue visibility; chat requests are
// the chat creator's.
func (h *Handler) ListAppRequests(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	var rows []db.AppActionRequest
	var err error
	switch {
	case q.Get("issue_id") != "":
		issue, ok := h.loadIssueForUser(w, r, q.Get("issue_id"))
		if !ok {
			return
		}
		rows, err = h.Queries.ListAppActionRequestsForIssue(ctx, db.ListAppActionRequestsForIssueParams{WorkspaceID: workspaceID, IssueID: issue.ID})
	case q.Get("chat_session_id") != "":
		sessionID, ok := parseUUIDOrBadRequest(w, q.Get("chat_session_id"), "chat_session_id")
		if !ok {
			return
		}
		session, serr := h.Queries.GetChatSession(ctx, sessionID)
		if serr != nil || session.WorkspaceID != workspaceID || session.CreatorID != member.UserID {
			writeError(w, http.StatusNotFound, "chat session not found")
			return
		}
		rows, err = h.Queries.ListAppActionRequestsForChatSession(ctx, db.ListAppActionRequestsForChatSessionParams{WorkspaceID: workspaceID, ChatSessionID: sessionID})
	default:
		rows, err = h.Queries.ListPendingAppActionRequestsForApprover(ctx, db.ListPendingAppActionRequestsForApproverParams{WorkspaceID: workspaceID, ApproverUserID: member.UserID})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list requests")
		return
	}
	out := make([]AppRequestResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.requestResponse(row, member.UserID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func writeDecisionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, connectedapps.ErrRequestNotFound):
		writeError(w, http.StatusNotFound, "request not found")
	case errors.Is(err, connectedapps.ErrNotApprover):
		writeError(w, http.StatusForbidden, "only the person asked can decide")
	case errors.Is(err, connectedapps.ErrNotPending):
		writeError(w, http.StatusConflict, "request is no longer pending")
	case errors.Is(err, connectedapps.ErrAccountMissing):
		writeError(w, http.StatusConflict, "connect the account first")
	default:
		writeError(w, http.StatusInternalServerError, "failed to decide")
	}
}

// ApproveAppRequest (POST /api/app-requests/{id}/approve). The approver may
// send edited arguments; Multica runs the call and resumes the agent.
func (h *Handler) ApproveAppRequest(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "request id")
	if !ok {
		return
	}
	var req struct {
		Arguments map[string]any `json:"arguments"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	row, err := h.ConnectedApps.Approve(r.Context(), workspaceID, member.UserID, id, req.Arguments)
	if err != nil {
		writeDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.requestResponse(row, member.UserID))
}

// RejectAppRequest (POST /api/app-requests/{id}/reject).
func (h *Handler) RejectAppRequest(w http.ResponseWriter, r *http.Request) {
	if !h.appsReady(w, r) {
		return
	}
	member, workspaceID, _, ok := h.appsMember(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "request id")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	row, err := h.ConnectedApps.Reject(r.Context(), workspaceID, member.UserID, id, req.Reason)
	if err != nil {
		writeDecisionError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.requestResponse(row, member.UserID))
}

// AppsGateway (POST /api/apps/mcp) is the MCP endpoint runs reach their apps
// through. It authenticates with the run's bearer token, not a session.
func (h *Handler) AppsGateway(w http.ResponseWriter, r *http.Request) {
	if h.ConnectedApps == nil || !h.composioMCPAppsEnabled(r.Context()) {
		http.Error(w, "connected apps are not configured", http.StatusServiceUnavailable)
		return
	}
	h.ConnectedApps.ServeHTTP(w, r)
}

// ResumeAfterDecision implements connectedapps.Resumer: a run on the issue,
// or a message in the chat from the person who decided.
func (h *Handler) ResumeAfterDecision(ctx context.Context, req db.AppActionRequest, actorID pgtype.UUID, note string) error {
	if req.IssueID.Valid {
		return h.TaskService.EnqueueAppDecisionRun(ctx, req.IssueID, req.AgentID, actorID, req.ID, note)
	}
	if !req.ChatSessionID.Valid {
		return nil
	}
	session, err := h.Queries.GetChatSession(ctx, req.ChatSessionID)
	if err != nil {
		return err
	}
	agent, err := h.Queries.GetAgent(ctx, session.AgentID)
	if err != nil {
		return err
	}
	content := "[Multica] " + strings.TrimSpace(note)
	sent, err := h.TaskService.SendDirectChatMessage(ctx, session, agent, actorID, content, nil, "member", actorID)
	if err != nil {
		return err
	}
	workspaceID := uuidToString(session.WorkspaceID)
	sessionID := uuidToString(session.ID)
	h.publishChat(protocol.EventChatMessage, workspaceID, "member", uuidToString(actorID), sessionID, protocol.ChatMessagePayload{
		ChatSessionID: sessionID,
		MessageID:     uuidToString(sent.Message.ID),
		Role:          "user",
		Content:       content,
		TaskID:        uuidToString(sent.Task.ID),
		CreatedAt:     timestampToString(sent.Message.CreatedAt),
	})
	return nil
}
