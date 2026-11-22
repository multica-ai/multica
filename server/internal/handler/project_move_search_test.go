package handler

import (
	"github.com/multica-ai/multica/server/internal/testutil"
	"testing"
)

func moveSearchCursor(t *testing.T, workspace string) string {
	t.Helper()
	req := newRequest("GET", "/", nil)
	req.Header.Set("X-Workspace-ID", workspace)
	var manifest SearchIndexManifestResponse
	testutil.Call(t, testHandler.GetSearchIndexManifest, req).Want(200).JSON(&manifest)
	return manifest.Cursor
}
func moveSearchChanges(t *testing.T, workspace, cursor string) SearchIndexChangesResponse {
	t.Helper()
	req := newRequest("POST", "/", map[string]any{"cursor": cursor})
	req.Header.Set("X-Workspace-ID", workspace)
	var result SearchIndexChangesResponse
	testutil.Call(t, testHandler.ListSearchIndexChanges, req).Want(200).JSON(&result)
	return result
}
func TestProjectMoveSearchCatchUp(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	source, target, project := moveFixture(t)
	issue := dbfx.Issue(t, "Search move", testutil.Cols{"workspace_id": source, "project_id": project})
	comment := dbfx.Comment(t, issue, "Search comment", testutil.Cols{"workspace_id": source})
	beforeSource, beforeTarget := moveSearchCursor(t, source), moveSearchCursor(t, target)
	executeMove(t, source, target, project)
	old := moveSearchChanges(t, source, beforeSource)
	new := moveSearchChanges(t, target, beforeTarget)
	if len(old.Deleted.Issues) != 1 || old.Deleted.Issues[0] != issue || len(old.Deleted.Comments) != 1 || old.Deleted.Comments[0] != comment || len(old.Deleted.Projects) != 1 || old.Deleted.Projects[0] != project {
		t.Fatalf("source did not see deletions: %+v", old)
	}
	if len(new.Issues) != 1 || new.Issues[0].ID != issue || len(new.Comments) != 1 || new.Comments[0].ID != comment || len(new.Projects) != 1 || new.Projects[0].ID != project {
		t.Fatalf("target missed upserts: %+v", new)
	}
	dbfx.Exec(t, `UPDATE member SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, target, testUserID)
	executeMove(t, target, source, project)
	returned := moveSearchChanges(t, source, old.Cursor)
	left := moveSearchChanges(t, target, new.Cursor)
	if len(returned.Issues) != 1 || len(returned.Comments) != 1 || len(returned.Projects) != 1 || len(returned.Deleted.Issues) != 0 {
		t.Fatalf("return move failed catch-up: %+v", returned)
	}
	if len(left.Deleted.Issues) != 1 || len(left.Deleted.Comments) != 1 || len(left.Deleted.Projects) != 1 {
		t.Fatalf("second source missed departures: %+v", left)
	}
	dbfx.Cleanup(t, `DELETE FROM issue_key_alias WHERE issue_id=$1`, issue)
	dbfx.Cleanup(t, `DELETE FROM project_move WHERE project_id=$1`, project)
}
