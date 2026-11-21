package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func previewMove(t *testing.T, source, target, project string) ProjectMovePlan {
	t.Helper()
	var plan ProjectMovePlan
	testutil.Call(t, testHandler.PreviewProjectMove, moveRequest(source, target, project, "")).Want(200).JSON(&plan)
	return plan
}
func executeMove(t *testing.T, source, target, project string) {
	t.Helper()
	plan := previewMove(t, source, target, project)
	testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, plan.PlanHash)).Want(200)
}

func TestProjectMoveExecution(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	source, target, project := moveFixture(t)
	sourceOnly := dbfx.User(t, "Source only", "source-only-"+uuid.NewString()+"@test.invalid")
	targetOnly := dbfx.User(t, "Target only", "target-only-"+uuid.NewString()+"@test.invalid")
	dbfx.Member(t, source, sourceOnly, "member")
	dbfx.Member(t, target, targetOnly, "member")
	dbfx.Exec(t, `UPDATE project SET lead_type='member',lead_id=$1 WHERE id=$2`, sourceOnly, project)
	dbfx.Insert(t, "issue_status", testutil.Cols{"workspace_id": source, "key": "review_custom", "name": "Custom", "category": "started", "color": "#123456"})
	sourceProperty := dbfx.Insert(t, "issue_property", testutil.Cols{"workspace_id": source, "name": "Risk", "type": "select", "config": `{"options":[{"id":"old-a","name":"High"},{"id":"old-b","name":"Low"}]}`})
	targetProperty := dbfx.Insert(t, "issue_property", testutil.Cols{"workspace_id": target, "name": "Risk", "type": "select", "config": `{"options":[{"id":"new-a","name":"High"}]}`})
	droppedProperty := dbfx.Insert(t, "issue_property", testutil.Cols{"workspace_id": source, "name": "Source only", "type": "text", "config": "{}"})
	i2 := dbfx.Issue(t, "Second", testutil.Cols{"workspace_id": source, "project_id": project, "number": 8, "status": "review_custom", "assignee_type": "member", "assignee_id": sourceOnly, "properties": fmt.Sprintf(`{"%s":"old-b","%s":"drop me"}`, sourceProperty, droppedProperty)})
	i1 := dbfx.Issue(t, "First", testutil.Cols{"workspace_id": source, "project_id": project, "number": 3, "assignee_type": "member", "assignee_id": testUserID, "properties": fmt.Sprintf(`{"%s":"old-a"}`, sourceProperty)})
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id=$1 WHERE id=$2`, i1, i2)
	dbfx.Insert(t, "issue_dependency", testutil.Cols{"issue_id": i2, "depends_on_issue_id": i1, "type": "blocks"})
	label := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": source, "name": "Bug", "color": "#123456", "description": "Source description", "resource_type": "issue"})
	newLabel := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": source, "name": "New label", "color": "#654321", "description": "Carry description", "resource_type": "issue"})
	targetLabel := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": target, "name": "bug", "color": "#ffffff", "resource_type": "issue"})
	for _, id := range []string{label, newLabel} {
		dbfx.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": i1, "label_id": id}, "issue_id=$1 AND label_id=$2", i1, id)
	}
	comment := dbfx.Comment(t, i1, "Original author", testutil.Cols{"workspace_id": source, "author_id": sourceOnly})
	attachment := dbfx.Insert(t, "attachment", testutil.Cols{"workspace_id": source, "comment_id": comment, "filename": "test.txt", "url": "/uploads/workspaces/" + source + "/original.txt", "content_type": "text/plain", "size_bytes": 4, "uploader_type": "member", "uploader_id": sourceOnly})
	activity := dbfx.Insert(t, "activity_log", testutil.Cols{"workspace_id": source, "issue_id": i1, "actor_type": "member", "actor_id": sourceOnly, "action": "created", "details": "{}"})
	reaction := dbfx.Insert(t, "comment_reaction", testutil.Cols{"workspace_id": source, "comment_id": comment, "actor_type": "member", "actor_id": sourceOnly, "emoji": "👍"})
	issueReaction := dbfx.Insert(t, "issue_reaction", testutil.Cols{"workspace_id": source, "issue_id": i1, "actor_type": "member", "actor_id": sourceOnly, "emoji": "👍"})
	dbfx.Insert(t, "pinned_item", testutil.Cols{"workspace_id": source, "user_id": sourceOnly, "item_type": "project", "item_id": project, "position": 0})
	workflow := dbfx.Insert(t, "issue_workflow", testutil.Cols{"workspace_id": source, "name": "Source flow", "initial_status_key": "todo", "steps": "[]"})
	dbfx.Exec(t, `UPDATE project SET workflow_id=$1 WHERE id=$2`, workflow, project)
	dbfx.InsertNoID(t, "issue_unread", testutil.Cols{"workspace_id": source, "user_id": sourceOnly, "issue_id": i1, "outcome": "manual"}, "issue_id=$1", i1)
	dbfx.Insert(t, "inbox_item", testutil.Cols{"workspace_id": source, "recipient_type": "member", "recipient_id": sourceOnly, "issue_id": i1, "type": "issue_assigned", "severity": "info", "title": "Moved"})
	for _, user := range []string{sourceOnly, testUserID} {
		dbfx.InsertNoID(t, "issue_subscriber", testutil.Cols{"issue_id": i1, "user_type": "member", "user_id": user, "reason": "manual"}, "issue_id=$1 AND user_id=$2", i1, user)
	}
	runtime := dbfx.Runtime(t, "Source runtime", testutil.Cols{"workspace_id": source})
	agent := dbfx.Agent(t, "Original agent", runtime, testutil.Cols{"workspace_id": source})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": i1, "runtime_id": runtime, "status": "completed", "result": `{"summary":"History result"}`})
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": task, "seq": 1, "type": "text", "content": "History message"})
	chat := dbfx.ChatSession(t, agent, testutil.Cols{"workspace_id": source, "project_id": project})
	plan := previewMove(t, source, target, project)
	if plan.Counts["issues"] != 2 || plan.Counts["comments"] != 1 || plan.Counts["attachments"] != 1 || plan.Counts["runs"] != 1 || !plan.LeadCleared || plan.SubscribersCleared != 1 || plan.VisibilityDelta["lose_access"] != 1 || plan.VisibilityDelta["gain_access"] != 1 {
		t.Fatalf("incomplete preview: %+v", plan)
	}
	testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, plan.PlanHash)).Want(200)
	for _, tc := range []struct{ table, id string }{{"project", project}, {"issue", i1}, {"issue", i2}, {"comment", comment}, {"attachment", attachment}, {"activity_log", activity}, {"comment_reaction", reaction}, {"issue_reaction", issueReaction}} {
		var workspace string
		dbfx.QueryRow(t, `SELECT workspace_id FROM `+tc.table+` WHERE id=$1`, tc.id).Scan(&workspace)
		if workspace != target {
			t.Errorf("%s remained in %s", tc.table, workspace)
		}
	}
	var first, second int
	var status string
	var assignee *string
	var props map[string]any
	dbfx.QueryRow(t, `SELECT number,assignee_id,properties FROM issue WHERE id=$1`, i1).Scan(&first, &assignee, &props)
	if first != 101 || assignee == nil || *assignee != testUserID || props[targetProperty] != "new-a" {
		t.Fatalf("first issue mapping: %d %v %v", first, assignee, props)
	}
	dbfx.QueryRow(t, `SELECT number,status,assignee_id,properties FROM issue WHERE id=$1`, i2).Scan(&second, &status, &assignee, &props)
	if second != 102 || status != "in_progress" || assignee != nil || len(props) != 0 {
		t.Fatalf("second issue mapping: %d %s %v %v", second, status, assignee, props)
	}
	for _, check := range []struct {
		sql  string
		want int
	}{
		{`SELECT count(*) FROM issue_to_label WHERE issue_id='` + i1 + `' AND label_id='` + targetLabel + `'`, 1},
		{`SELECT count(*) FROM issue_label WHERE workspace_id='` + target + `' AND name='New label' AND description='Carry description'`, 1},
		{`SELECT count(*) FROM issue_key_alias WHERE source_workspace_id='` + source + `' AND prefix='SRC' AND issue_id IN ('` + i1 + `','` + i2 + `')`, 2},
		{`SELECT count(*) FROM project_move WHERE source_workspace_id='` + source + `' AND target_workspace_id='` + target + `' AND project_id='` + project + `'`, 1},
		{`SELECT count(*) FROM pinned_item WHERE item_id='` + project + `'`, 0},
		{`SELECT count(*) FROM inbox_item WHERE issue_id='` + i1 + `'`, 0},
		{`SELECT count(*) FROM issue_unread WHERE issue_id='` + i1 + `'`, 0},
		{`SELECT count(*) FROM project WHERE id='` + project + `' AND workflow_id IS NULL`, 1},
		{`SELECT count(*) FROM issue_subscriber WHERE issue_id='` + i1 + `' AND user_id='` + sourceOnly + `'`, 0},
		{`SELECT count(*) FROM chat_session WHERE id='` + chat + `' AND project_id IS NULL`, 1},
	} {
		if got := dbfx.Count(t, check.sql); got != check.want {
			t.Errorf("%s: want %d got %d", check.sql, check.want, got)
		}
	}
	var lead *string
	dbfx.QueryRow(t, `SELECT lead_id FROM project WHERE id=$1`, project).Scan(&lead)
	if lead != nil {
		t.Error("lead not cleared")
	}
	var url string
	dbfx.QueryRow(t, `SELECT url FROM attachment WHERE id=$1`, attachment).Scan(&url)
	if url != "/uploads/workspaces/"+source+"/original.txt" {
		t.Error("storage key changed")
	}
	req := withURLParam(newRequest("GET", "/", nil), "id", i1)
	req.Header.Set("X-Workspace-ID", target)
	req.Header.Set("X-User-ID", targetOnly)
	var runs []map[string]any
	testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusOK).JSON(&runs)
	if len(runs) != 1 || runs[0]["agent_id"] != agent {
		t.Fatalf("run history lost: %#v", runs)
	}
	messagesReq := withURLParam(newRequest("GET", "/", nil), "taskId", task)
	messagesReq.Header.Set("X-Workspace-ID", target)
	messagesReq.Header.Set("X-User-ID", targetOnly)
	var messages []map[string]any
	testutil.Call(t, testHandler.ListTaskMessagesByUser, messagesReq.WithContext(middleware.SetMemberContext(messagesReq.Context(), target, db.Member{}))).Want(http.StatusOK).JSON(&messages)
	if len(messages) != 1 || messages[0]["content"] != "History message" {
		t.Fatalf("messages lost: %#v", messages)
	}
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id IN ($1,$2)`, i1, i2)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	dbfx.Cleanup(t, `DELETE FROM issue_label WHERE workspace_id=$1`, target)
}

func TestProjectMoveLateBlocker(t *testing.T) {
	source, target, project := moveFixture(t)
	issue := dbfx.Issue(t, "Move", testutil.Cols{"workspace_id": source, "project_id": project})
	plan := previewMove(t, source, target, project)
	dbfx.Insert(t, "project_resource", testutil.Cols{"workspace_id": source, "project_id": project, "resource_type": "local_directory", "resource_ref": "{}", "created_by": testUserID})
	testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, plan.PlanHash)).Want(409)
	if got := dbfx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND workspace_id=$2`, issue, source); got != 1 {
		t.Error("blocked move wrote data")
	}
}
