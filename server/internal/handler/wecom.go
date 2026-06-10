package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/integrations/wecom"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type WecomInstallationResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	AgentID           string  `json:"agent_id"`
	BotID             string  `json:"bot_id"`
	CorpID            string  `json:"corp_id"`
	SelfBuildAgentID  *string `json:"self_build_agent_id,omitempty"`
	InstallerUserID   string  `json:"installer_user_id"`
	Status            string  `json:"status"`
	InstalledAt       string  `json:"installed_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

func wecomInstallationToResponse(row db.WecomInstallation) WecomInstallationResponse {
	resp := WecomInstallationResponse{
		ID:              uuidToString(row.ID),
		WorkspaceID:     uuidToString(row.WorkspaceID),
		AgentID:         uuidToString(row.AgentID),
		BotID:           row.BotID,
		CorpID:          row.CorpID,
		InstallerUserID: uuidToString(row.InstallerUserID),
		Status:          row.Status,
		InstalledAt:     row.InstalledAt.Time.UTC().Format(time.RFC3339),
		CreatedAt:       row.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}
	if row.SelfBuildAgentID.Valid {
		v := row.SelfBuildAgentID.String
		resp.SelfBuildAgentID = &v
	}
	return resp
}

func (h *Handler) ListWecomInstallations(w http.ResponseWriter, r *http.Request) {
	if h.WecomInstallations == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"installations": []WecomInstallationResponse{},
			"configured":    false,
		})
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	rows, err := h.WecomInstallations.ListByWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list wecom installations")
		return
	}
	out := make([]WecomInstallationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, wecomInstallationToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"installations": out,
		"configured":    true,
	})
}

type CreateWecomInstallationRequest struct {
	AgentID          string `json:"agent_id"`
	BotID            string `json:"bot_id"`
	BotSecret        string `json:"bot_secret"`
	CorpID           string `json:"corp_id"`
	CorpSecret       string `json:"corp_secret"`
	SelfBuildAgentID string `json:"self_build_agent_id"`
}

func (h *Handler) CreateWecomInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WecomInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not configured")
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
	var req CreateWecomInstallationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
	if !ok {
		return
	}
	installerUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID: agentUUID, WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "agent not found in this workspace")
		return
	}
	row, err := h.WecomInstallations.Upsert(r.Context(), wecom.InstallationParams{
		WorkspaceID:      wsUUID,
		AgentID:          agentUUID,
		BotID:            strings.TrimSpace(req.BotID),
		BotSecret:        strings.TrimSpace(req.BotSecret),
		CorpID:           strings.TrimSpace(req.CorpID),
		CorpSecret:       strings.TrimSpace(req.CorpSecret),
		SelfBuildAgentID: strings.TrimSpace(req.SelfBuildAgentID),
		InstallerUserID:  installerUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.publish(protocol.EventWecomInstallationCreated, uuidToString(wsUUID), "user", userID, map[string]any{
		"id":       uuidToString(row.ID),
		"agent_id": uuidToString(row.AgentID),
	})
	writeJSON(w, http.StatusCreated, wecomInstallationToResponse(row))
}

func (h *Handler) RevokeWecomInstallation(w http.ResponseWriter, r *http.Request) {
	if h.WecomInstallations == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not configured")
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
	if _, err := h.WecomInstallations.GetInWorkspace(r.Context(), instUUID, wsUUID); err != nil {
		if errors.Is(err, wecom.ErrInstallationNotFound) {
			writeError(w, http.StatusNotFound, "wecom installation not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load installation")
		return
	}
	if err := h.WecomInstallations.Revoke(r.Context(), instUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke installation")
		return
	}
	h.publish(protocol.EventWecomInstallationRevoked, uuidToString(wsUUID), "user", userID, map[string]any{
		"id": uuidToString(instUUID),
	})
	w.WriteHeader(http.StatusNoContent)
}

type RedeemWecomBindingTokenRequest struct {
	Token string `json:"token"`
}

type RedeemWecomBindingTokenResponse struct {
	WorkspaceID    string `json:"workspace_id"`
	InstallationID string `json:"installation_id"`
	WecomUserid    string `json:"wecom_userid"`
}

func (h *Handler) RedeemWecomBindingToken(w http.ResponseWriter, r *http.Request) {
	if h.WecomBindingTokens == nil {
		writeError(w, http.StatusServiceUnavailable, "wecom integration not configured")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req RedeemWecomBindingTokenRequest
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
	redeemed, err := h.WecomBindingTokens.RedeemAndBind(r.Context(), req.Token, userUUID)
	if err != nil {
		switch {
		case errors.Is(err, wecom.ErrBindingTokenInvalid):
			writeError(w, http.StatusGone, "binding token invalid or expired")
		case errors.Is(err, wecom.ErrBindingAlreadyAssigned):
			writeError(w, http.StatusConflict, "this WeCom account is already bound to a different Multica user")
		case errors.Is(err, wecom.ErrBindingNotWorkspaceMember):
			writeError(w, http.StatusForbidden, "binding refused (are you a workspace member?)")
		default:
			writeError(w, http.StatusInternalServerError, "failed to redeem token")
		}
		return
	}
	writeJSON(w, http.StatusOK, RedeemWecomBindingTokenResponse{
		WorkspaceID:    uuidToString(redeemed.WorkspaceID),
		InstallationID: uuidToString(redeemed.InstallationID),
		WecomUserid:    string(redeemed.WecomUserid),
	})
}
