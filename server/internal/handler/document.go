package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/textpatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// --- Response structs ---

type DocumentResponse struct {
	ID                string   `json:"id"`
	WorkspaceID       string   `json:"workspace_id"`
	Path              string   `json:"path"`
	Title             *string  `json:"title"`
	Description       *string  `json:"description"`
	Content           string   `json:"content"`
	Format            string   `json:"format"`
	Tags              []string `json:"tags"`
	Pinned            bool     `json:"pinned"`
	CurrentRevisionID *string  `json:"current_revision_id"`
	CreatedBy         *string  `json:"created_by"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type DocumentIndexEntry struct {
	ID          string  `json:"id"`
	Path        string  `json:"path"`
	Description *string `json:"description"`
	Pinned      bool    `json:"pinned"`
}

type DocumentRevisionResponse struct {
	ID             string  `json:"id"`
	RevisionNumber int32   `json:"revision_number"`
	AuthorType     string  `json:"author_type"`
	AuthorID       *string `json:"author_id"`
	TaskID         *string `json:"task_id"`
	Operation      string  `json:"operation"`
	ChangeSummary  *string `json:"change_summary"`
	CreatedAt      string  `json:"created_at"`
}

type DocumentRevisionDetailResponse struct {
	DocumentRevisionResponse
	Title       *string  `json:"title"`
	Description *string  `json:"description"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags"`
}

type DocumentSearchResult struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Path        string  `json:"path"`
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Rank        float32 `json:"rank"`
}

// --- Converters ---

func documentToResponse(d db.WorkspaceDocument) DocumentResponse {
	return DocumentResponse{
		ID:                uuidToString(d.ID),
		WorkspaceID:       uuidToString(d.WorkspaceID),
		Path:              d.Path,
		Title:             textToPtr(d.Title),
		Description:       textToPtr(d.Description),
		Content:           d.Content,
		Format:            d.Format,
		Tags:              d.Tags,
		Pinned:            d.Pinned,
		CurrentRevisionID: uuidToPtr(d.CurrentRevisionID),
		CreatedBy:         uuidToPtr(d.CreatedBy),
		CreatedAt:         timestampToString(d.CreatedAt),
		UpdatedAt:         timestampToString(d.UpdatedAt),
	}
}

func revisionSummaryToResponse(r db.ListWorkspaceDocumentRevisionsRow) DocumentRevisionResponse {
	return DocumentRevisionResponse{
		ID:             uuidToString(r.ID),
		RevisionNumber: r.RevisionNumber,
		AuthorType:     r.AuthorType,
		AuthorID:       uuidToPtr(r.AuthorID),
		TaskID:         uuidToPtr(r.TaskID),
		Operation:      r.Operation,
		ChangeSummary:  textToPtr(r.ChangeSummary),
		CreatedAt:      timestampToString(r.CreatedAt),
	}
}

func revisionDetailToResponse(r db.WorkspaceDocumentRevision) DocumentRevisionDetailResponse {
	return DocumentRevisionDetailResponse{
		DocumentRevisionResponse: DocumentRevisionResponse{
			ID:             uuidToString(r.ID),
			RevisionNumber: r.RevisionNumber,
			AuthorType:     r.AuthorType,
			AuthorID:       uuidToPtr(r.AuthorID),
			TaskID:         uuidToPtr(r.TaskID),
			Operation:      r.Operation,
			ChangeSummary:  textToPtr(r.ChangeSummary),
			CreatedAt:      timestampToString(r.CreatedAt),
		},
		Title:       textToPtr(r.Title),
		Description: textToPtr(r.Description),
		Content:     r.Content,
		Tags:        r.Tags,
	}
}

// --- Request structs ---

type UpsertDocumentRequest struct {
	Title            *string  `json:"title"`
	Description      *string  `json:"description"`
	Content          string   `json:"content"`
	Tags             []string `json:"tags"`
	BaseRevisionID   *string  `json:"base_revision_id"`
	ChangeSummary    string   `json:"change_summary"`
	ForceNewRevision bool     `json:"force_new_revision"`
}

type PatchDocumentRequest struct {
	Find          string `json:"find"`
	Replace       string `json:"replace"`
	ChangeSummary string `json:"change_summary"`
}

type RenameDocumentRequest struct {
	NewPath string `json:"new_path"`
}

type RestoreDocumentRequest struct {
	RevisionNumber int `json:"revision_number"`
}

type UpdateTagsRequest struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

type LinkDocumentRequest struct {
	DocumentID *string `json:"document_id"`
	Path       *string `json:"path"`
	LinkType   string  `json:"link_type"`
}

// --- Helpers ---

// provenanceFromRequest converts the middleware Provenance to the service layer type.
func provenanceFromRequest(r *http.Request) service.DocumentProvenance {
	p := middleware.ProvenanceFromRequest(r)
	prov := service.DocumentProvenance{
		AuthorType: p.AuthorType,
	}
	if p.AuthorID != nil {
		pgUUID := uuidToPgtype(*p.AuthorID)
		prov.AuthorID = &pgUUID
	}
	if p.TaskID != nil {
		pgUUID := uuidToPgtype(*p.TaskID)
		prov.TaskID = &pgUUID
	}
	return prov
}

func uuidToPgtype(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

// loadDocumentForWorkspace loads a document by ID and verifies it belongs to the given workspace.
func (h *Handler) loadDocumentForWorkspace(w http.ResponseWriter, r *http.Request, docID, workspaceID string) (db.WorkspaceDocument, bool) {
	docUUID, ok := parseUUIDOrBadRequest(w, docID, "document id")
	if !ok {
		return db.WorkspaceDocument{}, false
	}

	doc, err := h.Queries.GetWorkspaceDocumentByID(r.Context(), docUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "document not found")
		return db.WorkspaceDocument{}, false
	}

	if uuidToString(doc.WorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "document not found")
		return db.WorkspaceDocument{}, false
	}

	return doc, true
}

// requireDocWriteAccess checks that the caller has write access to documents.
// Daemon tokens (mdt_*) are rejected when workspace.documents_agent_write_mode is 'read_only_for_agents'.
func (h *Handler) requireDocWriteAccess(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	authPath := middleware.DaemonAuthPathFromContext(r.Context())
	if authPath != middleware.DaemonAuthPathDaemonToken {
		return true // human tokens always allowed
	}

	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workspace")
		return false
	}
	if ws.DocumentsAgentWriteMode == "read_only_for_agents" {
		writeError(w, http.StatusForbidden, "documents are read-only for agents in this workspace")
		return false
	}
	return true
}

// --- Document CRUD handlers ---

func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	pathPrefix := r.URL.Query().Get("path-prefix")
	// Escape SQL LIKE wildcards (% _ \) in the user-supplied prefix — paths
	// legitimately contain underscores, but `_` is a single-char wildcard in
	// LIKE and would otherwise match `team1a` when the user typed `team_a`.
	// The query uses default ESCAPE so '\' is the escape char.
	if pathPrefix != "" {
		pathPrefix = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(pathPrefix)
	}
	var tagsFilter []string
	if t := r.URL.Query().Get("tag"); t != "" {
		tagsFilter = strings.Split(t, ",")
	}

	// Handle pinned-only filter.
	if r.URL.Query().Get("pinned") == "true" {
		docs, err := h.Queries.ListPinnedWorkspaceDocuments(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list documents")
			return
		}
		resp := make([]DocumentResponse, len(docs))
		for i, d := range docs {
			resp[i] = documentToResponse(d)
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	params := db.ListWorkspaceDocumentsParams{
		WorkspaceID: wsUUID,
	}
	if pathPrefix != "" {
		params.Column2 = pathPrefix
	}
	if tagsFilter != nil {
		params.Column3 = tagsFilter
	}

	docs, err := h.Queries.ListWorkspaceDocuments(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list documents")
		return
	}

	resp := make([]DocumentResponse, len(docs))
	for i, d := range docs {
		resp[i] = documentToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListDocumentIndex(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	entries, err := h.Queries.ListWorkspaceDocumentIndex(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list document index")
		return
	}

	resp := make([]DocumentIndexEntry, len(entries))
	for i, e := range entries {
		resp[i] = DocumentIndexEntry{
			ID:          uuidToString(e.ID),
			Path:        e.Path,
			Description: textToPtr(e.Description),
			Pinned:      e.Pinned,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListDocumentTree(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	entries, err := h.Queries.ListWorkspaceDocumentIndex(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list document index")
		return
	}

	format := r.URL.Query().Get("format")
	if format == "json" {
		writeJSON(w, http.StatusOK, buildTreeJSON(entries))
		return
	}

	// ASCII tree
	tree := buildASCIITree(entries)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(tree))
}

func (h *Handler) SearchDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q query parameter is required")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := int32(20)
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}

	results, err := h.Queries.SearchWorkspaceDocumentsByContent(r.Context(), db.SearchWorkspaceDocumentsByContentParams{
		WorkspaceID:    wsUUID,
		PlaintoTsquery: q,
		Limit:          limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}

	resp := make([]DocumentSearchResult, len(results))
	for i, r := range results {
		resp[i] = DocumentSearchResult{
			ID:          uuidToString(r.ID),
			WorkspaceID: uuidToString(r.WorkspaceID),
			Path:        r.Path,
			Title:       textToPtr(r.Title),
			Description: textToPtr(r.Description),
			Rank:        r.Rank,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetDocumentByPath(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	path := chi.URLParam(r, "*")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	doc, err := h.Queries.GetWorkspaceDocumentByPath(r.Context(), db.GetWorkspaceDocumentByPathParams{
		WorkspaceID: wsUUID,
		Path:        path,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "document not found")
		return
	}

	writeJSON(w, http.StatusOK, documentToResponse(doc))
}

func (h *Handler) GetDocumentByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, documentToResponse(doc))
}

func (h *Handler) UpsertDocumentByPath(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	path := chi.URLParam(r, "*")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}

	var req UpsertDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	payload := service.DocumentPayload{
		Title:            req.Title,
		Description:      req.Description,
		Content:          sanitizeNullBytes(req.Content),
		Tags:             req.Tags,
		ForceNewRevision: req.ForceNewRevision,
	}

	prov := provenanceFromRequest(r)

	var baseRevID *pgtype.UUID
	if req.BaseRevisionID != nil {
		u, err := util.ParseUUID(*req.BaseRevisionID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid base_revision_id")
			return
		}
		baseRevID = &u
	}

	doc, err := h.DocumentService.Put(r.Context(), wsUUID, sanitizeNullBytes(path), payload, prov, baseRevID, req.ChangeSummary)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPath) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, service.ErrDocumentConflict) {
			writeError(w, http.StatusConflict, "document revision conflict: base revision is stale")
			return
		}
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a document with this path already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to upsert document: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(*doc)})
	writeJSON(w, http.StatusOK, documentToResponse(*doc))
}

func (h *Handler) PatchDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	var req PatchDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Find == "" {
		writeError(w, http.StatusBadRequest, "find text is required")
		return
	}

	prov := provenanceFromRequest(r)

	updated, err := h.DocumentService.Patch(r.Context(), doc.ID, req.Find, req.Replace, prov, req.ChangeSummary)
	if err != nil {
		if errors.Is(err, textpatch.ErrNotFound) {
			writeError(w, http.StatusUnprocessableEntity, "find text not found in document")
			return
		}
		if errors.Is(err, textpatch.ErrAmbiguous) {
			writeError(w, http.StatusUnprocessableEntity, "find text matches multiple locations")
			return
		}
		if errors.Is(err, textpatch.ErrIdentical) {
			writeError(w, http.StatusUnprocessableEntity, "find and replace text are identical")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to patch document: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(*updated)})
	writeJSON(w, http.StatusOK, documentToResponse(*updated))
}

func (h *Handler) RenameDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	var req RenameDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.NewPath == "" {
		writeError(w, http.StatusBadRequest, "new_path is required")
		return
	}
	cleanPath := sanitizeNullBytes(req.NewPath)
	if !service.IsValidDocumentPath(cleanPath) {
		writeError(w, http.StatusBadRequest, service.ErrInvalidPath.Error())
		return
	}

	err := h.Queries.RenameWorkspaceDocument(r.Context(), db.RenameWorkspaceDocumentParams{
		ID:   doc.ID,
		Path: cleanPath,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a document already exists at the target path")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to rename document")
		return
	}

	doc.Path = req.NewPath
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(doc)})
	writeJSON(w, http.StatusOK, documentToResponse(doc))
}

func (h *Handler) UpdateTags(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	var req UpdateTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Add) == 0 && len(req.Remove) == 0 {
		writeError(w, http.StatusBadRequest, "at least one of add or remove is required")
		return
	}

	prov := provenanceFromRequest(r)

	updated, err := h.DocumentService.UpdateTags(r.Context(), doc.ID, req.Add, req.Remove, prov)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tags: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(*updated)})
	writeJSON(w, http.StatusOK, documentToResponse(*updated))
}

func (h *Handler) PinDocument(w http.ResponseWriter, r *http.Request) {
	h.setDocumentPinned(w, r, true)
}

func (h *Handler) UnpinDocument(w http.ResponseWriter, r *http.Request) {
	h.setDocumentPinned(w, r, false)
}

func (h *Handler) setDocumentPinned(w http.ResponseWriter, r *http.Request, pinned bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	prov := provenanceFromRequest(r)

	updated, err := h.DocumentService.SetPinned(r.Context(), doc.ID, pinned, prov)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update pin status: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(*updated)})
	writeJSON(w, http.StatusOK, documentToResponse(*updated))
}

func (h *Handler) ArchiveDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	prov := provenanceFromRequest(r)

	if err := h.DocumentService.Archive(r.Context(), doc.ID, prov); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive document: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentDeleted, workspaceID, actorType, actorID, map[string]any{"document_id": uuidToString(doc.ID)})
	w.WriteHeader(http.StatusNoContent)
}

// --- Revision handlers ---

func (h *Handler) ListDocumentRevisions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	revisions, err := h.Queries.ListWorkspaceDocumentRevisions(r.Context(), doc.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list revisions")
		return
	}

	resp := make([]DocumentRevisionResponse, len(revisions))
	for i, rev := range revisions {
		resp[i] = revisionSummaryToResponse(rev)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetDocumentRevision(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	nStr := chi.URLParam(r, "n")
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "invalid revision number")
		return
	}

	rev, err := h.Queries.GetWorkspaceDocumentRevision(r.Context(), db.GetWorkspaceDocumentRevisionParams{
		DocumentID:     doc.ID,
		RevisionNumber: int32(n),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}

	writeJSON(w, http.StatusOK, revisionDetailToResponse(rev))
}

func (h *Handler) RestoreDocument(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !h.requireDocWriteAccess(w, r, workspaceID) {
		return
	}

	id := chi.URLParam(r, "id")
	doc, ok := h.loadDocumentForWorkspace(w, r, id, workspaceID)
	if !ok {
		return
	}

	var req RestoreDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RevisionNumber < 1 {
		writeError(w, http.StatusBadRequest, "revision_number must be >= 1")
		return
	}

	prov := provenanceFromRequest(r)

	updated, err := h.DocumentService.Restore(r.Context(), doc.ID, req.RevisionNumber, prov)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "revision not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to restore document: "+err.Error())
		return
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	h.publish(protocol.EventDocumentUpdated, workspaceID, actorType, actorID, map[string]any{"document": documentToResponse(*updated)})
	writeJSON(w, http.StatusOK, documentToResponse(*updated))
}

// --- Issue-Document link handlers ---

func (h *Handler) LinkIssueDocument(w http.ResponseWriter, r *http.Request) {
	// loadIssueForUser is the authoritative gate: it enforces workspace
	// membership and that the issue belongs to the current workspace. Without
	// it an attacker can POST a link from a workspace-A issue UUID to a
	// workspace-B document UUID, then GET to exfiltrate that document's
	// content via the JOIN in ListLinkedDocumentsForIssue.
	issueIDParam := chi.URLParam(r, "issueId")
	issue, ok := h.loadIssueForUser(w, r, issueIDParam)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	var req LinkDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	validLinkTypes := map[string]bool{"referenced": true, "produced": true, "consumed": true}
	if !validLinkTypes[req.LinkType] {
		writeError(w, http.StatusBadRequest, "link_type must be one of: referenced, produced, consumed")
		return
	}

	var docUUID pgtype.UUID
	if req.DocumentID != nil {
		// Reuse loadDocumentForWorkspace to verify the doc belongs to the
		// same workspace as the issue — same defense as the path branch.
		doc, docOK := h.loadDocumentForWorkspace(w, r, *req.DocumentID, workspaceID)
		if !docOK {
			return
		}
		docUUID = doc.ID
	} else if req.Path != nil {
		doc, err := h.Queries.GetWorkspaceDocumentByPath(r.Context(), db.GetWorkspaceDocumentByPathParams{
			WorkspaceID: issue.WorkspaceID,
			Path:        *req.Path,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, "document not found at path: "+*req.Path)
			return
		}
		docUUID = doc.ID
	} else {
		writeError(w, http.StatusBadRequest, "document_id or path is required")
		return
	}

	err := h.Queries.LinkIssueDocument(r.Context(), db.LinkIssueDocumentParams{
		IssueID:    issue.ID,
		DocumentID: docUUID,
		LinkType:   req.LinkType,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link document")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) UnlinkIssueDocument(w http.ResponseWriter, r *http.Request) {
	issueIDParam := chi.URLParam(r, "issueId")
	issue, ok := h.loadIssueForUser(w, r, issueIDParam)
	if !ok {
		return
	}
	workspaceID := uuidToString(issue.WorkspaceID)

	docID := chi.URLParam(r, "documentId")
	doc, ok := h.loadDocumentForWorkspace(w, r, docID, workspaceID)
	if !ok {
		return
	}

	err := h.Queries.UnlinkIssueDocument(r.Context(), db.UnlinkIssueDocumentParams{
		IssueID:    issue.ID,
		DocumentID: doc.ID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink document")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListIssueDocumentLinks(w http.ResponseWriter, r *http.Request) {
	issueIDParam := chi.URLParam(r, "issueId")
	issue, ok := h.loadIssueForUser(w, r, issueIDParam)
	if !ok {
		return
	}

	docs, err := h.Queries.ListLinkedDocumentsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list linked documents")
		return
	}

	resp := make([]DocumentResponse, len(docs))
	for i, d := range docs {
		resp[i] = documentToResponse(d)
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- Tree helpers ---

type treeNode struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"` // "file" or "directory"
	Path     string      `json:"path,omitempty"`
	Pinned   bool        `json:"pinned,omitempty"`
	Children []*treeNode `json:"children,omitempty"`
}

func buildTreeJSON(entries []db.ListWorkspaceDocumentIndexRow) *treeNode {
	root := &treeNode{Name: "", Type: "directory", Children: []*treeNode{}}

	for _, e := range entries {
		parts := strings.Split(e.Path, "/")
		current := root
		for i, part := range parts {
			isLast := i == len(parts)-1
			found := false
			for _, child := range current.Children {
				if child.Name == part {
					current = child
					found = true
					break
				}
			}
			if !found {
				node := &treeNode{Name: part}
				if isLast {
					node.Type = "file"
					node.Path = e.Path
					node.Pinned = e.Pinned
				} else {
					node.Type = "directory"
					node.Children = []*treeNode{}
				}
				current.Children = append(current.Children, node)
				current = node
			}
		}
	}

	return root
}

func buildASCIITree(entries []db.ListWorkspaceDocumentIndexRow) string {
	if len(entries) == 0 {
		return "(empty)\n"
	}

	// Build a sorted tree structure.
	type fileEntry struct {
		path        string
		description string
		pinned      bool
	}
	files := make([]fileEntry, len(entries))
	for i, e := range entries {
		desc := ""
		if e.Description.Valid {
			desc = e.Description.String
		}
		files[i] = fileEntry{path: e.Path, description: desc, pinned: e.Pinned}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	var sb strings.Builder
	type dirState struct {
		name     string
		isLast   bool
		children []any // mix of dirs and files
	}

	// Simple flat rendering with tree connectors.
	type entry struct {
		parts       []string
		description string
		pinned      bool
	}
	items := make([]entry, len(files))
	for i, f := range files {
		items[i] = entry{
			parts:       strings.Split(f.path, "/"),
			description: f.description,
			pinned:      f.pinned,
		}
	}

	// Track which directories we've printed already.
	printedDirs := make(map[string]bool)

	for idx, item := range items {
		// Print any new directory prefixes.
		for depth := 0; depth < len(item.parts)-1; depth++ {
			dirPath := strings.Join(item.parts[:depth+1], "/")
			if !printedDirs[dirPath] {
				printedDirs[dirPath] = true
				prefix := strings.Repeat("    ", depth)
				sb.WriteString(prefix + item.parts[depth] + "/\n")
			}
		}

		// Print the file.
		depth := len(item.parts) - 1
		prefix := strings.Repeat("    ", depth)
		fileName := item.parts[depth]

		// Determine tree connector.
		isLast := true
		for j := idx + 1; j < len(items); j++ {
			if len(items[j].parts) > depth && strings.Join(items[j].parts[:depth], "/") == strings.Join(item.parts[:depth], "/") {
				isLast = false
				break
			}
		}

		connector := "├── "
		if isLast {
			connector = "└── "
		}

		line := prefix + connector + fileName
		if item.pinned {
			line += " 📌"
		}
		if item.description != "" {
			line += "  — " + item.description
		}
		sb.WriteString(line + "\n")
	}

	return sb.String()
}
