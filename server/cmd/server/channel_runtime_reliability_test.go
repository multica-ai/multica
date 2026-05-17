package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/facadeimpl"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestChannelActionIdempotency_CreateIssue(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	actorID := parseUUID(testUserID)
	inboundEventID := insertChannelInboundEventForAction(t, workspaceID)
	title := fmt.Sprintf("channel idempotent create %d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title = $2`, workspaceID, title)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_inbound_event WHERE id = $1`, inboundEventID)
	})

	svc := facadeimpl.NewIssueService(testPool)
	req := facade.CreateIssueReq{
		WorkspaceID:    workspaceID,
		ActorID:        actorID,
		InboundEventID: inboundEventID,
		Title:          title,
	}
	first, err := svc.CreateIssue(ctx, req)
	if err != nil {
		t.Fatalf("first CreateIssue: %v", err)
	}
	second, err := svc.CreateIssue(ctx, req)
	if err != nil {
		t.Fatalf("second CreateIssue: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second issue id = %v, want first id %v", second.ID, first.ID)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2
`, workspaceID, title).Scan(&count); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if count != 1 {
		t.Fatalf("issue count = %d, want 1", count)
	}
}

func TestChannelActionIdempotency_AddComment(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	actorID := parseUUID(testUserID)
	issueID := parseUUID(createTestIssue(t, testWorkspaceID, testUserID))
	inboundEventID := insertChannelInboundEventForAction(t, workspaceID)
	content := fmt.Sprintf("channel idempotent comment %d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_inbound_event WHERE id = $1`, inboundEventID)
	})

	issueSvc := facadeimpl.NewIssueService(testPool)
	commentSvc := facadeimpl.NewCommentService(db.New(testPool), issueSvc)
	req := facade.AddCommentReq{
		IssueID:        issueID,
		ActorID:        actorID,
		InboundEventID: inboundEventID,
		Content:        content,
	}
	first, err := commentSvc.AddComment(ctx, req)
	if err != nil {
		t.Fatalf("first AddComment: %v", err)
	}
	second, err := commentSvc.AddComment(ctx, req)
	if err != nil {
		t.Fatalf("second AddComment: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second comment id = %v, want first id %v", second.ID, first.ID)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
SELECT count(*) FROM comment WHERE issue_id = $1 AND content = $2
`, issueID, content).Scan(&count); err != nil {
		t.Fatalf("count comments: %v", err)
	}
	if count != 1 {
		t.Fatalf("comment count = %d, want 1", count)
	}
}

func TestChannelAction_SetPriorityNoneUsesDatabaseValue(t *testing.T) {
	requirePool(t)
	ctx := context.Background()
	workspaceID := parseUUID(testWorkspaceID)
	actorID := parseUUID(testUserID)
	issueID := parseUUID(createTestIssue(t, testWorkspaceID, testUserID))
	inboundEventID := insertChannelInboundEventForAction(t, workspaceID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_inbound_event WHERE id = $1`, inboundEventID)
	})

	if _, err := testPool.Exec(ctx, `UPDATE issue SET priority = 'high' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("prime issue priority: %v", err)
	}

	svc := facadeimpl.NewIssueService(testPool)
	if err := svc.SetIssuePriority(ctx, issueID, actorID, "none", facade.ChannelMutationContext{InboundEventID: inboundEventID}); err != nil {
		t.Fatalf("SetIssuePriority none: %v", err)
	}

	var priority string
	if err := testPool.QueryRow(ctx, `SELECT priority FROM issue WHERE id = $1`, issueID).Scan(&priority); err != nil {
		t.Fatalf("select priority: %v", err)
	}
	if priority != "none" {
		t.Fatalf("priority = %q, want none", priority)
	}
}

func insertChannelInboundEventForAction(t *testing.T, workspaceID pgtype.UUID) pgtype.UUID {
	t.Helper()
	ctx := context.Background()
	eventID := fmt.Sprintf("evt_action_%d", time.Now().UnixNano())
	var id pgtype.UUID
	if err := testPool.QueryRow(ctx, `
	INSERT INTO channel_inbound_event (
	    provider, connection_id, event_id, event_type, processing_key, chat_id, chat_type,
	    sender_external_id, sender_name, message_id, text, canonical_event,
	    raw_payload, status, phase, workspace_id
	) VALUES (
	    'feishu', 'feishu', $1, 'message_received', $2, 'oc_action_test', 'group',
	    'ou_action_test', 'Action Test', 'om_action_test', 'test', '{}'::jsonb,
	    '{}'::jsonb, 'processing', 'post', $3
	)
RETURNING id
`, eventID, "feishu:group:oc_action_test:ou_action_test:"+eventID, workspaceID).Scan(&id); err != nil {
		t.Fatalf("insert channel_inbound_event: %v", err)
	}
	return id
}
