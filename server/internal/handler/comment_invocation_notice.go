package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) noteCommentInvocationDenied(ctx context.Context, issue db.Issue, comment db.Comment, outcomes []CommentTriggerOutcome) {
	var denied []CommentTriggerOutcome
	for _, outcome := range outcomes {
		if outcome.Status == DispatchBlocked && outcome.ReasonCode == ReasonInvocationNotAllowed {
			denied = append(denied, outcome)
		}
	}
	if len(denied) == 0 {
		return
	}
	// Labels come only from the submitted text. Resolving a denied target's
	// name or runtime here would reveal information the caller cannot access.
	labels := make(map[string]string)
	for _, match := range util.MentionRe.FindAllStringSubmatch(comment.Content, -1) {
		key := match[2] + ":" + match[3]
		if _, exists := labels[key]; !exists {
			labels[key] = match[1]
		}
	}
	var content strings.Builder
	fmt.Fprintf(&content, "These mentions did not start when the parent comment was submitted on %s:\n", comment.UpdatedAt.Time.UTC().Format("Jan 2, 2006 at 15:04:05 MST"))
	for _, outcome := range denied {
		label := labels[outcome.TargetType+":"+outcome.TargetID]
		if label == "" {
			label = outcome.TargetType + " " + outcome.TargetID
		}
		fmt.Fprintf(&content, "\n- **%s**: Target not found or permission not granted.", escapeMarkdownInline(label))
	}
	h.noteTriggerFailure(ctx, issue, content.String(), comment.ID)
}

// noteTriggerFailure extends the existing runtime refusal comments. Notices
// are best-effort and bypass the comment-trigger pipeline to avoid new runs.
func (h *Handler) noteTriggerFailure(ctx context.Context, issue db.Issue, content string, parentID pgtype.UUID, logAttrs ...slog.Attr) {
	params := db.CreateCommentParams{
		ID: dbid.NewV7(), IssueID: issue.ID, WorkspaceID: issue.WorkspaceID,
		AuthorType: "system", AuthorID: pgtype.UUID{Valid: true},
		Content: content, Type: "system", ParentID: parentID,
	}
	created, err := h.createTriggerFailureComment(ctx, params)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			logAttrs = append(logAttrs, slog.Any("error", err), slog.String("issue_id", uuidToString(issue.ID)))
			slog.LogAttrs(ctx, slog.LevelWarn, "trigger failure notice: create system comment failed", logAttrs...)
		}
		return
	}
	comment := created.Comment()
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})
}

func (h *Handler) createTriggerFailureComment(ctx context.Context, params db.CreateCommentParams) (db.CreateCommentRow, error) {
	if !params.ParentID.Valid {
		return h.Queries.CreateComment(ctx, params)
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.CreateCommentRow{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	// Use the existing issue -> comment lock order so a deletion cannot leave
	// an orphan reply or attach a new notice to a tombstone.
	parent, err := qtx.LockLiveComment(ctx, db.LockLiveCommentParams{ID: params.ParentID, WorkspaceID: params.WorkspaceID})
	if err != nil {
		return db.CreateCommentRow{}, err
	}
	if parent.IssueID != params.IssueID {
		return db.CreateCommentRow{}, pgx.ErrNoRows
	}
	created, err := qtx.CreateComment(ctx, params)
	if err != nil {
		return db.CreateCommentRow{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.CreateCommentRow{}, err
	}
	return created, nil
}
