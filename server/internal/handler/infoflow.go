package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/infoflow"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// InfoFlowInstallationResponse is the wire shape for an InfoFlow installation
// row. The encrypted AppSecret in config is INTENTIONALLY absent — it is
// server-internal (only the outbound sender decrypts it). WS lease columns are
// runtime state, not API surface, so they are omitted too.
//
// AppKey is the robot identifier the user pasted from the InfoFlow console. It
// is not a secret (only the sealed AppSecret has to stay server-side), and it is
// the only name the UI can show for an installation: InfoFlow exposes no rename
// API to persist a display name. `bot_name` is therefore deliberately absent —
// an empty field would only make the frontend render a name that never arrives.
type InfoFlowInstallationResponse struct {
	ID                   string   `json:"id"`
	WorkspaceID          string   `json:"workspace_id"`
	AgentID              string   `json:"agent_id"`
	AppKey               string   `json:"app_key"`
	InstallerUserID      string   `json:"installer_user_id"`
	Status               string   `json:"status"`
	InstalledAt          string   `json:"installed_at"`
	CreatedAt            string   `json:"created_at"`
	UpdatedAt            string   `json:"updated_at"`
	AgentAvailable       bool     `json:"agent_available"`
	BoundInfoFlowUserIDs []string `json:"bound_infoflow_user_ids,omitempty"`
}

func infoflowInstallationToResponse(row db.ChannelInstallation) InfoFlowInstallationResponse {
	return InfoFlowInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		AppKey:          infoflow.AppKeyFromConfig(row.Config),
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		AgentAvailable:  true,
	}
}

// ListInfoFlowInstallations (GET /api/workspaces/{id}/infoflow/installations)
// is member-visible so the Integrations tab renders for non-admins. Response
// flags mirror Slack / DingTalk:
//   - configured: at-rest encryption key is set (InfoFlowInstall != nil).
//   - install_supported: kept for the management UI; true whenever configured,
//     since a BYO install needs only the at-rest key.
func (h *Handler) ListInfoFlowInstallations(w http.ResponseWriter, r *http.Request) {
	if h.InfoFlowInstall == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations":     []InfoFlowInstallationResponse{},
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
	availableAgentIDs, visibleAgentIDs, loaded := h.agentVisibilityForChannel(
		r.Context(), wsUUID, userID, member,
	)
	if !loaded {
		writeError(w, http.StatusInternalServerError, "failed to resolve infoflow installation visibility")
		return
	}
	rows, err := h.InfoFlowInstall.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list infoflow installations")
		return
	}
	bindingsByInstallation := map[pgtype.UUID][]string{}
	if roleAllowed(member.Role, "owner", "admin") {
		userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
		if !ok {
			return
		}
		bindings, err := h.Queries.ListChannelUserBindingsForMember(r.Context(), db.ListChannelUserBindingsForMemberParams{
			WorkspaceID:   wsUUID,
			MulticaUserID: userUUID,
			ChannelType:   string(infoflow.TypeInfoFlow),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list infoflow user bindings")
			return
		}
		for _, binding := range bindings {
			bindingsByInstallation[binding.InstallationID] = append(
				bindingsByInstallation[binding.InstallationID],
				binding.ChannelUserID,
			)
		}
	}
	canViewAll := roleAllowed(member.Role, "owner", "admin")
	out := make([]InfoFlowInstallationResponse, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if !canViewAll {
			if _, canView := visibleAgentIDs[agentID]; !canView {
				continue
			}
		}
		response := infoflowInstallationToResponse(row)
		_, response.AgentAvailable = availableAgentIDs[agentID]
		response.BoundInfoFlowUserIDs = bindingsByInstallation[row.ID]
		out = append(out, response)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations":     out,
		"configured":        true,
		"install_supported": true,
	})
}

// RegisterInfoFlowBYORequest is the body for a bring-your-own-app install: the
// two credentials the user pasted from their own InfoFlow robot.
type RegisterInfoFlowBYORequest struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
}

// RegisterInfoFlowBYO (POST /api/workspaces/{id}/infoflow/install/byo?agent_id=…)
// installs a user-supplied ("bring your own") InfoFlow robot for an agent. The
// router requires workspace membership; this handler then authorizes the target
// agent's owner or a workspace owner/admin.
func (h *Handler) RegisterInfoFlowBYO(w http.ResponseWriter, r *http.Request) {
	if h.InfoFlowInstall == nil {
		writeFeatureDisabled(w, "infoflow_not_configured", "infoflow integration not enabled")
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
	var body RegisterInfoFlowBYORequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	row, err := h.InfoFlowInstall.RegisterBYO(r.Context(), infoflow.RegisterBYOParams{
		WorkspaceID: wsUUID,
		AgentID:     agentUUID,
		InitiatorID: initiatorUUID,
		AppKey:      body.AppKey,
		AppSecret:   body.AppSecret,
	})
	if err != nil {
		writeInfoFlowInstallError(w, err)
		return
	}
	h.publishInfoFlowInstallationCreated(row, userID)
	writeJSON(w, http.StatusOK, infoflowInstallationToResponse(row))
}

// writeInfoFlowInstallError maps every error class the BYO install can produce
// onto the status + stable `code` the settings dialog switches on. The sentence
// stays for clients too old to know the codes, so it is deliberately specific to
// the one outcome each row describes — see infoflow_install_error_matrix_test.go
// for the contract, including that no two rows share a code.
func writeInfoFlowInstallError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, infoflow.ErrInvalidAppKey), errors.Is(err, infoflow.ErrInvalidAppSecret):
		writeErrorCode(w, http.StatusBadRequest, "infoflow_install_rejected", err.Error())
	case errors.Is(err, infoflow.ErrAppOwnedBySameWorkspace):
		writeErrorCode(w, http.StatusConflict, "infoflow_app_owned_by_same_workspace",
			"this InfoFlow app is already connected to another agent in this workspace — disconnect it there first, then connect it here")
	case errors.Is(err, infoflow.ErrAppOwnedByArchivedAgent):
		writeErrorCode(w, http.StatusConflict, "infoflow_app_owned_by_archived_agent",
			"this InfoFlow app is connected to an archived agent in this workspace — restore that agent, or disconnect its app, before connecting it here")
	case errors.Is(err, infoflow.ErrAppOwnedByAnotherWorkspace):
		writeErrorCode(w, http.StatusConflict, "infoflow_app_owned_by_another_workspace",
			"this InfoFlow app is already connected to a different Multica workspace — disconnect it there before connecting it here")
	default:
		writeErrorCode(w, http.StatusInternalServerError, "infoflow_install_failed",
			"could not connect the InfoFlow bot — something went wrong on our side. Your credentials were not changed; try again, and contact support if it keeps failing")
	}
}

func (h *Handler) publishInfoFlowInstallationCreated(row db.ChannelInstallation, actorID string) {
	h.publish(protocol.EventInfoFlowInstallationCreated, uuidToString(row.WorkspaceID), "user", actorID, map[string]any{
		"id": uuidToString(row.ID),
	})
}

// RevokeInfoFlowInstallation (DELETE /api/workspaces/{id}/infoflow/installations/{installationId})
// flips status to 'revoked'. The router requires workspace membership; this
// handler authorizes the bound agent's owner or a workspace owner/admin. The row
// is preserved for audit; a re-install flips status back to 'active'.
func (h *Handler) RevokeInfoFlowInstallation(w http.ResponseWriter, r *http.Request) {
	if h.InfoFlowInstall == nil {
		writeFeatureDisabled(w, "infoflow_not_configured", "infoflow integration not configured")
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
	inst, err := h.InfoFlowInstall.GetInWorkspace(r.Context(), instUUID, wsUUID)
	if err != nil {
		if errors.Is(err, infoflow.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "infoflow installation not found")
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
		if _, ok := h.requireWorkspaceRole(w, r, uuidToString(wsUUID), "infoflow installation not found", "owner", "admin"); !ok {
			return
		}
	} else if !h.canManageAgent(w, r, agent) {
		return
	}
	if err := h.InfoFlowInstall.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventInfoFlowInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

// RedeemInfoFlowBindingTokenRequest carries the raw token the user clicked
// through from the bot's "link your account" prompt.
type RedeemInfoFlowBindingTokenRequest struct {
	Token string `json:"token"`
}

// RedeemInfoFlowBindingTokenResponse echoes the bound workspace/installation/user
// so the frontend can confirm without a second fetch.
type RedeemInfoFlowBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	InfoFlowUserID string `json:"infoflow_user_id"`
}

// RedeemInfoFlowBindingToken (POST /api/infoflow/binding/redeem) binds the
// InfoFlow user id carried by the bearer token to the logged-in Multica user.
// Failure modes map to distinct status codes:
//   - 410 Gone:      token unknown / consumed / expired
//   - 409 Conflict:  this InfoFlow id is already bound to a different user
//   - 403 Forbidden: redeemer is not a workspace member
func (h *Handler) RedeemInfoFlowBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.InfoFlowBindingTokens == nil {
		writeFeatureDisabled(w, "infoflow_not_configured", "infoflow integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemInfoFlowBindingTokenRequest
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
	redeemed, err := h.InfoFlowBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, infoflow.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, infoflow.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this InfoFlow account is already bound to a different Multica user")
		case errors.Is(err, infoflow.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	h.publish(protocol.EventInfoFlowAccountBindingUpdated, uuidToString(redeemed.WorkspaceID), "user", userID, map[string]any{
		"id": uuidToString(redeemed.InstallationID),
	})
	writeJSON(w, http.StatusOK, RedeemInfoFlowBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		InfoFlowUserID: redeemed.InfoFlowUserID,
	})
}
