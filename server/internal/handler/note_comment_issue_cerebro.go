package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebronote "github.com/multica-ai/multica/server/internal/cerebro/note"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) ValidateIssueFromNoteCommentAssignee(ctx context.Context, r *http.Request, workspaceID, _ pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) (int, string) {
	return h.validateAssigneePair(ctx, r, uuidToString(workspaceID), pgtype.Text{String: assigneeType, Valid: true}, assigneeID)
}

// CreateIssueFromNoteComment adapts *Handler to cerebronote.IssueCreator
// (FIR-3102). It creates a standalone issue seeded from one note comment,
// routing through the shared IssueService.Create so the new issue gets the
// identical counter, top-of-column position, issue:created broadcast (full
// IssueResponse, same shape the board already renders), analytics and on-assign
// enqueue as every other create path. The issue is stamped
// origin_type='note_comment', origin_id=<comment id> for the reverse backlink;
// the forward link (comment -> issue) is written by the note handler after this
// returns. Wired in router.go via cerebroNoteHandler.Issues = h.
func (h *Handler) CreateIssueFromNoteComment(ctx context.Context, in cerebronote.IssueFromCommentInput) (cerebronote.IssueFromCommentResult, error) {
	origin := db.GetIssueByOriginParams{
		WorkspaceID: in.WorkspaceID,
		OriginType:  pgtype.Text{String: "note_comment", Valid: true},
		OriginID:    in.CommentID,
	}
	// A retry after issue creation but before both links were saved must repair
	// the links around the existing issue, never create a second issue.
	if existing, err := h.Queries.GetIssueByOrigin(ctx, origin); err == nil {
		return cerebronote.IssueFromCommentResult{IssueID: existing.ID, Number: existing.Number}, nil
	} else if err != pgx.ErrNoRows {
		return cerebronote.IssueFromCommentResult{}, err
	}

	// Pre-compute the prefix once so the broadcast payload and the returned
	// issue share the same MUL-123 rendering.
	prefix := h.getIssuePrefix(ctx, in.WorkspaceID)

	params := service.IssueCreateParams{
		WorkspaceID: in.WorkspaceID,
		Title:       in.Title,
		Description: pgtype.Text{String: in.Description, Valid: true},
		Status:      "todo",
		Priority:    "none",
		CreatorType: in.CreatorType,
		CreatorID:   in.CreatorID,
		ProjectID:   in.ProjectID,
		OriginType:  pgtype.Text{String: "note_comment", Valid: true},
		OriginID:    in.CommentID,
	}
	if in.AssigneeType != "" && in.AssigneeID.Valid {
		params.AssigneeType = pgtype.Text{String: in.AssigneeType, Valid: true}
		params.AssigneeID = in.AssigneeID
	}

	res, err := h.IssueService.Create(ctx, params, service.IssueCreateOpts{
		ActorID: uuidToString(in.CreatorID),
		BroadcastPayload: func(issue db.Issue, atts []db.Attachment) map[string]any {
			payload := issueToResponse(issue, prefix)
			if len(atts) > 0 {
				out := make([]AttachmentResponse, len(atts))
				for i, a := range atts {
					out[i] = h.attachmentToResponse(a)
				}
				payload.Attachments = out
			}
			return map[string]any{"issue": payload}
		},
	})
	if err != nil {
		// The unique origin index resolves concurrent requests: the loser reads
		// the winner and continues the idempotent link-up path in the note handler.
		if existing, lookupErr := h.Queries.GetIssueByOrigin(ctx, origin); lookupErr == nil {
			return cerebronote.IssueFromCommentResult{IssueID: existing.ID, Number: existing.Number}, nil
		}
		return cerebronote.IssueFromCommentResult{}, err
	}
	return cerebronote.IssueFromCommentResult{
		IssueID: res.Issue.ID,
		Number:  res.Issue.Number,
	}, nil
}
