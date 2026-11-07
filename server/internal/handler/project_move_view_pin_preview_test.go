package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectMoveViewPinPreviewRevision(t *testing.T) {
	for _, kind := range []string{"project_scope", "project_query", "issue_query"} {
		t.Run(kind, func(t *testing.T) {
			source, target, project := moveFixture(t)
			issue := dbfx.Issue(t, "View issue", testutil.Cols{"workspace_id": source, "project_id": project})
			cols := testutil.Cols{"workspace_id": source, "owner_id": testUserID, "name": "Referenced view", "scope_type": "workspace", "scope_id": nil, "visibility": "private", "definition_version": 1, "query": "{}", "display": "{}"}
			switch kind {
			case "project_scope":
				cols["scope_type"] = "project"
				cols["scope_id"] = project
			case "project_query":
				cols["query"] = `{"project_ids":["` + project + `"]}`
			case "issue_query":
				cols["query"] = `{"issue_ids":["` + issue + `"]}`
			}
			view := dbfx.Insert(t, "issue_view", cols)
			unrelated := dbfx.Insert(t, "issue_view", testutil.Cols{"workspace_id": source, "owner_id": testUserID, "name": "Unrelated view", "scope_type": "workspace", "scope_id": nil, "visibility": "private", "definition_version": 1, "query": "{}", "display": "{}"})
			ctx := context.Background()
			before := previewMove(t, source, target, project)
			add := func(id string) db.PinnedItem {
				pin, err := testHandler.Queries.CreatePinnedItem(ctx, db.CreatePinnedItemParams{WorkspaceID: parseUUID(source), UserID: parseUUID(testUserID), ItemType: "view", ItemID: parseUUID(id), Position: 1})
				if err != nil {
					t.Fatal(err)
				}
				dbfx.Cleanup(t, "DELETE FROM pinned_item WHERE id=$1", pin.ID)
				return pin
			}
			add(unrelated)
			control := previewMove(t, source, target, project)
			if before.RevisionHash != control.RevisionHash || before.PlanHash != control.PlanHash {
				t.Error("unrelated view pin changed move revision")
			}
			pin := add(view)
			after := previewMove(t, source, target, project)
			if after.SideEffects["pins"] != 1 || after.SideEffects["views"] != 1 {
				t.Errorf("side effects=%v, want pins=1 views=1", after.SideEffects)
			}
			if after.RevisionHash == control.RevisionHash || after.PlanHash == control.PlanHash {
				t.Error("adding a deleted-view pin did not change revision and plan hashes")
			}
			_, err := testPool.Exec(ctx, "UPDATE pinned_item SET position=2 WHERE id=$1", pin.ID)
			if err != nil {
				t.Fatal(err)
			}
			edited := previewMove(t, source, target, project)
			if edited.RevisionHash == after.RevisionHash || edited.PlanHash == after.PlanHash {
				t.Error("editing a deleted-view pin did not change revision and plan hashes")
			}
			_, err = testPool.Exec(ctx, "DELETE FROM pinned_item WHERE id=$1", pin.ID)
			if err != nil {
				t.Fatal(err)
			}
			removed := previewMove(t, source, target, project)
			if removed.RevisionHash == edited.RevisionHash || removed.PlanHash == edited.PlanHash {
				t.Error("removing a deleted-view pin did not change revision and plan hashes")
			}
			if removed.SideEffects["pins"] != 0 {
				t.Errorf("removed pin count=%d", removed.SideEffects["pins"])
			}
			add(view)
			stale := httptest.NewRecorder()
			testHandler.MoveProject(stale, moveRequest(source, target, project, removed.PlanHash))
			if stale.Code != 409 {
				t.Fatalf("view pin added after preview: got %d %s, want 409", stale.Code, stale.Body.String())
			}
			fresh := previewMove(t, source, target, project)
			moved := httptest.NewRecorder()
			testHandler.MoveProject(moved, moveRequest(source, target, project, fresh.PlanHash))
			if moved.Code != 200 {
				t.Fatalf("fresh plan: %d %s", moved.Code, moved.Body.String())
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM pinned_item WHERE item_id=$1", view); n != 0 {
				t.Fatalf("deleted view still has %d pins", n)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM pinned_item WHERE item_id=$1", unrelated); n != 1 {
				t.Fatalf("unrelated pin count=%d", n)
			}
		})
	}
}
