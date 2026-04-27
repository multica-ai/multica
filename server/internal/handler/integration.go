package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- Response types ----

type WorkspaceIntegrationResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Provider    string `json:"provider"`
	InstanceURL string `json:"instance_url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func workspaceIntegrationToResponse(i db.WorkspaceIntegration) WorkspaceIntegrationResponse {
	return WorkspaceIntegrationResponse{
		ID:          uuidToString(i.ID),
		WorkspaceID: uuidToString(i.WorkspaceID),
		Provider:    i.Provider,
		InstanceURL: i.InstanceUrl,
		CreatedAt:   timestampToString(i.CreatedAt),
		UpdatedAt:   timestampToString(i.UpdatedAt),
	}
}

// UserIntegrationCredentialResponse never exposes the raw API key.
type UserIntegrationCredentialResponse struct {
	Provider string `json:"provider"`
	HasKey   bool   `json:"has_key"`
}

type ProjectIntegrationLinkResponse struct {
	ID                  string  `json:"id"`
	ProjectID           string  `json:"project_id"`
	Provider            string  `json:"provider"`
	ExternalProjectID   string  `json:"external_project_id"`
	ExternalProjectName *string `json:"external_project_name"`
	CreatedAt           string  `json:"created_at"`
}

func projectLinkToResponse(l db.ProjectIntegrationLink) ProjectIntegrationLinkResponse {
	return ProjectIntegrationLinkResponse{
		ID:                  uuidToString(l.ID),
		ProjectID:           uuidToString(l.ProjectID),
		Provider:            l.Provider,
		ExternalProjectID:   l.ExternalProjectID,
		ExternalProjectName: textToPtr(l.ExternalProjectName),
		CreatedAt:           timestampToString(l.CreatedAt),
	}
}

type IssueIntegrationLinkResponse struct {
	ID                 string  `json:"id"`
	IssueID            string  `json:"issue_id"`
	Provider           string  `json:"provider"`
	ExternalIssueID    string  `json:"external_issue_id"`
	ExternalIssueURL   *string `json:"external_issue_url"`
	ExternalIssueTitle *string `json:"external_issue_title"`
	CreatedAt          string  `json:"created_at"`
}

func issueLinkToResponse(l db.IssueIntegrationLink) IssueIntegrationLinkResponse {
	return IssueIntegrationLinkResponse{
		ID:                 uuidToString(l.ID),
		IssueID:            uuidToString(l.IssueID),
		Provider:           l.Provider,
		ExternalIssueID:    l.ExternalIssueID,
		ExternalIssueURL:   textToPtr(l.ExternalIssueUrl),
		ExternalIssueTitle: textToPtr(l.ExternalIssueTitle),
		CreatedAt:          timestampToString(l.CreatedAt),
	}
}

// ---- Workspace integration handlers ----

func (h *Handler) ListWorkspaceIntegrations(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	rows, err := h.Queries.ListWorkspaceIntegrations(r.Context(), parseUUID(workspaceID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list integrations")
		return
	}
	resp := make([]WorkspaceIntegrationResponse, len(rows))
	for i, row := range rows {
		resp[i] = workspaceIntegrationToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"integrations": resp})
}

type UpsertWorkspaceIntegrationRequest struct {
	Provider    string `json:"provider"`
	InstanceURL string `json:"instance_url"`
}

func (h *Handler) UpsertWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	_, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	var req UpsertWorkspaceIntegrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" || req.InstanceURL == "" {
		writeError(w, http.StatusBadRequest, "provider and instance_url are required")
		return
	}

	row, err := h.Queries.UpsertWorkspaceIntegration(r.Context(), db.UpsertWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    req.Provider,
		InstanceUrl: req.InstanceURL,
		Settings:    []byte("{}"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save integration")
		return
	}
	writeJSON(w, http.StatusOK, workspaceIntegrationToResponse(row))
}

func (h *Handler) DeleteWorkspaceIntegration(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	_, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
	if !ok {
		return
	}

	provider := chi.URLParam(r, "provider")
	err := h.Queries.DeleteWorkspaceIntegration(r.Context(), db.DeleteWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    provider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete integration")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- User credential handlers ----

func (h *Handler) GetMyCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	provider := chi.URLParam(r, "provider")

	_, err := h.Queries.GetUserIntegrationCredential(r.Context(), db.GetUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    provider,
	})
	hasKey := err == nil
	writeJSON(w, http.StatusOK, UserIntegrationCredentialResponse{
		Provider: provider,
		HasKey:   hasKey,
	})
}

type UpsertCredentialRequest struct {
	APIKey string `json:"api_key"`
}

func (h *Handler) UpsertMyCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	provider := chi.URLParam(r, "provider")

	var req UpsertCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "api_key is required")
		return
	}

	_, err := h.Queries.UpsertUserIntegrationCredential(r.Context(), db.UpsertUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    provider,
		ApiKey:      req.APIKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save credential")
		return
	}
	writeJSON(w, http.StatusOK, UserIntegrationCredentialResponse{
		Provider: provider,
		HasKey:   true,
	})
}

func (h *Handler) DeleteMyCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	provider := chi.URLParam(r, "provider")

	err := h.Queries.DeleteUserIntegrationCredential(r.Context(), db.DeleteUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    provider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete credential")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Redmine proxy helpers ----

// redmineContext fetches the workspace integration config and the user's API key
// for Redmine, returning an error response and false if either is missing.
func (h *Handler) redmineContext(w http.ResponseWriter, r *http.Request) (instanceURL, apiKey string, ok bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", "", false
	}
	workspaceID := h.resolveWorkspaceID(r)

	integration, err := h.Queries.GetWorkspaceIntegration(r.Context(), db.GetWorkspaceIntegrationParams{
		WorkspaceID: parseUUID(workspaceID),
		Provider:    "redmine",
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "redmine integration not configured for this workspace")
		return "", "", false
	}

	cred, err := h.Queries.GetUserIntegrationCredential(r.Context(), db.GetUserIntegrationCredentialParams{
		WorkspaceID: parseUUID(workspaceID),
		UserID:      parseUUID(userID),
		Provider:    "redmine",
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "redmine API key not configured for your account")
		return "", "", false
	}

	return integration.InstanceUrl, cred.ApiKey, true
}

// ---- Redmine proxy handlers ----

func (h *Handler) ListRedmineProjects(w http.ResponseWriter, r *http.Request) {
	instanceURL, apiKey, ok := h.redmineContext(w, r)
	if !ok {
		return
	}
	projects, err := h.RedmineClient.ListProjects(instanceURL, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("redmine error: %s", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

type CreateRedmineProjectRequest struct {
	Name        string `json:"name"`
	Identifier  string `json:"identifier"`
	Description string `json:"description"`
}

func (h *Handler) CreateRedmineProject(w http.ResponseWriter, r *http.Request) {
	instanceURL, apiKey, ok := h.redmineContext(w, r)
	if !ok {
		return
	}

	var req CreateRedmineProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	project, err := h.RedmineClient.CreateProject(instanceURL, apiKey, service.CreateRedmineProjectReq{
		Name:        req.Name,
		Identifier:  req.Identifier,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("redmine error: %s", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, project)
}

func (h *Handler) ListRedmineIssues(w http.ResponseWriter, r *http.Request) {
	instanceURL, apiKey, ok := h.redmineContext(w, r)
	if !ok {
		return
	}

	projectIDStr := chi.URLParam(r, "projectId")
	projectID, err := strconv.Atoi(projectIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid project_id")
		return
	}

	issues, err := h.RedmineClient.ListIssues(instanceURL, apiKey, projectID)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("redmine error: %s", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

type CreateRedmineIssueRequest struct {
	ProjectID   int    `json:"project_id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
}

func (h *Handler) CreateRedmineIssue(w http.ResponseWriter, r *http.Request) {
	instanceURL, apiKey, ok := h.redmineContext(w, r)
	if !ok {
		return
	}

	var req CreateRedmineIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	issue, err := h.RedmineClient.CreateIssue(instanceURL, apiKey, service.CreateRedmineIssueReq{
		ProjectID:   req.ProjectID,
		Subject:     req.Subject,
		Description: req.Description,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("redmine error: %s", err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, issue)
}

// ---- Project integration link handlers ----

func (h *Handler) ListProjectIntegrationLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "id")

	rows, err := h.Queries.ListProjectIntegrationLinks(r.Context(), db.ListProjectIntegrationLinksParams{
		WorkspaceID: parseUUID(workspaceID),
		ProjectID:   parseUUID(projectID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project integration links")
		return
	}

	resp := make([]ProjectIntegrationLinkResponse, len(rows))
	for i, row := range rows {
		resp[i] = projectLinkToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": resp})
}

type UpsertProjectLinkRequest struct {
	Provider            string  `json:"provider"`
	ExternalProjectID   string  `json:"external_project_id"`
	ExternalProjectName *string `json:"external_project_name"`
}

func (h *Handler) UpsertProjectIntegrationLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "id")

	var req UpsertProjectLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" || req.ExternalProjectID == "" {
		writeError(w, http.StatusBadRequest, "provider and external_project_id are required")
		return
	}

	var nameText pgtype.Text
	if req.ExternalProjectName != nil {
		nameText = pgtype.Text{String: *req.ExternalProjectName, Valid: true}
	}

	row, err := h.Queries.UpsertProjectIntegrationLink(r.Context(), db.UpsertProjectIntegrationLinkParams{
		WorkspaceID:         parseUUID(workspaceID),
		ProjectID:           parseUUID(projectID),
		Provider:            req.Provider,
		ExternalProjectID:   req.ExternalProjectID,
		ExternalProjectName: nameText,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save project integration link")
		return
	}
	writeJSON(w, http.StatusOK, projectLinkToResponse(row))
}

func (h *Handler) DeleteProjectIntegrationLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	projectID := chi.URLParam(r, "id")
	provider := chi.URLParam(r, "provider")

	err := h.Queries.DeleteProjectIntegrationLink(r.Context(), db.DeleteProjectIntegrationLinkParams{
		WorkspaceID: parseUUID(workspaceID),
		ProjectID:   parseUUID(projectID),
		Provider:    provider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project integration link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Issue integration link handlers ----

func (h *Handler) ListIssueIntegrationLinks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	issueID := chi.URLParam(r, "id")

	rows, err := h.Queries.ListIssueIntegrationLinks(r.Context(), db.ListIssueIntegrationLinksParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list issue integration links")
		return
	}

	resp := make([]IssueIntegrationLinkResponse, len(rows))
	for i, row := range rows {
		resp[i] = issueLinkToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": resp})
}

type UpsertIssueLinkRequest struct {
	Provider           string  `json:"provider"`
	ExternalIssueID    string  `json:"external_issue_id"`
	ExternalIssueURL   *string `json:"external_issue_url"`
	ExternalIssueTitle *string `json:"external_issue_title"`
}

func (h *Handler) UpsertIssueIntegrationLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	issueID := chi.URLParam(r, "id")

	var req UpsertIssueLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" || req.ExternalIssueID == "" {
		writeError(w, http.StatusBadRequest, "provider and external_issue_id are required")
		return
	}

	row, err := h.Queries.UpsertIssueIntegrationLink(r.Context(), db.UpsertIssueIntegrationLinkParams{
		WorkspaceID:        parseUUID(workspaceID),
		IssueID:            parseUUID(issueID),
		Provider:           req.Provider,
		ExternalIssueID:    req.ExternalIssueID,
		ExternalIssueUrl:   ptrToText(req.ExternalIssueURL),
		ExternalIssueTitle: ptrToText(req.ExternalIssueTitle),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save issue integration link")
		return
	}
	writeJSON(w, http.StatusOK, issueLinkToResponse(row))
}

func (h *Handler) DeleteIssueIntegrationLink(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	issueID := chi.URLParam(r, "id")
	provider := chi.URLParam(r, "provider")

	err := h.Queries.DeleteIssueIntegrationLink(r.Context(), db.DeleteIssueIntegrationLinkParams{
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     parseUUID(issueID),
		Provider:    provider,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete issue integration link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
