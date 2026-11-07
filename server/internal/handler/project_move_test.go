package handler

import (
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"net/http"
	"testing"
)

func moveFixture(t *testing.T) (string, string, string) {
	t.Helper()
	source := dbfx.Workspace(t, "Move source", "move-src-"+uuid.NewString(), testutil.Cols{"issue_prefix": "SRC", "issue_counter": 10})
	target := dbfx.Workspace(t, "Move target", "move-dst-"+uuid.NewString(), testutil.Cols{"issue_prefix": "DST", "issue_counter": 100})
	dbfx.Member(t, source, testUserID, "owner")
	dbfx.Member(t, target, testUserID, "admin")
	project := dbfx.Project(t, "Moving project", testutil.Cols{"workspace_id": source})
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE source_workspace_id IN ($1,$2)
 OR issue_id IN (SELECT (item->>'id')::uuid FROM project_move m,
 jsonb_array_elements(m.summary->'issues') item WHERE m.project_id=$3)`, source, target, project)
	return source, target, project
}

func moveRequest(source, target, project, hash string) *http.Request {
	req := withURLParam(newRequest("POST", "/", map[string]any{"target_workspace_id": target, "plan_hash": hash}), "id", project)
	req.Header.Set("X-Workspace-ID", source)
	return req
}

func TestProjectMovePermissions(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	for _, tc := range []struct {
		source, target, actor string
		want                  int
	}{
		{"owner", "owner", "", 200}, {"owner", "admin", "", 200},
		{"admin", "owner", "", 403}, {"member", "owner", "", 403},
		{"owner", "member", "", 403}, {"owner", "none", "", 404},
		{"owner", "admin", "task_token", 403}, {"owner", "admin", "cloud_pat", 403},
	} {
		t.Run(tc.source+"-"+tc.target+"-"+tc.actor, func(t *testing.T) {
			source, target, project := moveFixture(t)
			dbfx.Exec(t, `UPDATE member SET role=$1 WHERE workspace_id=$2 AND user_id=$3`, tc.source, source, testUserID)
			if tc.target == "none" {
				dbfx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, target, testUserID)
			} else {
				dbfx.Exec(t, `UPDATE member SET role=$1 WHERE workspace_id=$2 AND user_id=$3`, tc.target, target, testUserID)
			}
			req := moveRequest(source, target, project, "")
			req.Header.Set("X-Actor-Source", tc.actor)
			testutil.Call(t, testHandler.PreviewProjectMove, req).Want(tc.want)
			if tc.want != 200 {
				testutil.Call(t, testHandler.MoveProject, moveRequestWithActor(source, target, project, tc.actor)).Want(tc.want)
			}
		})
	}
	source, _, project := moveFixture(t)
	testutil.Call(t, testHandler.PreviewProjectMove, moveRequest(source, source, project, "")).Want(400)
}

func TestProjectMovePreviewBlockers(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	for _, kind := range []string{"active_task", "enabled_wakeup", "autopilot", "open_github_pr", "open_vcs_pr", "parent_link", "dependency_link", "duplicate_link", "source_context_link", "local_directory"} {
		t.Run(kind, func(t *testing.T) {
			source, target, project := moveFixture(t)
			issue := dbfx.Issue(t, "Moving", testutil.Cols{"workspace_id": source, "project_id": project})
			outside := dbfx.Issue(t, "Outside", testutil.Cols{"workspace_id": source})
			runtime := dbfx.Runtime(t, "Move runtime", testutil.Cols{"workspace_id": source})
			agent := dbfx.Agent(t, "Move agent", runtime, testutil.Cols{"workspace_id": source})
			switch kind {
			case "active_task":
				dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": runtime, "status": "queued"})
			case "enabled_wakeup":
				dbfx.Insert(t, "issue_wakeup", testutil.Cols{"id": uuid.NewString(), "mode": "once", "workspace_id": source, "issue_id": issue, "agent_id": agent, "created_by": testUserID, "instruction": "wake", "kind": "event", "event_types": []string{"comment.created"}, "enabled": true})
			case "autopilot":
				dbfx.Insert(t, "autopilot", testutil.Cols{"workspace_id": source, "project_id": project, "title": "Move autopilot", "assignee_id": agent, "created_by_type": "member", "created_by_id": testUserID})
			case "local_directory":
				dbfx.Insert(t, "project_resource", testutil.Cols{"workspace_id": source, "project_id": project, "resource_type": "local_directory", "resource_ref": "{}", "created_by": testUserID})
			case "parent_link":
				dbfx.Exec(t, `UPDATE issue SET parent_issue_id=$1 WHERE id=$2`, outside, issue)
			case "duplicate_link":
				dbfx.Exec(t, `UPDATE issue SET duplicate_of_issue_id=$1 WHERE id=$2`, outside, issue)
			case "dependency_link":
				dbfx.Insert(t, "issue_dependency", testutil.Cols{"issue_id": outside, "depends_on_issue_id": issue, "type": "blocks"})
			case "source_context_link":
				dbfx.Insert(t, "issue_source_context", testutil.Cols{"id": uuid.NewString(), "state": "attached", "attached_at": testutil.Raw("now()"), "anchor_comment_id": uuid.NewString(), "snapshot_version": 1, "workspace_id": source, "issue_id": outside, "source_issue_id": issue, "captured_by_user_id": testUserID, "capture_digest": "move-test", "snapshot": "{}"})
			case "open_github_pr":
				pr := dbfx.Insert(t, "github_pull_request", testutil.Cols{"workspace_id": source, "installation_id": 987654321, "repo_owner": "move", "repo_name": "move", "pr_number": 1, "title": "Open", "state": "open", "pr_created_at": testutil.Raw("now()"), "pr_updated_at": testutil.Raw("now()"), "html_url": "https://example.invalid/pr"})
				dbfx.InsertNoID(t, "issue_pull_request", testutil.Cols{"issue_id": issue, "pull_request_id": pr}, "issue_id=$1", issue)
			case "open_vcs_pr":
				connection := dbfx.Insert(t, "vcs_connection", testutil.Cols{"workspace_id": source, "provider": "gitlab", "instance_url": "https://example.invalid", "account_login": "move", "access_token_encrypted": "test", "webhook_secret_encrypted": "test", "connected_by_id": testUserID})
				pr := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{"workspace_id": source, "connection_id": connection, "provider": "gitlab", "repo_owner": "move", "repo_name": "move", "pr_number": 1, "title": "Open", "state": "open", "pr_created_at": testutil.Raw("now()"), "pr_updated_at": testutil.Raw("now()"), "html_url": "https://example.invalid/pr"})
				dbfx.InsertNoID(t, "issue_vcs_pull_request", testutil.Cols{"issue_id": issue, "pull_request_id": pr}, "issue_id=$1", issue)
			}
			var plan map[string]any
			testutil.Call(t, testHandler.PreviewProjectMove, moveRequest(source, target, project, "")).Want(200).JSON(&plan)
			blockers, ok := plan["blockers"].([]any)
			if !ok || len(blockers) != 1 {
				t.Fatalf("want one %s blocker: %#v", kind, plan)
			}
			if blockers[0].(map[string]any)["kind"] != kind {
				t.Fatalf("wrong blocker: %#v", blockers)
			}
			hash, _ := plan["plan_hash"].(string)
			testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, hash)).Want(409)
		})
	}
}

func TestProjectMovePreviewDeterministicAndStale(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	source, target, project := moveFixture(t)
	issue := dbfx.Issue(t, "Moving", testutil.Cols{"workspace_id": source, "project_id": project})
	var first, second map[string]any
	testutil.Call(t, testHandler.PreviewProjectMove, moveRequest(source, target, project, "")).Want(200).JSON(&first)
	testutil.Call(t, testHandler.PreviewProjectMove, moveRequest(source, target, project, "")).Want(200).JSON(&second)
	hash, _ := first["plan_hash"].(string)
	if len(hash) != 64 || hash != second["plan_hash"] {
		t.Fatalf("non-deterministic plan: %#v %#v", first, second)
	}
	dbfx.Exec(t, `UPDATE issue SET title='Changed',revision=revision+1 WHERE id=$1`, issue)
	testutil.Call(t, testHandler.MoveProject, moveRequest(source, target, project, hash)).Want(409)
}

func moveRequestWithActor(source, target, project, actor string) *http.Request {
	req := moveRequest(source, target, project, "")
	req.Header.Set("X-Actor-Source", actor)
	return req
}
