package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Knowledge base docs (MUL-8370).
//
// Concurrency: every content write carries the revision the writer last read.
// A mismatch is a 409 revision_conflict; the writer re-reads, merges and
// retries. The server never merges — browsers merge block-wise against the
// version they started from, agents re-read and rewrite.
//
// History: each write appends a full snapshot to doc_version, except that a
// member's consecutive message-less edits fold into their newest version while
// the session is live (docCoalesceIdle since the last save, docCoalesceMax
// since it began). Restore and revert always append.

const (
	docTitleMaxLen    = 300
	docMessageMaxLen  = 500
	docContentMaxSize = 2 << 20
	docBodyMaxBytes   = 6 << 20
	docVersionsMax    = 500
	docActivityMax    = 500
	docActivityDays   = 14
	docCoalesceIdle   = 10 * time.Minute
	docCoalesceMax    = time.Hour
)

var (
	validDocFormats = []string{"markdown", "html"}
	validDocActions = []string{"edit", "restore", "revert"}
)

type DocSummaryResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	FolderID      *string `json:"folder_id"`
	Format        string  `json:"format"`
	Title         string  `json:"title"`
	Revision      int64   `json:"revision"`
	VersionCount  int32   `json:"version_count"`
	CreatedByType string  `json:"created_by_type"`
	CreatedByID   string  `json:"created_by_id"`
	UpdatedByType string  `json:"updated_by_type"`
	UpdatedByID   string  `json:"updated_by_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	Unread        bool    `json:"unread"`
}

type DocResponse struct {
	DocSummaryResponse
	Content    string   `json:"content"`
	ProjectIDs []string `json:"project_ids"`
	LastSeenAt *string  `json:"last_seen_at"`
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
	DocID        string  `json:"doc_id"`
	Number       int32   `json:"number"`
	Title        string  `json:"title"`
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	Message      string  `json:"message"`
	Action       string  `json:"action"`
	ActionRef    *int32  `json:"action_ref"`
	TaskID       *string `json:"task_id"`
	IssueID      *string `json:"issue_id"`
	LinesAdded   int32   `json:"lines_added"`
	LinesRemoved int32   `json:"lines_removed"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	Content      *string `json:"content,omitempty"`
}

type DocActivityResponse struct {
	DocID        string  `json:"doc_id"`
	DocTitle     string  `json:"doc_title"`
	DocFolderID  *string `json:"doc_folder_id"`
	DocFormat    string  `json:"doc_format"`
	Number       int32   `json:"number"`
	AuthorType   string  `json:"author_type"`
	AuthorID     string  `json:"author_id"`
	Message      string  `json:"message"`
	Action       string  `json:"action"`
	ActionRef    *int32  `json:"action_ref"`
	TaskID       *string `json:"task_id"`
	IssueID      *string `json:"issue_id"`
	LinesAdded   int32   `json:"lines_added"`
	LinesRemoved int32   `json:"lines_removed"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	Unread       bool    `json:"unread"`
}

func docToSummary(d db.Doc) DocSummaryResponse {
	return DocSummaryResponse{
		ID:            uuidToString(d.ID),
		WorkspaceID:   uuidToString(d.WorkspaceID),
		FolderID:      uuidToPtr(d.FolderID),
		Format:        d.Format,
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
		Format:        d.Format,
		Title:         d.Title,
		Revision:      d.Revision,
		VersionCount:  d.VersionCount,
		CreatedByType: d.CreatedByType,
		CreatedByID:   uuidToString(d.CreatedByID),
		UpdatedByType: d.UpdatedByType,
		UpdatedByID:   uuidToString(d.UpdatedByID),
		CreatedAt:     timestampToString(d.CreatedAt),
		UpdatedAt:     timestampToString(d.UpdatedAt),
		Unread:        d.Unread,
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
		DocID:        uuidToString(v.DocID),
		Number:       v.Number,
		Title:        v.Title,
		AuthorType:   v.AuthorType,
		AuthorID:     uuidToString(v.AuthorID),
		Message:      v.Message,
		Action:       v.Action,
		ActionRef:    int4ToPtr(v.ActionRef),
		TaskID:       uuidToPtr(v.TaskID),
		IssueID:      uuidToPtr(v.IssueID),
		LinesAdded:   v.LinesAdded,
		LinesRemoved: v.LinesRemoved,
		CreatedAt:    timestampToString(v.CreatedAt),
		UpdatedAt:    timestampToString(v.UpdatedAt),
	}
	if withContent {
		content := v.Content
		resp.Content = &content
	}
	return resp
}

func docVersionRowToResponse(v db.ListDocVersionsRow) DocVersionResponse {
	return DocVersionResponse{
		DocID:        uuidToString(v.DocID),
		Number:       v.Number,
		Title:        v.Title,
		AuthorType:   v.AuthorType,
		AuthorID:     uuidToString(v.AuthorID),
		Message:      v.Message,
		Action:       v.Action,
		ActionRef:    int4ToPtr(v.ActionRef),
		TaskID:       uuidToPtr(v.TaskID),
		IssueID:      uuidToPtr(v.IssueID),
		LinesAdded:   v.LinesAdded,
		LinesRemoved: v.LinesRemoved,
		CreatedAt:    timestampToString(v.CreatedAt),
		UpdatedAt:    timestampToString(v.UpdatedAt),
	}
}

// docMemberSince is the unread baseline for docs the caller never opened.
// Without a member row (should not happen behind the workspace middleware)
// nothing counts as unread.
func docMemberSince(r *http.Request) pgtype.Timestamptz {
	if m, ok := ctxMember(r.Context()); ok && m.CreatedAt.Valid {
		return m.CreatedAt
	}
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
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
	userID, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	folders, err := h.Queries.ListDocFolders(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list folders")
		return
	}
	docs, err := h.Queries.ListDocs(r.Context(), db.ListDocsParams{
		MemberSince: docMemberSince(r),
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	})
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

// ListDocActivity feeds "recently updated": versions written in the last two
// weeks, newest first, without content.
func (h *Handler) ListDocActivity(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDocActivity(r.Context(), db.ListDocActivityParams{
		MemberSince: docMemberSince(r),
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-docActivityDays * 24 * time.Hour), Valid: true},
		MaxRows:     docActivityMax,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list doc activity")
		return
	}
	items := make([]DocActivityResponse, len(rows))
	for i, v := range rows {
		items[i] = DocActivityResponse{
			DocID:        uuidToString(v.DocID),
			DocTitle:     v.DocTitle,
			DocFolderID:  uuidToPtr(v.DocFolderID),
			DocFormat:    v.DocFormat,
			Number:       v.Number,
			AuthorType:   v.AuthorType,
			AuthorID:     uuidToString(v.AuthorID),
			Message:      v.Message,
			Action:       v.Action,
			ActionRef:    int4ToPtr(v.ActionRef),
			TaskID:       uuidToPtr(v.TaskID),
			IssueID:      uuidToPtr(v.IssueID),
			LinesAdded:   v.LinesAdded,
			LinesRemoved: v.LinesRemoved,
			CreatedAt:    timestampToString(v.CreatedAt),
			UpdatedAt:    timestampToString(v.UpdatedAt),
			Unread:       v.Unread,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) docResponse(ctx context.Context, doc db.Doc, userID string) (DocResponse, error) {
	projectIDs, err := h.Queries.ListDocProjectIDs(ctx, db.ListDocProjectIDsParams{DocID: doc.ID, WorkspaceID: doc.WorkspaceID})
	if err != nil {
		return DocResponse{}, err
	}
	resp := DocResponse{
		DocSummaryResponse: docToSummary(doc),
		Content:            doc.Content,
		ProjectIDs:         uuidStringsOrEmpty(projectIDs),
	}
	read, err := h.Queries.GetDocRead(ctx, db.GetDocReadParams{DocID: doc.ID, UserID: parseUUID(userID)})
	switch {
	case err == nil:
		resp.LastSeenAt = timestampToPtr(read.SeenAt)
	case !isNotFound(err):
		return DocResponse{}, err
	}
	return resp, nil
}

func (h *Handler) GetDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	resp, err := h.docResponse(r.Context(), doc, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load doc")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type CreateDocRequest struct {
	Title    string  `json:"title"`
	Content  string  `json:"content"`
	Format   string  `json:"format"`
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
	if req.Format == "" {
		req.Format = "markdown"
	}
	if !contains(validDocFormats, req.Format) {
		writeError(w, http.StatusBadRequest, "format must be markdown or html")
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
		Format:      req.Format,
		Title:       req.Title,
		Content:     req.Content,
		AuthorType:  actorType,
		AuthorID:    parseUUID(actorID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	added, removed := docLineStats("", req.Content)
	if _, err := qtx.InsertDocVersion(ctx, db.InsertDocVersionParams{
		DocID:        doc.ID,
		WorkspaceID:  wsUUID,
		Number:       1,
		Title:        doc.Title,
		Content:      doc.Content,
		AuthorType:   actorType,
		AuthorID:     parseUUID(actorID),
		Message:      req.Message,
		Action:       "create",
		TaskID:       taskID,
		IssueID:      issueID,
		LinesAdded:   int32(added),
		LinesRemoved: int32(removed),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}
	if actorType == "member" {
		if _, err := qtx.MarkDocRead(ctx, db.MarkDocReadParams{DocID: doc.ID, UserID: parseUUID(userID), WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create doc")
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create doc")
		return
	}

	h.publish(protocol.EventDocCreated, workspaceID, actorType, actorID, map[string]any{"doc": docToSummary(doc)})
	resp, err := h.docResponse(ctx, doc, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load doc")
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

type UpdateDocRequest struct {
	Title        *string `json:"title"`
	Content      *string `json:"content"`
	BaseRevision int64   `json:"base_revision"`
	Message      string  `json:"message"`
	Action       string  `json:"action"`
	ActionRef    *int32  `json:"action_ref"`
}

// UpdateDoc writes a doc's title and/or content as a new version (or folds it
// into the writer's live editing session). action=restore copies an older
// version back; action=revert records content the client computed by undoing
// one version on top of the current text.
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
		writeError(w, http.StatusBadRequest, "action must be edit, restore or revert")
		return
	}
	if req.Action != "edit" && (req.ActionRef == nil || *req.ActionRef <= 0) {
		writeError(w, http.StatusBadRequest, "action_ref is required for restore and revert")
		return
	}
	if req.Action == "revert" && req.Content == nil {
		writeError(w, http.StatusBadRequest, "content is required for revert")
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
		if req.Action == "revert" && *req.ActionRef > doc.VersionCount {
			writeError(w, http.StatusBadRequest, "version not found")
			return
		}
		if req.Title != nil {
			title = *req.Title
		}
		if req.Content != nil {
			content = *req.Content
		}
	}
	if title == doc.Title && content == doc.Content {
		resp, err := h.docResponse(ctx, doc, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load doc")
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	latest, err := qtx.GetDocVersion(ctx, db.GetDocVersionParams{DocID: doc.ID, Number: doc.VersionCount})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update doc")
		return
	}
	number := doc.VersionCount
	if shouldCoalesceDocVersion(latest, actorType, actorID, req.Action, req.Message, time.Now()) {
		base := ""
		if latest.Number > 1 {
			prev, err := qtx.GetDocVersion(ctx, db.GetDocVersionParams{DocID: doc.ID, Number: latest.Number - 1})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update doc")
				return
			}
			base = prev.Content
		}
		added, removed := docLineStats(base, content)
		if _, err := qtx.CoalesceDocVersion(ctx, db.CoalesceDocVersionParams{
			Title:        title,
			Content:      content,
			LinesAdded:   int32(added),
			LinesRemoved: int32(removed),
			DocID:        doc.ID,
			Number:       latest.Number,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update doc")
			return
		}
	} else {
		number = doc.VersionCount + 1
		added, removed := docLineStats(doc.Content, content)
		var actionRef pgtype.Int4
		if req.Action != "edit" {
			actionRef = pgtype.Int4{Int32: *req.ActionRef, Valid: true}
		}
		if _, err := qtx.InsertDocVersion(ctx, db.InsertDocVersionParams{
			DocID:        doc.ID,
			WorkspaceID:  wsUUID,
			Number:       number,
			Title:        title,
			Content:      content,
			AuthorType:   actorType,
			AuthorID:     parseUUID(actorID),
			Message:      req.Message,
			Action:       req.Action,
			ActionRef:    actionRef,
			TaskID:       taskID,
			IssueID:      issueID,
			LinesAdded:   int32(added),
			LinesRemoved: int32(removed),
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update doc")
			return
		}
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
	resp, err := h.docResponse(ctx, updated, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load doc")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// shouldCoalesceDocVersion folds a member's autosaves into one version while
// they keep typing. Agents always append — each agent write is a deliberate
// change worth its own entry — and so does anything carrying a message or a
// restore/revert, so those stay individually addressable in history.
func shouldCoalesceDocVersion(latest db.DocVersion, actorType, actorID, action, message string, now time.Time) bool {
	if actorType != "member" || action != "edit" || message != "" {
		return false
	}
	if latest.AuthorType != "member" || uuidToString(latest.AuthorID) != actorID || latest.Message != "" {
		return false
	}
	if latest.Action != "edit" && latest.Action != "create" {
		return false
	}
	if !latest.UpdatedAt.Valid || !latest.CreatedAt.Valid {
		return false
	}
	return now.Sub(latest.UpdatedAt.Time) < docCoalesceIdle && now.Sub(latest.CreatedAt.Time) < docCoalesceMax
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

// DeleteDoc removes a doc with its history, read marks and project links.
// Members may delete their own docs; admins and owners may delete any. An
// agent may delete a doc it created.
func (h *Handler) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	allowed := doc.CreatedByType == actorType && uuidToString(doc.CreatedByID) == actorID
	if !allowed && actorType == "member" {
		if m, ok := ctxMember(r.Context()); ok && roleAllowed(m.Role, "owner", "admin") {
			allowed = true
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "only the doc's creator or a workspace admin can delete it")
		return
	}
	if err := h.Queries.DeleteDoc(r.Context(), db.DeleteDocParams{ID: doc.ID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete doc")
		return
	}
	h.publish(protocol.EventDocDeleted, workspaceID, actorType, actorID, map[string]any{"doc_id": uuidToString(doc.ID)})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MarkDocRead(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	read, err := h.Queries.MarkDocRead(r.Context(), db.MarkDocReadParams{DocID: doc.ID, UserID: parseUUID(userID), WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark doc read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc_id": uuidToString(doc.ID), "last_seen_at": timestampToString(read.SeenAt)})
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
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publish(protocol.EventDocFolderDeleted, workspaceID, actorType, actorID, map[string]any{"folder_id": uuidToString(folderUUID)})
	w.WriteHeader(http.StatusNoContent)
}

// --- Project links ---

func (h *Handler) loadProjectForDocs(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.Project, bool) {
	projectUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load project")
		}
		return db.Project{}, false
	}
	return project, true
}

func (h *Handler) ListProjectDocs(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	project, ok := h.loadProjectForDocs(w, r, wsUUID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListProjectDocs(r.Context(), db.ListProjectDocsParams{ProjectID: project.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project docs")
		return
	}
	docs := make([]DocSummaryResponse, len(rows))
	for i, d := range rows {
		docs[i] = DocSummaryResponse{
			ID:            uuidToString(d.ID),
			WorkspaceID:   uuidToString(d.WorkspaceID),
			FolderID:      uuidToPtr(d.FolderID),
			Format:        d.Format,
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
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs})
}

type LinkProjectDocRequest struct {
	DocID string `json:"doc_id"`
}

func (h *Handler) LinkProjectDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	project, ok := h.loadProjectForDocs(w, r, wsUUID)
	if !ok {
		return
	}
	var req LinkProjectDocRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	docUUID, ok := parseUUIDOrBadRequest(w, req.DocID, "doc_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetDoc(r.Context(), db.GetDocParams{ID: docUUID, WorkspaceID: wsUUID}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusBadRequest, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load doc")
		}
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	n, err := h.Queries.LinkProjectDoc(r.Context(), db.LinkProjectDocParams{
		ProjectID:     project.ID,
		DocID:         docUUID,
		WorkspaceID:   wsUUID,
		CreatedByType: actorType,
		CreatedByID:   parseUUID(actorID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link doc")
		return
	}
	payload := map[string]any{"project_id": uuidToString(project.ID), "doc_id": req.DocID}
	if n > 0 {
		h.publish(protocol.EventProjectDocLinked, workspaceID, actorType, actorID, payload)
		writeJSON(w, http.StatusCreated, payload)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *Handler) UnlinkProjectDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	project, ok := h.loadProjectForDocs(w, r, wsUUID)
	if !ok {
		return
	}
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "docId"), "doc id")
	if !ok {
		return
	}
	n, err := h.Queries.UnlinkProjectDoc(r.Context(), db.UnlinkProjectDocParams{ProjectID: project.ID, DocID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink doc")
		return
	}
	if n > 0 {
		actorType, actorID := h.resolveActor(r, userID, workspaceID)
		h.publish(protocol.EventProjectDocUnlinked, workspaceID, actorType, actorID, map[string]any{
			"project_id": uuidToString(project.ID),
			"doc_id":     uuidToString(docUUID),
		})
	}
	w.WriteHeader(http.StatusNoContent)
}
