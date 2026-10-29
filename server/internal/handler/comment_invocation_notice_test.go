package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCommentInvocationDeniedNoticePersistsWithoutDisclosure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateID, _, readerID := privateAgentTestFixture(t)
	allowedID := createHandlerTestAgent(t, "Allowed notice target", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Denied notice squad", privateID)
	foreignWorkspace := dbfx.Workspace(t, "Other notice workspace", "notice-"+uuid.NewString())
	foreignID := dbfx.Agent(t, "Other workspace secret", handlerTestRuntimeID(t), testutil.Cols{"workspace_id": foreignWorkspace})
	missingID := uuid.NewString()
	issueID := dbfx.Issue(t, "Persist denied mentions")
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	content := fmt.Sprintf("[@Private alias](mention://agent/%s) [@Missing alias](mention://agent/%s) [@Foreign alias](mention://agent/%s) [@Team alias](mention://squad/%s) [@Allowed alias](mention://agent/%s)", privateID, missingID, foreignID, squadID, allowedID)
	preview := previewCommentTriggersForTest(t, issueID, map[string]any{"content": content})
	if len(preview.Blocked) != 4 {
		t.Fatalf("blocked preview = %+v, want four targets", preview.Blocked)
	}
	var count int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote a comment")
	}
	h := *testHandler
	h.Bus = events.New()
	var notices []CommentResponse
	h.Bus.Subscribe(protocol.EventCommentCreated, func(event events.Event) {
		if payload, ok := event.Payload.(map[string]any); ok {
			if comment, ok := payload["comment"].(CommentResponse); ok && comment.AuthorType == "system" {
				notices = append(notices, comment)
			}
		}
	})
	var posted CommentResponse
	testutil.Call(t, h.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)).Want(http.StatusCreated).JSON(&posted)
	if len(posted.TriggerOutcomes) != 5 || findCommentOutcome(t, posted.TriggerOutcomes, allowedID).Status != DispatchQueued {
		t.Fatalf("partial submission outcomes = %+v", posted.TriggerOutcomes)
	}
	if len(notices) != 1 {
		t.Fatalf("broadcast system notices = %d, want one for this submission", len(notices))
	}
	notice := notices[0]
	if notice.ParentID == nil || *notice.ParentID != posted.ID || notice.Type != "system" {
		t.Fatalf("notice is not a system reply to the submitted comment: %+v", notice)
	}
	for _, alias := range []string{"Private alias", "Missing alias", "Foreign alias", "Team alias"} {
		if !strings.Contains(notice.Content, alias) {
			t.Errorf("notice omitted %q: %s", alias, notice.Content)
		}
	}
	if strings.Count(notice.Content, "Target not found or permission not granted") != 4 {
		t.Errorf("denied targets do not share the generic reason: %s", notice.Content)
	}
	for _, secret := range []string{"private-access-test-agent", "Other workspace secret", "private-agent-owner@multica.test", "Allowed alias"} {
		if strings.Contains(notice.Content, secret) {
			t.Errorf("notice exposed an unrelated or private value %q", secret)
		}
	}
	if len(util.ParseMentions(notice.Content)) != 0 {
		t.Fatalf("notice contains actionable mentions: %s", notice.Content)
	}
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count)
	if count != 1 || countQueuedCommentTriggerTasks(t, issueID, allowedID) != 1 {
		t.Fatal("denied mentions or the system notice created an extra task")
	}
	var comments []CommentResponse
	testutil.Call(t, h.ListComments, withURLParam(newRequestAs(readerID, http.MethodGet, "/api/issues/"+issueID+"/comments", nil), "id", issueID)).Want(http.StatusOK).JSON(&comments)
	if len(comments) != 2 || comments[1].ID != notice.ID || comments[1].Content != notice.Content {
		t.Fatalf("another reader did not receive the saved notice: %+v", comments)
	}
	var timeline []TimelineEntry
	testutil.Call(t, h.ListTimeline, withURLParam(newRequestAs(readerID, http.MethodGet, "/api/issues/"+issueID+"/timeline", nil), "id", issueID)).Want(http.StatusOK).JSON(&timeline)
	for _, entry := range timeline {
		if entry.ID == notice.ID && entry.Content != nil && *entry.Content == notice.Content {
			return
		}
	}
	t.Fatal("saved notice is absent from the timeline")
}

func TestCommentInvocationDeniedNoticeForAgentEditsKeepsHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateID, _, _ := privateAgentTestFixture(t)
	leaderID := createHandlerTestAgent(t, "Denied handoff leader", nil)
	issueID := dbfx.Issue(t, "Denied agent handoff", testutil.Cols{"status": "in_progress"})
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	sourceTaskID := dbfx.Task(t, leaderID, testutil.Cols{"issue_id": issueID, "runtime_id": handlerTestRuntimeID(t), "status": "running", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	content := fmt.Sprintf("[@Worker alias](mention://agent/%s) take over", privateID)
	request := func(method, path string, body any) *http.Request {
		req := newRequest(method, path, body)
		req.Header.Set("X-Agent-ID", leaderID)
		req.Header.Set("X-Task-ID", sourceTaskID)
		return req
	}
	var posted CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(request(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content}), "id", issueID)).Want(http.StatusCreated).JSON(&posted)
	if posted.AuthorType != "agent" || findCommentOutcome(t, posted.TriggerOutcomes, privateID).Status != DispatchBlocked {
		t.Fatalf("agent handoff = %+v", posted)
	}
	var firstNoticeID, firstNotice string
	dbfx.QueryRow(t, `SELECT id, content FROM comment WHERE parent_id = $1 AND author_type = 'system'`, posted.ID).Scan(&firstNoticeID, &firstNotice)
	submittedAt, err := time.Parse(time.RFC3339Nano, posted.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(firstNotice, submittedAt.UTC().Format("Jan 2, 2006 at 15:04:05 MST")) || strings.Contains(firstNotice, "revision") {
		t.Fatalf("notice should identify the submission time without internal version details: %s", firstNotice)
	}
	edit := func(text string) CommentResponse {
		var updated CommentResponse
		testutil.Call(t, testHandler.UpdateComment, withURLParam(request(http.MethodPut, "/api/comments/"+posted.ID, map[string]any{"content": text}), "commentId", posted.ID)).Want(http.StatusOK).JSON(&updated)
		return updated
	}
	edit(content)
	edit("Handoff withdrawn")
	updated := edit(content)
	var notices []CommentResponse
	_, notices = listComments(t, issueID, "?thread="+posted.ID)
	if len(notices) != 3 || notices[1].ID != firstNoticeID || notices[1].Content != firstNotice {
		t.Fatalf("edits changed the old receipt or created an extra receipt: %+v", notices)
	}
	editedAt, err := time.Parse(time.RFC3339Nano, updated.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if notices[2].ID == firstNoticeID || !strings.Contains(notices[2].Content, editedAt.UTC().Format("Jan 2, 2006 at 15:04:05 MST")) || strings.Contains(notices[2].Content, "revision") {
		t.Fatalf("edited-back submission did not get its own historical receipt: %+v", notices[2])
	}
	var tasks int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&tasks)
	if tasks != 1 {
		t.Fatalf("handoff or notices created extra runs: %d tasks", tasks)
	}
	stored, err := testHandler.Queries.GetComment(context.Background(), parseUUID(firstNoticeID))
	if err != nil || stored.AuthorType != "system" || stored.Content != firstNotice {
		t.Fatalf("historical notice changed: %+v, %v", stored, err)
	}
}

func TestCommentInvocationDeniedNoticeSkipsOtherSubmissions(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateID, _, _ := privateAgentTestFixture(t)
	allowedID := createHandlerTestAgent(t, "Successful notice target", nil)
	for _, tc := range []struct{ name, content string }{
		{"successful", fmt.Sprintf("[@Allowed](mention://agent/%s) proceed", allowedID)},
		{"note", fmt.Sprintf("/note [@Private](mention://agent/%s) reference only", privateID)},
		{"code", fmt.Sprintf("`[@Private](mention://agent/%s)` is an example", privateID)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "No permission notice for "+tc.name)
			dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
			testutil.Call(t, testHandler.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": tc.content}), "id", issueID)).Want(http.StatusCreated)
			var comments int
			dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&comments)
			if comments != 1 {
				t.Fatalf("submission added a refusal notice: %d comments", comments)
			}
		})
	}
}

func TestCommentInvocationDeniedNoticeSkipsDeletedParent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	privateID, _, _ := privateAgentTestFixture(t)
	issueID := dbfx.Issue(t, "Delete before permission notice")
	h := *testHandler
	h.Bus = events.New()
	h.Bus.Subscribe(protocol.EventCommentCreated, func(event events.Event) {
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			return
		}
		comment, ok := payload["comment"].(CommentResponse)
		if ok && comment.AuthorType == "member" {
			dbfx.Exec(t, `DELETE FROM comment WHERE id = $1 AND workspace_id = $2`, comment.ID, testWorkspaceID)
		}
	})
	var posted CommentResponse
	testutil.Call(t, h.CreateComment, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": fmt.Sprintf("[@Private](mention://agent/%s)", privateID),
	}), "id", issueID)).Want(http.StatusCreated).JSON(&posted)
	if findCommentOutcome(t, posted.TriggerOutcomes, privateID).Status != DispatchBlocked {
		t.Fatal("notice failure changed the admission result")
	}
	var comments int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&comments)
	if comments != 0 {
		t.Fatalf("notice left an orphan after deletion: %d comments", comments)
	}
}
