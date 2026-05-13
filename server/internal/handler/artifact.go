package handler

// CEREBRO-PATCH(artifact-handler): cerebro modification of upstream file

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

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

var validArtifactFormats = map[string]bool{
	"md":   true,
	"html": true,
	"pdf":  true,
}

type ArtifactResponse struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspace_id"`
	ProjectID     *string        `json:"project_id"`
	IssueID       *string        `json:"issue_id"`
	FolderID      *string        `json:"folder_id"`
	OriginIssueID   *string        `json:"origin_issue_id"`
	Kind            string         `json:"kind"`
	Format          string         `json:"format"`
	Title           string         `json:"title"`
	Body            string         `json:"body"`
	FileURL         *string        `json:"file_url"`
	FileSizeBytes   *int64         `json:"file_size_bytes"`
	Metadata        map[string]any `json:"metadata"`
	AuthorType      string         `json:"author_type"`
	AuthorID        string         `json:"author_id"`
	RequesterUserID *string        `json:"requester_user_id"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

func artifactToResponse(a db.Artifact) ArtifactResponse {
	resp := ArtifactResponse{
		ID:          uuidToString(a.ID),
		WorkspaceID: uuidToString(a.WorkspaceID),
		Kind:        a.Kind,
		Format:      a.Format,
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
	if a.FolderID.Valid {
		s := uuidToString(a.FolderID)
		resp.FolderID = &s
	}
	if a.OriginIssueID.Valid {
		s := uuidToString(a.OriginIssueID)
		resp.OriginIssueID = &s
	}
	if a.RequesterUserID.Valid {
		s := uuidToString(a.RequesterUserID)
		resp.RequesterUserID = &s
	}
	if a.FileUrl.Valid {
		s := a.FileUrl.String
		resp.FileURL = &s
	}
	if a.FileSizeBytes.Valid {
		s := a.FileSizeBytes.Int64
		resp.FileSizeBytes = &s
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
	Kind            string         `json:"kind"`
	Format          string         `json:"format"`
	Title           string         `json:"title"`
	Body            string         `json:"body"`
	FileURL         *string        `json:"file_url"`
	FileSizeBytes   *int64         `json:"file_size_bytes"`
	Metadata        map[string]any `json:"metadata"`
	ProjectID       *string        `json:"project_id"`
	IssueID         *string        `json:"issue_id"`
	FolderID        *string        `json:"folder_id"`
	OriginIssueID   *string        `json:"origin_issue_id"`
	RequesterUserID *string        `json:"requester_user_id"`
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
	format := req.Format
	if format == "" {
		format = "md"
	}
	if !validArtifactFormats[format] {
		writeError(w, http.StatusBadRequest, "invalid format; expected one of md|html|pdf")
		return
	}
	if format == "pdf" && (req.FileURL == nil || *req.FileURL == "") {
		writeError(w, http.StatusBadRequest, "pdf format requires file_url (upload via /api/artifact-uploads first)")
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
	var projectID, issueID, folderID pgtype.UUID
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
	if req.FolderID != nil {
		folder, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
			ID:          parseUUID(*req.FolderID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		folderID = folder.ID
	}
	var originIssueID pgtype.UUID
	if req.OriginIssueID != nil && *req.OriginIssueID != "" {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          parseUUID(*req.OriginIssueID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "origin issue not found")
			return
		}
		originIssueID = issue.ID
	}
	authorType, authorID := h.resolveActor(r, userID, workspaceID)

	// Requester defaults to the authenticated user when an agent acts on
	// their behalf and omits the field. Members get nil (their own author
	// link is the requester).
	var requesterUserID pgtype.UUID
	if req.RequesterUserID != nil && *req.RequesterUserID != "" {
		requesterUserID = parseUUID(*req.RequesterUserID)
	} else if authorType == "agent" {
		requesterUserID = parseUUID(userID)
	}

	metadata := []byte("{}")
	if req.Metadata != nil {
		if encoded, err := json.Marshal(req.Metadata); err == nil {
			metadata = encoded
		}
	}

	var fileURL pgtype.Text
	if req.FileURL != nil && *req.FileURL != "" {
		fileURL = pgtype.Text{String: *req.FileURL, Valid: true}
	}
	var fileSize pgtype.Int8
	if req.FileSizeBytes != nil {
		fileSize = pgtype.Int8{Int64: *req.FileSizeBytes, Valid: true}
	}

	id, _ := uuid.NewV7()
	artifact, err := h.Queries.CreateArtifact(r.Context(), db.CreateArtifactParams{
		ID:              pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:     parseUUID(workspaceID),
		ProjectID:       projectID,
		IssueID:         issueID,
		FolderID:        folderID,
		OriginIssueID:   originIssueID,
		Kind:            req.Kind,
		Format:          format,
		Title:           req.Title,
		Body:            req.Body,
		FileUrl:         fileURL,
		FileSizeBytes:   fileSize,
		Metadata:        metadata,
		AuthorType:      authorType,
		AuthorID:        parseUUID(authorID),
		RequesterUserID: requesterUserID,
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
	resp := artifactToResponse(artifact)
	// CEREBRO-PATCH(persona-mask-artifact-get): JEH-1173 redaction.
	if !h.maskArtifactForCaller(w, r, artifact, &resp) {
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	Title         *string        `json:"title"`
	Body          *string        `json:"body"`
	FileURL       *string        `json:"file_url"`
	FileSizeBytes *int64         `json:"file_size_bytes"`
	Metadata      map[string]any `json:"metadata"`
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

	fileURL := existing.FileUrl
	if req.FileURL != nil {
		if *req.FileURL == "" {
			fileURL = pgtype.Text{}
		} else {
			fileURL = pgtype.Text{String: *req.FileURL, Valid: true}
		}
	}
	fileSize := existing.FileSizeBytes
	if req.FileSizeBytes != nil {
		fileSize = pgtype.Int8{Int64: *req.FileSizeBytes, Valid: true}
	}

	updated, err := h.Queries.UpdateArtifact(r.Context(), db.UpdateArtifactParams{
		ID:            existing.ID,
		Title:         title,
		Body:          body,
		Metadata:      metadata,
		WorkspaceID:   existing.WorkspaceID,
		FileUrl:       fileURL,
		FileSizeBytes: fileSize,
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
// SearchArtifacts — GET /api/artifacts?kind=&scope=&q=&limit=&offset=
// ---------------------------------------------------------------------------

func (h *Handler) SearchArtifacts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	q := r.URL.Query()

	params := db.SearchArtifactsInWorkspaceParams{
		WorkspaceID: parseUUID(workspaceID),
		Limit:       50,
		Offset:      0,
	}

	if v := q.Get("kind"); v != "" {
		if !validArtifactKinds[v] {
			writeError(w, http.StatusBadRequest, "invalid kind filter")
			return
		}
		params.Kind = pgtype.Text{String: v, Valid: true}
	}
	if v := q.Get("scope"); v != "" {
		switch v {
		case "all", "workspace", "project", "issue":
			params.Scope = pgtype.Text{String: v, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "invalid scope; expected all|workspace|project|issue")
			return
		}
	}
	if v := q.Get("author_type"); v != "" {
		switch v {
		case "all", "member", "agent":
			params.AuthorType = pgtype.Text{String: v, Valid: true}
		default:
			writeError(w, http.StatusBadRequest, "invalid author_type; expected all|member|agent")
			return
		}
	}
	if v := q.Get("author_id"); v != "" {
		params.AuthorID = parseUUID(v)
	}
	if v := q.Get("origin_issue_id"); v != "" {
		params.OriginIssueID = parseUUID(v)
	}
	if v := q.Get("q"); v != "" {
		params.Q = pgtype.Text{String: v, Valid: true}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid limit (1-200)")
			return
		}
		params.Limit = int32(n)
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		params.Offset = int32(n)
	}

	artifacts, err := h.Queries.SearchArtifactsInWorkspace(r.Context(), params)
	if err != nil {
		slog.Error("search artifacts failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to search artifacts")
		return
	}

	resp := make([]ArtifactResponse, len(artifacts))
	for i, a := range artifacts {
		resp[i] = artifactToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// UpdateArtifactScope — PUT /api/artifacts/{id}/scope
// ---------------------------------------------------------------------------

type UpdateArtifactScopeRequest struct {
	ProjectID *string `json:"project_id"`
	IssueID   *string `json:"issue_id"`
}

func (h *Handler) UpdateArtifactScope(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "not authorized to move this artifact")
		return
	}

	var req UpdateArtifactScopeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ProjectID != nil && req.IssueID != nil {
		writeError(w, http.StatusBadRequest, "scope is exclusive: provide project_id, issue_id, or neither (workspace)")
		return
	}

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

	updated, err := h.Queries.UpdateArtifactScope(r.Context(), db.UpdateArtifactScopeParams{
		ID:          existing.ID,
		WorkspaceID: existing.WorkspaceID,
		ProjectID:   projectID,
		IssueID:     issueID,
	})
	if err != nil {
		slog.Error("update artifact scope failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to move artifact")
		return
	}

	resp := artifactToResponse(updated)
	// Emit BOTH delete (to clear from old scope's list) and updated (to surface
	// in new scope's list). The server can't easily diff old vs new scope on
	// the client side; sending both is simpler than crafting per-scope events.
	previous := artifactToResponse(existing)
	h.publish(EventArtifactDeleted, workspaceID, authorType, authorID, map[string]any{
		"artifact": previous,
	})
	h.publish(EventArtifactCreated, workspaceID, authorType, authorID, map[string]any{
		"artifact": resp,
	})
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// MoveArtifactToFolder — PUT /api/artifacts/{id}/folder
// ---------------------------------------------------------------------------

type MoveArtifactToFolderRequest struct {
	FolderID *string `json:"folder_id"`
}

func (h *Handler) MoveArtifactToFolder(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusForbidden, "not authorized to move this artifact")
		return
	}

	var req MoveArtifactToFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var folderID pgtype.UUID
	if req.FolderID != nil && *req.FolderID != "" {
		folder, err := h.Queries.GetArtifactFolder(r.Context(), db.GetArtifactFolderParams{
			ID:          parseUUID(*req.FolderID),
			WorkspaceID: parseUUID(workspaceID),
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "folder not found")
			return
		}
		folderID = folder.ID
	}

	updated, err := h.Queries.MoveArtifactToFolder(r.Context(), db.MoveArtifactToFolderParams{
		ID:          existing.ID,
		WorkspaceID: existing.WorkspaceID,
		FolderID:    folderID,
	})
	if err != nil {
		slog.Error("move artifact to folder failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to move artifact")
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
