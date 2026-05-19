// Package facade is the single-direction outlet from the channel layer
// (adapter / inbound / outbound / intent / binding) into Multica's existing
// services. Per DESIGN §3.2 the channel/facade package is the ONLY place
// channel-layer code is allowed to reach toward issue / comment behaviour;
// it must NOT import the persistence layer (pkg/db). The AST-level test in
// import_test.go enforces that boundary.
//
// The package is intentionally a thin shell: it defines its own DTOs and
// service interfaces, and delegates to caller-injected implementations. No
// business logic lives here — see DESIGN §4.2 ("薄壳，调既有 service，不写业务").
package facade

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Issue is the facade-layer projection of an issue. It deliberately exposes
// only the fields the channel layer needs today (DESIGN §4.2 thin shell);
// new fields are added when a real caller demands them, not preemptively.
type Issue struct {
	ID          pgtype.UUID
	WorkspaceID pgtype.UUID
	Identifier  string
	Title       string
	Status      string
}

// Comment is the facade-layer projection of a comment. Same minimalism rule
// as Issue applies.
type Comment struct {
	ID      pgtype.UUID
	IssueID pgtype.UUID
	Content string
}

type ChannelMutationContext struct {
	InboundEventID pgtype.UUID
}

// CreateIssueReq carries the inputs CreateIssue needs from the channel layer.
// ActorID is the Multica user_id resolved from `channel_user_binding`; it is
// passed through verbatim so existing service-level permission checks stay
// the single source of truth (TC-facade-1).
type CreateIssueReq struct {
	WorkspaceID        pgtype.UUID
	ActorID            pgtype.UUID
	ProjectID          pgtype.UUID
	InboundEventID     pgtype.UUID
	Title              string
	Description        string
	AssigneeIdentifier string
}

// AddCommentReq carries the inputs AddComment needs from the channel layer.
// Content is forwarded verbatim — the facade does no sanitisation; the
// existing service layer's validation is the single source of truth
// (TC-facade-2 / PRD E9).
type AddCommentReq struct {
	IssueID        pgtype.UUID
	ActorID        pgtype.UUID
	InboundEventID pgtype.UUID
	Content        string
}

// Attachment is the facade-layer projection of an attachment record.
type Attachment struct {
	ID pgtype.UUID
}

// UploadIssueAttachmentReq carries the inputs needed to create an attachment
// record from the channel layer.
type UploadIssueAttachmentReq struct {
	WorkspaceID  pgtype.UUID
	IssueID      pgtype.UUID
	UploaderID   pgtype.UUID
	UploaderType string
	Filename     string
	URL          string
	ContentType  string
	SizeBytes    int64
}

// AttachmentFacade is the channel-layer entry point for attachment operations.
// Same single-direction dependency contract as IssueFacade (DESIGN §3.2).
type AttachmentFacade interface {
	UploadIssueAttachment(ctx context.Context, req UploadIssueAttachmentReq) (Attachment, error)
}

type IssueDigest struct {
	Issue         IssueDigestIssue
	ProjectName   string
	AssigneeName  string
	AssigneeType  string
	CreatorName   string
	CreatorType   string
	WorkspaceSlug string
	Labels        []string
	RecentEvents  []IssueDigestEvent
	AgentSummary  *IssueAgentSummary
}

type IssueDigestIssue struct {
	ID          pgtype.UUID
	WorkspaceID pgtype.UUID
	Identifier  string
	Title       string
	Description string
	Status      string
	Priority    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type IssueDigestEvent struct {
	Kind      string
	ActorName string
	Summary   string
	CreatedAt time.Time
}

type IssueAgentSummary struct {
	TaskID        string
	AgentName     string
	Status        string
	Progress      string
	ResultSummary string
	FailureReason string
	UpdatedAt     time.Time
}

type IssueDetail struct {
	Digest        IssueDigest
	StatusHistory []IssueDigestEvent
}

type IssueTimelinePage struct {
	Issue    IssueDigestIssue
	Events   []IssueDigestEvent
	Page     int
	PageSize int
	HasMore  bool
}

type IssueLogPage struct {
	Issue         IssueDigestIssue
	TaskID        string
	AgentName     string
	TaskStatus    string
	ResultSummary string
	FailureReason string
	Messages      []IssueTaskLogEvent
	Page          int
	PageSize      int
	HasMore       bool
}

type IssueTaskLogEvent struct {
	Seq       int32
	Type      string
	Tool      string
	Content   string
	CreatedAt time.Time
}

type IssueProgress struct {
	Digest          IssueDigest
	LatestReply     *IssueProgressReply
	LatestStatus    *IssueDigestEvent
	RecommendedNext string
}

type IssueProgressReply struct {
	AuthorType string
	AuthorName string
	Content    string
	CreatedAt  time.Time
}

type ProjectProgress struct {
	ProjectID   pgtype.UUID
	ProjectName string
	Total       int64
	Open        int64
	InProgress  int64
	InReview    int64
	Blocked     int64
	Done        int64
	FocusIssues []ProjectProgressIssue
}

type ProjectProgressIssue struct {
	Identifier string
	Title      string
	Status     string
	Assignee   string
	UpdatedAt  time.Time
}
