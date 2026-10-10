package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAssignedSquadHandoffRequiresVerifiedDelivery(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderRuntime := dbfx.Runtime(t, "handoff leader runtime")
	leader := dbfx.Agent(t, "handoff leader", leaderRuntime)
	workerRuntime := dbfx.Runtime(t, "handoff worker runtime")
	worker := dbfx.Agent(t, "handoff worker", workerRuntime)
	squad := dbfx.Squad(t, "handoff squad", leader)
	issueID := dbfx.Issue(t, "handoff gate", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": squad,
	})
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	leaderTask := dbfx.Task(t, leader, testutil.Cols{
		"runtime_id": leaderRuntime, "issue_id": issueID, "status": "completed",
		"is_leader_task": true, "squad_id": squad,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	delegated := dbfx.Task(t, worker, testutil.Cols{
		"runtime_id": workerRuntime, "issue_id": issueID, "status": "running",
		"squad_id": squad, "delegated_from_task_id": leaderTask,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	unrelated := dbfx.Task(t, worker, testutil.Cols{
		"runtime_id": workerRuntime, "issue_id": issueID, "status": "running",
		"squad_id":           squad,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	check := func(name, actorType, taskID, commentType string, want int) {
		t.Helper()
		opts := commentTriggerComputeOptions{OriginatorUserID: testUserID, CommentType: commentType}
		if taskID != "" {
			opts.AuthoringTaskID = parseUUID(taskID)
		}
		got, _ := testHandler.computeCommentAgentTriggers(ctx, issue, "Work status", nil, actorType, worker, opts)
		if len(got) != want {
			t.Errorf("%s: got %d leader triggers, want %d", name, len(got), want)
		}
	}
	check("verified delivery", "agent", delegated, "comment", 1)
	check("progress update", "agent", delegated, "progress_update", 0)
	check("status narration", "agent", delegated, "status_change", 0)
	check("system comment type", "agent", delegated, "system", 0)
	check("unrelated run", "agent", unrelated, "comment", 0)
	check("missing source task", "agent", "", "comment", 0)
	check("system failure relay", "system", delegated, "comment", 0)
	// An explicit @ remains a deliberate dispatch even from an interim note.
	mention := "please continue [@handoff squad](mention://squad/" + squad + ")"
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, mention, nil, "agent", worker,
		commentTriggerComputeOptions{OriginatorUserID: testUserID, CommentType: "progress_update", AuthoringTaskID: parseUUID(delegated)})
	if len(triggers) != 1 || triggers[0].Source != commentTriggerSourceMentionSquadLeader {
		t.Fatalf("explicit squad mention in progress note did not dispatch: %+v", triggers)
	}
	for _, tc := range []struct {
		kind string
		want int
	}{{"comment", 1}, {"progress_update", 0}} {
		r := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments/trigger-preview", CommentTriggerPreviewRequest{Content: "Work status", Type: tc.kind}), "id", issueID)
		r.Header.Set("X-Agent-ID", worker)
		r.Header.Set("X-Task-ID", delegated)
		w := httptest.NewRecorder()
		testHandler.PreviewCommentTriggers(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s preview status %d: %s", tc.kind, w.Code, w.Body.String())
		}
		var preview CommentTriggerPreviewResponse
		if err := json.NewDecoder(w.Body).Decode(&preview); err != nil {
			t.Fatal(err)
		}
		if len(preview.Agents) != tc.want {
			t.Fatalf("%s preview agents = %d, want %d", tc.kind, len(preview.Agents), tc.want)
		}
	}
}
