package note

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

// IssueCreator creates an issue seeded from a single note comment. It is a
// narrow seam (like TaskDispatcher) so this package never imports the service
// package: cmd/server wires a small adapter over *service.IssueService.
type IssueCreator interface {
	CreateIssueFromNoteComment(ctx context.Context, in IssueFromCommentInput) (IssueFromCommentResult, error)
	ValidateIssueFromNoteCommentAssignee(ctx context.Context, r *http.Request, workspaceID, actorID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (int, string)
}

// IssueFromCommentInput is the already-resolved input for creating an issue from
// a note comment. OriginType/OriginID stamping (issue -> source comment) is the
// adapter's responsibility; this struct just names the source comment.
type IssueFromCommentInput struct {
	WorkspaceID  pgtype.UUID
	Title        string
	Description  string
	CreatorType  string
	CreatorID    pgtype.UUID
	ProjectID    pgtype.UUID // optional (zero value = none)
	AssigneeType string      // optional ("" = none)
	AssigneeID   pgtype.UUID // optional (zero value = none)
	CommentID    pgtype.UUID // source comment, stamped as the issue's origin
}

// IssueFromCommentResult is the created issue's identity.
type IssueFromCommentResult struct {
	IssueID pgtype.UUID
	Number  int32
}

// createIssueFromCommentRequest is the client payload. Every field is optional:
// with no title the comment's first line is used; project/assignee are left
// unset when omitted so the issue lands in the workspace inbox unassigned.
type createIssueFromCommentRequest struct {
	Title        string `json:"title"`
	ProjectID    string `json:"project_id"`
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
}

// createIssueFromCommentResponse returns the new issue plus the updated comment
// (now carrying issue_id) so the client can render "opened <issue>" on the exact
// comment without a refetch.
type createIssueFromCommentResponse struct {
	IssueID string          `json:"issue_id"`
	Number  int32           `json:"number"`
	Comment CommentResponse `json:"comment"`
}

// CreateIssueFromComment (FIR-3102) creates a new issue from one note comment.
// The comment's text becomes the issue body, its first line the default title,
// and the comment is pointed at the new issue (cerebro_note_comment.issue_id)
// so the note UI shows the issue on that exact comment. The issue is stamped
// origin_type='note_comment', origin_id=<comment id> for the reverse backlink.
//
// Anyone who can comment on the note may do this (same access level as posting a
// comment). Per the note<->issue model, this couples at the COMMENT level: the
// note keeps its own single issue; the comment gets its own separate one.
func (h *Handler) CreateIssueFromComment(w http.ResponseWriter, r *http.Request) {
	if h.Issues == nil {
		writeError(w, http.StatusServiceUnavailable, "issue creation is not configured")
		return
	}
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
	if !h.requireCanComment(w, r, noteID, ownerUUID) {
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
	// A comment owns at most one standalone issue. Treat a retried request as a
	// successful idempotent replay instead of creating a hidden duplicate.
	if comment.IssueID.Valid {
		writeJSON(w, http.StatusOK, createIssueFromCommentResponse{
			IssueID: uuidStr(comment.IssueID),
			Comment: commentToResponse(comment),
		})
		return
	}

	var req createIssueFromCommentRequest
	// An empty body is valid (all fields optional); decodeJSON tolerates it.
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = firstLineTitle(comment.Body)
	}
	if title == "" {
		writeError(w, http.StatusBadRequest, "cannot create an issue from an empty comment — provide a title")
		return
	}

	in := IssueFromCommentInput{
		WorkspaceID: wsUUID,
		Title:       title,
		Description: comment.Body,
		CreatorType: "member",
		CreatorID:   ownerUUID,
		CommentID:   commentID,
	}
	if pid := strings.TrimSpace(req.ProjectID); pid != "" {
		parsed, perr := util.ParseUUID(pid)
		if perr != nil {
			writeError(w, http.StatusBadRequest, "invalid project_id")
			return
		}
		in.ProjectID = parsed
	}
	assigneeType := strings.TrimSpace(req.AssigneeType)
	assigneeID := strings.TrimSpace(req.AssigneeID)
	if (assigneeType == "") != (assigneeID == "") {
		writeError(w, http.StatusBadRequest, "assignee_type and assignee_id must be provided together")
		return
	}
	if assigneeID != "" {
		parsed, aerr := util.ParseUUID(assigneeID)
		if aerr != nil {
			writeError(w, http.StatusBadRequest, "invalid assignee_id")
			return
		}
		in.AssigneeID = parsed
		in.AssigneeType = assigneeType
		if status, message := h.Issues.ValidateIssueFromNoteCommentAssignee(r.Context(), r, wsUUID, ownerUUID, in.AssigneeType, in.AssigneeID); status != 0 {
			writeError(w, status, message)
			return
		}
	}

	res, err := h.Issues.CreateIssueFromNoteComment(r.Context(), in)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	metadata, err := json.Marshal(map[string]string{
		"note_id":    uuidStr(noteID),
		"comment_id": uuidStr(commentID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build issue backlink")
		return
	}
	linkParams := cerebrodb.InsertReferenceParams{
		IssueID:       res.IssueID,
		Object:        "note_comment",
		Type:          pgtype.Text{String: "source", Valid: true},
		RefID:         uuidStr(commentID),
		Label:         pgtype.Text{String: "Source note comment", Valid: true},
		Metadata:      metadata,
		CreatedByType: "member",
		CreatedByID:   ownerUUID,
	}
	commentParams := cerebrodb.SetNoteCommentIssueParams{
		ID:      commentID,
		IssueID: res.IssueID,
	}

	// Save both directions together. Issue creation itself is idempotently
	// recoverable through its unique note_comment origin, so a retry repairs a
	// failed link transaction without ever creating a second issue.
	var updated cerebrodb.CerebroNoteComment
	if h.Pool != nil {
		tx, txErr := h.Pool.Begin(r.Context())
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "issue created but failed to link it to the note comment")
			return
		}
		defer tx.Rollback(r.Context())
		queries := h.Cerebro.WithTx(tx)
		if _, txErr = queries.InsertReference(r.Context(), linkParams); txErr == nil {
			updated, txErr = queries.SetNoteCommentIssue(r.Context(), commentParams)
		}
		if txErr == nil {
			txErr = tx.Commit(r.Context())
		}
		if txErr != nil {
			writeError(w, http.StatusInternalServerError, "issue created but failed to link it to the note comment")
			return
		}
	} else {
		// Tests and intentionally pool-less embeddings keep the same behavior.
		if _, err = h.Cerebro.InsertReference(r.Context(), linkParams); err == nil {
			updated, err = h.Cerebro.SetNoteCommentIssue(r.Context(), commentParams)
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "issue created but failed to link it to the comment")
		return
	}

	writeJSON(w, http.StatusCreated, createIssueFromCommentResponse{
		IssueID: uuidStr(res.IssueID),
		Number:  res.Number,
		Comment: commentToResponse(updated),
	})
}

// firstLineTitle derives a one-line issue title from a comment body: the first
// non-empty line, trimmed, capped at 120 runes so a long paragraph does not
// become an unwieldy title. Returns "" when the body is empty/whitespace.
func firstLineTitle(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 120 {
			return strings.TrimSpace(string(runes[:120]))
		}
		return line
	}
	return ""
}
