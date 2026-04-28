package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Event types for artifact lifecycle. Defined here (not in pkg/protocol/events.go)
// to keep this feature additive and avoid merge conflicts with upstream.
const (
	EventArtifactCreated = "artifact:created"
	EventArtifactUpdated = "artifact:updated"
	EventArtifactDeleted = "artifact:deleted"
)

var validArtifactKinds = map[string]bool{
	"report":   true,
	"plan":     true,
	"decision": true,
	"diagram":  true,
	"note":     true,
}

type ArtifactResponse struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	ProjectID   *string        `json:"project_id"`
	IssueID     *string        `json:"issue_id"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Metadata    map[string]any `json:"metadata"`
	AuthorType  string         `json:"author_type"`
	AuthorID    string         `json:"author_id"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func artifactToResponse(a db.Artifact) ArtifactResponse {
	resp := ArtifactResponse{
		ID:          uuidToString(a.ID),
		WorkspaceID: uuidToString(a.WorkspaceID),
		Kind:        a.Kind,
		Title:       a.Title,
		Body:        a.Body,
		Metadata:    map[string]any{},
		AuthorType:  a.AuthorType,
		AuthorID:    uuidToString(a.AuthorID),
		CreatedAt:   timestampToString(a.CreatedAt),
		UpdatedAt:   timestampToString(a.UpdatedAt),
	}
	if a.ProjectID.Valid {
		s := uuidToString(a.ProjectID)
		resp.ProjectID = &s
	}
	if a.IssueID.Valid {
		s := uuidToString(a.IssueID)
		resp.IssueID = &s
	}
	if len(a.Metadata) > 0 {
		_ = json.Unmarshal(a.Metadata, &resp.Metadata)
	}
	return resp
}

// ---------------------------------------------------------------------------
// CreateArtifact — POST /api/artifacts
// ---------------------------------------------------------------------------

type CreateArtifactRequest struct {
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Metadata  map[string]any `json:"metadata"`
	ProjectID *string        `json:"project_id"`
	IssueID   *string        `json:"issue_id"`
}

func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	var req CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validArtifactKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "invalid kind; expected one of report|plan|decision|diagram|note")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.ProjectID != nil && req.IssueID != nil {
		writeError(w, http.StatusBadRequest, "artifact cannot be scoped to both a project and an issue")
		return
	}

	// Validate scope target belongs to workspace.
	var projectID, issueID pgtype.UUID
	if req.ProjectID != nil {
		project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          parseUUID(*req.ProjectID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		projectID = project.ID
	}
	if req.IssueID != nil {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(*req.IssueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issueID = issue.ID
	}

	authorType, authorID := h.resolveActor(r, userID, workspaceID)

	metadata := []byte("{}")
	if req.Metadata != nil {
		if encoded, err := json.Marshal(req.Metadata); err == nil {
			metadata = encoded
		}
	}

	id, _ := uuid.NewV7()
	artifact, err := h.Queries.CreateArtifact(r.Context(), db.CreateArtifactParams{
		ID:          pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID: parseUUID(workspaceID),
		ProjectID:   projectID,
		IssueID:     issueID,
		Kind:        req.Kind,
		Title:       req.Title,
		Body:        req.Body,
		Metadata:    metadata,
		AuthorType:  authorType,
		AuthorID:    parseUUID(authorID),
	})
	if err != nil {
		slog.Error("create artifact failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create artifact")
		return
	}

	resp := artifactToResponse(artifact)
	h.publish(EventArtifactCreated, workspaceID, authorType, authorID, map[string]any{
		"artifact": resp,
	})
	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// GetArtifact — GET /api/artifacts/{id}
// ---------------------------------------------------------------------------

func (h *Handler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	artifact, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}
	writeJSON(w, http.StatusOK, artifactToResponse(artifact))
}

// ---------------------------------------------------------------------------
// ListArtifactsForIssue — GET /api/issues/{id}/artifacts
// ---------------------------------------------------------------------------

func (h *Handler) ListArtifactsForIssue(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	artifacts, err := h.Queries.ListArtifactsByIssue(r.Context(), db.ListArtifactsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Error("list artifacts by issue failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list artifacts")
		return
	}

	resp := make([]ArtifactResponse, len(artifacts))
	for i, a := range artifacts {
		resp[i] = artifactToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// ListArtifactsForProject — GET /api/projects/{id}/artifacts
// ---------------------------------------------------------------------------

func (h *Handler) ListArtifactsForProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID:          parseUUID(projectID),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	artifacts, err := h.Queries.ListArtifactsByProject(r.Context(), db.ListArtifactsByProjectParams{
		ProjectID:   project.ID,
		WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		slog.Error("list artifacts by project failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list artifacts")
		return
	}

	resp := make([]ArtifactResponse, len(artifacts))
	for i, a := range artifacts {
		resp[i] = artifactToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// UpdateArtifact — PUT /api/artifacts/{id}
// ---------------------------------------------------------------------------

type UpdateArtifactRequest struct {
	Title    *string        `json:"title"`
	Body     *string        `json:"body"`
	Metadata map[string]any `json:"metadata"`
}

func (h *Handler) UpdateArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	existing, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}

	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == authorType && uuidToString(existing.AuthorID) == authorID
	isAdmin := member.Role == "admin" || member.Role == "owner"
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "not authorized to edit this artifact")
		return
	}

	var req UpdateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := existing.Title
	if req.Title != nil {
		if *req.Title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		title = *req.Title
	}
	body := existing.Body
	if req.Body != nil {
		body = *req.Body
	}
	metadata := existing.Metadata
	if req.Metadata != nil {
		encoded, err := json.Marshal(req.Metadata)
		if err == nil {
			metadata = encoded
		}
	}

	updated, err := h.Queries.UpdateArtifact(r.Context(), db.UpdateArtifactParams{
		ID:          existing.ID,
		Title:       title,
		Body:        body,
		Metadata:    metadata,
		WorkspaceID: existing.WorkspaceID,
	})
	if err != nil {
		slog.Error("update artifact failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update artifact")
		return
	}

	resp := artifactToResponse(updated)
	h.publish(EventArtifactUpdated, workspaceID, authorType, authorID, map[string]any{
		"artifact": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// DeleteArtifact — DELETE /api/artifacts/{id}
// ---------------------------------------------------------------------------

func (h *Handler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	existing, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "artifact not found")
		return
	}

	authorType, authorID := h.resolveActor(r, userID, workspaceID)
	isAuthor := existing.AuthorType == authorType && uuidToString(existing.AuthorID) == authorID
	isAdmin := member.Role == "admin" || member.Role == "owner"
	if !isAuthor && !isAdmin {
		writeError(w, http.StatusForbidden, "not authorized to delete this artifact")
		return
	}

	if err := h.Queries.DeleteArtifact(r.Context(), db.DeleteArtifactParams{
		ID:          existing.ID,
		WorkspaceID: existing.WorkspaceID,
	}); err != nil {
		slog.Error("delete artifact failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete artifact")
		return
	}

	resp := artifactToResponse(existing)
	h.publish(EventArtifactDeleted, workspaceID, authorType, authorID, map[string]any{
		"artifact": resp,
	})
	w.WriteHeader(http.StatusNoContent)
}
