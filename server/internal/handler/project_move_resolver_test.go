package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func resolveMoveRequest(source, query, user string) *http.Request {
	req := newRequest("GET", "/api/moves/resolve?"+query, nil)
	req.Header.Set("X-Workspace-ID", source)
	req.Header.Set("X-User-ID", user)
	return req
}

func TestProjectMoveResolverAndMultiHop(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	a, b, project := moveFixture(t)
	c := dbfx.Workspace(t, "Third", "move-third-"+uuid.NewString(), testutil.Cols{"issue_prefix": "THIRD", "issue_counter": 200})
	dbfx.Member(t, c, testUserID, "owner")
	dbfx.Exec(t, `UPDATE member SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, b, testUserID)
	sourceOnly := dbfx.User(t, "Source reader", "move-reader-"+uuid.NewString()+"@test.invalid")
	dbfx.Member(t, a, sourceOnly, "member")
	issue := dbfx.Issue(t, "Hopping", testutil.Cols{"workspace_id": a, "project_id": project, "number": 7})
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	executeMove(t, a, b, project)
	executeMove(t, b, c, project)
	for _, tc := range []struct{ source, key string }{{a, "SRC-7"}, {b, "DST-101"}} {
		var result map[string]any
		testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(tc.source, "issue="+tc.key, testUserID)).Want(200).JSON(&result)
		if result["moved"] != true || result["issue_id"] != issue || result["identifier"] != "THIRD-201" || result["workspace_slug"] == nil || result["moved_at"] == nil {
			t.Fatalf("alias not one-hop: %#v", result)
		}
	}
	var hidden map[string]any
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(a, "issue=SRC-7", sourceOnly)).Want(200).JSON(&hidden)
	if len(hidden) != 2 || hidden["moved"] != true || hidden["moved_at"] == nil {
		t.Fatalf("target leaked: %#v", hidden)
	}
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(a, "project="+project, sourceOnly)).Want(200).JSON(&hidden)
	if len(hidden) != 2 {
		t.Fatalf("project location leaked: %#v", hidden)
	}
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(a, "issue=SRC-99999", testUserID)).Want(404)
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(b, "issue=DST-101", sourceOnly)).Want(404)
	req := resolveMoveRequest(a, "issue=SRC-7", testUserID)
	req.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.ResolveProjectMove, req).Want(403)
	executeMove(t, c, a, project)
	var number int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id=$1`, issue).Scan(&number)
	if number != 7 {
		t.Fatalf("return move got %d, want 7", number)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM issue_key_alias WHERE source_workspace_id=$1 AND issue_id=$2`, a, issue); got != 0 {
		t.Errorf("return alias not consumed: %d", got)
	}
	var counter int
	dbfx.QueryRow(t, `SELECT issue_counter FROM workspace WHERE id=$1`, a).Scan(&counter)
	if counter != 10 {
		t.Errorf("return move advanced counter: %d", counter)
	}
}

func TestProjectMoveReturnOccupiedNumber(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	a, b, project := moveFixture(t)
	dbfx.Exec(t, `UPDATE member SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, b, testUserID)
	issue := dbfx.Issue(t, "Return", testutil.Cols{"workspace_id": a, "project_id": project, "number": 7})
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	executeMove(t, a, b, project)
	occupied := dbfx.Issue(t, "Legacy collision", testutil.Cols{"workspace_id": a, "number": 7})
	executeMove(t, b, a, project)
	var number int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id=$1`, issue).Scan(&number)
	if number != 11 {
		t.Fatalf("occupied alias number overwritten: %d", number)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_key_alias WHERE source_workspace_id=$1 AND issue_id=$2`, a, issue); n != 0 {
		t.Errorf("return move retained stale target alias: %d", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE id=$1 AND number=7`, occupied); n != 1 {
		t.Error("occupied issue was changed")
	}
}

func TestProjectMoveReturnRestoresNumbers(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	a, b, project := moveFixture(t)
	dbfx.Exec(t, `UPDATE member SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, b, testUserID)
	issue := dbfx.Issue(t, "Return", testutil.Cols{"workspace_id": a, "project_id": project, "number": 7})
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	executeMove(t, a, b, project)
	executeMove(t, b, a, project)
	var number int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id=$1`, issue).Scan(&number)
	if number != 7 {
		t.Errorf("return number=%d", number)
	}
}

func TestProjectMoveDeletionGraph(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	source, target, project := moveFixture(t)
	issue := dbfx.Issue(t, "Delete alias", testutil.Cols{"workspace_id": source, "project_id": project, "number": 7})
	executeMove(t, source, target, project)
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
	req := withURLParam(newRequest("DELETE", "/", nil), "id", issue)
	req.Header.Set("X-Workspace-ID", target)
	testutil.Call(t, testHandler.DeleteIssue, req).Want(204)
	// Ordinary deletion remains unchanged. Retained history is inert when its
	// owner is gone, because resolution joins the live issue/project/workspace.
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(source, "issue=SRC-7", testUserID)).Want(404)
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_key_alias WHERE issue_id=$1`, issue); n != 1 {
		t.Errorf("retained alias count=%d", n)
	}
	req = withURLParam(newRequest("DELETE", "/", nil), "id", source)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(204)
	testutil.Call(t, testHandler.ResolveProjectMove, resolveMoveRequest(source, "project="+project, testUserID)).Want(404)
	if n := dbfx.Count(t, `SELECT count(*) FROM project_move WHERE source_workspace_id=$1`, source); n != 1 {
		t.Errorf("retained audit count=%d", n)
	}
}
