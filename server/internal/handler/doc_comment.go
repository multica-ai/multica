package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Doc comments are discussions on a doc's text (MUL-8370). A root comment
// carries the anchor — the quoted text and a little context on each side,
// which clients re-locate by text after every save — and replies hang off the
// root, one level deep. Agents take part by replying (MUL-9933): an agent can
// answer in an existing discussion but cannot start one, and resolving,
// reopening and deleting stay with people.

const (
	docCommentMaxContent = 20000
	docCommentMaxQuote   = 4000
	docCommentMaxContext = 64
)

type DocCommentResponse struct {
	ID             string  `json:"id"`
	DocID          string  `json:"doc_id"`
	ParentID       *string `json:"parent_id"`
	AuthorType     string  `json:"author_type"`
	AuthorID       string  `json:"author_id"`
	Content        string  `json:"content"`
	AnchorQuote    string  `json:"anchor_quote"`
	AnchorPrefix   string  `json:"anchor_prefix"`
	AnchorSuffix   string  `json:"anchor_suffix"`
	ResolvedAt     *string `json:"resolved_at"`
	ResolvedByType *string `json:"resolved_by_type"`
	ResolvedByID   *string `json:"resolved_by_id"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func docCommentToResponse(c db.DocComment) DocCommentResponse {
	return DocCommentResponse{
		ID:             uuidToString(c.ID),
		DocID:          uuidToString(c.DocID),
		ParentID:       uuidToPtr(c.ParentID),
		AuthorType:     c.AuthorType,
		AuthorID:       uuidToString(c.AuthorID),
		Content:        c.Content,
		AnchorQuote:    c.AnchorQuote,
		AnchorPrefix:   c.AnchorPrefix,
		AnchorSuffix:   c.AnchorSuffix,
		ResolvedAt:     timestampToPtr(c.ResolvedAt),
		ResolvedByType: textToPtr(c.ResolvedByType),
		ResolvedByID:   uuidToPtr(c.ResolvedByID),
		CreatedAt:      timestampToString(c.CreatedAt),
		UpdatedAt:      timestampToString(c.UpdatedAt),
	}
}

func (h *Handler) ListDocComments(w http.ResponseWriter, r *http.Request) {
	_, wsUUID, _, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	rows, err := h.Queries.ListDocComments(r.Context(), db.ListDocCommentsParams{DocID: doc.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	resp := make([]DocCommentResponse, len(rows))
	for i, c := range rows {
		resp[i] = docCommentToResponse(c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": resp})
}

type CreateDocCommentRequest struct {
	Content      string  `json:"content"`
	ParentID     *string `json:"parent_id"`
	AnchorQuote  string  `json:"anchor_quote"`
	AnchorPrefix string  `json:"anchor_prefix"`
	AnchorSuffix string  `json:"anchor_suffix"`
}

func (h *Handler) CreateDocComment(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	var req CreateDocCommentRequest
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(strings.ReplaceAll(req.Content, "\x00", ""))
	if content == "" || utf8.RuneCountInString(content) > docCommentMaxContent {
		writeError(w, http.StatusBadRequest, "content must be 1 to 20000 characters")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType != "member" && req.ParentID == nil {
		writeError(w, http.StatusForbidden, "agents can only reply in an existing discussion")
		return
	}
	params := db.CreateDocCommentParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, DocID: doc.ID, Content: content,
	}
	var root db.DocComment
	var replyTo *db.DocComment
	if req.ParentID != nil {
		parentUUID, ok := parseUUIDOrBadRequest(w, *req.ParentID, "parent_id")
		if !ok {
			return
		}
		parent, err := h.Queries.GetDocComment(r.Context(), db.GetDocCommentParams{ID: parentUUID, DocID: doc.ID, WorkspaceID: wsUUID})
		if err != nil {
			if isNotFound(err) {
				writeError(w, http.StatusBadRequest, "parent comment not found")
			} else {
				writeError(w, http.StatusInternalServerError, "failed to create comment")
			}
			return
		}
		// One level of replies: answering a reply joins its discussion. The
		// comment answered still decides which agent a plain reply is for.
		answered := parent
		replyTo = &answered
		if parent.ParentID.Valid {
			parent, err = h.Queries.GetDocComment(r.Context(), db.GetDocCommentParams{ID: parent.ParentID, DocID: doc.ID, WorkspaceID: wsUUID})
			if err != nil {
				writeError(w, http.StatusBadRequest, "parent comment not found")
				return
			}
		}
		root = parent
		params.ParentID = parent.ID
	} else {
		quote := strings.ReplaceAll(req.AnchorQuote, "\x00", "")
		if strings.TrimSpace(quote) == "" || utf8.RuneCountInString(quote) > docCommentMaxQuote {
			writeError(w, http.StatusBadRequest, "anchor_quote must be 1 to 4000 characters")
			return
		}
		params.AnchorQuote = quote
		params.AnchorPrefix = lastRunes(strings.ReplaceAll(req.AnchorPrefix, "\x00", ""), docCommentMaxContext)
		params.AnchorSuffix = firstRunes(strings.ReplaceAll(req.AnchorSuffix, "\x00", ""), docCommentMaxContext)
	}

	params.AuthorType = actorType
	params.AuthorID = parseUUID(actorID)
	params.SourceTaskID, _ = h.docTaskContext(r, actorType)
	comment, err := h.Queries.CreateDocComment(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}
	if !params.ParentID.Valid {
		root = comment
	}
	h.publish(protocol.EventDocCommentChanged, workspaceID, actorType, actorID, map[string]any{
		"doc_id": uuidToString(doc.ID), "comment_id": uuidToString(comment.ID),
	})
	service.NotifyDocComment(r.Context(), h.Queries, h.Bus, doc, root, comment, actorType, actorID)
	h.triggerDocCommentAgents(r, doc, root, replyTo, comment, actorType, actorID)
	writeJSON(w, http.StatusCreated, docCommentToResponse(comment))
}

func (h *Handler) loadDocComment(w http.ResponseWriter, r *http.Request, doc db.Doc) (db.DocComment, bool) {
	commentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "docCommentId"), "comment id")
	if !ok {
		return db.DocComment{}, false
	}
	comment, err := h.Queries.GetDocComment(r.Context(), db.GetDocCommentParams{ID: commentUUID, DocID: doc.ID, WorkspaceID: doc.WorkspaceID})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "comment not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load comment")
		}
		return db.DocComment{}, false
	}
	return comment, true
}

func (h *Handler) setDocCommentResolution(w http.ResponseWriter, r *http.Request, resolve bool) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	comment, ok := h.loadDocComment(w, r, doc)
	if !ok {
		return
	}
	if comment.ParentID.Valid {
		writeError(w, http.StatusBadRequest, "only a discussion's first comment can be resolved")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	params := db.SetDocCommentResolutionParams{ID: comment.ID, DocID: doc.ID, WorkspaceID: wsUUID}
	if resolve {
		params.ResolvedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		params.ResolvedByType = pgtype.Text{String: actorType, Valid: true}
		params.ResolvedByID = parseUUID(actorID)
	}
	updated, err := h.Queries.SetDocCommentResolution(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}
	h.publish(protocol.EventDocCommentChanged, workspaceID, actorType, actorID, map[string]any{
		"doc_id": uuidToString(doc.ID), "comment_id": uuidToString(updated.ID),
	})
	writeJSON(w, http.StatusOK, docCommentToResponse(updated))
}

func (h *Handler) ResolveDocComment(w http.ResponseWriter, r *http.Request) {
	h.setDocCommentResolution(w, r, true)
}

func (h *Handler) ReopenDocComment(w http.ResponseWriter, r *http.Request) {
	h.setDocCommentResolution(w, r, false)
}

// DeleteDocComment removes the caller's own comment; removing a discussion's
// first comment removes the discussion.
func (h *Handler) DeleteDocComment(w http.ResponseWriter, r *http.Request) {
	userID, wsUUID, workspaceID, ok := h.docWorkspace(w, r)
	if !ok {
		return
	}
	doc, ok := h.loadDoc(w, r, wsUUID)
	if !ok {
		return
	}
	comment, ok := h.loadDocComment(w, r, doc)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if comment.AuthorType != actorType || uuidToString(comment.AuthorID) != actorID {
		writeError(w, http.StatusForbidden, "only the author can delete a comment")
		return
	}
	if err := h.Queries.DeleteDocCommentThread(r.Context(), db.DeleteDocCommentThreadParams{
		WorkspaceID: wsUUID, DocID: doc.ID, ID: comment.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}
	h.publish(protocol.EventDocCommentChanged, workspaceID, actorType, actorID, map[string]any{
		"doc_id": uuidToString(doc.ID), "comment_id": uuidToString(comment.ID),
	})
	w.WriteHeader(http.StatusNoContent)
}

func firstRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func lastRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}
