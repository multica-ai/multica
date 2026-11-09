package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	docTitleMaxLen    = 300
	docMessageMaxLen  = 500
	docContentMaxSize = 2 << 20
	docBodyMaxBytes   = 6 << 20
	docVersionsMax    = 500
)

var (
	validDocActions = []string{"edit", "restore"}
)

type DocSummaryResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	FolderID      *string `json:"folder_id"`
	Title         string  `json:"title"`
	Revision      int64   `json:"revision"`
	VersionCount  int32   `json:"version_count"`
	CreatedByType string  `json:"created_by_type"`
	CreatedByID   string  `json:"created_by_id"`
	UpdatedByType string  `json:"updated_by_type"`
	UpdatedByID   string  `json:"updated_by_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type DocResponse struct {
	DocSummaryResponse
	Content string `json:"content"`
}

type DocFolderResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	ParentID      *string `json:"parent_id"`
	Title         string  `json:"title"`
	CreatedByType string  `json:"created_by_type"`
	CreatedByID   string  `json:"created_by_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type DocVersionResponse struct {
	DocID      string  `json:"doc_id"`
	Number     int32   `json:"number"`
	Title      string  `json:"title"`
	AuthorType string  `json:"author_type"`
	AuthorID   string  `json:"author_id"`
	Message    string  `json:"message"`
	Action     string  `json:"action"`
	ActionRef  *int32  `json:"action_ref"`
	TaskID     *string `json:"task_id"`
	IssueID    *string `json:"issue_id"`
	CreatedAt  string  `json:"created_at"`
	UpdatedAt  string  `json:"updated_at"`
	Content    *string `json:"content,omitempty"`
}

func docToSummary(d db.Doc) DocSummaryResponse {
	return DocSummaryResponse{
		ID:            uuidToString(d.ID),
		WorkspaceID:   uuidToString(d.WorkspaceID),
		FolderID:      uuidToPtr(d.FolderID),
		Title:         d.Title,
		Revision:      d.Revision,
		VersionCount:  d.VersionCount,
		CreatedByType: d.CreatedByType,
		CreatedByID:   uuidToString(d.CreatedByID),
		UpdatedByType: d.UpdatedByType,
		UpdatedByID:   uuidToString(d.UpdatedByID),
		CreatedAt:     timestampToString(d.CreatedAt),
		UpdatedAt:     timestampToString(d.UpdatedAt),
	}
}

func docListRowToSummary(d db.ListDocsRow) DocSummaryResponse {
	return DocSummaryResponse{
		ID:            uuidToString(d.ID),
		WorkspaceID:   uuidToString(d.WorkspaceID),
		FolderID:      uuidToPtr(d.FolderID),
		Title:         d.Title,
		Revision:      d.Revision,
		VersionCount:  d.VersionCount,
		CreatedByType: d.CreatedByType,
		CreatedByID:   uuidToString(d.CreatedByID),
		UpdatedByType: d.UpdatedByType,
		UpdatedByID:   uuidToString(d.UpdatedByID),
		CreatedAt:     timestampToString(d.CreatedAt),
		UpdatedAt:     timestampToString(d.UpdatedAt),
	}
}

func docFolderToResponse(f db.DocFolder) DocFolderResponse {
	return DocFolderResponse{
		ID:            uuidToString(f.ID),
		WorkspaceID:   uuidToString(f.WorkspaceID),
		ParentID:      uuidToPtr(f.ParentID),
		Title:         f.Title,
		CreatedByType: f.CreatedByType,
		CreatedByID:   uuidToString(f.CreatedByID),
		CreatedAt:     timestampToString(f.CreatedAt),
		UpdatedAt:     timestampToString(f.UpdatedAt),
	}
}

func docVersionToResponse(v db.DocVersion, withContent bool) DocVersionResponse {
	resp := DocVersionResponse{
		DocID:      uuidToString(v.DocID),
		Number:     v.Number,
		Title:      v.Title,
		AuthorType: v.AuthorType,
		AuthorID:   uuidToString(v.AuthorID),
		Message:    v.Message,
		Action:     v.Action,
		ActionRef:  int4ToPtr(v.ActionRef),
		TaskID:     uuidToPtr(v.TaskID),
		IssueID:    uuidToPtr(v.IssueID),
		CreatedAt:  timestampToString(v.CreatedAt),
		UpdatedAt:  timestampToString(v.UpdatedAt),
	}
	if withContent {
		content := v.Content
		resp.Content = &content
	}
	return resp
}

func docVersionRowToResponse(v db.ListDocVersionsRow) DocVersionResponse {
	return DocVersionResponse{
		DocID:      uuidToString(v.DocID),
		Number:     v.Number,
		Title:      v.Title,
		AuthorType: v.AuthorType,
		AuthorID:   uuidToString(v.AuthorID),
		Message:    v.Message,
		Action:     v.Action,
		ActionRef:  int4ToPtr(v.ActionRef),
		TaskID:     uuidToPtr(v.TaskID),
		IssueID:    uuidToPtr(v.IssueID),
		CreatedAt:  timestampToString(v.CreatedAt),
		UpdatedAt:  timestampToString(v.UpdatedAt),
	}
}

// docTaskContext attributes an agent write to the task (and its issue) that
// made it, so history can link back to the run.
func (h *Handler) docTaskContext(r *http.Request, actorType string) (pgtype.UUID, pgtype.UUID) {
	if actorType != "agent" {
		return pgtype.UUID{}, pgtype.UUID{}
	}
	taskUUID, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		return taskUUID, pgtype.UUID{}
	}
	return taskUUID, task.IssueID
}

func validateDocTitle(w http.ResponseWriter, title string) bool {
	if utf8.RuneCountInString(title) > docTitleMaxLen {
		writeError(w, http.StatusBadRequest, "title must be at most 300 characters")
		return false
	}
	return true
}

func validateDocContent(w http.ResponseWriter, content string) bool {
	if len(content) > docContentMaxSize {
		writeError(w, http.StatusRequestEntityTooLarge, "content must be at most 2 MB")
		return false
	}
	if !utf8.ValidString(content) {
		writeError(w, http.StatusBadRequest, "content must be valid UTF-8")
		return false
	}
	return true
}

func validateDocMessage(w http.ResponseWriter, message string) bool {
	if utf8.RuneCountInString(message) > docMessageMaxLen {
		writeError(w, http.StatusBadRequest, "message must be at most 500 characters")
		return false
	}
	return true
}

func (h *Handler) docWorkspace(w http.ResponseWriter, r *http.Request) (string, pgtype.UUID, string, bool) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return "", pgtype.UUID{}, "", false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return "", pgtype.UUID{}, "", false
	}
	return userID, wsUUID, workspaceID, true
}

func (h *Handler) loadDoc(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.Doc, bool) {
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return db.Doc{}, false
	}
	doc, err := h.Queries.GetDoc(r.Context(), db.GetDocParams{ID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load doc")
		}
		return db.Doc{}, false
	}
	return doc, true
}

// requireDocFolder checks that a target folder exists in the workspace. A nil
// or empty id means the workspace root.
func requireDocFolder(ctx context.Context, w http.ResponseWriter, q *db.Queries, wsUUID pgtype.UUID, id *string, field string) (pgtype.UUID, bool) {
	if id == nil || *id == "" {
		return pgtype.UUID{}, true
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, *id, field)
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := q.GetDocFolder(ctx, db.GetDocFolderParams{ID: folderUUID, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusBadRequest, "folder not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load folder")
		}
		return pgtype.UUID{}, false
	}
	return folderUUID, true
}

// ListDocTree returns every folder and doc (metadata only) in the workspace.
// Knowledge bases are small enough that the client builds the tree itself.
func (h *Handler) ListDocTree(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	folders, err := h.Queries.ListDocFolders(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	docs, err := h.Queries.ListDocs(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list docs")
		return
	}
	folderResp := make([]DocFolderResponse, len(folders))
	for i, f := range folders {
		folderResp[i] = docFolderToResponse(f)
	}
	docResp := make([]DocSummaryResponse, len(docs))
	for i, d := range docs {
		docResp[i] = docListRowToSummary(d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folderResp, "docs": docResp})
}

func (h *Handler) GetDoc(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, DocResponse{DocSummaryResponse: docToSummary(doc), Content: doc.Content})
}

type CreateDocRequest struct {
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	FolderID *string `json:"folder_id"`
	Message  string  `json:"message"`
}

func (h *Handler) CreateDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateDocRequest
	r.Body = http.MaxBytesReader(w, r.Body, docBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if !validateDocTitle(w, req.Title) || !validateDocContent(w, req.Content) || !validateDocMessage(w, req.Message) {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	taskID, issueID := h.docTaskContext(r, actorType)

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockDocTree(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	folderUUID, ok := requireDocFolder(ctx, w, qtx, wsUUID, req.FolderID, "folder_id")
	if !ok {
		return
	}
	doc, err := qtx.CreateDoc(ctx, db.CreateDocParams{
		WorkspaceID: wsUUID,
		FolderID:    folderUUID,
		Title:       req.Title,
		Content:     req.Content,
		AuthorType:  actorType,
		AuthorID:    parseUUID(actorID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	if _, err := qtx.InsertDocVersion(ctx, db.InsertDocVersionParams{
		DocID:       doc.ID,
		WorkspaceID: wsUUID,
		Number:      1,
		Title:       doc.Title,
		Content:     doc.Content,
		AuthorType:  actorType,
		AuthorID:    parseUUID(actorID),
		Message:     req.Message,
		Action:      "create",
		TaskID:      taskID,
		IssueID:     issueID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}

	h.publish(protocol.EventDocCreated, workspaceID, actorType, actorID, map[string]any{"doc": docToSummary(doc)})
	writeJSON(w, http.StatusCreated, DocResponse{DocSummaryResponse: docToSummary(doc), Content: doc.Content})
}

type UpdateDocRequest struct {
	Title        *string `json:"title"`
	Content      *string `json:"content"`
	BaseRevision int64   `json:"base_revision"`
	Message      string  `json:"message"`
	Action       string  `json:"action"`
	ActionRef    *int32  `json:"action_ref"`
}

// UpdateDoc appends a snapshot after checking the writer's base revision.
func (h *Handler) UpdateDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	var req UpdateDocRequest
	r.Body = http.MaxBytesReader(w, r.Body, docBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BaseRevision <= 0 {
		writeError(w, http.StatusBadRequest, "base_revision is required")
		return
	}
	if req.Action == "" {
		req.Action = "edit"
	}
	if !contains(validDocActions, req.Action) {
		writeError(w, http.StatusBadRequest, "action must be edit or restore")
		return
	}
	if req.Action != "edit" && (req.ActionRef == nil || *req.ActionRef <= 0) {
		writeError(w, http.StatusBadRequest, "action_ref is required for restore")
		return
	}
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		req.Title = &trimmed
		if !validateDocTitle(w, trimmed) {
			return
		}
	}
	if req.Content != nil && !validateDocContent(w, *req.Content) {
		return
	}
	if !validateDocMessage(w, req.Message) {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	taskID, issueID := h.docTaskContext(r, actorType)

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update doc")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	doc, err := qtx.GetDocForUpdate(ctx, db.GetDocForUpdateParams{ID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update doc")
		}
		return
	}
	if doc.Revision != req.BaseRevision {
		writeRevisionConflict(w, "doc", doc.ID, req.BaseRevision, doc.Revision)
		return
	}

	title, content := doc.Title, doc.Content
	if req.Action == "restore" {
		source, err := qtx.GetDocVersion(ctx, db.GetDocVersionParams{DocID: doc.ID, Number: *req.ActionRef})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusBadRequest, "version not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to update doc")
			}
			return
		}
		title, content = source.Title, source.Content
	} else {
		if req.Title != nil {
			title = *req.Title
		}
		if req.Content != nil {
			content = *req.Content
		}
	}
	if req.Action == "edit" && title == doc.Title && content == doc.Content {
		writeJSON(w, http.StatusOK, DocResponse{DocSummaryResponse: docToSummary(doc), Content: doc.Content})
		return
	}
	number := doc.VersionCount + 1
	var actionRef pgtype.Int4
	if req.Action == "restore" {
		actionRef = pgtype.Int4{Int32: *req.ActionRef, Valid: true}
	}
	if _, err := qtx.InsertDocVersion(ctx, db.InsertDocVersionParams{
		DocID: doc.ID, WorkspaceID: wsUUID, Number: number, Title: title, Content: content,
		AuthorType: actorType, AuthorID: parseUUID(actorID), Message: req.Message,
		Action: req.Action, ActionRef: actionRef, TaskID: taskID, IssueID: issueID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save version")
		return
	}
	updated, err := qtx.UpdateDocContent(ctx, db.UpdateDocContentParams{
		Title:         title,
		Content:       content,
		VersionCount:  number,
		UpdatedByType: actorType,
		UpdatedByID:   parseUUID(actorID),
		ID:            doc.ID,
		WorkspaceID:   wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update doc")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update doc")
		return
	}

	h.publish(protocol.EventDocUpdated, workspaceID, actorType, actorID, map[string]any{
		"doc":     docToSummary(updated),
		"version": number,
	})
	writeJSON(w, http.StatusOK, DocResponse{DocSummaryResponse: docToSummary(updated), Content: updated.Content})
}

type MoveDocRequest struct {
	FolderID *string `json:"folder_id"`
}

func (h *Handler) MoveDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	var req MoveDocRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move doc")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockDocTree(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move doc")
		return
	}
	folderUUID, ok := requireDocFolder(ctx, w, qtx, wsUUID, req.FolderID, "folder_id")
	if !ok {
		return
	}
	doc, err := qtx.MoveDoc(ctx, db.MoveDocParams{FolderID: folderUUID, ID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to move doc")
		}
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move doc")
		return
	}
	h.publish(protocol.EventDocUpdated, workspaceID, actorType, actorID, map[string]any{"doc": docToSummary(doc)})
	writeJSON(w, http.StatusOK, docToSummary(doc))
}

func (h *Handler) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if isMachineCredentialActor(r) || actorType != "member" || r.Header.Get("X-Agent-ID") != "" {
		writeError(w, http.StatusForbidden, "only humans can delete docs")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, 500, "failed to delete doc")
		return
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)
	if _, err = q.GetDocForUpdate(ctx, db.GetDocForUpdateParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, 404, "doc not found")
		} else {
			writeError(w, 500, "failed to delete doc")
		}
		return
	}
	if err = q.DeleteDoc(ctx, db.DeleteDocParams{ID: id, WorkspaceID: wsUUID}); err != nil {
		writeError(w, 500, "failed to delete doc")
		return
	}
	if err = tx.Commit(ctx); err != nil {
		writeError(w, 500, "failed to delete doc")
		return
	}
	h.publish(protocol.EventDocDeleted, workspaceID, actorType, actorID, map[string]any{"doc_id": uuidToString(id)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDocVersions(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDocVersions(r.Context(), db.ListDocVersionsParams{DocID: doc.ID, Limit: docVersionsMax})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list versions")
		return
	}
	versions := make([]DocVersionResponse, len(rows))
	for i, v := range rows {
		versions[i] = docVersionRowToResponse(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions})
}

func (h *Handler) GetDocVersion(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	number, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 32)
	if err != nil || number <= 0 {
		writeError(w, http.StatusBadRequest, "invalid version number")
		return
	}
	v, err := h.Queries.GetDocVersion(r.Context(), db.GetDocVersionParams{DocID: doc.ID, Number: int32(number)})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "version not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load version")
		}
		return
	}
	writeJSON(w, http.StatusOK, docVersionToResponse(v, true))
}

// --- Folders ---

type CreateDocFolderRequest struct {
	Title    string  `json:"title"`
	ParentID *string `json:"parent_id"`
}

func validateDocFolderTitle(w http.ResponseWriter, title string) bool {
	if l := utf8.RuneCountInString(title); l < 1 || l > 200 {
		writeError(w, http.StatusBadRequest, "folder name must be between 1 and 200 characters")
		return false
	}
	return true
}

func (h *Handler) CreateDocFolder(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	var req CreateDocFolderRequest
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if !validateDocFolderTitle(w, req.Title) {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockDocTree(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	parentUUID, ok := requireDocFolder(ctx, w, qtx, wsUUID, req.ParentID, "parent_id")
	if !ok {
		return
	}
	folder, err := qtx.CreateDocFolder(ctx, db.CreateDocFolderParams{
		WorkspaceID:   wsUUID,
		ParentID:      parentUUID,
		Title:         req.Title,
		CreatedByType: actorType,
		CreatedByID:   parseUUID(actorID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create folder")
		return
	}
	resp := docFolderToResponse(folder)
	h.publish(protocol.EventDocFolderCreated, workspaceID, actorType, actorID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateDocFolderRequest struct {
	Title string `json:"title"`
}

func (h *Handler) UpdateDocFolder(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	var req UpdateDocFolderRequest
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if !validateDocFolderTitle(w, req.Title) {
		return
	}
	folder, err := h.Queries.RenameDocFolder(r.Context(), db.RenameDocFolderParams{Title: req.Title, ID: folderUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "folder not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to rename folder")
		}
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	resp := docFolderToResponse(folder)
	h.publish(protocol.EventDocFolderUpdated, workspaceID, actorType, actorID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusOK, resp)
}

type MoveDocFolderRequest struct {
	ParentID *string `json:"parent_id"`
}

// docFolderCreatesCycle reports whether putting folder under newParent would
// make it its own ancestor.
func docFolderCreatesCycle(folders []db.DocFolder, folder, newParent pgtype.UUID) bool {
	if !newParent.Valid {
		return false
	}
	parentOf := make(map[pgtype.UUID]pgtype.UUID, len(folders))
	for _, f := range folders {
		parentOf[f.ID] = f.ParentID
	}
	cur := newParent
	for steps := 0; cur.Valid && steps <= len(folders); steps++ {
		if cur == folder {
			return true
		}
		cur = parentOf[cur]
	}
	return false
}

func (h *Handler) MoveDocFolder(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	var req MoveDocFolderRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move folder")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockDocTree(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move folder")
		return
	}
	parentUUID, ok := requireDocFolder(ctx, w, qtx, wsUUID, req.ParentID, "parent_id")
	if !ok {
		return
	}
	folders, err := qtx.ListDocFolders(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move folder")
		return
	}
	if docFolderCreatesCycle(folders, folderUUID, parentUUID) {
		writeErrorCode(w, http.StatusBadRequest, "folder_cycle", "a folder cannot be moved into itself or one of its subfolders")
		return
	}
	folder, err := qtx.MoveDocFolder(ctx, db.MoveDocFolderParams{ParentID: parentUUID, ID: folderUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "folder not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to move folder")
		}
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to move folder")
		return
	}
	resp := docFolderToResponse(folder)
	h.publish(protocol.EventDocFolderUpdated, workspaceID, actorType, actorID, map[string]any{"folder": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteDocFolder removes an empty folder. Non-empty folders are refused so a
// misclick can never take docs and their history with it.
func (h *Handler) DeleteDocFolder(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if isMachineCredentialActor(r) || actorType != "member" || r.Header.Get("X-Agent-ID") != "" {
		writeError(w, http.StatusForbidden, "only humans can delete folders")
		return
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	if err := qtx.LockDocTree(ctx, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}
	if _, err := qtx.GetDocFolder(ctx, db.GetDocFolderParams{ID: folderUUID, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "folder not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to delete folder")
		}
		return
	}
	children, err := qtx.CountDocFolderChildren(ctx, db.CountDocFolderChildrenParams{WorkspaceID: wsUUID, FolderID: folderUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}
	if children > 0 {
		writeErrorCode(w, http.StatusConflict, "folder_not_empty", "move or delete the folder's contents first")
		return
	}
	if err := qtx.DeleteDocFolder(ctx, db.DeleteDocFolderParams{ID: folderUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete folder")
		return
	}
	_, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventDocFolderDeleted, workspaceID, actorType, actorID, map[string]any{"folder_id": uuidToString(folderUUID)})
	w.WriteHeader(http.StatusNoContent)
}
