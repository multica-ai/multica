package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DingTalkInstallationResponse is the wire shape for a DingTalk installation
// row. The encrypted AppSecret in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it). WS lease columns are
// runtime state, not API surface, so they are omitted too.
type DingTalkInstallationResponse struct {
	ID                   string   `json:"id"`
	WorkspaceID          string   `json:"workspace_id"`
	AgentID              string   `json:"agent_id"`
	InstallerUserID      string   `json:"installer_user_id"`
	Status               string   `json:"status"`
	InstalledAt          string   `json:"installed_at"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
	AgentAvailable       bool     `json:"agent_available"`
	BoundDingTalkUserIDs []string `json:"bound_dingtalk_user_ids,omitempty"`
}

// DingTalkGroupBotResponse identifies one connected Multica bot observed in a
// DingTalk group. AgentID is the product-facing identity; BotName is the
// readable DingTalk identity when the app has qyapi_chat_manage permission.
type DingTalkGroupBotResponse struct {
	InstallationID   string `json:"installation_id"`
	AgentID          string `json:"agent_id"`
	BotName          string `json:"bot_name"`
	BotIdentityIssue string `json:"bot_identity_issue"`
	LastActiveAt     string `json:"last_active_at"`
	MentionCount     int64  `json:"mention_count"`
}

// DingTalkGroupResponse groups every connected bot observed under the same
// DingTalk openConversationId.
type DingTalkGroupResponse struct {
	ConversationID    string                     `json:"conversation_id"`
	ConversationTitle string                     `json:"conversation_title"`
	Bots              []DingTalkGroupBotResponse `json:"bots"`
}

type ListDingTalkGroupsResponse struct {
	Groups                  []DingTalkGroupResponse             `json:"groups"`
	GroupDiscoverySupported bool                                `json:"group_discovery_supported"`
	InactiveGroupCounts     map[string]int64                    `json:"inactive_group_counts"`
	BotIdentities           map[string]DingTalkGroupBotResponse `json:"bot_identities"`
	NextOffset              *int                                `json:"next_offset,omitempty"`
}

const (
	dingTalkActiveGroupWindow = 90 * 24 * time.Hour
	dingTalkInactivePageSize  = 20
	dingTalkInactiveMaxPage   = 100
)

func dingtalkInstallationToResponse(row db.ChannelInstallation) DingTalkInstallationResponse {
	return DingTalkInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		AgentAvailable:  true,
	}
}

// dingtalkAgentVisibility resolves the Agent side of the Settings inventory in
// one batch. The available set distinguishes an orphaned installation from an
// inaccessible Agent for admins. The visible set enforces the same owner / role
// / invocation-target rules as the Agent list for ordinary members.
func (h *Handler) dingtalkAgentVisibility(
	ctx context.Context,
	workspaceID pgtype.UUID,
	userID string,
	member db.Member,
) (available map[string]struct{}, visible map[string]struct{}, ok bool) {
	agents, err := h.Queries.ListAllAgents(ctx, workspaceID)
	if err != nil {
		return nil, nil, false
	}
	available = make(map[string]struct{}, len(agents))
	visible = make(map[string]struct{}, len(agents))
	for _, agent := range agents {
		agentID := uuidToString(agent.ID)
		available[agentID] = struct{}{}
		if roleAllowed(member.Role, "owner", "admin") {
			visible[agentID] = struct{}{}
		}
	}
	if roleAllowed(member.Role, "owner", "admin") {
		return available, visible, true
	}
	targetsByAgent, loaded := h.loadInvocationTargetsByAgent(ctx, agents)
	if !loaded {
		return nil, nil, false
	}
	for _, agent := range agents {
		agentID := uuidToString(agent.ID)
		if memberAllowedToViewAgent(agent, targetsByAgent[agentID], userID, member.Role) {
			visible[agentID] = struct{}{}
		}
	}
	return available, visible, true
}

// ListDingTalkInstallations (GET /api/workspaces/{id}/dingtalk/installations) is
// member-visible so the Integrations tab renders for non-admins. Response flags
// mirror Slack:
//   - configured: at-rest encryption key is set (DingTalkInstall != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a BYO install needs only the at-rest key.
func (h *Handler) ListDingTalkInstallations(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []DingTalkInstallationResponse{},
			"configured":        false,
			"install_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(wsUUID))
	if !ok {
		return
	}
	availableAgentIDs, visibleAgentIDs, loaded := h.dingtalkAgentVisibility(
		r.Context(), wsUUID, userID, member,
	)
	if !loaded {
		writeError(w, http.StatusInternalServerError, "failed to resolve dingtalk installation visibility")
		return
	}
	rows, err := h.DingTalkInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dingtalk installations")
		return
	}
	bindingsByInstallation := map[pgtype.UUID][]string{}
	canViewAll := roleAllowed(member.Role, "owner", "admin")
	canViewAccountBindings := canViewAll
	if canViewAccountBindings {
		userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
		if !ok {
			return
		}
		bindings, err := h.Queries.ListDingTalkUserBindingsForMember(r.Context(), db.ListDingTalkUserBindingsForMemberParams{
			WorkspaceID:   wsUUID,
			MulticaUserID: userUUID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list dingtalk user bindings")
			return
		}
		for _, binding := range bindings {
			bindingsByInstallation[binding.InstallationID] = append(
				bindingsByInstallation[binding.InstallationID],
				binding.ChannelUserID,
			)
		}
	}
	out := make([]DingTalkInstallationResponse, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if !canViewAll {
			if _, canView := visibleAgentIDs[agentID]; !canView {
				continue
			}
		}
		response := dingtalkInstallationToResponse(row)
		_, response.AgentAvailable = availableAgentIDs[agentID]
		response.BoundDingTalkUserIDs = bindingsByInstallation[row.ID]
		out = append(out, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// ListDingTalkGroups (GET /api/workspaces/{id}/dingtalk/groups) returns the
// Settings inventory of observed groups and connected Multica bots. Workspace
// owners/admins see the full inventory; ordinary members only receive bots for
// Agents they can open, matching ListAgents and Agent Detail.
func (h *Handler) ListDingTalkGroups(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeJSON(w, http.StatusOK, ListDingTalkGroupsResponse{
			Groups:                  []DingTalkGroupResponse{},
			GroupDiscoverySupported: true,
			InactiveGroupCounts:     map[string]int64{},
			BotIdentities:           map[string]DingTalkGroupBotResponse{},
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, uuidToString(wsUUID))
	if !ok {
		return
	}
	var visibleAgentIDs map[string]struct{}
	if !roleAllowed(member.Role, "owner", "admin") {
		_, visible, loaded := h.dingtalkAgentVisibility(r.Context(), wsUUID, userID, member)
		if !loaded {
			writeError(w, http.StatusInternalServerError, "failed to resolve dingtalk group visibility")
			return
		}
		visibleAgentIDs = visible
	}
	h.listDingTalkGroups(w, r, wsUUID, "", visibleAgentIDs)
}

// ListDingTalkGroupsForAgent (GET /api/agents/{id}/dingtalk/groups) exposes
// only the selected agent's 1:1 bot and its observed groups. It deliberately
// reuses the Agent detail view gate: if the caller cannot open this Agent,
// they cannot infer its DingTalk group activity either.
func (h *Handler) ListDingTalkGroupsForAgent(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return
	}
	workspaceID := uuidToString(agent.WorkspaceID)
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if !h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return
	}
	if h.DingTalkInstall == nil {
		writeJSON(w, http.StatusOK, ListDingTalkGroupsResponse{
			Groups:                  []DingTalkGroupResponse{},
			GroupDiscoverySupported: true,
			InactiveGroupCounts:     map[string]int64{},
			BotIdentities:           map[string]DingTalkGroupBotResponse{},
		})
		return
	}
	h.listDingTalkGroups(w, r, agent.WorkspaceID, agentID, nil)
}

func (h *Handler) listDingTalkGroups(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID pgtype.UUID,
	agentID string,
	visibleAgentIDs map[string]struct{},
) {
	filterByAgent := agentID != ""
	var agentUUID pgtype.UUID
	if filterByAgent {
		agentUUID = parseUUID(agentID)
	}
	activeSince := pgtype.Timestamptz{Time: time.Now().UTC().Add(-dingTalkActiveGroupWindow), Valid: true}
	activity := strings.TrimSpace(r.URL.Query().Get("activity"))
	if activity != "" && activity != "inactive" {
		writeError(w, http.StatusBadRequest, "activity must be inactive when provided")
		return
	}
	includeInactive := activity == "inactive"
	installationID := strings.TrimSpace(r.URL.Query().Get("installation_id"))
	var installationUUID pgtype.UUID
	if includeInactive {
		if installationID == "" {
			writeError(w, http.StatusBadRequest, "installation_id is required for inactive groups")
			return
		}
		var ok bool
		installationUUID, ok = parseUUIDOrBadRequest(w, installationID, "installation_id")
		if !ok {
			return
		}
	}
	pageOffset := 0
	pageLimit := 0
	if includeInactive {
		pageLimit = dingTalkInactivePageSize
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > dingTalkInactiveMaxPage {
				writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
				return
			}
			pageLimit = parsed
		}
		if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				writeError(w, http.StatusBadRequest, "offset must be a non-negative integer")
				return
			}
			pageOffset = parsed
		}
	}
	mayListPresence := true
	if includeInactive {
		var err error
		mayListPresence, err = h.mayListInactiveDingTalkInstallation(
			r.Context(), workspaceID, installationUUID, filterByAgent, agentUUID, visibleAgentIDs,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to authorize dingtalk group installation")
			return
		}
	}
	rows := []db.ListDingTalkGroupPresencesByWorkspaceRow{}
	if mayListPresence {
		var err error
		rows, err = h.Queries.ListDingTalkGroupPresencesByWorkspace(r.Context(), db.ListDingTalkGroupPresencesByWorkspaceParams{
			WorkspaceID:          workspaceID,
			FilterByAgent:        filterByAgent,
			AgentID:              agentUUID,
			FilterByInstallation: includeInactive,
			FilterInstallationID: installationUUID,
			IncludeInactive:      includeInactive,
			ActiveSince:          activeSince,
			PageOffset:           int32(pageOffset),
			PageLimit:            int32(pageLimit + boolToInt(includeInactive)),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list dingtalk groups")
			return
		}
	}
	var nextOffset *int
	if includeInactive && len(rows) > pageLimit {
		rows = rows[:pageLimit]
		next := pageOffset + pageLimit
		nextOffset = &next
	}
	inactiveRows, err := h.Queries.CountInactiveDingTalkGroupPresencesByWorkspace(r.Context(), db.CountInactiveDingTalkGroupPresencesByWorkspaceParams{
		WorkspaceID:   workspaceID,
		ActiveSince:   activeSince,
		FilterByAgent: filterByAgent,
		AgentID:       agentUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count inactive dingtalk groups")
		return
	}
	inactiveCounts := make(map[string]int64, len(inactiveRows))
	for _, row := range inactiveRows {
		rowAgentID := uuidToString(row.AgentID)
		if visibleAgentIDs != nil {
			if _, visible := visibleAgentIDs[rowAgentID]; !visible {
				continue
			}
		}
		inactiveCounts[uuidToString(row.InstallationID)] = row.GroupCount
	}
	identityRows, err := h.Queries.ListDingTalkBotIdentitiesByWorkspace(r.Context(), db.ListDingTalkBotIdentitiesByWorkspaceParams{
		WorkspaceID:   workspaceID,
		FilterByAgent: filterByAgent,
		AgentID:       agentUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dingtalk bot identities")
		return
	}
	botIdentities := make(map[string]DingTalkGroupBotResponse, len(identityRows))
	for _, row := range identityRows {
		rowAgentID := uuidToString(row.AgentID)
		if visibleAgentIDs != nil {
			if _, visible := visibleAgentIDs[rowAgentID]; !visible {
				continue
			}
		}
		installationID := uuidToString(row.InstallationID)
		botIdentities[installationID] = DingTalkGroupBotResponse{
			InstallationID:   installationID,
			AgentID:          rowAgentID,
			BotName:          row.BotName,
			BotIdentityIssue: row.BotIdentityIssue,
		}
	}

	groupIndexByID := make(map[string]int, len(rows))
	groups := make([]DingTalkGroupResponse, 0, len(rows))
	for _, row := range rows {
		rowAgentID := uuidToString(row.AgentID)
		if agentID != "" && rowAgentID != agentID {
			continue
		}
		if visibleAgentIDs != nil {
			if _, visible := visibleAgentIDs[rowAgentID]; !visible {
				continue
			}
		}
		groupIndex, exists := groupIndexByID[row.ConversationID]
		if !exists {
			groups = append(groups, DingTalkGroupResponse{
				ConversationID:    row.ConversationID,
				ConversationTitle: row.ConversationTitle,
				Bots:              []DingTalkGroupBotResponse{},
			})
			groupIndex = len(groups) - 1
			groupIndexByID[row.ConversationID] = groupIndex
		} else if groups[groupIndex].ConversationTitle == "" && row.ConversationTitle != "" {
			groups[groupIndex].ConversationTitle = row.ConversationTitle
		}
		lastActiveAt := ""
		if row.LastActiveAt.Valid {
			lastActiveAt = row.LastActiveAt.Time.UTC().Format(time.RFC3339)
		}
		groups[groupIndex].Bots = append(groups[groupIndex].Bots, DingTalkGroupBotResponse{
			InstallationID:   uuidToString(row.InstallationID),
			AgentID:          rowAgentID,
			BotName:          row.BotName,
			BotIdentityIssue: row.BotIdentityIssue,
			LastActiveAt:     lastActiveAt,
			MentionCount:     row.MentionCount,
		})
	}
	for index := range groups {
		sort.SliceStable(groups[index].Bots, func(i, j int) bool {
			return groups[index].Bots[i].InstallationID < groups[index].Bots[j].InstallationID
		})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left := groups[i].ConversationTitle
		right := groups[j].ConversationTitle
		if left == right {
			return groups[i].ConversationID < groups[j].ConversationID
		}
		if left == "" {
			return false
		}
		if right == "" {
			return true
		}
		return left < right
	})
	writeJSON(w, http.StatusOK, ListDingTalkGroupsResponse{
		Groups:                  groups,
		GroupDiscoverySupported: true,
		InactiveGroupCounts:     inactiveCounts,
		BotIdentities:           botIdentities,
		NextOffset:              nextOffset,
	})
}

// mayListInactiveDingTalkInstallation resolves the requested installation at
// the server boundary before pagination can derive a cursor from its rows.
// Missing, inactive, cross-workspace, and inaccessible installations all return
// false so callers cannot distinguish them through group data or next_offset.
func (h *Handler) mayListInactiveDingTalkInstallation(
	ctx context.Context,
	workspaceID pgtype.UUID,
	installationID pgtype.UUID,
	filterByAgent bool,
	agentID pgtype.UUID,
	visibleAgentIDs map[string]struct{},
) (bool, error) {
	installation, err := h.Queries.GetChannelInstallationInWorkspace(ctx, db.GetChannelInstallationInWorkspaceParams{
		ID:          installationID,
		WorkspaceID: workspaceID,
		ChannelType: string(dingtalk.TypeDingTalk),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if installation.Status != "active" {
		return false, nil
	}
	if filterByAgent {
		return installation.AgentID == agentID, nil
	}
	if visibleAgentIDs == nil {
		return true, nil
	}
	_, visible := visibleAgentIDs[uuidToString(installation.AgentID)]
	return visible, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// ForgetDingTalkGroup removes one observation from the Settings inventory.
// Sessions and message history are retained. A later successfully addressed
// message from the same group observes it again automatically.
func (h *Handler) ForgetDingTalkGroup(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "dingtalk group not found", "owner", "admin"); !ok {
		return
	}
	installationUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	conversationID, err := url.PathUnescape(chi.URLParam(r, "conversationId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid conversation id")
		return
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		writeError(w, http.StatusBadRequest, "conversation id is required")
		return
	}
	if _, err = h.Queries.ForgetDingTalkGroupPresence(r.Context(), db.ForgetDingTalkGroupPresenceParams{
		WorkspaceID:    wsUUID,
		InstallationID: installationUUID,
		ConversationID: conversationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "dingtalk group not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to forget dingtalk group")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegisterDingTalkBYORequest is the body for a bring-your-own-app install: the
// two credentials the user pasted from their own DingTalk Stream-mode robot.
type RegisterDingTalkBYORequest struct {
	ClientID     string `json:"client_id"`     // AppKey
	ClientSecret string `json:"client_secret"` // AppSecret
}

// RegisterDingTalkBYO (POST /api/workspaces/{id}/dingtalk/install/byo?agent_id=…)
// installs a user-supplied ("bring your own") DingTalk robot for an agent, so
// several agents can each have their own bot identity in the SAME DingTalk
// organization. The router requires workspace membership; this handler then
// authorizes the target agent's owner or a workspace owner/admin. Like Slack's
// BYO path this needs only the at-rest key configured (DingTalkInstall != nil).
func (h *Handler) RegisterDingTalkBYO(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk integration not enabled")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	agentIDStr := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if agentIDStr == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, agentIDStr, "agent_id")
	if !ok {
		return
	}
	// Resolve and authorize the target agent at the boundary so a wrong agent_id
	// is a clear 404 and an unrelated member cannot connect a bot to it.
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	initiatorUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var body RegisterDingTalkBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.DingTalkInstall.RegisterBYO(r.Context(), dingtalk.RegisterBYOParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		AppKey:      body.ClientID,
		AppSecret:   body.ClientSecret,
	})
	if err != nil {
		switch {
		case errors.Is(err, dingtalk.ErrInvalidAppKey), errors.Is(err, dingtalk.ErrInvalidAppSecret):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, dingtalk.ErrRobotOwnedBySameWorkspace):
			writeError(w, http.StatusConflict, "this DingTalk robot is already connected to another agent in this workspace — disconnect it there first, then connect it here")
		case errors.Is(err, dingtalk.ErrRobotOwnedByArchivedAgent):
			writeError(w, http.StatusConflict, "this DingTalk robot is connected to an archived agent in this workspace — restore that agent, or disconnect its robot, before connecting it here")
		case errors.Is(err, dingtalk.ErrRobotOwnedByAnotherWorkspace):
			writeError(w, http.StatusConflict, "this DingTalk robot is already connected to a different Multica workspace — disconnect it there before connecting it here")
		case errors.Is(err, dingtalk.ErrCredentialValidation):
			// The access-token mint rejected the pasted credentials (a user error),
			// so guide the user to recheck them.
			writeError(w, http.StatusBadRequest, "could not verify the DingTalk credentials — check the AppKey (client id) and AppSecret (client secret), and that the robot is a Stream-mode robot in your organization")
		default:
			// Encrypt / persist / unexpected failures are server-side, not the
			// user's credentials — surface a 500 instead of misreporting them as bad
			// credentials.
			writeError(w, http.StatusInternalServerError, "could not connect the DingTalk robot")
		}
		return
	}
	// Broadcast so every open client (Settings, Agent Integrations, other tabs)
	// invalidates its installations query and shows the new bot — matching the
	// revoke event and Slack's install semantics.
	h.publishDingTalkInstallationCreated(row, userID)
	writeJSON(w, http.StatusOK, dingtalkInstallationToResponse(row))
}

// publishDingTalkInstallationCreated emits dingtalk_installation:created for a
// newly connected bot. The realtime layer fans it out to the workspace; the web
// app listens on dingtalk_installation:* to invalidate the installations query.
func (h *Handler) publishDingTalkInstallationCreated(row db.ChannelInstallation, actorID string) {
	h.publish(protocol.EventDingTalkInstallationCreated, uuidToString(row.WorkspaceID), "user", actorID, map[string]any{
		"id": uuidToString(row.ID),
	})
}

// RevokeDingTalkInstallation (DELETE /api/workspaces/{id}/dingtalk/installations/{installationId})
// flips status to 'revoked'. The router requires workspace membership; this
// handler authorizes the bound agent's owner or a workspace owner/admin. The row
// is preserved for audit; a re-install (re-pasting the robot's credentials)
// flips status back to 'active'. An orphaned installation falls back to
// workspace owner/admin-only cleanup because there is no agent owner to resolve.
func (h *Handler) RevokeDingTalkInstallation(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	instUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "installationId"), "installation id")
	if !ok {
		return
	}
	// Workspace-scoped lookup so one workspace cannot revoke another's
	// installation by guessing the UUID.
	inst, err := h.DingTalkInstall.GetInWorkspace(r.Context(), instUUID, wsUUID)
	if err != nil {
		if errors.Is(err, dingtalk.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "dingtalk installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	agent, agentErr := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          inst.AgentID,
		WorkspaceID: wsUUID,
	})
	if agentErr != nil {
		if _, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "dingtalk installation not found", "owner", "admin"); !ok {
			return
		}
	} else if !h.canManageAgent(w, r, agent) {
		return
	}
	if err := h.DingTalkInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventDingTalkInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemDingTalkBindingTokenRequest carries the raw token the user clicked
// through from the bot's "link your account" prompt.
type RedeemDingTalkBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemDingTalkBindingTokenResponse echoes the bound workspace/installation/user
// so the frontend can confirm without a second fetch.
type RedeemDingTalkBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	DingTalkUserID string `json:"dingtalk_user_id"`
}

// RedeemDingTalkBindingToken (POST /api/dingtalk/binding/redeem) binds the
// DingTalk user id carried by the bearer token to the logged-in Multica user.
// The redeemer's identity comes from the session, while token possession proves
// control of the link delivered to that DingTalk account. Failure modes map to
// distinct status codes:
//   - 410 Gone:      token unknown / consumed / expired
//   - 409 Conflict:  this DingTalk id is already bound to a different user
//   - 403 Forbidden: redeemer is not a workspace member
func (h *Handler) RedeemDingTalkBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "dingtalk integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemDingTalkBindingTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	redeemed, err := h.DingTalkBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, dingtalk.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, dingtalk.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this DingTalk account is already bound to a different Multica user")
		case errors.Is(err, dingtalk.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	h.publish(protocol.EventDingTalkAccountBindingUpdated, uuidToString(redeemed.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(redeemed.InstallationID),
	})
	writeJSON(w, http.StatusOK, RedeemDingTalkBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		DingTalkUserID: redeemed.DingTalkUserID,
	})
}
