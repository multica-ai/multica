// Comments + suggestions on notes (Wave 3 / G1, TECH-3556). Registered on the
// existing note.Handler.Routes under /api/notes/{id}/comments. Reuses the same
// scope + visibility gating as the rest of the Notes API.
//
// Access model:
//   - read + create comment/reply/suggestion: anyone who can SEE the note,
//   - edit/delete a comment: its author (or the note owner),
//   - resolve/reopen a thread: anyone who can see the note,
//   - accept/reject a suggestion: the note OWNER only (it mutates the note).
//
// Suggestions carry a proposed replacement for an anchored span. Accepting one
// re-locates the anchor in the CURRENT body (quote + prefix/suffix context, so
// it survives edits) and splices in the replacement; if the span can no longer
// be found the accept fails with 409 rather than corrupting the note.
package note

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- request / response shapes ---

type createCommentRequest struct {
	Kind           string  `json:"kind,omitempty"` // "comment" (default) | "suggestion"
	Body           string  `json:"body"`
	ThreadRootID   *string `json:"thread_root_id,omitempty"`
	AnchorQuote    *string `json:"anchor_quote,omitempty"`
	AnchorPrefix   *string `json:"anchor_prefix,omitempty"`
	AnchorSuffix   *string `json:"anchor_suffix,omitempty"`
	AnchorStart    *int32  `json:"anchor_start,omitempty"`
	AnchorEnd      *int32  `json:"anchor_end,omitempty"`
	SuggestionText *string `json:"suggestion_text,omitempty"`
}

type updateCommentRequest struct {
	Body string `json:"body"`
}

type resolveCommentRequest struct {
	Resolved bool `json:"resolved"`
}

type suggestionStateRequest struct {
	State string `json:"state"` // "accepted" | "rejected"
}

// CommentResponse is the wire shape for one comment (root or reply).
type CommentResponse struct {
	ID              string  `json:"id"`
	NoteID          string  `json:"note_id"`
	ThreadRootID    *string `json:"thread_root_id"`
	Kind            string  `json:"kind"`
	Body            string  `json:"body"`
	AnchorQuote     *string `json:"anchor_quote"`
	AnchorPrefix    *string `json:"anchor_prefix"`
	AnchorSuffix    *string `json:"anchor_suffix"`
	AnchorStart     *int32  `json:"anchor_start"`
	AnchorEnd       *int32  `json:"anchor_end"`
	SuggestionText  *string `json:"suggestion_text"`
	SuggestionState string  `json:"suggestion_state"`
	Resolved        bool    `json:"resolved"`
	ResolvedBy      *string `json:"resolved_by"`
	ResolvedAt      string  `json:"resolved_at"`
	AuthorType      string  `json:"author_type"`
	AuthorID        string  `json:"author_id"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

func commentToResponse(c cerebrodb.CerebroNoteComment) CommentResponse {
	return CommentResponse{
		ID:              uuidStr(c.ID),
		NoteID:          uuidStr(c.NoteID),
		ThreadRootID:    uuidPtr(c.ThreadRootID),
		Kind:            c.Kind,
		Body:            c.Body,
		AnchorQuote:     textPtr(c.AnchorQuote),
		AnchorPrefix:    textPtr(c.AnchorPrefix),
		AnchorSuffix:    textPtr(c.AnchorSuffix),
		AnchorStart:     int4Ptr(c.AnchorStart),
		AnchorEnd:       int4Ptr(c.AnchorEnd),
		SuggestionText:  textPtr(c.SuggestionText),
		SuggestionState: c.SuggestionState,
		Resolved:        c.Resolved,
		ResolvedBy:      uuidPtr(c.ResolvedBy),
		ResolvedAt:      tsStr(c.ResolvedAt),
		AuthorType:      c.AuthorType,
		AuthorID:        uuidStr(c.AuthorID),
		CreatedAt:       tsStr(c.CreatedAt),
		UpdatedAt:       tsStr(c.UpdatedAt),
	}
}

// --- handlers ---

// ListComments returns every comment (roots + replies) for a note, oldest
// first. Any viewer who can see the note may read its comments.
func (h *Handler) ListComments(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, ownerUUID) {
		return
	}
	rows, err := h.Cerebro.ListNoteComments(r.Context(), noteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	out := make([]CommentResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, commentToResponse(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateComment adds a comment, reply, or suggestion. Anyone who can see the
// note may post. A reply sets thread_root_id; a suggestion sets kind +
// suggestion_text. Anchor fields position a root comment against the body.
func (h *Handler) CreateComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, ownerUUID) {
		return
	}
	var req createCommentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	kind := req.Kind
	if kind == "" {
		kind = "comment"
	}
	if kind != "comment" && kind != "suggestion" {
		writeError(w, http.StatusBadRequest, "invalid kind; expected comment|suggestion")
		return
	}
	if strings.TrimSpace(req.Body) == "" && (kind != "suggestion" || req.SuggestionText == nil) {
		writeError(w, http.StatusBadRequest, "comment body is required")
		return
	}

	var threadRoot pgtype.UUID
	if req.ThreadRootID != nil && *req.ThreadRootID != "" {
		root, err := parseUUIDField(*req.ThreadRootID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid thread_root_id")
			return
		}
		// The root must belong to this note (prevents cross-note threading).
		rootRow, err := h.Cerebro.GetNoteComment(r.Context(), root)
		if err != nil || uuidStr(rootRow.NoteID) != uuidStr(noteID) {
			writeError(w, http.StatusBadRequest, "thread root not found on this note")
			return
		}
		threadRoot = root
	}

	created, err := h.Cerebro.CreateNoteComment(r.Context(), cerebrodb.CreateNoteCommentParams{
		NoteID:         noteID,
		ThreadRootID:   threadRoot,
		Kind:           kind,
		Body:           req.Body,
		AnchorQuote:    optText(req.AnchorQuote),
		AnchorPrefix:   optText(req.AnchorPrefix),
		AnchorSuffix:   optText(req.AnchorSuffix),
		AnchorStart:    optInt4(req.AnchorStart),
		AnchorEnd:      optInt4(req.AnchorEnd),
		SuggestionText: optText(req.SuggestionText),
		AuthorType:     "member",
		AuthorID:       ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}
	writeJSON(w, http.StatusCreated, commentToResponse(created))
}

// UpdateComment edits a comment's own text. Author-only (or note owner).
func (h *Handler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	ctx, comment, ownerUUID, noteOwned, ok := h.loadCommentForWrite(w, r)
	if !ok {
		return
	}
	if uuidStr(comment.AuthorID) != uuidStr(ownerUUID) && !noteOwned {
		writeError(w, http.StatusForbidden, "only the author can edit this comment")
		return
	}
	var req updateCommentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.Cerebro.UpdateNoteCommentBody(ctx, cerebrodb.UpdateNoteCommentBodyParams{
		ID:   comment.ID,
		Body: req.Body,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update comment")
		return
	}
	writeJSON(w, http.StatusOK, commentToResponse(updated))
}

// DeleteComment removes a comment (replies cascade). Author or note owner.
func (h *Handler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	ctx, comment, ownerUUID, noteOwned, ok := h.loadCommentForWrite(w, r)
	if !ok {
		return
	}
	if uuidStr(comment.AuthorID) != uuidStr(ownerUUID) && !noteOwned {
		writeError(w, http.StatusForbidden, "only the author can delete this comment")
		return
	}
	if _, err := h.Cerebro.DeleteNoteComment(ctx, comment.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResolveComment resolves or reopens a thread. Any viewer who can see the note.
func (h *Handler) ResolveComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	_, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireCanSee(w, r, noteID, ownerUUID) {
		return
	}
	commentID, ok := subPathUUID(w, r, "commentId")
	if !ok {
		return
	}
	comment, err := h.Cerebro.GetNoteComment(r.Context(), commentID)
	if err != nil || uuidStr(comment.NoteID) != uuidStr(noteID) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	var req resolveCommentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	updated, err := h.Cerebro.SetNoteCommentResolved(r.Context(), cerebrodb.SetNoteCommentResolvedParams{
		ID:         commentID,
		Resolved:   req.Resolved,
		ResolvedBy: ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve comment")
		return
	}
	writeJSON(w, http.StatusOK, commentToResponse(updated))
}

// DecideSuggestion accepts or rejects a suggestion. Note OWNER only — accepting
// splices the proposed text into the note body, so it is a write to the note.
func (h *Handler) DecideSuggestion(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ownerUUID, ok := h.scope(w, r, userID)
	if !ok {
		return
	}
	noteID, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if !h.requireOwner(w, r, noteID, ownerUUID) {
		return
	}
	commentID, ok := subPathUUID(w, r, "commentId")
	if !ok {
		return
	}
	var req suggestionStateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.State != "accepted" && req.State != "rejected" {
		writeError(w, http.StatusBadRequest, "invalid state; expected accepted|rejected")
		return
	}
	comment, err := h.Cerebro.GetNoteComment(r.Context(), commentID)
	if err != nil || uuidStr(comment.NoteID) != uuidStr(noteID) {
		writeError(w, http.StatusNotFound, "suggestion not found")
		return
	}
	if comment.Kind != "suggestion" {
		writeError(w, http.StatusBadRequest, "comment is not a suggestion")
		return
	}
	if comment.SuggestionState != "pending" {
		writeError(w, http.StatusConflict, "suggestion already decided")
		return
	}

	// Accepting applies the replacement to the note body first; only if that
	// succeeds do we flip the state, so a failed splice leaves it pending.
	if req.State == "accepted" {
		if err := h.applySuggestion(r.Context(), wsUUID, noteID, ownerUUID, comment); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	}

	updated, err := h.Cerebro.SetNoteSuggestionState(r.Context(), cerebrodb.SetNoteSuggestionStateParams{
		ID:              commentID,
		SuggestionState: req.State,
		ResolvedBy:      ownerUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update suggestion")
		return
	}
	writeJSON(w, http.StatusOK, commentToResponse(updated))
}

// applySuggestion re-locates the suggestion's anchored span in the CURRENT note
// body and replaces it with the proposed text, then saves + snapshots a
// version. Returns a user-facing error when the span can no longer be found
// (the body changed too much) so the caller can refuse the accept cleanly.
func (h *Handler) applySuggestion(ctx context.Context, wsUUID, noteID, ownerUUID pgtype.UUID, c cerebrodb.CerebroNoteComment) error {
	quote := c.AnchorQuote.String
	if !c.AnchorQuote.Valid || quote == "" {
		return errCannotApply("this suggestion has no anchored text")
	}
	artifact, err := h.Upstream.GetArtifact(ctx, db.GetArtifactParams{ID: noteID, WorkspaceID: wsUUID})
	if err != nil {
		return errCannotApply("note not found")
	}
	idx := relocateAnchor(artifact.Body, c.AnchorPrefix.String, quote, c.AnchorSuffix.String)
	if idx < 0 {
		return errCannotApply("the text this suggestion points at has changed — can't apply automatically")
	}
	newBody := artifact.Body[:idx] + c.SuggestionText.String + artifact.Body[idx+len(quote):]

	// Snapshot the pre-accept state, then write the spliced body, then snapshot
	// the result — so the change shows up in version history and is undoable.
	h.snapshotVersion(ctx, noteID, ownerUUID, artifact.Title, artifact.Body, "edit", "", false)
	updated, err := h.Upstream.UpdateArtifact(ctx, db.UpdateArtifactParams{
		ID:            noteID,
		WorkspaceID:   wsUUID,
		Title:         artifact.Title,
		Body:          newBody,
		Metadata:      artifact.Metadata,
		FileUrl:       artifact.FileUrl,
		FileSizeBytes: artifact.FileSizeBytes,
	})
	if err != nil {
		return errCannotApply("failed to apply suggestion")
	}
	h.snapshotVersion(ctx, noteID, ownerUUID, updated.Title, updated.Body, "edit", "", false)
	return nil
}

// relocateAnchor finds the byte offset of the anchored span in body, surviving
// edits by trying progressively weaker matches: prefix+quote+suffix, then
// quote+suffix, then prefix+quote, then quote alone. Returns the index of the
// quote within body, or -1 if it cannot be located.
func relocateAnchor(body, prefix, quote, suffix string) int {
	if quote == "" {
		return -1
	}
	// 1. Full context: prefix+quote+suffix.
	if prefix != "" && suffix != "" {
		if i := strings.Index(body, prefix+quote+suffix); i >= 0 {
			return i + len(prefix)
		}
	}
	// 2. Leading context: prefix+quote.
	if prefix != "" {
		if i := strings.Index(body, prefix+quote); i >= 0 {
			return i + len(prefix)
		}
	}
	// 3. Trailing context: quote+suffix.
	if suffix != "" {
		if i := strings.Index(body, quote+suffix); i >= 0 {
			return i
		}
	}
	// 4. Bare quote (best effort — first occurrence).
	return strings.Index(body, quote)
}

// --- helpers ---

type cannotApplyError struct{ msg string }

func (e cannotApplyError) Error() string { return e.msg }
func errCannotApply(msg string) error    { return cannotApplyError{msg: msg} }

// loadCommentForWrite resolves the note + comment for an author-scoped write
// (edit/delete), returning whether the caller owns the note (note owners may
// moderate any comment). Writes the error + returns ok=false on any failure.
func (h *Handler) loadCommentForWrite(w http.ResponseWriter, r *http.Request) (ctx context.Context, comment cerebrodb.CerebroNoteComment, ownerUUID pgtype.UUID, noteOwned bool, ok bool) {
	userID, good := requireUserID(w, r)
	if !good {
		return
	}
	_, ownerUUID, good = h.scope(w, r, userID)
	if !good {
		return
	}
	noteID, good := pathUUID(w, r)
	if !good {
		return
	}
	if !h.requireCanSee(w, r, noteID, ownerUUID) {
		return
	}
	commentID, good := subPathUUID(w, r, "commentId")
	if !good {
		return
	}
	comment, err := h.Cerebro.GetNoteComment(r.Context(), commentID)
	if err != nil || uuidStr(comment.NoteID) != uuidStr(noteID) {
		writeError(w, http.StatusNotFound, "comment not found")
		return
	}
	noteRow, err := h.Cerebro.GetNote(r.Context(), noteID)
	if err == nil && uuidStr(noteRow.OwnerID) == uuidStr(ownerUUID) {
		noteOwned = true
	}
	return r.Context(), comment, ownerUUID, noteOwned, true
}

func parseUUIDField(s string) (pgtype.UUID, error) {
	return util.ParseUUID(s)
}

func optText(s *string) pgtype.Text {
	if s == nil || *s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func optInt4(i *int32) pgtype.Int4 {
	if i == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *i, Valid: true}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

func int4Ptr(i pgtype.Int4) *int32 {
	if !i.Valid {
		return nil
	}
	v := i.Int32
	return &v
}
