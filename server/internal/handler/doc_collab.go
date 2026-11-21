package handler

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/collab"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	docCollabTicketTTL    = 5 * time.Minute
	docCollabFlushTimeout = 2 * time.Second
	docCollabBodyMaxBytes = 16 << 20
	docEditsMax           = 100
	// Live editing saves every 5 to 30 seconds while someone types, each save
	// a full copy of the doc. A writer whose base is more than this many
	// revisions old gets a revision conflict instead of a merge.
	docRevisionsKept = 50
)

// DocContributor is someone whose edits a version carries.
type DocContributor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func docContributorsJSON(contributors []DocContributor) []byte {
	if len(contributors) == 0 {
		return []byte("[]")
	}
	raw, _ := json.Marshal(contributors)
	return raw
}

func parseDocContributors(raw []byte) []DocContributor {
	contributors := []DocContributor{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &contributors)
	}
	return contributors
}

// docWrite is one change to a doc's title or content, committed by
// commitDocWrite inside the caller's transaction.
type docWrite struct {
	title, content     string
	actorType, actorID string
	contributors       []DocContributor
	message, action    string
	actionRef          *int32
	taskID, issueID    pgtype.UUID
}

// commitDocWrite snapshots a write into the version history and updates the
// doc. Unlabeled edits fold into the latest snapshot while the same editing
// stretch lasts: the same author and task, or, for people co-editing live,
// any member, with everyone added as a contributor.
func commitDocWrite(ctx context.Context, qtx *db.Queries, doc db.Doc, wr docWrite) (db.Doc, int32, error) {
	if len(wr.contributors) == 0 {
		wr.contributors = []DocContributor{{Type: wr.actorType, ID: wr.actorID}}
	}
	merged := false
	if wr.action == "edit" && wr.message == "" {
		var rows int64
		var err error
		if wr.actorType == "member" && !wr.taskID.Valid {
			rows, err = qtx.MergeCollabEditIntoVersion(ctx, db.MergeCollabEditIntoVersionParams{
				Title: wr.title, Content: wr.content, Contributors: docContributorsJSON(wr.contributors),
				DocID: doc.ID, Number: doc.VersionCount,
			})
		} else {
			rows, err = qtx.MergeDocEditIntoVersion(ctx, db.MergeDocEditIntoVersionParams{
				Title: wr.title, Content: wr.content, DocID: doc.ID, Number: doc.VersionCount,
				AuthorType: wr.actorType, AuthorID: parseUUID(wr.actorID), TaskID: wr.taskID,
			})
		}
		if err != nil {
			return db.Doc{}, 0, err
		}
		merged = rows == 1
	}
	number := doc.VersionCount
	if !merged {
		number++
		var actionRef pgtype.Int4
		if wr.action == "restore" && wr.actionRef != nil {
			actionRef = pgtype.Int4{Int32: *wr.actionRef, Valid: true}
		}
		if _, err := qtx.InsertDocVersion(ctx, db.InsertDocVersionParams{
			DocID: doc.ID, WorkspaceID: doc.WorkspaceID, Number: number, Title: wr.title, Content: wr.content,
			AuthorType: wr.actorType, AuthorID: parseUUID(wr.actorID), Message: wr.message,
			Action: wr.action, ActionRef: actionRef, TaskID: wr.taskID, IssueID: wr.issueID,
			Contributors: docContributorsJSON(wr.contributors),
		}); err != nil {
			return db.Doc{}, 0, err
		}
	}
	updated, err := qtx.UpdateDocContent(ctx, db.UpdateDocContentParams{
		Title: wr.title, Content: wr.content, VersionCount: number,
		UpdatedByType: wr.actorType, UpdatedByID: parseUUID(wr.actorID),
		ID: doc.ID, WorkspaceID: doc.WorkspaceID,
	})
	if err != nil {
		return db.Doc{}, 0, err
	}
	if err := recordDocRevision(ctx, qtx, updated); err != nil {
		return db.Doc{}, 0, err
	}
	return updated, number, nil
}

// recordDocRevision keeps the content of recent revisions, the merge base for
// a write that started from one of them.
func recordDocRevision(ctx context.Context, qtx *db.Queries, doc db.Doc) error {
	if err := qtx.InsertDocRevision(ctx, db.InsertDocRevisionParams{DocID: doc.ID, Revision: doc.Revision, Content: doc.Content}); err != nil {
		return err
	}
	return qtx.PruneDocRevisions(ctx, db.PruneDocRevisionsParams{DocID: doc.ID, KeepFrom: doc.Revision - docRevisionsKept})
}

func (h *Handler) collabActor(ctx context.Context, actorType, actorID string) collab.Actor {
	actor := collab.Actor{Type: actorType, ID: actorID}
	switch actorType {
	case "agent":
		if agent, err := h.Queries.GetAgent(ctx, parseUUID(actorID)); err == nil {
			actor.Name = agent.Name
		}
	case "member":
		if user, err := h.Queries.GetUser(ctx, parseUUID(actorID)); err == nil {
			actor.Name = user.Name
		}
	}
	return actor
}

// flushDocCollab persists what people have typed into a doc that is open for
// live editing, so a read returns it. Best effort: a slow or absent service
// leaves the last save in place.
func (h *Handler) flushDocCollab(ctx context.Context, docID pgtype.UUID) {
	if h.Collab == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, docCollabFlushTimeout)
	defer cancel()
	if err := h.Collab.Flush(ctx, uuidToString(docID)); err != nil {
		slog.Warn("doc collab flush failed", "doc_id", uuidToString(docID), "error", err)
	}
}

type DocCollabTicketResponse struct {
	Enabled   bool   `json:"enabled"`
	Token     string `json:"token,omitempty"`
	Document  string `json:"document,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// CreateDocCollabTicket lets an editor join a doc's live session. Editors ask
// again on every reconnect, so tickets are short-lived.
func (h *Handler) CreateDocCollabTicket(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	if h.Collab == nil {
		writeJSON(w, http.StatusOK, DocCollabTicketResponse{Enabled: false})
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	expires := time.Now().Add(docCollabTicketTTL)
	document := collab.DocumentName(uuidToString(doc.ID))
	token := collab.SignTicket(h.Collab.Secret(), collab.Ticket{
		Document:    document,
		WorkspaceID: workspaceID,
		Actor:       h.collabActor(r.Context(), actorType, actorID),
		ExpiresAt:   expires,
	})
	writeJSON(w, http.StatusOK, DocCollabTicketResponse{
		Enabled: true, Token: token, Document: document, ExpiresAt: expires.UTC().Format(time.RFC3339),
	})
}

type DocWriteResponse struct {
	DocResponse
	// Merged is true when the write was based on an older revision and was
	// merged with the changes made since.
	Merged bool `json:"merged"`
}

func writeDocMergeConflict(w http.ResponseWriter, result collab.Result) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":           "the change overlaps edits made since its base revision",
		"code":            "doc_merge_conflict",
		"conflicts":       result.Conflicts,
		"actual_revision": result.Revision,
		"content":         result.Content,
	})
}

func writeDocEditFailed(w http.ResponseWriter, failure *collab.EditFailure, revision int64, content string) {
	msg := "old_text was not found in the doc"
	switch failure.Reason {
	case "ambiguous":
		msg = "old_text matches more than once; quote more context or set replace_all"
	case "empty":
		msg = "old_text must not be empty"
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"error":           msg,
		"code":            "doc_edit_failed",
		"failure":         failure,
		"actual_revision": revision,
		"content":         content,
	})
}

// updateDocViaCollab sends a write through the live session, so it merges
// into what people are typing instead of overwriting it. It reports false
// when the service is unreachable and the caller should write directly.
func (h *Handler) updateDocViaCollab(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, docUUID pgtype.UUID, req UpdateDocRequest, actor collab.Actor, taskID pgtype.UUID) bool {
	ctx := r.Context()
	doc, err := h.Queries.GetDoc(ctx, db.GetDocParams{ID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to update doc")
		}
		return true
	}
	write := collab.WriteRequest{
		Actor: actor, Message: req.Message, Action: req.Action, ActionRef: req.ActionRef,
	}
	if taskID.Valid {
		write.TaskID = uuidToString(taskID)
	}
	switch {
	case req.Action == "restore":
		source, err := h.Queries.GetDocVersion(ctx, db.GetDocVersionParams{DocID: doc.ID, Number: *req.ActionRef})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusBadRequest, "version not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to update doc")
			}
			return true
		}
		write.Title, write.Content = &source.Title, &source.Content
	case req.Content != nil:
		// The content the writer started from is the merge base.
		var base string
		if req.BaseRevision == doc.Revision {
			base = doc.Content
		} else if req.BaseRevision < doc.Revision {
			base, err = h.Queries.GetDocRevisionContent(ctx, db.GetDocRevisionContentParams{DocID: doc.ID, Revision: req.BaseRevision})
			if err != nil {
				if isNotFound(err) {
					writeRevisionConflict(w, "doc", doc.ID, req.BaseRevision, doc.Revision)
				} else {
					writeError(w, http.StatusInternalServerError, "failed to update doc")
				}
				return true
			}
		} else {
			writeRevisionConflict(w, "doc", doc.ID, req.BaseRevision, doc.Revision)
			return true
		}
		write.BaseRevision, write.BaseContent, write.Content, write.Title = req.BaseRevision, &base, req.Content, req.Title
	default:
		// Title only: last write wins, the content is untouched.
		write.Title = req.Title
	}

	result, err := h.Collab.Write(ctx, uuidToString(doc.ID), write)
	if err != nil {
		if errors.Is(err, collab.ErrUnavailable) {
			slog.Warn("doc collab write failed; writing directly", "doc_id", uuidToString(doc.ID), "error", err)
			return false
		}
		writeError(w, http.StatusInternalServerError, "failed to update doc")
		return true
	}
	h.writeCollabResult(w, r, wsUUID, doc.ID, result)
	return true
}

func (h *Handler) writeCollabResult(w http.ResponseWriter, r *http.Request, wsUUID, docID pgtype.UUID, result collab.Result) {
	switch result.Status {
	case collab.StatusOK:
		updated, err := h.Queries.GetDoc(r.Context(), db.GetDocParams{ID: docID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load doc")
			return
		}
		writeJSON(w, http.StatusOK, DocWriteResponse{
			DocResponse: DocResponse{DocSummaryResponse: docToSummary(updated), Content: updated.Content},
			Merged:      result.Merged,
		})
	case collab.StatusConflict:
		writeDocMergeConflict(w, result)
	case collab.StatusEditFailed:
		failure := result.Failure
		if failure == nil {
			failure = &collab.EditFailure{Reason: "not_found"}
		}
		writeDocEditFailed(w, failure, result.Revision, result.Content)
	case collab.StatusGone:
		writeError(w, http.StatusNotFound, "doc not found")
	case collab.StatusTooLarge:
		writeError(w, http.StatusRequestEntityTooLarge, "content must be at most 2 MB")
	default:
		writeError(w, http.StatusInternalServerError, "failed to update doc")
	}
}

type EditDocRequest struct {
	Edits   []collab.TextEdit `json:"edits"`
	Message string            `json:"message"`
}

// EditDoc applies find-and-replace edits. Each edit quotes the text it
// changes, so it lands wherever that text now is, even while others are
// editing, and fails only when someone changed that very text.
func (h *Handler) EditDoc(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	var req EditDocRequest
	r.Body = http.MaxBytesReader(w, r.Body, docBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Edits) == 0 || len(req.Edits) > docEditsMax {
		writeError(w, http.StatusBadRequest, "edits must contain between 1 and 100 edits")
		return
	}
	for i := range req.Edits {
		req.Edits[i].OldText = sanitizeNullBytes(req.Edits[i].OldText)
		req.Edits[i].NewText = sanitizeNullBytes(req.Edits[i].NewText)
		if !utf8.ValidString(req.Edits[i].OldText) || !utf8.ValidString(req.Edits[i].NewText) {
			writeError(w, http.StatusBadRequest, "edits must be valid UTF-8")
			return
		}
	}
	req.Message = sanitizeNullBytes(req.Message)
	if !validateDocMessage(w, req.Message) {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	taskID, issueID := h.docTaskContext(r, actorType)

	ctx := r.Context()
	if h.Collab != nil {
		if _, err := h.Queries.GetDoc(ctx, db.GetDocParams{ID: docUUID, WorkspaceID: wsUUID}); err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusNotFound, "doc not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to edit doc")
			}
			return
		}
		edit := collab.EditRequest{Actor: h.collabActor(ctx, actorType, actorID), Edits: req.Edits, Message: req.Message}
		if taskID.Valid {
			edit.TaskID = uuidToString(taskID)
		}
		result, err := h.Collab.Edit(ctx, uuidToString(docUUID), edit)
		if err == nil {
			h.writeCollabResult(w, r, wsUUID, docUUID, result)
			return
		}
		if !errors.Is(err, collab.ErrUnavailable) {
			writeError(w, http.StatusInternalServerError, "failed to edit doc")
			return
		}
		slog.Warn("doc collab edit failed; editing directly", "doc_id", uuidToString(docUUID), "error", err)
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to edit doc")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	doc, err := qtx.GetDocForUpdate(ctx, db.GetDocForUpdateParams{ID: docUUID, WorkspaceID: wsUUID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to edit doc")
		}
		return
	}
	content, failure := collab.ApplyTextEdits(doc.Content, req.Edits)
	if failure != nil {
		writeDocEditFailed(w, failure, doc.Revision, doc.Content)
		return
	}
	if !validateDocContent(w, content) {
		return
	}
	if content == doc.Content {
		writeJSON(w, http.StatusOK, DocWriteResponse{DocResponse: DocResponse{DocSummaryResponse: docToSummary(doc), Content: doc.Content}})
		return
	}
	updated, number, err := commitDocWrite(ctx, qtx, doc, docWrite{
		title: doc.Title, content: content, actorType: actorType, actorID: actorID,
		message: req.Message, action: "edit", taskID: taskID, issueID: issueID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to edit doc")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to edit doc")
		return
	}
	h.publish(protocol.EventDocUpdated, workspaceID, actorType, actorID, map[string]any{
		"doc": docToSummary(updated), "version": number,
	})
	writeJSON(w, http.StatusOK, DocWriteResponse{DocResponse: DocResponse{DocSummaryResponse: docToSummary(updated), Content: updated.Content}})
}

// --- Internal endpoints called by the collaboration service ---

// RequireCollabSecret admits only the collaboration service.
func (h *Handler) RequireCollabSecret(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get(collab.SecretHeader)
		if h.Collab == nil || subtle.ConstantTimeCompare([]byte(given), []byte(h.Collab.Secret())) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type collabDocResponse struct {
	WorkspaceID    string  `json:"workspace_id"`
	Title          string  `json:"title"`
	Content        string  `json:"content"`
	Revision       int64   `json:"revision"`
	YState         *string `json:"ystate"`
	YStateRevision *int64  `json:"ystate_revision"`
}

func (h *Handler) GetCollabDoc(w http.ResponseWriter, r *http.Request) {
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	row, err := h.Queries.GetDocForCollab(r.Context(), docUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load doc")
		}
		return
	}
	resp := collabDocResponse{
		WorkspaceID: uuidToString(row.WorkspaceID), Title: row.Title, Content: row.Content, Revision: row.Revision,
	}
	if row.Ystate != nil && row.YstateRevision.Valid {
		state := base64.StdEncoding.EncodeToString(row.Ystate)
		resp.YState, resp.YStateRevision = &state, &row.YstateRevision.Int64
	}
	writeJSON(w, http.StatusOK, resp)
}

type collabStoreRequest struct {
	BaseRevision int64            `json:"base_revision"`
	Content      string           `json:"content"`
	YState       string           `json:"ystate"`
	Contributors []DocContributor `json:"contributors"`
	Title        *string          `json:"title"`
	Message      string           `json:"message"`
	Action       string           `json:"action"`
	ActionRef    *int32           `json:"action_ref"`
	TaskID       string           `json:"task_id"`
	// StateOnly saves the Yjs state for the current revision and leaves the
	// doc itself untouched.
	StateOnly bool `json:"state_only"`
}

// StoreCollabDoc saves the live document: a version crediting everyone whose
// edits it carries, the new content, and the Yjs state behind it.
func (h *Handler) StoreCollabDoc(w http.ResponseWriter, r *http.Request) {
	docUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "doc id")
	if !ok {
		return
	}
	var req collabStoreRequest
	r.Body = http.MaxBytesReader(w, r.Body, docCollabBodyMaxBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = sanitizeNullBytes(req.Content)
	req.Message = sanitizeNullBytes(req.Message)
	if !validateDocContent(w, req.Content) || !validateDocMessage(w, req.Message) {
		return
	}
	if req.Title != nil {
		title := strings.TrimSpace(sanitizeNullBytes(*req.Title))
		if !validateDocTitle(w, title) {
			return
		}
		req.Title = &title
	}
	if req.Action == "" {
		req.Action = "edit"
	}
	if !contains(validDocActions, req.Action) || (req.Action == "restore" && (req.ActionRef == nil || *req.ActionRef <= 0)) {
		writeError(w, http.StatusBadRequest, "invalid action")
		return
	}
	ystate, err := base64.StdEncoding.DecodeString(req.YState)
	if err != nil || len(ystate) == 0 {
		writeError(w, http.StatusBadRequest, "ystate must be base64")
		return
	}
	contributors := make([]DocContributor, 0, len(req.Contributors))
	for _, c := range req.Contributors {
		if _, err := util.ParseUUID(c.ID); err != nil || (c.Type != "member" && c.Type != "agent") {
			writeError(w, http.StatusBadRequest, "invalid contributor")
			return
		}
		contributors = append(contributors, c)
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store doc")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	doc, err := qtx.GetDocByIDForUpdate(ctx, docUUID)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "doc not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to store doc")
		}
		return
	}
	if doc.Revision != req.BaseRevision {
		writeJSON(w, http.StatusConflict, map[string]any{"revision": doc.Revision, "content": doc.Content})
		return
	}
	title := doc.Title
	if req.Title != nil {
		title = *req.Title
	}

	updated, number := doc, int32(0)
	actorType, actorID := doc.UpdatedByType, uuidToString(doc.UpdatedByID)
	if len(contributors) > 0 {
		actorType, actorID = contributors[0].Type, contributors[0].ID
	}
	changed := !req.StateOnly && (req.Action != "edit" || req.Message != "" || title != doc.Title || req.Content != doc.Content)
	if changed {
		var taskID, issueID pgtype.UUID
		if actorType == "agent" {
			if parsed, err := util.ParseUUID(req.TaskID); err == nil {
				taskID = parsed
				if task, err := qtx.GetAgentTask(ctx, parsed); err == nil {
					issueID = task.IssueID
				}
			}
		}
		updated, number, err = commitDocWrite(ctx, qtx, doc, docWrite{
			title: title, content: req.Content, actorType: actorType, actorID: actorID,
			contributors: contributors, message: req.Message, action: req.Action, actionRef: req.ActionRef,
			taskID: taskID, issueID: issueID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store doc")
			return
		}
	}
	if err := qtx.UpsertDocCollabState(ctx, db.UpsertDocCollabStateParams{
		DocID: updated.ID, WorkspaceID: updated.WorkspaceID, Ystate: ystate, Revision: updated.Revision,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store doc")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store doc")
		return
	}
	if changed {
		h.publish(protocol.EventDocUpdated, uuidToString(updated.WorkspaceID), actorType, actorID, map[string]any{
			"doc": docToSummary(updated), "version": number,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"revision": updated.Revision, "title": updated.Title})
}
