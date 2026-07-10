package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRequestHasDaemonCapability(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   bool
	}{
		{name: "absent"},
		{name: "exact", header: protocol.DaemonCapabilityCoalescedCommentsV1, want: true},
		{name: "comma separated and trimmed", header: "skill-bundles-v1,  coalesced-comments-v1  ", want: true},
		{name: "substring", header: "xcoalesced-comments-v1", want: false},
		{name: "case sensitive", header: "Coalesced-Comments-V1", want: false},
		{name: "unknown", header: "future-comments-v2", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/claim", nil)
			if tc.header != "" {
				req.Header.Set("X-Client-Capabilities", tc.header)
			}
			if got := requestHasDaemonCapability(req, protocol.DaemonCapabilityCoalescedCommentsV1); got != tc.want {
				t.Fatalf("requestHasDaemonCapability(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func TestSelectCommentDelivery_BudgetKeepsTriggerAndStablePrefix(t *testing.T) {
	comments := []CoalescedCommentData{
		{ID: "00000000-0000-0000-0000-000000000001", Content: "oldest"},
		{ID: "00000000-0000-0000-0000-000000000002", Content: "overflow"},
		{ID: "00000000-0000-0000-0000-000000000003", Content: "trigger"},
	}
	triggerID := comments[2].ID
	limit := commentDeliveryBaseSize(false) +
		commentDeliveryEntrySize(comments[2], false) +
		commentDeliveryEntrySize(comments[0], false)

	selected := selectCommentDelivery(comments, triggerID, false, limit)
	got := make([]string, 0, len(selected))
	for _, comment := range selected {
		got = append(got, comment.ID)
	}
	want := []string{comments[0].ID, comments[2].ID}
	if !slices.Equal(got, want) {
		t.Fatalf("selected ids = %v, want stable prefix + trigger %v", got, want)
	}
}

func TestFormatLegacyCommentBundle_PreservesBodiesAndOrder(t *testing.T) {
	comments := []CoalescedCommentData{
		{ID: "00000000-0000-0000-0000-000000000001", ThreadID: "thread-a", AuthorType: "member", AuthorName: "A", Content: "  first body\n", CreatedAt: "2026-07-10T01:00:00Z"},
		{ID: "00000000-0000-0000-0000-000000000002", ThreadID: "thread-b", AuthorType: "agent", AuthorName: "B", Content: "second body", CreatedAt: "2026-07-10T02:00:00Z"},
	}
	bundle := formatLegacyCommentBundle(comments)
	for _, want := range []string{comments[0].ID, comments[1].ID, "thread-a", "thread-b", "member: A", "agent: B", comments[0].Content, comments[1].Content} {
		if !strings.Contains(bundle, want) {
			t.Fatalf("legacy bundle missing %q:\n%s", want, bundle)
		}
	}
	if strings.Index(bundle, comments[0].ID) > strings.Index(bundle, comments[1].ID) {
		t.Fatalf("legacy bundle is not chronological:\n%s", bundle)
	}
}

func TestCommentDeliveryEntrySize_AccountsForLegacyJSONEscaping(t *testing.T) {
	comment := CoalescedCommentData{
		ID:      "00000000-0000-0000-0000-000000000001",
		Content: `quotes " backslashes \\ and html <>&`,
	}
	raw := len(formatLegacyCommentEntry(comment))
	if got := commentDeliveryEntrySize(comment, true); got <= raw {
		t.Fatalf("legacy escaped size = %d, want greater than raw size %d", got, raw)
	}
}

type commentDeliveryFixture struct {
	runtimeID string
	agentID   string
	issueID   string
	taskID    string
	commentID []string
	threadID  []string
	content   []string
}

type failNthBegin struct {
	delegate *pgxpool.Pool
	failAt   int
	calls    int
}

type failDeleteCommentDB struct {
	delegate db.DBTX
}

func (f *failDeleteCommentDB) Exec(ctx context.Context, query string, args ...interface{}) (pgconn.CommandTag, error) {
	if strings.Contains(query, "-- name: DeleteComment") {
		return pgconn.CommandTag{}, errors.New("injected comment deletion failure")
	}
	return f.delegate.Exec(ctx, query, args...)
}

func (f *failDeleteCommentDB) Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error) {
	return f.delegate.Query(ctx, query, args...)
}

func (f *failDeleteCommentDB) QueryRow(ctx context.Context, query string, args ...interface{}) pgx.Row {
	return f.delegate.QueryRow(ctx, query, args...)
}

func (f *failNthBegin) Begin(ctx context.Context) (pgx.Tx, error) {
	f.calls++
	if f.calls == f.failAt {
		return nil, errors.New("injected claim finalization transaction failure")
	}
	return f.delegate.Begin(ctx)
}

func createCommentDeliveryFixture(t *testing.T, label string) commentDeliveryFixture {
	t.Helper()
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, label+" runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, label+" agent")

	contents := []string{"first cross-thread instruction", "second instruction", "latest instruction"}
	ids := make([]string, 3)
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '3 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[0]).Scan(&ids[0]); err != nil {
		t.Fatalf("insert first comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - interval '2 minutes')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[1]).Scan(&ids[1]); err != nil {
		t.Fatalf("insert second comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, now() - interval '1 minute')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, contents[2], ids[1]).Scan(&ids[2]); err != nil {
		t.Fatalf("insert trigger comment: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			trigger_comment_id, coalesced_comment_ids
		)
		VALUES ($1, $2, $3, 'queued', 0, $4, ARRAY[$5::uuid, $6::uuid])
		RETURNING id
	`, agentID, runtimeID, issueID, ids[2], ids[1], ids[0]).Scan(&taskID); err != nil {
		t.Fatalf("insert comment delivery task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	return commentDeliveryFixture{
		runtimeID: runtimeID,
		agentID:   agentID,
		issueID:   issueID,
		taskID:    taskID,
		commentID: ids,
		threadID:  []string{ids[0], ids[1], ids[1]},
		content:   contents,
	}
}

func claimCommentDeliveryFixture(t *testing.T, fixture commentDeliveryFixture, capabilities string) AgentTaskResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+fixture.runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "comment-delivery-matrix")
	if capabilities != "" {
		req.Header.Set("X-Client-Capabilities", capabilities)
	}
	req = withURLParam(req, "runtimeId", fixture.runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if response.Task == nil {
		t.Fatalf("claim returned no task: %s", w.Body.String())
	}
	return *response.Task
}

func deliveredCommentIDsForTask(t *testing.T, taskID string) []string {
	t.Helper()
	var ids []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT delivered_comment_ids::text[]
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&ids); err != nil {
		t.Fatalf("load delivered comment ids: %v", err)
	}
	return ids
}

func TestClaimTaskByRuntime_CoalescedDeliveryMatrix(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	t.Run("legacy daemon receives one compatible bundle", func(t *testing.T) {
		fixture := createCommentDeliveryFixture(t, "Legacy comment delivery")
		task := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilitySkillBundlesV1)

		// A v0.3.41-shaped decoder knows only this old comment field. All input
		// must still be available through it; structured fields are deliberately
		// absent to avoid duplicate rendering in intermediate clients.
		wire, err := json.Marshal(map[string]any{"task": task})
		if err != nil {
			t.Fatalf("marshal legacy-shaped response: %v", err)
		}
		var legacy struct {
			Task struct {
				TriggerCommentContent string `json:"trigger_comment_content"`
			} `json:"task"`
		}
		if err := json.Unmarshal(wire, &legacy); err != nil {
			t.Fatalf("legacy decode: %v", err)
		}
		for i := range fixture.commentID {
			for _, want := range []string{fixture.commentID[i], fixture.threadID[i], fixture.content[i]} {
				if !strings.Contains(legacy.Task.TriggerCommentContent, want) {
					t.Fatalf("legacy trigger bundle missing %q:\n%s", want, legacy.Task.TriggerCommentContent)
				}
			}
		}
		if len(task.CoalescedComments) != 0 || len(task.CoalescedCommentIDs) != 0 {
			t.Fatalf("legacy claim leaked structured coalesced fields: ids=%v comments=%v", task.CoalescedCommentIDs, task.CoalescedComments)
		}
		if !slices.Equal(deliveredCommentIDsForTask(t, fixture.taskID), fixture.commentID) {
			t.Fatalf("legacy receipt does not match embedded comments")
		}
	})

	t.Run("capable daemon receives ordered structured comments", func(t *testing.T) {
		fixture := createCommentDeliveryFixture(t, "Structured comment delivery")
		capabilities := protocol.DaemonCapabilitySkillBundlesV1 + "," + protocol.DaemonCapabilityCoalescedCommentsV1
		task := claimCommentDeliveryFixture(t, fixture, capabilities)

		if task.TriggerCommentContent != fixture.content[2] {
			t.Fatalf("trigger content = %q, want %q", task.TriggerCommentContent, fixture.content[2])
		}
		if !slices.Equal(task.CoalescedCommentIDs, fixture.commentID[:2]) {
			t.Fatalf("structured ids = %v, want %v", task.CoalescedCommentIDs, fixture.commentID[:2])
		}
		if len(task.CoalescedComments) != 2 {
			t.Fatalf("structured comments = %d, want 2", len(task.CoalescedComments))
		}
		for i, comment := range task.CoalescedComments {
			if comment.ID != fixture.commentID[i] || comment.ThreadID != fixture.threadID[i] || comment.Content != fixture.content[i] {
				t.Fatalf("structured comment[%d] = %+v", i, comment)
			}
		}
		if !slices.Equal(task.DeliveredCommentIDs, fixture.commentID) {
			t.Fatalf("response receipt = %v, want %v", task.DeliveredCommentIDs, fixture.commentID)
		}
		if !slices.Equal(deliveredCommentIDsForTask(t, fixture.taskID), fixture.commentID) {
			t.Fatalf("database receipt does not match embedded comments")
		}

		if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(fixture.taskID)); err != nil {
			t.Fatalf("start claimed task: %v", err)
		}
		if w := completeTaskViaHandler(t, fixture.taskID, "done"); w.Code != http.StatusOK {
			t.Fatalf("complete claimed task: %d %s", w.Code, w.Body.String())
		}
		if n := pendingTaskCountForAgentIssue(t, fixture.issueID, fixture.agentID); n != 0 {
			t.Fatalf("full receipt produced %d follow-up tasks, want 0", n)
		}
	})
}

func TestClaimTaskByRuntime_CoalescedOnlyStaleTaskDoesNotReuseDeletedTriggerCapabilities(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Deleted trigger stale claim")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign stale-claim issue: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM comment WHERE id = $1`, fixture.commentID[2]); err != nil {
		t.Fatalf("delete trigger directly: %v", err)
	}

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+fixture.runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "stale-comment-plan-repair")
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	req = withURLParam(req, "runtimeId", fixture.runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("repair stale claim: got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode stale-plan repair response: %v", err)
	}
	if response.Task != nil {
		t.Fatalf("stale plan was dispatched instead of repaired: %+v", response.Task)
	}
	assertRepairedCommentBatch(t, fixture, fixture.commentID[1], fixture.commentID[:1])
}

func TestUpdateComment_RequeuesSurvivingCoalescedBatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Edited trigger batch repair")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign edited-trigger issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/comments/"+fixture.commentID[2], map[string]any{
		"content": "edited latest instruction",
	})
	req = withURLParam(req, "commentId", fixture.commentID[2])
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: got %d: %s", w.Code, w.Body.String())
	}

	assertRepairedCommentBatch(t, fixture, fixture.commentID[2], fixture.commentID[:2])
}

func TestDeleteComment_RequeuesSurvivingCoalescedBatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Deleted trigger batch repair")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign deleted-trigger issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/comments/"+fixture.commentID[2], nil)
	req = withURLParam(req, "commentId", fixture.commentID[2])
	testHandler.DeleteComment(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteComment: got %d: %s", w.Code, w.Body.String())
	}

	assertRepairedCommentBatch(t, fixture, fixture.commentID[1], fixture.commentID[:1])
	var deletedCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE id = $1`, fixture.commentID[2]).Scan(&deletedCount); err != nil {
		t.Fatalf("check deleted trigger: %v", err)
	}
	if deletedCount != 0 {
		t.Fatalf("deleted trigger still exists")
	}
}

func TestUpdateComment_CancelsAndRequeuesWhenEditedInputIsCoalesced(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Edited coalesced input repair")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign edited-coalesced issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPut, "/api/comments/"+fixture.commentID[0], map[string]any{
		"content": "edited earlier instruction",
	})
	req = withURLParam(req, "commentId", fixture.commentID[0])
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: got %d: %s", w.Code, w.Body.String())
	}

	assertRepairedCommentBatch(t, fixture, fixture.commentID[0], fixture.commentID[1:])
}

func TestDeleteComment_CancelsAndRequeuesWhenDeletedInputIsCoalesced(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Deleted coalesced input repair")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign deleted-coalesced issue: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/comments/"+fixture.commentID[0], nil)
	req = withURLParam(req, "commentId", fixture.commentID[0])
	testHandler.DeleteComment(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteComment: got %d: %s", w.Code, w.Body.String())
	}

	assertRepairedCommentBatch(t, fixture, fixture.commentID[2], fixture.commentID[1:2])
}

func TestDeleteComment_FailureRestoresCancelledCompleteBatch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Failed deletion batch repair")
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign failed-delete issue: %v", err)
	}

	failingHandler := *testHandler
	failingHandler.Queries = db.New(&failDeleteCommentDB{delegate: testPool})
	w := httptest.NewRecorder()
	req := newRequest(http.MethodDelete, "/api/comments/"+fixture.commentID[2], nil)
	req = withURLParam(req, "commentId", fixture.commentID[2])
	failingHandler.DeleteComment(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("DeleteComment failure: got %d: %s", w.Code, w.Body.String())
	}

	var existing int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM comment WHERE id = $1`, fixture.commentID[2]).Scan(&existing); err != nil {
		t.Fatalf("check trigger after failed delete: %v", err)
	}
	if existing != 1 {
		t.Fatalf("failed delete unexpectedly removed trigger")
	}
	assertRepairedCommentBatch(t, fixture, fixture.commentID[2], fixture.commentID[:2])
}

func assertRepairedCommentBatch(t *testing.T, fixture commentDeliveryFixture, wantTrigger string, wantCoalesced []string) {
	t.Helper()
	var originalStatus string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, fixture.taskID).Scan(&originalStatus); err != nil {
		t.Fatalf("load original task: %v", err)
	}
	if originalStatus != "cancelled" {
		t.Fatalf("original task status = %s, want cancelled", originalStatus)
	}

	var trigger string
	var coalesced, delivered []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT trigger_comment_id::text, coalesced_comment_ids::text[], delivered_comment_ids::text[]
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, fixture.issueID, fixture.agentID).Scan(&trigger, &coalesced, &delivered); err != nil {
		t.Fatalf("load repaired task: %v", err)
	}
	if trigger != wantTrigger {
		t.Fatalf("repaired trigger = %s, want %s", trigger, wantTrigger)
	}
	gotCoalesced := append([]string{}, coalesced...)
	want := append([]string{}, wantCoalesced...)
	slices.Sort(gotCoalesced)
	slices.Sort(want)
	if !slices.Equal(gotCoalesced, want) {
		t.Fatalf("repaired coalesced ids = %v, want %v", gotCoalesced, want)
	}
	if len(delivered) != 0 {
		t.Fatalf("repaired batch inherited receipt: %v", delivered)
	}
}

func TestBuildCoalescedCommentData_SortsEqualTimestampsByID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Comment delivery tie order")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE comment
		SET created_at = TIMESTAMPTZ '2026-07-10 01:00:00+00'
		WHERE id = ANY($1::uuid[])
	`, fixture.commentID[:2]); err != nil {
		t.Fatalf("set equal comment timestamps: %v", err)
	}
	ids := []pgtype.UUID{
		util.MustParseUUID(fixture.commentID[1]),
		util.MustParseUUID(fixture.commentID[0]),
	}
	comments := testHandler.buildCoalescedCommentData(context.Background(), util.MustParseUUID(testWorkspaceID), ids)
	want := append([]string{}, fixture.commentID[:2]...)
	slices.Sort(want)
	got := []string{comments[0].ID, comments[1].ID}
	if !slices.Equal(got, want) {
		t.Fatalf("equal-timestamp order = %v, want id order %v", got, want)
	}
}

func TestClaimTaskByRuntime_PayloadOverflowReceiptsOnlyEmbeddedPrefix(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Comment payload overflow")
	oversized := strings.Repeat("x", maxClaimCommentPayloadBytes+1024)
	if _, err := testPool.Exec(context.Background(), `UPDATE comment SET content = $2 WHERE id = $1`, fixture.commentID[0], oversized); err != nil {
		t.Fatalf("make first coalesced comment oversized: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign overflow issue: %v", err)
	}

	task := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	if !slices.Equal(task.DeliveredCommentIDs, []string{fixture.commentID[2]}) {
		t.Fatalf("overflow receipt = %v, want primary trigger only", task.DeliveredCommentIDs)
	}
	if len(task.CoalescedComments) != 0 || len(task.CoalescedCommentIDs) != 0 {
		t.Fatalf("overflow claim embedded comments beyond stable boundary: ids=%v comments=%d", task.CoalescedCommentIDs, len(task.CoalescedComments))
	}

	if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(fixture.taskID)); err != nil {
		t.Fatalf("start overflow task: %v", err)
	}
	if w := completeTaskViaHandler(t, fixture.taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("complete overflow task: %d %s", w.Code, w.Body.String())
	}
	if n := pendingTaskCountForAgentIssue(t, fixture.issueID, fixture.agentID); n != 1 {
		t.Fatalf("overflow should create one bounded follow-up, got %d", n)
	}
	followupTrigger, _, followupCoalesced := taskTriggerOriginatorCoalesced(t, fixture.issueID, fixture.agentID)
	covered := append([]string{}, followupCoalesced...)
	covered = append(covered, followupTrigger)
	slices.Sort(covered)
	want := append([]string{}, fixture.commentID[:2]...)
	slices.Sort(want)
	if !slices.Equal(covered, want) {
		t.Fatalf("overflow follow-up coverage = %v, want omitted suffix %v", covered, want)
	}
}

func TestClaimTaskByRuntime_StaleReclaimReplacesDeliveryReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Stale comment receipt reclaim")
	first := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	if !slices.Equal(first.DeliveredCommentIDs, fixture.commentID) {
		t.Fatalf("first receipt = %v, want all comments", first.DeliveredCommentIDs)
	}

	oversized := strings.Repeat("x", maxClaimCommentPayloadBytes+1024)
	if _, err := testPool.Exec(context.Background(), `UPDATE comment SET content = $2 WHERE id = $1`, fixture.commentID[0], oversized); err != nil {
		t.Fatalf("make reclaimed comment oversized: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET dispatched_at = now() - interval '2 minutes', prepare_lease_expires_at = NULL
		WHERE id = $1
	`, fixture.taskID); err != nil {
		t.Fatalf("prepare stale receipt reclaim: %v", err)
	}

	reclaimed := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	want := []string{fixture.commentID[2]}
	if !slices.Equal(reclaimed.DeliveredCommentIDs, want) {
		t.Fatalf("reclaimed response receipt = %v, want replacement %v", reclaimed.DeliveredCommentIDs, want)
	}
	if got := deliveredCommentIDsForTask(t, fixture.taskID); !slices.Equal(got, want) {
		t.Fatalf("reclaimed database receipt = %v, want replacement %v", got, want)
	}
}

func TestCreateRetryTask_PreservesCommentPlanAndResetsReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment retry runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment retry agent")

	commentIDs := make([]string, 3)
	for i := range commentIDs {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, $4, 'comment')
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, "retry comment").Scan(&commentIDs[i]); err != nil {
			t.Fatalf("insert retry comment: %v", err)
		}
	}

	var parentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			trigger_comment_id, coalesced_comment_ids, delivered_comment_ids,
			attempt, max_attempts, failure_reason
		)
		VALUES (
			$1, $2, $3, 'failed', 0,
			$4, ARRAY[$5::uuid, $6::uuid], ARRAY[$4::uuid, $5::uuid, $6::uuid],
			1, 3, 'timeout'
		)
		RETURNING id
	`, agentID, runtimeID, issueID, commentIDs[2], commentIDs[0], commentIDs[1]).Scan(&parentID); err != nil {
		t.Fatalf("insert retry parent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	child, err := testHandler.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: util.MustParseUUID(parentID)})
	if err != nil {
		t.Fatalf("CreateRetryTask: %v", err)
	}
	if got := uuidToString(child.TriggerCommentID); got != commentIDs[2] {
		t.Fatalf("child trigger = %s, want %s", got, commentIDs[2])
	}
	gotCoalesced := uuidsToStrings(child.CoalescedCommentIds)
	wantCoalesced := commentIDs[:2]
	slices.Sort(gotCoalesced)
	slices.Sort(wantCoalesced)
	if !slices.Equal(gotCoalesced, wantCoalesced) {
		t.Fatalf("child coalesced ids = %v, want %v", gotCoalesced, wantCoalesced)
	}
	if len(child.DeliveredCommentIds) != 0 {
		t.Fatalf("retry child inherited delivery receipt: %v", uuidsToStrings(child.DeliveredCommentIds))
	}
}

func TestRerunIssue_PreservesSourceCommentPlanAndResetsReceipt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment manual rerun runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment manual rerun agent")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("assign rerun issue: %v", err)
	}

	commentIDs := make([]string, 3)
	for i := range commentIDs {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, 'manual rerun comment', 'comment')
			RETURNING id
		`, issueID, testWorkspaceID, testUserID).Scan(&commentIDs[i]); err != nil {
			t.Fatalf("insert manual rerun comment: %v", err)
		}
	}

	var sourceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority,
			trigger_comment_id, coalesced_comment_ids, delivered_comment_ids,
			started_at, completed_at
		)
		VALUES (
			$1, $2, $3, 'completed', 0,
			$4, ARRAY[$5::uuid, $6::uuid], ARRAY[$4::uuid, $5::uuid, $6::uuid],
			now() - interval '2 minutes', now() - interval '1 minute'
		)
		RETURNING id
	`, agentID, runtimeID, issueID, commentIDs[2], commentIDs[0], commentIDs[1]).Scan(&sourceID); err != nil {
		t.Fatalf("insert manual rerun source: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})

	rerun, err := testHandler.TaskService.RerunIssue(ctx, util.MustParseUUID(issueID), util.MustParseUUID(sourceID), pgtype.UUID{})
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}
	if got := uuidToString(rerun.TriggerCommentID); got != commentIDs[2] {
		t.Fatalf("rerun trigger = %s, want %s", got, commentIDs[2])
	}
	gotCoalesced := uuidsToStrings(rerun.CoalescedCommentIds)
	wantCoalesced := append([]string{}, commentIDs[:2]...)
	slices.Sort(gotCoalesced)
	slices.Sort(wantCoalesced)
	if !slices.Equal(gotCoalesced, wantCoalesced) {
		t.Fatalf("rerun coalesced ids = %v, want %v", gotCoalesced, wantCoalesced)
	}
	if len(rerun.DeliveredCommentIds) != 0 {
		t.Fatalf("manual rerun inherited receipt: %v", uuidsToStrings(rerun.DeliveredCommentIds))
	}
}

func TestRerunIssue_PromotesNewestSurvivorAfterSourceTriggerDeleted(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := createCommentDeliveryFixture(t, "Deleted trigger manual rerun")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, fixture.issueID, fixture.agentID); err != nil {
		t.Fatalf("assign deleted-trigger rerun issue: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_task_queue
		SET status = 'completed', started_at = now() - interval '2 minutes', completed_at = now() - interval '1 minute'
		WHERE id = $1
	`, fixture.taskID); err != nil {
		t.Fatalf("complete deleted-trigger rerun source: %v", err)
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, fixture.commentID[2]); err != nil {
		t.Fatalf("delete rerun source trigger: %v", err)
	}

	rerun, err := testHandler.TaskService.RerunIssue(ctx, parseUUID(fixture.issueID), parseUUID(fixture.taskID), pgtype.UUID{})
	if err != nil {
		t.Fatalf("RerunIssue: %v", err)
	}
	if got := uuidToString(rerun.TriggerCommentID); got != fixture.commentID[1] {
		t.Fatalf("rerun promoted trigger = %s, want newest survivor %s", got, fixture.commentID[1])
	}
	if got := uuidsToStrings(rerun.CoalescedCommentIds); !slices.Equal(got, []string{fixture.commentID[0]}) {
		t.Fatalf("rerun survivor plan = %v, want [%s]", got, fixture.commentID[0])
	}
	if len(rerun.DeliveredCommentIds) != 0 {
		t.Fatalf("rerun inherited delivery receipt: %v", uuidsToStrings(rerun.DeliveredCommentIds))
	}
	if got := uuidToString(rerun.OriginatorUserID); got != testUserID {
		t.Fatalf("rerun originator = %s, want promoted comment author %s", got, testUserID)
	}
}

func TestSetTaskDeliveredCommentIDs_CASAndSubset(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Delivery receipt CAS runtime")
	otherRuntimeID := createClaimReclaimRuntime(t, ctx, "Delivery receipt CAS other runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Delivery receipt CAS agent")

	commentIDs := make([]string, 2)
	for i := range commentIDs {
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'member', $3, 'receipt CAS comment', 'comment')
			RETURNING id
		`, issueID, testWorkspaceID, testUserID).Scan(&commentIDs[i]); err != nil {
			t.Fatalf("insert receipt CAS comment: %v", err)
		}
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at,
			trigger_comment_id, coalesced_comment_ids
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now(), $4, ARRAY[$5::uuid])
		RETURNING id
	`, agentID, runtimeID, issueID, commentIDs[1], commentIDs[0]).Scan(&taskID); err != nil {
		t.Fatalf("insert receipt CAS task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})
	task, err := testHandler.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("GetAgentTask: %v", err)
	}

	params := db.SetTaskDeliveredCommentIDsParams{
		DeliveredCommentIds:      []pgtype.UUID{util.MustParseUUID(commentIDs[1])},
		TaskID:                   task.ID,
		RuntimeID:                task.RuntimeID,
		DispatchedAt:             task.DispatchedAt,
		ExpectedTriggerCommentID: task.TriggerCommentID,
	}
	if got, err := testHandler.Queries.SetTaskDeliveredCommentIDs(ctx, params); err != nil || len(got) != 1 {
		t.Fatalf("valid receipt = %v, %v; want one id", got, err)
	}
	params.DeliveredCommentIds = []pgtype.UUID{}
	if got, err := testHandler.Queries.SetTaskDeliveredCommentIDs(ctx, params); err != nil || len(got) != 0 {
		t.Fatalf("empty receipt = %v, %v; want authoritative empty array", got, err)
	}

	params.DeliveredCommentIds = []pgtype.UUID{util.MustParseUUID("00000000-0000-0000-0000-000000000099")}
	if _, err := testHandler.Queries.SetTaskDeliveredCommentIDs(ctx, params); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign receipt id error = %v, want pgx.ErrNoRows", err)
	}
	params.DeliveredCommentIds = []pgtype.UUID{util.MustParseUUID(commentIDs[1])}
	params.RuntimeID = util.MustParseUUID(otherRuntimeID)
	if _, err := testHandler.Queries.SetTaskDeliveredCommentIDs(ctx, params); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("wrong runtime error = %v, want pgx.ErrNoRows", err)
	}
	params.RuntimeID = task.RuntimeID
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatalf("mark task running: %v", err)
	}
	if _, err := testHandler.Queries.SetTaskDeliveredCommentIDs(ctx, params); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("running task receipt error = %v, want pgx.ErrNoRows", err)
	}
}

func TestClaimTaskByRuntime_FinalizationFailureRequeuesImmediately(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fixture := createCommentDeliveryFixture(t, "Claim finalization failure")
	priorTokenHash := "prior-valid-token-" + fixture.taskID
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO task_token (token_hash, task_id, agent_id, workspace_id, user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, now() + interval '1 hour')
	`, priorTokenHash, fixture.taskID, fixture.agentID, testWorkspaceID, testUserID); err != nil {
		t.Fatalf("insert prior task token: %v", err)
	}

	failingHandler := *testHandler
	// Inject a failing transaction starter without copying TaskService by value:
	// it embeds a sync.Mutex (go vet rejects the copy) and a value copy would also
	// split the analytics-cache lock from the map it guards. Swap the field on the
	// shared service and restore it when the test ends. failingHandler shares the
	// same *TaskService pointer, so it observes the failing starter.
	originalTxStarter := testHandler.TaskService.TxStarter
	testHandler.TaskService.TxStarter = &failNthBegin{delegate: testPool, failAt: 2}
	defer func() { testHandler.TaskService.TxStarter = originalTxStarter }()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+fixture.runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "comment-delivery-finalize-failure")
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	req = withURLParam(req, "runtimeId", fixture.runtimeID)
	failingHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failed finalization status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "auth_token") || strings.Contains(w.Body.String(), `"task"`) {
		t.Fatalf("failed claim leaked a task payload: %s", w.Body.String())
	}

	var status string
	var dispatched bool
	var delivered []string
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, dispatched_at IS NOT NULL, delivered_comment_ids::text[]
		FROM agent_task_queue
		WHERE id = $1
	`, fixture.taskID).Scan(&status, &dispatched, &delivered); err != nil {
		t.Fatalf("load requeued task: %v", err)
	}
	if status != "queued" || dispatched || len(delivered) != 0 {
		t.Fatalf("failed claim state = status=%s dispatched=%v delivered=%v; want queued/false/[]", status, dispatched, delivered)
	}
	var tokens int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM task_token WHERE task_id = $1`, fixture.taskID).Scan(&tokens); err != nil {
		t.Fatalf("count task tokens: %v", err)
	}
	if tokens != 1 {
		t.Fatalf("failed claim left %d task token(s), want only the prior valid token", tokens)
	}
	var priorStillExists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM task_token WHERE token_hash = $1)`, priorTokenHash).Scan(&priorStillExists); err != nil {
		t.Fatalf("check prior task token: %v", err)
	}
	if !priorStillExists {
		t.Fatalf("failed reclaim revoked an earlier execution's valid token")
	}

	// No 90-second stale-dispatch delay: the very next normal poll can claim
	// the same complete batch and earn a receipt.
	task := claimCommentDeliveryFixture(t, fixture, protocol.DaemonCapabilityCoalescedCommentsV1)
	if task.ID != fixture.taskID {
		t.Fatalf("immediate retry claimed task %s, want %s", task.ID, fixture.taskID)
	}
	if !slices.Equal(task.DeliveredCommentIDs, fixture.commentID) {
		t.Fatalf("immediate retry receipt = %v, want %v", task.DeliveredCommentIDs, fixture.commentID)
	}
}

func TestFinalizeTaskClaim_ReceiptCASFailureRollsBackInsertedToken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := createCommentDeliveryFixture(t, "Claim receipt rollback")
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(fixture.runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim fixture task: task=%v err=%v", task, err)
	}
	tokenHash := "rolled-back-token-" + fixture.taskID
	_, err = testHandler.TaskService.FinalizeTaskClaim(ctx, *task, db.CreateTaskTokenParams{
		TokenHash:   tokenHash,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(testUserID),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, []pgtype.UUID{parseUUID("00000000-0000-0000-0000-000000000099")}, true)
	if err == nil {
		t.Fatalf("FinalizeTaskClaim accepted an out-of-plan receipt")
	}
	var tokenCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE token_hash = $1`, tokenHash).Scan(&tokenCount); err != nil {
		t.Fatalf("count rolled-back token: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("receipt CAS failure committed %d generated token(s)", tokenCount)
	}
	if got := deliveredCommentIDsForTask(t, fixture.taskID); len(got) != 0 {
		t.Fatalf("receipt CAS failure advanced receipt: %v", got)
	}
}

func TestFinalizeTaskClaim_TriggerDeletedAfterClaimRejectsStaleProvenance(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fixture := createCommentDeliveryFixture(t, "Claim trigger deletion race")
	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(fixture.runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim fixture task: task=%v err=%v", task, err)
	}
	if !task.TriggerCommentID.Valid {
		t.Fatalf("claim snapshot unexpectedly lacks trigger")
	}
	if _, err := testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, fixture.commentID[2]); err != nil {
		t.Fatalf("delete trigger after claim snapshot: %v", err)
	}

	tokenHash := "deleted-trigger-race-token-" + fixture.taskID
	_, err = testHandler.TaskService.FinalizeTaskClaim(ctx, *task, db.CreateTaskTokenParams{
		TokenHash:   tokenHash,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(testUserID),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, []pgtype.UUID{parseUUID(fixture.commentID[0]), parseUUID(fixture.commentID[1])}, true)
	if err == nil {
		t.Fatalf("FinalizeTaskClaim accepted a receipt after persisted trigger changed")
	}
	var tokenCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE token_hash = $1`, tokenHash).Scan(&tokenCount); err != nil {
		t.Fatalf("count stale-provenance token: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("stale trigger race committed %d generated token(s)", tokenCount)
	}
	if got := deliveredCommentIDsForTask(t, fixture.taskID); len(got) != 0 {
		t.Fatalf("stale trigger race advanced receipt: %v", got)
	}
}
