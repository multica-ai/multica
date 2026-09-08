package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/notify"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The full loop the design exists to deliver: an in_review inbox item becomes
// a DM, the DM's message id is recorded, a reply to that id resolves back to
// the issue, and the resulting comment fires the trigger that wakes the agent.
//
// Split across packages in production (notify sends, engine routes, handler
// posts the comment); asserted here in one place because no single package
// sees both ends.
func TestLarkPushReplyRoundTrip(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wsID := dbfx.Workspace(t, "Push Round Trip", "push-roundtrip-"+uuid.NewString())
	userID := dbfx.User(t, "Push Round Trip Recipient", "push-roundtrip-"+uuid.NewString()+"@multica.ai")
	dbfx.Member(t, wsID, userID, "member")

	runtimeID := dbfx.Runtime(t, "push-roundtrip-runtime", testutil.Cols{
		"workspace_id": wsID, "owner_id": userID,
	})
	// The owner must be the replying user: canInvokeAgent's only pass for a
	// private agent is "the agent owner may always invoke their own agent",
	// and the round trip must not depend on a separate permission grant.
	agentID := dbfx.Agent(t, "Push Round Trip Assignee", runtimeID, testutil.Cols{
		"workspace_id": wsID, "owner_id": userID,
	})
	issueID := dbfx.Issue(t, "Needs review", testutil.Cols{
		"workspace_id": wsID, "status": "in_review",
		"assignee_type": "agent", "assignee_id": agentID,
	})

	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id": wsID, "agent_id": agentID, "channel_type": "feishu",
		"installer_user_id": userID,
	})
	dbfx.Insert(t, "channel_user_binding", testutil.Cols{
		"workspace_id": wsID, "multica_user_id": userID, "installation_id": installationID,
		"channel_type": "feishu", "channel_user_id": "ou_recipient",
	})
	// Rows the handlers under test write themselves, so the fixture's own
	// cleanup (registered at insert time, for rows THIS test inserted) does
	// not know about them.
	dbfx.Cleanup(t, `DELETE FROM channel_push_message WHERE installation_id = $1`, installationID)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

	// --- push half -------------------------------------------------------
	fake := &recordingDeliverer{messageID: "om_push_1"}
	n := notify.New(testHandler.Queries, nil, nil)
	n.Register(map[string]notify.DMDeliverer{"feishu": fake})

	n.HandleInboxNew(inboxNewEvent(t, wsID, userID, issueID, "status_changed", "in_review"))

	if fake.calls != 1 {
		t.Fatalf("adapter called %d times, want 1", fake.calls)
	}

	// --- the ledger row is the hinge -------------------------------------
	push, ok, err := testHandler.LookupPush(ctx, parseUUID(installationID), "om_push_1")
	if err != nil {
		t.Fatalf("LookupPush: %v", err)
	}
	if !ok {
		t.Fatal("no ledger row: the push was sent but can never be replied to")
	}
	if uuidToString(push.IssueID) != issueID {
		t.Fatalf("ledger points at %v, want issue %v", push.IssueID, issueID)
	}

	// --- reply half ------------------------------------------------------
	res, err := testHandler.PostPushReplyComment(ctx, push, parseUUID(userID), "确认审核")
	if err != nil {
		t.Fatalf("PostPushReplyComment: %v", err)
	}
	if !res.Posted {
		t.Fatalf("Posted = false: %q", res.Message)
	}
	approved, err := testHandler.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(wsID),
	})
	if err != nil {
		t.Fatalf("GetIssueInWorkspace after approval: %v", err)
	}
	if approved.Status != "done" {
		t.Fatalf("reply status = %q, want done", approved.Status)
	}

	// --- the wake --------------------------------------------------------
	// The comment trigger is the mechanism the whole design leans on; assert
	// it fired rather than trusting the comment row alone.
	tasks, err := testHandler.Queries.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatalf("ListTasksByIssue: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no agent task enqueued: the reply landed but nothing woke up")
	}
}

// inboxNewEvent builds the real EventInboxNew payload a push notification
// reacts to: an actual inbox_item row created through CreateInboxItem, mapped
// exactly as inboxItemToResponse does (cmd/server/notification_listeners.go),
// plus the issue_status key that function's callers inject separately before
// publishing. The payload has no typed struct, so building it by hand here —
// rather than trusting a shortcut shape — is the only place that mapping gets
// checked against the production one.
func inboxNewEvent(t *testing.T, workspaceID, recipientID, issueID, notifType, issueStatus string) events.Event {
	t.Helper()

	itemID := dbfx.Insert(t, "inbox_item", testutil.Cols{
		"workspace_id": workspaceID, "recipient_type": "member",
		"recipient_id": recipientID, "type": notifType,
		"severity": "info", "issue_id": issueID, "title": "Needs review",
	})
	row, err := testHandler.Queries.GetInboxItem(context.Background(), parseUUID(itemID))
	if err != nil {
		t.Fatalf("GetInboxItem: %v", err)
	}

	resp := map[string]any{
		"id":             util.UUIDToString(row.ID),
		"workspace_id":   util.UUIDToString(row.WorkspaceID),
		"recipient_type": row.RecipientType,
		"recipient_id":   util.UUIDToString(row.RecipientID),
		"type":           row.Type,
		"severity":       row.Severity,
		"issue_id":       util.UUIDToPtr(row.IssueID),
		"title":          row.Title,
		"body":           util.TextToPtr(row.Body),
		"read":           row.Read,
		"archived":       row.Archived,
		"created_at":     util.TimestampToString(row.CreatedAt),
		"actor_type":     util.TextToPtr(row.ActorType),
		"actor_id":       util.UUIDToPtr(row.ActorID),
		"details":        json.RawMessage(row.Details),
	}
	resp["issue_status"] = issueStatus

	return events.Event{
		Type:    protocol.EventInboxNew,
		Payload: map[string]any{"item": resp},
	}
}

// recordingDeliverer is a DMDeliverer test double that hands back a fixed
// platform message id, standing in for the Lark adapter this test proves the
// notifier <-> router loop around without a live socket.
type recordingDeliverer struct {
	calls     int
	messageID string
}

func (d *recordingDeliverer) DeliverDM(context.Context, notify.PushRef, db.ChannelUserBinding, string) (notify.DeliverResult, error) {
	d.calls++
	return notify.DeliverResult{State: notify.StateDelivered, MessageID: d.messageID}, nil
}

func (d *recordingDeliverer) AcceptsReplies() bool { return true }
