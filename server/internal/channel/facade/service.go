package facade

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// IssueService is the dependency contract the channel facade requires for
// issue-related behaviour. It is defined here (in the facade package) rather
// than in `internal/service` because:
//
//  1. DESIGN §3.2 forbids facade from importing pkg/db, and a real
//     IssueService implementation today would inevitably touch db types;
//     defining the interface on the consumer side decouples the facade from
//     any particular implementation.
//  2. T2 is a Junior thin-shell task — it intentionally does NOT implement
//     this interface. A later task (e.g. T11 Inbound dispatcher, or a
//     dedicated service-extraction PR) provides the concrete adapter from
//     this interface to the existing handler-layer logic.
//
// All method arguments and return values use facade-owned DTOs / pgtype
// primitives so the facade never sees `db.Issue` etc.
type IssueService interface {
	CreateIssue(ctx context.Context, req CreateIssueReq) (Issue, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (Issue, error)
	GetIssueByIdentifier(ctx context.Context, workspaceID pgtype.UUID, identifier string) (Issue, error)
	SetIssueStatus(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, status string, action ChannelMutationContext) error
	SetIssueAssignee(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, assigneeIdentifier string, action ChannelMutationContext) error
	SetIssuePriority(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, priority string, action ChannelMutationContext) error
	AddIssueLabel(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, action ChannelMutationContext) error
	RemoveIssueLabel(ctx context.Context, id pgtype.UUID, actorID pgtype.UUID, labelName string, action ChannelMutationContext) error
	ListMyTodos(ctx context.Context, workspaceID, userID pgtype.UUID) ([]Issue, error)
}

// CommentService is the dependency contract the channel facade requires for
// comment-related behaviour. Same rationale as IssueService.
type CommentService interface {
	AddComment(ctx context.Context, req AddCommentReq) (Comment, error)
}

// AttachmentService is the dependency contract the channel facade requires for
// attachment-related behaviour.
type AttachmentService interface {
	UploadIssueAttachment(ctx context.Context, req UploadIssueAttachmentReq) (Attachment, error)
}

type IssueDigestService interface {
	GetIssueDigest(ctx context.Context, workspaceID pgtype.UUID, identifier string) (IssueDigest, error)
	GetIssueProgress(ctx context.Context, workspaceID pgtype.UUID, identifier string) (IssueProgress, error)
	ListProjectProgress(ctx context.Context, workspaceID pgtype.UUID) ([]ProjectProgress, error)
	GetIssueDetail(ctx context.Context, workspaceID pgtype.UUID, identifier string) (IssueDetail, error)
	GetIssueTimeline(ctx context.Context, workspaceID pgtype.UUID, identifier string, page, pageSize int) (IssueTimelinePage, error)
	GetIssueLogs(ctx context.Context, workspaceID pgtype.UUID, identifier string, page, pageSize int) (IssueLogPage, error)
}
