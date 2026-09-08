package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestIsExplicitReviewApproval(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "review approved", content: "审核通过", want: true},
		{name: "confirm review", content: "确认审核", want: true},
		{name: "surrounding whitespace", content: "  \n审核通过\t", want: true},
		{name: "approval with instructions", content: "审核通过，请继续 Stage 4", want: false},
		{name: "requested changes", content: "请补充回归测试", want: false},
		{name: "empty", content: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExplicitReviewApproval(tt.content); got != tt.want {
				t.Fatalf("isExplicitReviewApproval(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

// An unambiguous approval is both durable feedback and the human-owned status
// decision. The reply stays as a comment, while the in-review issue enters
// done and emits the ordinary member-authored status event.
func TestPostPushReplyCommentExplicitApprovalCompletesReview(t *testing.T) {
	for _, reply := range []string{"审核通过", "确认审核"} {
		t.Run(reply, func(t *testing.T) {
			ctx := context.Background()
			wsID := dbfx.Workspace(t, "Push Reply Approval", "push-reply-approval-"+uuid.NewString())
			userID := dbfx.User(t, "Push Reply Approver", "push-reply-approval-"+uuid.NewString()+"@multica.ai")
			dbfx.Member(t, wsID, userID, "member")
			issueID := dbfx.Issue(t, "Push reply approval", testutil.Cols{
				"workspace_id": wsID, "status": "in_review",
			})
			dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

			h := *testHandler
			h.Bus = events.New()
			var updated events.Event
			h.Bus.Subscribe(protocol.EventIssueUpdated, func(event events.Event) {
				updated = event
			})

			push := db.ChannelPushMessage{
				WorkspaceID:     parseUUID(wsID),
				RecipientUserID: parseUUID(userID),
				IssueID:         parseUUID(issueID),
			}
			res, err := h.PostPushReplyComment(ctx, push, parseUUID(userID), reply)
			if err != nil {
				t.Fatalf("PostPushReplyComment: %v", err)
			}
			if !res.Posted {
				t.Fatalf("Posted = false, message = %q", res.Message)
			}

			comments := listPushReplyTestComments(t, issueID, wsID)
			if len(comments) != 1 || comments[0].Content != reply {
				t.Fatalf("comments = %#v, want the original approval reply", comments)
			}
			issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
				ID: parseUUID(issueID), WorkspaceID: parseUUID(wsID),
			})
			if err != nil {
				t.Fatalf("GetIssueInWorkspace: %v", err)
			}
			if issue.Status != "done" {
				t.Fatalf("Status = %q, want done", issue.Status)
			}
			if updated.Type != protocol.EventIssueUpdated || updated.ActorType != "member" || updated.ActorID != userID {
				t.Fatalf("updated event = %#v, want member actor %s", updated, userID)
			}
			payload, ok := updated.Payload.(map[string]any)
			if !ok || payload["status_changed"] != true || payload["prev_status"] != "in_review" {
				t.Fatalf("updated payload = %#v, want in_review -> done", updated.Payload)
			}
		})
	}
}

func TestPostPushReplyCommentApprovalDoesNotCompleteANonReviewIssue(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Non Review", "push-reply-nonreview-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Non Review User", "push-reply-nonreview-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")
	issueID := dbfx.Issue(t, "Push reply non-review issue", testutil.Cols{
		"workspace_id": wsID, "status": "in_progress",
	})
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(issueID),
	}
	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "审核通过")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false, message = %q", res.Message)
	}

	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		t.Fatalf("GetIssueInWorkspace: %v", err)
	}
	if issue.Status != "in_progress" {
		t.Fatalf("Status = %q, want in_progress", issue.Status)
	}
	if comments := listPushReplyTestComments(t, issueID, wsID); len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
}

func TestPostPushReplyCommentApprovalCompletesCustomReviewStatus(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Custom Review", "push-reply-custom-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Custom Approver", "push-reply-custom-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")
	statusKey := "review_" + uuid.NewString()[:8]
	dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": wsID,
		"key":          statusKey,
		"name":         "Customer review",
		"category":     "in_review",
		"color":        "#123456",
	})
	issueID := dbfx.Issue(t, "Push reply custom review", testutil.Cols{
		"workspace_id": wsID, "status": statusKey,
	})
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(issueID),
	}
	if _, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "审核通过"); err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		t.Fatalf("GetIssueInWorkspace: %v", err)
	}
	if issue.Status != "done" {
		t.Fatalf("Status = %q, want done", issue.Status)
	}
}

func TestPostPushReplyCommentApprovalNotifiesAParent(t *testing.T) {
	ctx := context.Background()
	fx := newChildDoneFixture(t, "in_progress")
	if _, err := testHandler.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          parseUUID(fx.child.ID),
		Status:      "in_review",
		WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("put child in review: %v", err)
	}

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(testWorkspaceID),
		RecipientUserID: parseUUID(testUserID),
		IssueID:         parseUUID(fx.child.ID),
	}
	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(testUserID), "审核通过")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false, message = %q", res.Message)
	}
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("parent system comments = %d, want 1", got)
	}
}

// The happy path: the person the push was addressed to replies, and the reply
// becomes a member-authored comment on the issue the push was about.
func TestPostPushReplyCommentCreatesAMemberComment(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply", "push-reply-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply User", "push-reply-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")
	issueID := dbfx.Issue(t, "Push reply issue", testutil.Cols{
		"workspace_id": wsID, "status": "in_review",
	})
	// The handler writes this one, so the fixture's own cleanup does not know
	// about it.
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(issueID),
	}

	reply := "审核通过，请继续 Stage 4；父任务暂不设为 done"
	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), reply)
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false, message = %q", res.Message)
	}
	if res.Message != "已记录审核意见，Multica 会结合任务上下文继续处理。" {
		t.Errorf("Message = %q", res.Message)
	}

	comments := listPushReplyTestComments(t, issueID, wsID)
	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1", len(comments))
	}
	if comments[0].AuthorType != "member" {
		t.Errorf("AuthorType = %q, want member", comments[0].AuthorType)
	}
	// The comment must be attributed to the human who replied — an agent
	// woken by it inherits this identity as the originator for its own
	// invocation checks.
	if uuidToString(comments[0].AuthorID) != userID {
		t.Errorf("AuthorID = %v, want the replying user", comments[0].AuthorID)
	}
	if comments[0].Content != reply {
		t.Errorf("Content = %q, want the complete contextual reply %q", comments[0].Content, reply)
	}
	// "comment", never a machine type: this is a human speaking.
	if comments[0].Type != "comment" {
		t.Errorf("Type = %q, want comment", comments[0].Type)
	}
	issue, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          parseUUID(issueID),
		WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		t.Fatalf("GetIssueInWorkspace: %v", err)
	}
	if issue.Status != "in_review" {
		t.Errorf("Status = %q, want in_review: the reply handler must not reduce approval to a direct status transition", issue.Status)
	}
}

// Someone else in the same workspace replying to a push addressed to another
// person must not be able to speak as them.
func TestPostPushReplyCommentRejectsAnotherUser(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Other User", "push-reply-other-"+uuid.NewString())
	ownerID := dbfx.User(t, "Push Reply Owner", "push-reply-owner-"+uuid.NewString()+"@multica.ai")
	otherID := dbfx.User(t, "Push Reply Bystander", "push-reply-other-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, ownerID, "member")
	dbfx.Member(t, wsID, otherID, "member")
	issueID := dbfx.Issue(t, "Push reply issue (other user)", testutil.Cols{"workspace_id": wsID})

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(ownerID),
		IssueID:         parseUUID(issueID),
	}

	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(otherID), "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true; a non-recipient must not be able to reply")
	}
	if res.Message == "" {
		t.Error("denial carried no message; the user would see silence")
	}
	assertNoPushReplyComments(t, issueID, wsID)
}

// A ledger row outlives membership. Removal from the workspace must revoke
// the reply path even though the row still points at the issue.
func TestPostPushReplyCommentRejectsANonMember(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Non Member", "push-reply-nonmember-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Departed User", "push-reply-nonmember-"+uuid.NewString()+"@multica.ai")
	issueID := dbfx.Issue(t, "Push reply issue (non member)", testutil.Cols{"workspace_id": wsID})
	// deliberately no dbfx.Member

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(issueID),
	}

	res, _ := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "确认审核")
	if res.Posted {
		t.Fatal("Posted = true for a user who is no longer a member")
	}
	assertNoPushReplyComments(t, issueID, wsID)
}

// quick_create_failed pushes have no issue, so there is nowhere to inject.
// The user gets told rather than ignored.
func TestPostPushReplyCommentWithoutAnIssueExplainsItself(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply No Issue", "push-reply-noissue-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply No Issue User", "push-reply-noissue-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         pgtype.UUID{}, // NULL
	}

	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true with no issue to post to")
	}
	if res.Message == "" {
		t.Error("no explanation for an unreplyable push")
	}
}

// An issue deleted between push and reply must not 500 the inbound pipeline.
func TestPostPushReplyCommentOnAMissingIssue(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Missing Issue", "push-reply-missing-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Missing Issue User", "push-reply-missing-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID("00000000-0000-7000-8000-000000000001"),
	}

	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment returned err %v; want a denial result", err)
	}
	if res.Posted {
		t.Fatal("Posted = true for a missing issue")
	}
}

// Empty content (an IM sticker or an image-only reply) is not a decision.
func TestPostPushReplyCommentRejectsEmptyContent(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Empty Content", "push-reply-empty-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Empty Content User", "push-reply-empty-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")
	issueID := dbfx.Issue(t, "Push reply issue (empty content)", testutil.Cols{"workspace_id": wsID})

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(issueID),
	}

	res, _ := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "   ")
	if res.Posted {
		t.Fatal("Posted = true for whitespace-only content")
	}
	assertNoPushReplyComments(t, issueID, wsID)
}

// A ledger row names both a workspace and an issue. If they ever disagree —
// a corrupted row, a bug upstream — the reply must not cross into a workspace
// the sender's membership was never checked against. The issue lookup is
// workspace-scoped so the two cannot come apart.
func TestPostPushReplyCommentRejectsAnIssueInAnotherWorkspace(t *testing.T) {
	ctx := context.Background()
	wsID := dbfx.Workspace(t, "Push Reply Home", "push-reply-home-"+uuid.NewString())
	otherWsID := dbfx.Workspace(t, "Push Reply Foreign", "push-reply-foreign-"+uuid.NewString())
	userID := dbfx.User(t, "Push Reply Crosser", "push-reply-cross-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")
	foreignIssueID := dbfx.Issue(t, "Issue in another workspace", testutil.Cols{"workspace_id": otherWsID})

	push := db.ChannelPushMessage{
		WorkspaceID:     parseUUID(wsID),
		RecipientUserID: parseUUID(userID),
		IssueID:         parseUUID(foreignIssueID),
	}

	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if res.Posted {
		t.Fatal("Posted = true against an issue outside the push's workspace")
	}
	assertNoPushReplyComments(t, foreignIssueID, otherWsID)
}

func listPushReplyTestComments(t *testing.T, issueID, workspaceID string) []db.Comment {
	t.Helper()
	comments, err := testHandler.Queries.ListCommentsForIssue(context.Background(), db.ListCommentsForIssueParams{
		IssueID:     parseUUID(issueID),
		WorkspaceID: parseUUID(workspaceID),
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("ListCommentsForIssue: %v", err)
	}
	return comments
}

func assertNoPushReplyComments(t *testing.T, issueID, workspaceID string) {
	t.Helper()
	comments := listPushReplyTestComments(t, issueID, workspaceID)
	if len(comments) != 0 {
		t.Fatalf("got %d comments, want none — a denied reply must not write", len(comments))
	}
}
