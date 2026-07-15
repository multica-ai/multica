package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func createScheduledMessageTestChannel(t *testing.T, memberIDs ...string) string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateChannel(w, newRequest(http.MethodPost, "/api/channels", map[string]any{
		"kind":       "channel",
		"name":       "scheduled-message-" + uuid.NewString(),
		"member_ids": memberIDs,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateChannel: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var channel ChannelResponse
	if err := json.NewDecoder(w.Body).Decode(&channel); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, channel.ID)
	})
	return channel.ID
}

func createScheduledMessageTestComment(t *testing.T, issueID, content string) string {
	t.Helper()
	w := httptest.NewRecorder()
	r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	}), "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var comment CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&comment); err != nil {
		t.Fatalf("decode comment: %v", err)
	}
	return comment.ID
}

func scheduledMessageRequest(method, path, userID, workspaceID, paramName, paramValue string, body any) *http.Request {
	r := newRequest(method, path, body)
	r.Header.Set("X-User-ID", userID)
	r.Header.Set("X-Workspace-ID", workspaceID)
	return withURLParam(r, paramName, paramValue)
}

func scheduleMessageForTest(t *testing.T, issueID, content string) scheduledMessageResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := scheduledMessageRequest(
		http.MethodPost,
		"/api/issues/"+issueID+"/scheduled-messages",
		testUserID,
		testWorkspaceID,
		"id",
		issueID,
		map[string]any{"content": content, "send_at": time.Now().Add(time.Hour)},
	)
	testHandler.CreateScheduledMessage(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateScheduledMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var message scheduledMessageResponse
	if err := json.NewDecoder(w.Body).Decode(&message); err != nil {
		t.Fatalf("decode scheduled message: %v", err)
	}
	return message
}

func TestScheduledMessageAutomaticDeliveryIsExactlyOnceAndCanonical(t *testing.T) {
	ctx := context.Background()
	issueID := createScheduledMessageTestChannel(t)
	mentionedAgentID := createHandlerTestAgent(t, "Scheduled Mention Agent", nil)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
	})
	parentID := createScheduledMessageTestComment(t, issueID, "scheduled parent")
	attachmentID := seedAttachmentURL(t, "https://example.test/scheduled.png", "scheduled.png", "image/png", 123)
	if _, err := testPool.Exec(ctx, `UPDATE attachment SET issue_id=$1 WHERE id=$2`, issueID, attachmentID); err != nil {
		t.Fatalf("link seeded attachment to channel: %v", err)
	}

	var scheduledID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO cerebro_scheduled_message (
			workspace_id, issue_id, author_user_id, content, parent_id, attachment_ids, send_at
		) VALUES ($1,$2,$3,$4,$5,ARRAY[$6::uuid],now()-interval '1 minute')
		RETURNING id::text
	`, testWorkspaceID, issueID, testUserID, fmt.Sprintf("automatic scheduled reply [@Agent](mention://agent/%s)", mentionedAgentID), parentID, attachmentID).Scan(&scheduledID); err != nil {
		t.Fatalf("seed due scheduled message: %v", err)
	}

	if processed := testHandler.sweepCerebroScheduledMessagesOnce(ctx, 50); processed != 1 {
		t.Fatalf("first sweep processed %d messages, want 1", processed)
	}
	if processed := testHandler.sweepCerebroScheduledMessagesOnce(ctx, 50); processed != 0 {
		t.Fatalf("second sweep processed %d messages, want 0", processed)
	}

	var status, sentCommentID string
	if err := testPool.QueryRow(ctx, `
		SELECT status, sent_comment_id::text
		FROM cerebro_scheduled_message WHERE id=$1
	`, scheduledID).Scan(&status, &sentCommentID); err != nil {
		t.Fatalf("read delivered scheduled message: %v", err)
	}
	if status != "sent" || sentCommentID == "" {
		t.Fatalf("delivered status/comment = %q/%q, want sent/non-empty", status, sentCommentID)
	}

	var content, deliveredParentID, authorType, authorID string
	if err := testPool.QueryRow(ctx, `
		SELECT content, parent_id::text, author_type, author_id::text
		FROM comment WHERE id=$1
	`, sentCommentID).Scan(&content, &deliveredParentID, &authorType, &authorID); err != nil {
		t.Fatalf("read delivered comment: %v", err)
	}
	expectedContent := fmt.Sprintf("automatic scheduled reply [@Agent](mention://agent/%s)", mentionedAgentID)
	if content != expectedContent || deliveredParentID != parentID {
		t.Fatalf("delivered content/parent = %q/%q, want %q/%q", content, deliveredParentID, expectedContent, parentID)
	}
	if authorType != "member" || authorID != testUserID {
		t.Fatalf("delivered author = %s/%s, want member/%s", authorType, authorID, testUserID)
	}

	var attachmentCommentID, attachmentIssueID string
	if err := testPool.QueryRow(ctx, `
		SELECT comment_id::text, issue_id::text FROM attachment WHERE id=$1
	`, attachmentID).Scan(&attachmentCommentID, &attachmentIssueID); err != nil {
		t.Fatalf("read delivered attachment: %v", err)
	}
	if attachmentCommentID != sentCommentID || attachmentIssueID != issueID {
		t.Fatalf("attachment links = comment %q issue %q, want %q/%q", attachmentCommentID, attachmentIssueID, sentCommentID, issueID)
	}

	var duplicateCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM comment WHERE issue_id=$1 AND content=$2
	`, issueID, expectedContent).Scan(&duplicateCount); err != nil {
		t.Fatalf("count delivered comments: %v", err)
	}
	if duplicateCount != 1 {
		t.Fatalf("delivered comment count = %d, want exactly 1", duplicateCount)
	}
	if queuedTasks := countQueuedCommentTriggerTasks(t, issueID, mentionedAgentID); queuedTasks != 1 {
		t.Fatalf("mentioned agent queued tasks = %d, want exactly 1", queuedTasks)
	}
}

func TestScheduledMessageAuthorAndWorkspaceIsolation(t *testing.T) {
	peerID, cleanupPeer := createSecondTestUser(t, "scheduled-isolation")
	t.Cleanup(cleanupPeer)
	issueID := createScheduledMessageTestChannel(t, peerID)

	updateMessage := scheduleMessageForTest(t, issueID, "update me")
	sendMessage := scheduleMessageForTest(t, issueID, "send me")
	deleteMessage := scheduleMessageForTest(t, issueID, "delete me")

	list := func(userID, workspaceID string) (*httptest.ResponseRecorder, []scheduledMessageResponse) {
		w := httptest.NewRecorder()
		r := scheduledMessageRequest(http.MethodGet, "/api/issues/"+issueID+"/scheduled-messages", userID, workspaceID, "id", issueID, nil)
		testHandler.ListScheduledMessages(w, r)
		var messages []scheduledMessageResponse
		if w.Code == http.StatusOK {
			if err := json.NewDecoder(w.Body).Decode(&messages); err != nil {
				t.Fatalf("decode scheduled list: %v", err)
			}
		}
		return w, messages
	}

	if w, messages := list(testUserID, testWorkspaceID); w.Code != http.StatusOK || len(messages) != 3 {
		t.Fatalf("author list = status %d count %d, want 200/3", w.Code, len(messages))
	}
	if w, messages := list(peerID, testWorkspaceID); w.Code != http.StatusOK || len(messages) != 0 {
		t.Fatalf("peer list = status %d count %d, want 200/0", w.Code, len(messages))
	}

	otherWorkspaceID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace (id,name,slug,description,issue_prefix)
		VALUES ($1,$2,$3,'','ISO')
	`, otherWorkspaceID, "Scheduled isolation", "scheduled-isolation-"+uuid.NewString()); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')
	`, otherWorkspaceID, peerID); err != nil {
		t.Fatalf("add peer to other workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id=$1`, otherWorkspaceID)
	})
	if w, _ := list(peerID, otherWorkspaceID); w.Code != http.StatusNotFound {
		t.Fatalf("other workspace list status = %d, want 404", w.Code)
	}

	assertMutationDenied := func(userID, workspaceID, method, path, messageID string, body any, call func(http.ResponseWriter, *http.Request)) {
		t.Helper()
		w := httptest.NewRecorder()
		r := scheduledMessageRequest(method, path, userID, workspaceID, "scheduledMessageId", messageID, body)
		call(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("denied %s %s returned %d, want 404", method, path, w.Code)
		}
	}

	for _, principal := range []struct{ userID, workspaceID string }{
		{peerID, testWorkspaceID},
		{peerID, otherWorkspaceID},
	} {
		assertMutationDenied(principal.userID, principal.workspaceID, http.MethodPatch, "/api/scheduled-messages/"+updateMessage.ID, updateMessage.ID, map[string]any{"content": "stolen"}, testHandler.UpdateScheduledMessage)
		assertMutationDenied(principal.userID, principal.workspaceID, http.MethodPost, "/api/scheduled-messages/"+sendMessage.ID+"/send", sendMessage.ID, nil, testHandler.SendScheduledMessageNow)
		assertMutationDenied(principal.userID, principal.workspaceID, http.MethodDelete, "/api/scheduled-messages/"+deleteMessage.ID, deleteMessage.ID, nil, testHandler.DeleteScheduledMessage)
	}

	updatedContent := "author updated"
	w := httptest.NewRecorder()
	r := scheduledMessageRequest(http.MethodPatch, "/api/scheduled-messages/"+updateMessage.ID, testUserID, testWorkspaceID, "scheduledMessageId", updateMessage.ID, map[string]any{"content": updatedContent})
	testHandler.UpdateScheduledMessage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("author update: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = scheduledMessageRequest(http.MethodPost, "/api/scheduled-messages/"+sendMessage.ID+"/send", testUserID, testWorkspaceID, "scheduledMessageId", sendMessage.ID, nil)
	testHandler.SendScheduledMessageNow(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("author send: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r = scheduledMessageRequest(http.MethodDelete, "/api/scheduled-messages/"+deleteMessage.ID, testUserID, testWorkspaceID, "scheduledMessageId", deleteMessage.ID, nil)
	testHandler.DeleteScheduledMessage(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("author delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	if w, messages := list(testUserID, testWorkspaceID); w.Code != http.StatusOK || len(messages) != 1 || messages[0].Content != updatedContent {
		t.Fatalf("author final list = status %d messages %+v, want one updated pending message", w.Code, messages)
	}
}
