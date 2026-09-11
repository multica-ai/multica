package handler

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Fail one input read without affecting claim admission or other comments.
type failClaimCommentDB struct {
	db.DBTX
	query string
	id    pgtype.UUID
	err   error
	hits  int
}

func (f *failClaimCommentDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, f.query) && len(args) > 0 && args[0] == f.id {
		f.hits++
		return errRow{err: f.err}
	}
	return f.DBTX.QueryRow(ctx, sql, args...)
}

func TestClaimCommentLoadFailurePreservesTaskForRedelivery(t *testing.T) {
	for _, endpoint := range []string{"single", "batch"} {
		for _, input := range []string{"trigger", "coalesced", "thread"} {
			t.Run(endpoint+"/"+input, func(t *testing.T) {
				ctx := context.Background()
				var agentID string
				dbfx.QueryRow(t, `SELECT id FROM agent WHERE runtime_id = $1 LIMIT 1`, testRuntimeID).Scan(&agentID)
				issueID := dbfx.Issue(t, "Claim must carry every handoff instruction")
				rootID := dbfx.Comment(t, issueID, "handoff thread")
				firstID := dbfx.Comment(t, issueID, "inspect the failure", testutil.Cols{
					"parent_id": rootID, "author_type": "agent", "author_id": agentID,
				})
				triggerID := dbfx.Comment(t, issueID, "also preserve the user's edits", testutil.Cols{"parent_id": rootID})
				taskID := dbfx.Task(t, agentID, testutil.Cols{
					"runtime_id": testRuntimeID, "issue_id": issueID,
					"trigger_comment_id":    triggerID,
					"coalesced_comment_ids": []string{firstID},
				})
				fault := &failClaimCommentDB{
					DBTX: testPool, query: "-- name: GetCommentInWorkspace :one",
					id: parseUUID(triggerID), err: errors.New("injected comment read failure"),
				}
				if input != "trigger" {
					fault.id = parseUUID(firstID)
				}
				if input == "thread" {
					fault.query = "-- name: GetCommentThreadRootID :one"
					fault.err = context.DeadlineExceeded
				}
				broken := *testHandler
				broken.Queries = db.New(fault)
				if endpoint == "single" {
					req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+testRuntimeID+"/tasks/claim", nil, testWorkspaceID, "comment-load-test")
					req = withURLParam(req, "runtimeId", testRuntimeID)
					testutil.Call(t, broken.ClaimTaskByRuntime, req).Want(http.StatusInternalServerError)
				} else {
					req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/claim", map[string]any{
						"runtime_ids": []string{testRuntimeID}, "daemon_id": "comment-load-test", "max_tasks": 1,
					}, testWorkspaceID, "comment-load-test")
					var response struct {
						Tasks []AgentTaskResponse `json:"tasks"`
					}
					testutil.Call(t, broken.ClaimTasksByRuntime, req).Want(http.StatusOK).JSON(&response)
					if len(response.Tasks) != 0 {
						t.Fatalf("failed input read dispatched %d tasks", len(response.Tasks))
					}
				}
				if fault.hits == 0 {
					t.Fatal("the input read fault was not exercised")
				}
				task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
				if err != nil {
					t.Fatal(err)
				}
				if task.Status != "dispatched" || len(task.DeliveredCommentIds) != 0 {
					t.Fatalf("failed claim status=%s receipt=%v; want dispatched with no receipt", task.Status, task.DeliveredCommentIds)
				}
				var tokenCount int
				dbfx.QueryRow(t, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount)
				if tokenCount != 0 {
					t.Fatalf("failed claim minted %d task tokens", tokenCount)
				}

				// Advance the lease/recovery clocks without sleeping, then let the
				// normal claim endpoint reclaim the same unstarted task.
				dbfx.Exec(t, `UPDATE agent_task_queue
					SET dispatched_at = now() - interval '1 hour',
					    prepare_lease_expires_at = now() - interval '1 hour'
					WHERE id = $1`, taskID)
				req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+testRuntimeID+"/tasks/claim", nil, testWorkspaceID, "comment-load-test")
				req = withURLParam(req, "runtimeId", testRuntimeID)
				req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
				var response struct {
					Task *AgentTaskResponse `json:"task"`
				}
				testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
				if response.Task == nil || response.Task.ID != taskID {
					t.Fatalf("recovery did not deliver original task: %+v", response.Task)
				}
				if response.Task.TriggerCommentContent != "also preserve the user's edits" || len(response.Task.CoalescedComments) != 1 || response.Task.CoalescedComments[0].Content != "inspect the failure" || response.Task.CoalescedComments[0].AuthorType != "agent" {
					t.Fatal("recovered claim lost handoff instructions or agent authorship")
				}
				wantReceipt := []string{firstID, triggerID}
				slices.Sort(wantReceipt)
				slices.Sort(response.Task.DeliveredCommentIDs)
				if !slices.Equal(response.Task.DeliveredCommentIDs, wantReceipt) {
					t.Fatalf("recovered receipt = %v", response.Task.DeliveredCommentIDs)
				}
			})
		}
	}
}

func TestBuildCoalescedCommentDataMissingInputAndOptionalAuthor(t *testing.T) {
	issueID := dbfx.Issue(t, "Comment input read boundaries")
	rootID := dbfx.Comment(t, issueID, "root")
	replyID := dbfx.Comment(t, issueID, "reply", testutil.Cols{"parent_id": rootID})
	for _, tc := range []struct {
		name, query string
		id          pgtype.UUID
		err         error
		wantCount   int
	}{
		{"missing comment", "-- name: GetCommentInWorkspace :one", parseUUID(replyID), pgx.ErrNoRows, 1},
		{"missing thread", "-- name: GetCommentThreadRootID :one", parseUUID(replyID), pgx.ErrNoRows, 1},
		{"optional author", "-- name: GetUser :one", parseUUID(testUserID), errors.New("author lookup unavailable"), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := &failClaimCommentDB{DBTX: testPool, query: tc.query, id: tc.id, err: tc.err}
			h := *testHandler
			h.Queries = db.New(fault)
			comments, err := h.buildCoalescedCommentData(context.Background(), parseUUID(testWorkspaceID), []pgtype.UUID{parseUUID(rootID), parseUUID(replyID)})
			if err != nil {
				t.Fatal(err)
			}
			if fault.hits == 0 {
				t.Fatal("read fault was not exercised")
			}
			if len(comments) != tc.wantCount {
				t.Fatalf("got %d comments, want %d", len(comments), tc.wantCount)
			}
			if tc.wantCount == 1 && comments[0].ID != rootID {
				t.Fatal("missing reply must not discard the surviving root")
			}
			if tc.name == "optional author" {
				for _, c := range comments {
					if c.AuthorName != "" || c.AuthorType != "member" || c.Content == "" {
						t.Fatalf("author enrichment changed the input: %+v", c)
					}
				}
			}
		})
	}
}
