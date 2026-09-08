package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PostPushReplyComment turns a reply-to-a-push into an issue comment and, for
// a narrow explicit approval, records the human-owned review decision.
//
// Exact "审核通过" / "确认审核" replies move an in-review issue to done as the
// replying member. The original comment is still persisted and wakes the
// issue's agent for any follow-on work. Any reply with additional context is
// comment-only: the agent must interpret it rather than the platform guessing
// which part of the workflow the user approved.
//
// Modeled on TaskService.createAgentComment, the other non-HTTP comment path.
func (h *Handler) PostPushReplyComment(
	ctx context.Context,
	push db.ChannelPushMessage,
	senderUserID pgtype.UUID,
	content string,
) (engine.PushReplyResult, error) {
	content, denial, ok := engine.PushReplyPrecondition(
		uuidToString(push.RecipientUserID), uuidToString(senderUserID), content)
	if !ok {
		return denial, nil
	}

	// A ledger row outlives membership; re-check rather than trusting it. Only
	// "no such member" is a denial — a connection reset is a fault the sender
	// cannot act on, and answering it with "你没有权限" would tell a legitimate
	// member they had lost access and drop their reply with nothing to retry.
	if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      senderUserID,
		WorkspaceID: push.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.PushReplyResult{Message: engine.PushReplyDenied}, nil
		}
		return engine.PushReplyResult{}, err
	}

	// quick_create pushes carry no issue: there is no thread to inject into.
	if !push.IssueID.Valid {
		return engine.PushReplyResult{Message: "这条推送不能直接回复决策，请打开 Multica 处理。"}, nil
	}

	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          push.IssueID,
		WorkspaceID: push.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.PushReplyResult{Message: "关联的 issue 已不存在。"}, nil
		}
		return engine.PushReplyResult{}, err
	}

	createParams := db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "member",
		AuthorID:    senderUserID,
		Content:     content,
		Type:        "comment",
	}

	var created db.CreateCommentRow
	var approvedIssue *db.Issue
	if isExplicitReviewApproval(content) {
		// Lock the issue and commit the comment plus review decision together.
		// Without the lock, a stale in_review read could overwrite a concurrent
		// human transition to another state. Comment-first keeps event revisions
		// ordered: comment N, then status N+1.
		tx, txErr := h.TxStarter.Begin(ctx)
		if txErr != nil {
			return engine.PushReplyResult{}, fmt.Errorf("begin push reply approval: %w", txErr)
		}
		defer tx.Rollback(ctx)
		qtx := h.Queries.WithTx(tx)

		current, lockErr := qtx.LockIssueForDescriptionUpdate(ctx, db.LockIssueForDescriptionUpdateParams{
			ID: issue.ID, WorkspaceID: issue.WorkspaceID,
		})
		if lockErr != nil {
			return engine.PushReplyResult{}, fmt.Errorf("lock issue for push reply approval: %w", lockErr)
		}
		issue = current
		created, err = qtx.CreateComment(ctx, createParams)
		if err != nil {
			return engine.PushReplyResult{}, err
		}
		if issuestatus.Effective(ctx, qtx, current.WorkspaceID, current.Status) == issuestatus.InReview {
			updated, updateErr := qtx.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
				ID: current.ID, Status: issuestatus.Done, WorkspaceID: current.WorkspaceID,
			})
			if updateErr != nil {
				return engine.PushReplyResult{}, fmt.Errorf("complete approved issue: %w", updateErr)
			}
			approvedIssue = &updated
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return engine.PushReplyResult{}, fmt.Errorf("commit push reply approval: %w", commitErr)
		}
	} else {
		created, err = h.Queries.CreateComment(ctx, createParams)
		if err != nil {
			return engine.PushReplyResult{}, err
		}
	}
	comment := created.Comment()

	actorID := uuidToString(senderUserID)
	resp := commentToResponse(comment, nil, nil)
	resp.IssueRevision = created.IssueRevision
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "member", actorID, map[string]any{
		"comment":             resp,
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})

	// The wake. originatorUserID is the replying human, so the agent this
	// starts inherits exactly that person's invocation authority — the same
	// value CreateComment passes for a member author
	// (invokeOriginatorFromRequest returns actorID unchanged for "member").
	//
	// Two recipients each approving produces two comments; the second is
	// folded into the pending task by mergeCommentIntoPendingTask rather than
	// starting a second run. Do not add a second guard here.
	h.triggerTasksForComment(ctx, issue, comment, nil, "member", actorID, actorID, nil)
	if approvedIssue != nil {
		h.publishPushReplyApproval(ctx, issue, *approvedIssue, actorID)
		h.notifyParentOfChildDone(ctx, issue, *approvedIssue)
	}

	slog.Info("push reply posted as comment",
		"issue_id", uuidToString(issue.ID),
		"comment_id", uuidToString(comment.ID),
		"channel_type", push.ChannelType,
	)
	if approvedIssue != nil {
		return engine.PushReplyResult{Posted: true, Message: "审核已通过，任务已流转为 done，Multica 会继续处理后续工作。"}, nil
	}
	return engine.PushReplyResult{Posted: true, Message: "已记录审核意见，Multica 会结合任务上下文继续处理。"}, nil
}

func isExplicitReviewApproval(content string) bool {
	switch strings.TrimSpace(content) {
	case "审核通过", "确认审核":
		return true
	default:
		return false
	}
}

// publishPushReplyApproval mirrors the status-change event contract emitted by
// UpdateIssue. Activity and inbox listeners consume this event, so the IM path
// gets the same audit and notification side effects as an in-app member edit.
func (h *Handler) publishPushReplyApproval(ctx context.Context, prev, issue db.Issue, actorID string) {
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	resp := issueToResponse(issue, prefix)
	h.fillStatusCategory(ctx, issue.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "member", actorID, map[string]any{
		"issue":               resp,
		"assignee_changed":    false,
		"status_changed":      true,
		"priority_changed":    false,
		"project_changed":     false,
		"start_date_changed":  false,
		"due_date_changed":    false,
		"description_changed": false,
		"title_changed":       false,
		"prev_title":          prev.Title,
		"prev_assignee_type":  textToPtr(prev.AssigneeType),
		"prev_assignee_id":    uuidToPtr(prev.AssigneeID),
		"prev_status":         prev.Status,
		"prev_priority":       prev.Priority,
		"prev_start_date":     dateToPtr(prev.StartDate),
		"prev_due_date":       dateToPtr(prev.DueDate),
		"prev_description":    textToPtr(prev.Description),
		"creator_type":        prev.CreatorType,
		"creator_id":          uuidToString(prev.CreatorID),
		"source":              "channel_push_reply",
	})
}

// LookupPush finds the push a reply is answering. A miss is not an error:
// nearly every inbound reply is ordinary chat, and the router uses the bool
// to fall through to that path.
func (h *Handler) LookupPush(ctx context.Context, installationID pgtype.UUID, channelMessageID string) (db.ChannelPushMessage, bool, error) {
	if channelMessageID == "" {
		return db.ChannelPushMessage{}, false, nil
	}
	row, err := h.Queries.FindChannelPushMessage(ctx, db.FindChannelPushMessageParams{
		InstallationID:   installationID,
		ChannelMessageID: channelMessageID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelPushMessage{}, false, nil
	}
	if err != nil {
		return db.ChannelPushMessage{}, false, err
	}
	return row, true, nil
}

// Compile-time assertion: a future signature drift on either side fails the
// build rather than silently disabling the feature.
var _ engine.PushReplyPoster = (*Handler)(nil)
