package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentSelfMention_SourceIssueAndThread(t *testing.T) {
	for _, mode := range []string{"same thread", "different thread", "different issue"} {
		t.Run(mode, func(t *testing.T) {
			runtimeID := dbfx.Runtime(t, "Self mention runtime")
			agentID := dbfx.Agent(t, "Self mention author", runtimeID)
			otherAgentID := dbfx.Agent(t, "Other mentioned agent", runtimeID)
			issueID := dbfx.Issue(t, "Authoring issue")
			rootID := dbfx.Comment(t, issueID, "Please investigate")
			var triggerID any = rootID
			if mode == "different thread" {
				// Assignment runs may comment in any thread; comment-triggered
				// runs must reply only to their covered parents.
				triggerID = nil
			}
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
				"trigger_comment_id": triggerID, "delivered_comment_ids": []string{rootID},
				"created_at": testutil.Raw("now() - interval '1 minute'"), "started_at": testutil.Raw("now()"),
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
			})
			targetIssue, parentID := issueID, rootID
			if mode == "different issue" {
				targetIssue = dbfx.Issue(t, "Cross issue notification")
			}
			if mode != "same thread" {
				parentID = dbfx.Comment(t, targetIssue, "Another thread")
			}
			if mode == "different issue" {
				// A newer local run of the same agent is not evidence that
				// this comment was authored on the target issue.
				dbfx.Task(t, agentID, testutil.Cols{
					"runtime_id": runtimeID, "issue_id": targetIssue, "status": "running",
					"trigger_comment_id": parentID,
				})
			}
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1 AND id <> $2`, targetIssue, taskID)
			dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1 AND source_task_id = $2`, targetIssue, taskID)
			token := "mat_self_mention_" + taskID
			dbfx.Insert(t, "task_token", testutil.Cols{
				"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID,
				"workspace_id": testWorkspaceID, "user_id": testUserID, "expires_at": time.Now().Add(time.Hour),
			})
			call := func(handle http.HandlerFunc, method, path, param, id string, body map[string]any) *testutil.Response {
				t.Helper()
				req := withURLParam(newRequest(method, path, body), param, id)
				req.Header.Set("Authorization", "Bearer "+token)
				// The authenticated token, not these client-supplied identities,
				// must determine whose run authored the comment.
				req.Header.Set("X-Agent-ID", "forged-agent")
				req.Header.Set("X-Task-ID", "forged-task")
				protected := middleware.Auth(testHandler.Queries, nil, nil, nil)(
					middleware.RequireWorkspaceMember(testHandler.Queries)(handle),
				)
				return testutil.Call(t, protected.ServeHTTP, req)
			}
			content := mention(agentID) + " " + mention(otherAgentID)
			body := map[string]any{"content": content, "parent_id": parentID}
			var preview CommentTriggerPreviewResponse
			call(testHandler.PreviewCommentTriggers, http.MethodPost, "/api/issues/"+targetIssue+"/comments/trigger-preview", "id", targetIssue, body).
				Want(http.StatusOK).JSON(&preview)
			wantQueued := 0
			if mode == "different issue" {
				wantQueued = 1
			}
			if mode == "different issue" {
				requirePreviewAgents(t, preview, agentID, otherAgentID)
			} else {
				requirePreviewAgents(t, preview, otherAgentID)
			}
			var comment CommentResponse
			call(testHandler.CreateComment, http.MethodPost, "/api/issues/"+targetIssue+"/comments", "id", targetIssue, body).
				Want(http.StatusCreated).JSON(&comment)
			if comment.AuthorType != "agent" || comment.AuthorID != agentID || comment.SourceTaskID == nil || *comment.SourceTaskID != taskID {
				t.Fatalf("comment lost its token-bound author/source task: %+v", comment)
			}
			outcome := findCommentOutcome(t, comment.TriggerOutcomes, agentID)
			wantStatus, wantReason := DispatchBlocked, ReasonSelfTriggerSuppressed
			if mode == "same thread" {
				wantStatus, wantReason = DispatchDeferred, ReasonAlreadyActive
			} else if mode == "different issue" {
				wantStatus, wantReason = DispatchQueued, ReasonQueued
			}
			if outcome.Status != wantStatus || outcome.ReasonCode != wantReason {
				t.Errorf("outcome = %+v, want %s/%s", outcome, wantStatus, wantReason)
			}
			if got := countQueuedOrDispatched(t, agentID, targetIssue); got != wantQueued {
				t.Errorf("queued tasks = %d, want %d", got, wantQueued)
			}
			if other := findCommentOutcome(t, comment.TriggerOutcomes, otherAgentID); other.Status != DispatchQueued {
				t.Errorf("mention of the other agent = %+v, want queued", other)
			}
			body["content"] = content + " Updated evidence."
			body["editing_comment_id"] = comment.ID
			call(testHandler.PreviewCommentTriggers, http.MethodPost, "/api/issues/"+targetIssue+"/comments/trigger-preview", "id", targetIssue, body).
				Want(http.StatusOK).JSON(&preview)
			if mode == "different issue" {
				requirePreviewAgents(t, preview, agentID, otherAgentID)
			} else {
				requirePreviewAgents(t, preview, otherAgentID)
			}
			call(testHandler.UpdateComment, http.MethodPatch, "/api/comments/"+comment.ID, "commentId", comment.ID, body).
				Want(http.StatusOK).JSON(&comment)
			if comment.SourceTaskID == nil || *comment.SourceTaskID != taskID {
				t.Fatal("edit lost its token-bound source task")
			}
			if edited := findCommentOutcome(t, comment.TriggerOutcomes, agentID); edited.Status != wantStatus || edited.ReasonCode != wantReason {
				t.Errorf("edit outcome = %+v, want %s/%s", edited, wantStatus, wantReason)
			}
			if got := countQueuedOrDispatched(t, agentID, targetIssue); got != wantQueued {
				t.Errorf("queued tasks after edit = %d, want %d", got, wantQueued)
			}
			if mode == "same thread" {
				completeWorkerReplyRun(t, taskID)
				if got := countQueuedOrDispatched(t, agentID, issueID); got != 0 {
					t.Errorf("completion replay queued %d self-triggered tasks, want 0", got)
				}
			}
		})
	}
}

func TestCommentSelfMention_RequiresMatchingSourceTask(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Source identity runtime")
	agentID := dbfx.Agent(t, "Source identity author", runtimeID)
	otherAgentID := dbfx.Agent(t, "Other source identity", runtimeID)
	issueID := dbfx.Issue(t, "Source identity issue")
	otherIssueID := dbfx.Issue(t, "Other source issue")
	for _, tc := range []struct {
		name, sourceAgent, sourceIssue, authorType string
		missing                                    bool
	}{
		{name: "legacy unstamped comment", missing: true, authorType: "agent"},
		{name: "different source agent", sourceAgent: otherAgentID, sourceIssue: issueID, authorType: "agent"},
		{name: "different source issue", sourceAgent: agentID, sourceIssue: otherIssueID, authorType: "agent"},
		{name: "source without issue", sourceAgent: agentID, authorType: "agent"},
		{name: "member mention", sourceAgent: agentID, sourceIssue: issueID, authorType: "member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sourceID pgtype.UUID
			if !tc.missing {
				var sourceIssue any
				if tc.sourceIssue != "" {
					sourceIssue = tc.sourceIssue
				}
				sourceID = parseUUID(dbfx.Task(t, tc.sourceAgent, testutil.Cols{
					"runtime_id": runtimeID, "issue_id": sourceIssue, "status": "completed",
				}))
			}
			authorID := agentID
			if tc.authorType == "member" {
				authorID = testUserID
			}
			triggers, _ := testHandler.computeCommentAgentTriggers(context.Background(), db.Issue{
				ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
			}, mention(agentID), nil, tc.authorType, authorID, commentTriggerComputeOptions{
				AuthoringTaskID: sourceID, OriginatorUserID: testUserID,
			})
			if len(triggers) != 1 || triggers[0].Agent.ID != parseUUID(agentID) {
				t.Fatalf("non-self/cross-issue mention was suppressed: %+v", triggers)
			}
		})
	}
}

func TestCommentSelfMention_QueryErrors(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Self mention query errors")
	agentID := dbfx.Agent(t, "Self mention query author", runtimeID)
	issueID := dbfx.Issue(t, "Self mention query issue")
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
	})
	for _, query := range []string{"GetAgentTask", "HasActiveTaskForIssueAndAgentInThread"} {
		t.Run(query, func(t *testing.T) {
			h := *testHandler
			h.Queries = db.New(failQueryDBTX{DBTX: testPool, failOn: "-- name: " + query + " :one", err: errors.New("database unavailable")})
			triggers, targets := h.computeCommentAgentTriggers(context.Background(), db.Issue{
				ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
			}, mention(agentID), nil, "agent", agentID, commentTriggerComputeOptions{
				AuthoringTaskID: parseUUID(sourceID), OriginatorUserID: testUserID,
			})
			if len(triggers) != 0 {
				t.Fatal("query failure must not enqueue a self-trigger")
			}
			outcome := findCommentOutcome(t, commentTriggerOutcomes(targets, nil), agentID)
			if outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInternalError {
				t.Fatalf("query failure outcome = %+v, want blocked/internal_error", outcome)
			}
		})
	}
}

func TestCommentSelfMention_PermissionGatePrecedesSuppression(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "Self mention permissions")
	agentID := dbfx.Agent(t, "Private self mention author", runtimeID)
	issueID := dbfx.Issue(t, "Self mention permission issue")
	sourceID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
	})
	otherUserID := dbfx.User(t, "Other originator", "self-mention-originator@multica.test")
	dbfx.Member(t, testWorkspaceID, otherUserID, "member")
	triggers, targets := testHandler.computeCommentAgentTriggers(context.Background(), db.Issue{
		ID: parseUUID(issueID), WorkspaceID: parseUUID(testWorkspaceID),
	}, mention(agentID), nil, "agent", agentID, commentTriggerComputeOptions{
		AuthoringTaskID: parseUUID(sourceID), OriginatorUserID: otherUserID,
	})
	if len(triggers) != 0 {
		t.Fatal("an originator who cannot invoke the agent must not enqueue it")
	}
	outcome := findCommentOutcome(t, commentTriggerOutcomes(targets, nil), agentID)
	if outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("permission denial = %+v, want blocked/invocation_not_allowed", outcome)
	}
}
