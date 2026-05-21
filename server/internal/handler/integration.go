package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type IntegrationResponse struct {
	ID             string  `json:"id"`
	WorkspaceID    string  `json:"workspace_id"`
	Provider       string  `json:"provider"`
	Enabled        bool    `json:"enabled"`
	Config         any     `json:"config"`
	DefaultAgentID *string `json:"default_agent_id"`
	WebhookSecret  *string `json:"webhook_secret,omitempty"`
	WebhookURL     string  `json:"webhook_url,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func integrationToResponse(i db.WorkspaceIntegration) IntegrationResponse {
	var config any
	json.Unmarshal(i.Config, &config)

	return IntegrationResponse{
		ID:             uuidToString(i.ID),
		WorkspaceID:    uuidToString(i.WorkspaceID),
		Provider:       i.Provider,
		Enabled:        i.Enabled,
		Config:         config,
		DefaultAgentID: uuidToPtr(i.DefaultAgentID),
		WebhookSecret:  textToPtr(i.WebhookSecret),
		CreatedAt:      timestampToString(i.CreatedAt),
		UpdatedAt:      timestampToString(i.UpdatedAt),
	}
}

func (h *Handler) ListIntegrations(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	integrations, err := h.Queries.ListWorkspaceIntegrations(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list integrations")
		return
	}

	resp := make([]IntegrationResponse, len(integrations))
	for i, integ := range integrations {
		resp[i] = integrationToResponse(integ)
	}
	writeJSON(w, http.StatusOK, resp)
}

type CreateIntegrationRequest struct {
	Provider       string `json:"provider"`
	Enabled        *bool  `json:"enabled"`
	Config         any    `json:"config"`
	DefaultAgentID *string `json:"default_agent_id"`
}

func (h *Handler) CreateIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var req CreateIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Provider != "linear" && req.Provider != "github" {
		writeError(w, http.StatusBadRequest, "provider must be 'linear' or 'github'")
		return
	}

	configBytes, _ := json.Marshal(req.Config)
	if req.Config == nil {
		configBytes = []byte("{}")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	var agentID pgtype.UUID
	if req.DefaultAgentID != nil {
		agentID = parseUUID(*req.DefaultAgentID)
	}

	// Generate a webhook secret for signature verification.
	secret := generateWebhookSecret()

	integration, err := h.Queries.CreateWorkspaceIntegration(r.Context(), db.CreateWorkspaceIntegrationParams{
		WorkspaceID:    parseUUID(workspaceID),
		Provider:       req.Provider,
		Enabled:        enabled,
		Config:         configBytes,
		DefaultAgentID: agentID,
		WebhookSecret:  pgtype.Text{String: secret, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "integration already exists for this provider")
			return
		}
		slog.Warn("create integration failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create integration")
		return
	}

	writeJSON(w, http.StatusCreated, integrationToResponse(integration))
}

func (h *Handler) GetIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	integration, err := h.Queries.GetWorkspaceIntegration(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusNotFound, "integration not found")
		return
	}
	writeJSON(w, http.StatusOK, integrationToResponse(integration))
}

type UpdateIntegrationRequest struct {
	Enabled        *bool   `json:"enabled"`
	Config         any     `json:"config"`
	DefaultAgentID *string `json:"default_agent_id"`
	WebhookSecret  *string `json:"webhook_secret"`
}

func (h *Handler) UpdateIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req UpdateIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var configBytes []byte
	if req.Config != nil {
		configBytes, _ = json.Marshal(req.Config)
	}

	var enabled pgtype.Bool
	if req.Enabled != nil {
		enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	var agentID pgtype.UUID
	if req.DefaultAgentID != nil {
		agentID = parseUUID(*req.DefaultAgentID)
	}

	var webhookSecret pgtype.Text
	if req.WebhookSecret != nil {
		webhookSecret = pgtype.Text{String: *req.WebhookSecret, Valid: true}
	}

	integration, err := h.Queries.UpdateWorkspaceIntegration(r.Context(), db.UpdateWorkspaceIntegrationParams{
		ID:             parseUUID(id),
		Enabled:        enabled,
		Config:         configBytes,
		DefaultAgentID: agentID,
		WebhookSecret:  webhookSecret,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update integration")
		return
	}

	writeJSON(w, http.StatusOK, integrationToResponse(integration))
}

func (h *Handler) DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.Queries.DeleteWorkspaceIntegration(r.Context(), parseUUID(id))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete integration")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListExternalLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	links, err := h.Queries.ListExternalIssueLinksByWorkspace(r.Context(), db.ListExternalIssueLinksByWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       100,
		Offset:      0,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list links")
		return
	}

	type LinkResponse struct {
		ID                 string  `json:"id"`
		IssueID            string  `json:"issue_id"`
		Provider           string  `json:"provider"`
		ExternalID         string  `json:"external_id"`
		ExternalIdentifier *string `json:"external_identifier"`
		ExternalURL        *string `json:"external_url"`
		SyncDirection      string  `json:"sync_direction"`
		CreatedAt          string  `json:"created_at"`
	}

	resp := make([]LinkResponse, len(links))
	for i, l := range links {
		resp[i] = LinkResponse{
			ID:                 uuidToString(l.ID),
			IssueID:            uuidToString(l.IssueID),
			Provider:           l.Provider,
			ExternalID:         l.ExternalID,
			ExternalIdentifier: textToPtr(l.ExternalIdentifier),
			ExternalURL:        textToPtr(l.ExternalUrl),
			SyncDirection:      l.SyncDirection,
			CreatedAt:          timestampToString(l.CreatedAt),
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func generateWebhookSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
