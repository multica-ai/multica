package handler

import (
	"context"
	"testing"
)

// TestChannelChat_AttachmentOnlyMessageIsDelivered reproduces a dropped file on
// a self-hosted Slack DM: the user drops a PDF with no caption, then explains
// it in a second message a few seconds later.
//
//	13:46:35  chat_message content="" + attachment of2026-quote.pdf
//	13:46:38  ERR chat claim: task-owned direct task has no user input; cancelling
//	13:46:45  chat_message "that is the quote we sent" -> runs, without the PDF
//
// The claim guard treated "no text" as "no input", although the message owned
// an attachment. The task must be delivered with the attachment instead.
func TestChannelChat_AttachmentOnlyMessageIsDelivered(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "channel attachment only")
	seedChannelBindingOfChatType(t, ctx, agentID, sessionID, "slack", "p2p", "", "")

	msgID := appendChannelUserMessage(t, ctx, sessionID, "")
	var attachmentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes, chat_session_id, chat_message_id)
		VALUES ($1, 'member', $2, 'of2026-quote.pdf', 'https://cdn.example.com/of2026-quote.pdf', 'application/pdf', 2048, $3, $4)
		RETURNING id::text
	`, testWorkspaceID, testUserID, sessionID, msgID).Scan(&attachmentID); err != nil {
		t.Fatalf("insert attachment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachmentID) })

	run := flushChannelChatRun(t, ctx, sessionID)
	res := claimTaskRaw(t, runtimeID, daemonID)
	if res.code != 200 || res.task == nil {
		var status string
		_ = testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, run.ID).Scan(&status)
		t.Fatalf("claim = %d %s; task %s is now %q. An attachment-only message must reach the agent, not be cancelled.",
			res.code, res.body, uuidToString(run.ID), status)
	}
	if len(res.task.ChatMessageAttachments) != 1 || res.task.ChatMessageAttachments[0].ID != attachmentID {
		t.Fatalf("claim attachments = %+v, want exactly %s", res.task.ChatMessageAttachments, attachmentID)
	}
}

// A message with neither text nor attachments is still corrupt input and keeps
// failing closed (MUL-4351).
func TestChannelChat_EmptyMessageWithoutAttachmentStillCancels(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, sessionID, runtimeID, daemonID := setupDirectChatSession(t, ctx, "channel empty input")
	seedChannelBindingOfChatType(t, ctx, agentID, sessionID, "slack", "p2p", "", "")

	appendChannelUserMessage(t, ctx, sessionID, "   ")
	run := flushChannelChatRun(t, ctx, sessionID)
	res := claimTaskRaw(t, runtimeID, daemonID)
	if res.code == 200 && res.task != nil {
		t.Fatalf("claim delivered an empty prompt: %+v", res.task)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatalf("load task: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", status)
	}
}
