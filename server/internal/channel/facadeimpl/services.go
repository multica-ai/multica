package facadeimpl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type IssueService struct {
	pool *pgxpool.Pool
}

func NewIssueService(pool *pgxpool.Pool) *IssueService {
	return &IssueService{pool: pool}
}

func (s *IssueService) CreateIssue(ctx context.Context, req facade.CreateIssueReq) (facade.Issue, error) {
	payload, err := withChannelAction(ctx, s.pool, req.InboundEventID, channelActionCreateIssue, func(tx pgx.Tx) (channelActionPayload, error) {
		var number int32
		if err := tx.QueryRow(ctx, `
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1
			RETURNING issue_counter
		`, req.WorkspaceID).Scan(&number); err != nil {
			return channelActionPayload{}, fmt.Errorf("bump issue counter: %w", err)
		}

		queries := db.New(tx)
		issue, err := queries.CreateIssue(ctx, db.CreateIssueParams{
			WorkspaceID: req.WorkspaceID,
			Title:       req.Title,
			Description: pgtype.Text{String: req.Description, Valid: req.Description != ""},
			Status:      "todo",
			Priority:    "none",
			CreatorType: "member",
			CreatorID:   req.ActorID,
			Number:      number,
			ProjectID:   req.ProjectID,
		})
		if err != nil {
			return channelActionPayload{}, fmt.Errorf("insert issue: %w", err)
		}
		if strings.TrimSpace(req.AssigneeIdentifier) != "" {
			if err := setIssueAssigneeTx(ctx, tx, issue.ID, req.AssigneeIdentifier); err != nil {
				return channelActionPayload{}, err
			}
		}
		return channelActionPayload{IssueID: util.UUIDToString(issue.ID)}, nil
	})
	if err != nil {
		return facade.Issue{}, err
	}
	issueID, err := payloadUUID(payload.IssueID)
	if err != nil {
		return facade.Issue{}, err
	}
	issue, err := db.New(s.pool).GetIssue(ctx, issueID)
	if err != nil {
		return facade.Issue{}, err
	}
	return s.toFacadeIssue(ctx, issue)
}

func (s *IssueService) GetIssue(ctx context.Context, id pgtype.UUID) (facade.Issue, error) {
	issue, err := db.New(s.pool).GetIssue(ctx, id)
	if err != nil {
		return facade.Issue{}, err
	}
	return s.toFacadeIssue(ctx, issue)
}

func (s *IssueService) GetIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, identifier string) (facade.Issue, error) {
	var issue db.Issue
	err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
		       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
		       i.parent_issue_id, i.acceptance_criteria, i.context_refs, i.position,
		       i.due_date, i.created_at, i.updated_at, i.number, i.project_id,
		       i.origin_type, i.origin_id, i.first_executed_at
		FROM issue i
		JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1
		  AND (w.issue_prefix || '-' || i.number::text) = $2
	`, workspaceID, identifier).Scan(
		&issue.ID,
		&issue.WorkspaceID,
		&issue.Title,
		&issue.Description,
		&issue.Status,
		&issue.Priority,
		&issue.AssigneeType,
		&issue.AssigneeID,
		&issue.CreatorType,
		&issue.CreatorID,
		&issue.ParentIssueID,
		&issue.AcceptanceCriteria,
		&issue.ContextRefs,
		&issue.Position,
		&issue.DueDate,
		&issue.CreatedAt,
		&issue.UpdatedAt,
		&issue.Number,
		&issue.ProjectID,
		&issue.OriginType,
		&issue.OriginID,
		&issue.FirstExecutedAt,
	)
	if err != nil {
		return facade.Issue{}, err
	}
	return s.toFacadeIssue(ctx, issue)
}

func (s *IssueService) SetIssueStatus(ctx context.Context, id pgtype.UUID, _ pgtype.UUID, status string, action facade.ChannelMutationContext) error {
	_, err := withChannelAction(ctx, s.pool, action.InboundEventID, channelActionSetStatus, func(tx pgx.Tx) (channelActionPayload, error) {
		if _, err := db.New(tx).UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: id, Status: status}); err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(id)}, nil
	})
	return err
}

func (s *IssueService) SetIssueAssignee(ctx context.Context, id pgtype.UUID, _ pgtype.UUID, assigneeIdentifier string, action facade.ChannelMutationContext) error {
	_, err := withChannelAction(ctx, s.pool, action.InboundEventID, channelActionSetAssignee, func(tx pgx.Tx) (channelActionPayload, error) {
		if err := setIssueAssigneeTx(ctx, tx, id, assigneeIdentifier); err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(id)}, nil
	})
	return err
}

func setIssueAssigneeTx(ctx context.Context, tx pgx.Tx, id pgtype.UUID, assigneeIdentifier string) error {
	var assigneeID pgtype.UUID
	clean := strings.TrimPrefix(assigneeIdentifier, "@")
	if err := tx.QueryRow(ctx, `
			SELECT m.user_id
			FROM member m
			JOIN issue i ON i.workspace_id = m.workspace_id
		LEFT JOIN "user" u ON u.id = m.user_id
		WHERE i.id = $1
		  AND (u.name = $2 OR m.user_id::text = $2)
		LIMIT 1
	`, id, clean).Scan(&assigneeID); err != nil {
		return fmt.Errorf("user %s is not in this workspace: %w", assigneeIdentifier, err)
	}
	_, err := tx.Exec(ctx, `
			UPDATE issue SET assignee_type = 'member', assignee_id = $1, updated_at = now()
			WHERE id = $2
	`, assigneeID, id)
	return err
}

func (s *IssueService) SetIssuePriority(ctx context.Context, id pgtype.UUID, _ pgtype.UUID, priority string, action facade.ChannelMutationContext) error {
	valid := map[string]bool{"urgent": true, "high": true, "medium": true, "low": true, "no_priority": true, "none": true}
	if !valid[priority] {
		return fmt.Errorf("unsupported priority %q", priority)
	}
	if priority == "no_priority" {
		priority = "none"
	}
	_, err := withChannelAction(ctx, s.pool, action.InboundEventID, channelActionSetPriority, func(tx pgx.Tx) (channelActionPayload, error) {
		if _, err := tx.Exec(ctx, `UPDATE issue SET priority = $1, updated_at = now() WHERE id = $2`, priority, id); err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(id)}, nil
	})
	return err
}

func (s *IssueService) AddIssueLabel(ctx context.Context, id pgtype.UUID, _ pgtype.UUID, labelName string, action facade.ChannelMutationContext) error {
	_, err := withChannelAction(ctx, s.pool, action.InboundEventID, channelActionAddLabel, func(tx pgx.Tx) (channelActionPayload, error) {
		wsID, labelID, err := resolveIssueLabelTx(ctx, tx, id, labelName)
		if err != nil {
			return channelActionPayload{}, err
		}
		if err := db.New(tx).AttachLabelToIssue(ctx, db.AttachLabelToIssueParams{IssueID: id, LabelID: labelID, WorkspaceID: wsID}); err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(id)}, nil
	})
	return err
}

func (s *IssueService) RemoveIssueLabel(ctx context.Context, id pgtype.UUID, _ pgtype.UUID, labelName string, action facade.ChannelMutationContext) error {
	_, err := withChannelAction(ctx, s.pool, action.InboundEventID, channelActionRemoveLabel, func(tx pgx.Tx) (channelActionPayload, error) {
		wsID, labelID, err := resolveIssueLabelTx(ctx, tx, id, labelName)
		if err != nil {
			return channelActionPayload{}, err
		}
		if err := db.New(tx).DetachLabelFromIssue(ctx, db.DetachLabelFromIssueParams{IssueID: id, LabelID: labelID, WorkspaceID: wsID}); err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(id)}, nil
	})
	return err
}

func (s *IssueService) ListMyTodos(ctx context.Context, workspaceID, userID pgtype.UUID) ([]facade.Issue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workspace_id, title, status, number
		FROM issue
		WHERE workspace_id = $1
		  AND assignee_type = 'member'
		  AND assignee_id = $2
		  AND status NOT IN ('done', 'canceled')
		ORDER BY updated_at DESC
		LIMIT 10
	`, workspaceID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []facade.Issue{}
	for rows.Next() {
		var issue db.Issue
		if err := rows.Scan(&issue.ID, &issue.WorkspaceID, &issue.Title, &issue.Status, &issue.Number); err != nil {
			return nil, err
		}
		f, err := s.toFacadeIssue(ctx, issue)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *IssueService) resolveIssueLabel(ctx context.Context, issueID pgtype.UUID, labelName string) (pgtype.UUID, pgtype.UUID, error) {
	return resolveIssueLabelTx(ctx, s.pool, issueID, labelName)
}

func resolveIssueLabelTx(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, issueID pgtype.UUID, labelName string) (pgtype.UUID, pgtype.UUID, error) {
	var wsID, labelID pgtype.UUID
	if err := q.QueryRow(ctx, `SELECT workspace_id FROM issue WHERE id = $1`, issueID).Scan(&wsID); err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	if err := q.QueryRow(ctx, `
		SELECT id FROM issue_label WHERE workspace_id = $1 AND name = $2
	`, wsID, labelName).Scan(&labelID); err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("label %q not found: %w", labelName, err)
	}
	return wsID, labelID, nil
}

func (s *IssueService) toFacadeIssue(ctx context.Context, issue db.Issue) (facade.Issue, error) {
	var prefix string
	if err := s.pool.QueryRow(ctx, `SELECT issue_prefix FROM workspace WHERE id = $1`, issue.WorkspaceID).Scan(&prefix); err != nil {
		return facade.Issue{}, err
	}
	return facade.Issue{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Identifier:  fmt.Sprintf("%s-%d", prefix, issue.Number),
		Title:       issue.Title,
		Status:      issue.Status,
	}, nil
}

type CommentService struct {
	queries  *db.Queries
	issueSvc *IssueService
}

func NewCommentService(queries *db.Queries, issueSvc *IssueService) *CommentService {
	return &CommentService{queries: queries, issueSvc: issueSvc}
}

func (s *CommentService) AddComment(ctx context.Context, req facade.AddCommentReq) (facade.Comment, error) {
	payload, err := withChannelAction(ctx, s.issueSvc.pool, req.InboundEventID, channelActionAddComment, func(tx pgx.Tx) (channelActionPayload, error) {
		issue, err := db.New(tx).GetIssue(ctx, req.IssueID)
		if err != nil {
			return channelActionPayload{}, err
		}
		comment, err := db.New(tx).CreateComment(ctx, db.CreateCommentParams{
			IssueID:     req.IssueID,
			WorkspaceID: issue.WorkspaceID,
			AuthorType:  "member",
			AuthorID:    req.ActorID,
			Content:     req.Content,
			Type:        "comment",
		})
		if err != nil {
			return channelActionPayload{}, err
		}
		return channelActionPayload{IssueID: util.UUIDToString(req.IssueID), CommentID: util.UUIDToString(comment.ID)}, nil
	})
	if err != nil {
		return facade.Comment{}, err
	}
	commentID, err := payloadUUID(payload.CommentID)
	if err != nil {
		return facade.Comment{}, err
	}
	comment, err := s.queries.GetComment(ctx, commentID)
	if err != nil {
		return facade.Comment{}, err
	}
	return facade.Comment{
		ID:      comment.ID,
		IssueID: comment.IssueID,
		Content: comment.Content,
	}, nil
}

type AttachmentService struct {
	queries *db.Queries
}

func NewAttachmentService(queries *db.Queries) *AttachmentService {
	return &AttachmentService{queries: queries}
}

func (s *AttachmentService) UploadIssueAttachment(ctx context.Context, req facade.UploadIssueAttachmentReq) (facade.Attachment, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return facade.Attachment{}, fmt.Errorf("generate attachment id: %w", err)
	}
	att, err := s.queries.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID:           pgtype.UUID{Bytes: id, Valid: true},
		WorkspaceID:  req.WorkspaceID,
		IssueID:      req.IssueID,
		UploaderType: req.UploaderType,
		UploaderID:   req.UploaderID,
		Filename:     req.Filename,
		Url:          req.URL,
		ContentType:  req.ContentType,
		SizeBytes:    req.SizeBytes,
	})
	if err != nil {
		return facade.Attachment{}, err
	}
	return facade.Attachment{ID: att.ID}, nil
}

var (
	_ facade.IssueService      = (*IssueService)(nil)
	_ facade.CommentService    = (*CommentService)(nil)
	_ facade.AttachmentService = (*AttachmentService)(nil)
)
