package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DingTalkInstallationResponse is the wire shape for a DingTalk installation
// row. The encrypted AppSecret in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it). WS lease columns are
// runtime state, not API surface, so they are omitted too.
type DingTalkInstallationResponse struct {
	ID              string `json:"id"`
	WorkspaceID     string `json:"workspace_id"`
	AgentID         string `json:"agent_id"`
	InstallerUserID string `json:"installer_user_id"`
	Status          string `json:"status"`
	InstalledAt     string `json:"installed_at"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// DingTalkGroupRouteResponse is one group discovered from a Stream callback.
// The conversation id is returned for diagnostics, but clients should use the
// stable route id for updates. Credentials and callback payloads are never
// exposed.
type DingTalkGroupRouteResponse struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	InstallationID    string `json:"installation_id"`
	ConversationID    string `json:"conversation_id"`
	ConversationTitle string `json:"conversation_title"`
	AgentID           string `json:"agent_id"`
	DiscoveredAt      string `json:"discovered_at"`
	UpdatedAt         string `json:"updated_at"`
}

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
	}
}

func dingtalkGroupRouteToResponse(row db.DingtalkGroupRoute) DingTalkGroupRouteResponse {
	return DingTalkGroupRouteResponse{
		ID:                uuidToString(row.ID),
		WorkspaceID:       uuidToString(row.WorkspaceID),
		InstallationID:    uuidToString(row.InstallationID),
		ConversationID:    row.ConversationID,
		ConversationTitle: row.ConversationTitle,
		AgentID:           uuidToString(row.AgentID),
		DiscoveredAt:      row.DiscoveredAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:         row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
}

// ListDingTalkGroupRoutes (GET /api/workspaces/{id}/dingtalk/group-routes)
// lists groups the robot has observed. It is member-visible like installation
// listing; only owner/admin callers may reassign a route.
func (h *Handler) ListDingTalkGroupRoutes(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{"routes": []DingTalkGroupRouteResponse{}})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListDingTalkGroupRoutesByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dingtalk group routes")
		return
	}
	out := make([]DingTalkGroupRouteResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, dingtalkGroupRouteToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": out})
}

// UpdateDingTalkGroupRouteRequest selects the one agent that handles messages
// from a discovered DingTalk group.
type UpdateDingTalkGroupRouteRequest struct {
	AgentID string `json:"agent_id"`
}

// UpdateDingTalkGroupRoute (PATCH
// /api/workspaces/{id}/dingtalk/group-routes/{routeId}) reassigns a discovered
// group. The query atomically removes the old chat binding when the agent
// changes, preventing the new agent from inheriting the old transcript.
func (h *Handler) UpdateDingTalkGroupRoute(w http.ResponseWriter, r *http.Request) {
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
	routeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "routeId"), "route id")
	if !ok {
		return
	}
	var body UpdateDingTalkGroupRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(body.AgentID), "agent_id")
	if !ok {
		return
	}
	// Fail closed before agent diagnosis so a retained route behind a revoked
	// installation is always an absent PATCH resource. The reassignment query
	// repeats this check under an installation lock to close the revoke race.
	if _, err := h.Queries.GetDingTalkGroupRouteInWorkspace(r.Context(), db.GetDingTalkGroupRouteInWorkspaceParams{
		ID: routeUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "dingtalk group route not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load dingtalk group route")
		return
	}
	// Load without a kind filter so the explicit non-user validation below is
	// reachable. Keep the workspace boundary fail-closed in the handler: an
	// Agent from another workspace remains indistinguishable from a missing ID.
	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "agent not found in this workspace")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load agent")
		return
	}
	if agent.WorkspaceID != wsUUID {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	if agent.Kind != "user" {
		writeError(w, http.StatusBadRequest, "only user agents can handle a DingTalk group")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusConflict, "an archived agent cannot handle a DingTalk group")
		return
	}
	row, err := h.Queries.ReassignDingTalkGroupRoute(r.Context(), db.ReassignDingTalkGroupRouteParams{
		ID: routeUUID, WorkspaceID: wsUUID, AgentID: agentUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Diagnose the route boundary first. Revoke-first makes the locked
			// reassignment return no rows; the retained route must still look
			// absent and must not be mislabeled as an agent lifecycle conflict.
			if _, routeErr := h.Queries.GetDingTalkGroupRouteInWorkspace(r.Context(), db.GetDingTalkGroupRouteInWorkspaceParams{
				ID: routeUUID, WorkspaceID: wsUUID,
			}); errors.Is(routeErr, pgx.ErrNoRows) {
				writeError(w, http.StatusNotFound, "dingtalk group route not found")
				return
			} else if routeErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to load dingtalk group route")
				return
			}
			// The query re-checks and locks the target agent so a concurrent
			// archive cannot land an assignment from a stale active snapshot.
			// Distinguish that lifecycle conflict from a genuinely missing route.
			currentAgent, agentErr := h.Queries.GetAgent(r.Context(), agentUUID)
			if agentErr == nil && currentAgent.WorkspaceID == wsUUID && currentAgent.Kind == "user" && currentAgent.ArchivedAt.Valid {
				writeError(w, http.StatusConflict, "an archived agent cannot handle a DingTalk group")
				return
			}
			if agentErr != nil && !errors.Is(agentErr, pgx.ErrNoRows) {
				writeError(w, http.StatusInternalServerError, "failed to load agent")
				return
			}
			writeError(w, http.StatusConflict, "the selected agent is no longer available")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update dingtalk group route")
		return
	}
	h.publish(protocol.EventDingTalkGroupRouteUpdated, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(row.ID),
	})
	writeJSON(w, http.StatusOK, dingtalkGroupRouteToResponse(db.DingtalkGroupRoute{
		ID: row.ID, WorkspaceID: row.WorkspaceID, InstallationID: row.InstallationID,
		ConversationID: row.ConversationID, ConversationTitle: row.ConversationTitle,
		AgentID: row.AgentID, DiscoveredAt: row.DiscoveredAt, UpdatedAt: row.UpdatedAt,
	}))
}

// ListDingTalkInstallations (GET /api/workspaces/{id}/dingtalk/installations) is
// member-visible so the Integrations tab renders for non-admins. Response flags
// mirror Slack:
//   - configured: at-rest encryption key is set (DingTalkInstall != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a BYO install needs only the at-rest key.
//   - group_routing_supported: explicit capability gate for version-skewed
//     Web/Desktop clients. Older backends omit it, so newer clients must require
//     an exact true before calling the group-routes endpoints.
func (h *Handler) ListDingTalkInstallations(w http.ResponseWriter, r *http.Request) {
	if h.DingTalkInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":           []DingTalkInstallationResponse{},
			"configured":              false,
			"install_supported":       false,
			"group_routing_supported": false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.DingTalkInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list dingtalk installations")
		return
	}
	out := make([]DingTalkInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, dingtalkInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":           out,
		"configured":              true,
		"install_supported":       true,
		"group_routing_supported": true,
	})
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
// organization. Admin-only at the router. Like Slack's BYO path this needs only
// the at-rest key configured (DingTalkInstall != nil).
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
	// Ownership pre-check at the boundary so a wrong agent_id is a clear 404.
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
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
// flips status to 'revoked'. Admin-only at the router. The row is preserved for
// audit; a re-install (re-pasting the robot's credentials) flips status back to
// 'active'.
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
	if _, err := h.DingTalkInstall.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, dingtalk.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "dingtalk installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
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
	writeJSON(w, http.StatusOK, RedeemDingTalkBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		DingTalkUserID: redeemed.DingTalkUserID,
	})
}
