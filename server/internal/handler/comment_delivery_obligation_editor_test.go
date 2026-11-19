package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCommentDeliveryObligationReplaysAcceptedEditor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, access := range []string{"retained", "ownership transferred", "membership removed"} {
		t.Run(access, func(t *testing.T) {
			editor := dbfx.User(t, "Accepted instruction editor", "obligation-editor-"+testWorkspaceID+"-"+access+"@example.test")
			dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": editor, "role": "admin"})
			runtimeID := dbfx.Runtime(t, "editor obligation runtime", testutil.Cols{"owner_id": editor, "visibility": "public"})
			agentID := dbfx.Agent(t, "editor private recipient", runtimeID, testutil.Cols{"owner_id": editor, "permission_mode": "private"})
			issueID := dbfx.Issue(t, "editor obligation issue")
			seedDupRacePR(t, issueID, 9995988)
			root := dbfx.Comment(t, issueID, "Old-head instruction")
			commentID := dbfx.Comment(t, issueID, "Original author text", testutil.Cols{"parent_id": root})
			blocker := dbfx.Task(t, agentID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root,
				"status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA},
			})
			dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
			var response CommentResponse
			testutil.Call(t, testHandler.UpdateComment, withURLParam(newRequestAs(editor, "PATCH", "/api/comments/"+commentID, map[string]any{
				"content": "[@Editor agent](mention://agent/" + agentID + ") Review this head", "expected_revision": 1,
			}), "commentId", commentID)).Want(200).JSON(&response)
			if len(response.TriggerOutcomes) != 1 || response.TriggerOutcomes[0].Status != DispatchDeferred {
				t.Fatalf("editor's instruction was not accepted for deferred delivery: %+v", response.TriggerOutcomes)
			}
			switch access {
			case "ownership transferred":
				dbfx.Exec(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, agentID, testUserID)
			case "membership removed":
				dbfx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, editor)
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now(), delivered_comment_ids=ARRAY[trigger_comment_id] WHERE id=$1`, blocker)
			dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, commentID)
			if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
				t.Fatal(err)
			}
			active := commentCoveredByAnyActiveTask(t, issueID, agentID, commentID)
			if active != (access == "retained") {
				t.Fatalf("accepted editor access=%s, active=%v; original author's authority must not substitute for the editor", access, active)
			}
			if active {
				var originator, accountable string
				dbfx.QueryRow(t, `SELECT originator_user_id, accountable_user_id FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, issueID, agentID).Scan(&originator, &accountable)
				if originator != editor || accountable != editor {
					t.Fatalf("editor attribution originator=%s accountable=%s, want %s", originator, accountable, editor)
				}
			}
		})
	}
}

func TestCommentDeliveryObligationRechecksProjectAccessBeforeMerge(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	f := newPrivateProjectFixture(t)
	runtimeID := dbfx.Runtime(t, "private project obligation runtime", testutil.Cols{"visibility": "public"})
	agentID := dbfx.Agent(t, "private project obligation recipient", runtimeID, testutil.Cols{"owner_id": f.insider})
	seedDupRacePR(t, f.secretIssue, 9995989)
	blocker := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": f.secretIssue, "trigger_comment_id": f.secretComment,
		"status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA},
	})
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, f.secretIssue)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, f.secretIssue)
	var accepted CommentResponse
	testutil.Call(t, testHandler.CreateComment, withURLParam(newRequestAs(f.insider, "POST", "/api/issues/"+f.secretIssue+"/comments", map[string]any{
		"content": "[@Target](mention://agent/" + agentID + ") Review this private project", "parent_id": f.secretComment,
	}), "id", f.secretIssue)).Want(201).JSON(&accepted)
	if len(accepted.TriggerOutcomes) != 1 || accepted.TriggerOutcomes[0].Status != DispatchDeferred {
		t.Fatalf("input not accepted for deferred delivery: %+v", accepted.TriggerOutcomes)
	}
	removeProjectMemberRow(t, f.projectID, f.insider)
	// The queued task can now accept the reviewed head, but that does not
	// preserve authority to add new input after project access was revoked.
	dbfx.Exec(t, `UPDATE agent_task_queue SET context=jsonb_build_object('head_sha',$2::text) WHERE id=$1`, blocker, dupRaceHeadB)
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, accepted.ID)
	if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if commentCovered(t, f.secretIssue, agentID, accepted.ID, "queued") {
		t.Fatal("merged new input after the accepted actor lost private project access")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM comment_delivery_obligation WHERE comment_id=$1`, accepted.ID); n != 0 {
		t.Error("explicit project access denial retained obsolete input")
	}
	addProjectMemberRow(t, f.projectID, f.insider)
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, accepted.ID)
	if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	if commentCovered(t, f.secretIssue, agentID, accepted.ID, "queued") {
		t.Fatal("restoring project access replayed obsolete input")
	}
}

func TestCommentDeliveryObligationKeepsEditingAgentLineageAfterTaskEnds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	human := dbfx.User(t, "Editing agent's human", "obligation-agent-editor-"+testWorkspaceID+"@example.test")
	dbfx.Insert(t, "member", testutil.Cols{"workspace_id": testWorkspaceID, "user_id": human, "role": "member"})
	runtimeID := dbfx.Runtime(t, "editing agent runtime", testutil.Cols{"visibility": "public"})
	writer := dbfx.Agent(t, "editing agent", runtimeID, testutil.Cols{"owner_id": human})
	target := dbfx.Agent(t, "editing agent recipient", runtimeID, testutil.Cols{"owner_id": human})
	issueID := dbfx.Issue(t, "editing agent lineage")
	seedDupRacePR(t, issueID, 9995990)
	root := dbfx.Comment(t, issueID, "Old-head blocker")
	oldSource := dbfx.Task(t, writer, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "completed", "originator_user_id": testUserID, "accountable_user_id": testUserID})
	editingTask := dbfx.Task(t, writer, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "originator_user_id": human, "accountable_user_id": human})
	commentID := dbfx.Comment(t, issueID, "Earlier agent text", testutil.Cols{"parent_id": root, "author_type": "agent", "author_id": writer, "source_task_id": oldSource})
	blocker := dbfx.Task(t, target, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root,
		"status": "queued", "context": map[string]any{"head_sha": dupRaceHeadA},
	})
	dbfx.Cleanup(t, `DELETE FROM comment_delivery_obligation WHERE issue_id=$1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, issueID)
	req := newRequestAs(human, "PATCH", "/api/comments/"+commentID, map[string]any{
		"content": "[@Target](mention://agent/" + target + ") Review the current head", "expected_revision": 1,
	})
	req.Header.Set("X-Agent-ID", writer)
	req.Header.Set("X-Task-ID", editingTask)
	var accepted CommentResponse
	testutil.Call(t, testHandler.UpdateComment, withURLParam(req, "commentId", commentID)).Want(200).JSON(&accepted)
	if len(accepted.TriggerOutcomes) != 1 || accepted.TriggerOutcomes[0].Status != DispatchDeferred {
		t.Fatalf("editing action was not accepted: %+v", accepted.TriggerOutcomes)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id IN ($1,$2)`, editingTask, blocker)
	dbfx.Exec(t, `UPDATE comment_delivery_obligation SET next_attempt_at='-infinity' WHERE comment_id=$1`, commentID)
	if err := testHandler.DrainCommentDeliveryObligations(t.Context(), 50); err != nil {
		t.Fatal(err)
	}
	var originator, accountable, delegatedFrom, source string
	dbfx.QueryRow(t, `SELECT originator_user_id, accountable_user_id, delegated_from_task_id, originator_source FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, issueID, target).Scan(&originator, &accountable, &delegatedFrom, &source)
	if originator != human || accountable != human || delegatedFrom != editingTask || source != "delegation" {
		t.Fatalf("editing action lineage originator=%s accountable=%s parent=%s source=%s", originator, accountable, delegatedFrom, source)
	}
}
