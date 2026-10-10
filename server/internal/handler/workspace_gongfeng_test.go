package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDeleteWorkspace_GongfengRepositories(t *testing.T) {
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Gongfeng deletion", "slug": "gongfeng-deletion-test",
	})
	dbfx.Exec(t, "INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, testUserID)
	connections := make([]string, 0, 2)
	for _, workspaceID := range []string{wsID, testWorkspaceID} {
		connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
			"workspace_id": workspaceID, "provider": "gongfeng",
			"instance_url": "https://git.code.tencent.com", "account_login": "alice",
			"access_token_encrypted": "test-ciphertext", "webhook_secret_encrypted": "test-ciphertext",
		})
		_, err := testHandler.Queries.AddGongfengRepository(context.Background(), db.AddGongfengRepositoryParams{
			ConnectionID: parseUUID(connID), ProjectID: 55, Path: "acme/widget",
			WebUrl: "https://git.code.tencent.com/acme/widget", CloneUrl: "git@git.code.tencent.com:acme/widget.git",
		})
		if err != nil {
			t.Fatal(err)
		}
		dbfx.Cleanup(t, "DELETE FROM gongfeng_repository WHERE connection_id = $1", connID)
		connections = append(connections, connID)
	}
	req := withURLParam(newRequest("DELETE", "/api/workspaces/"+wsID, nil), "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	for i, connID := range connections {
		var count int
		dbfx.QueryRow(t, "SELECT count(*) FROM gongfeng_repository WHERE connection_id = $1", connID).Scan(&count)
		if count != i {
			t.Fatalf("workspace %d repository count = %d, want %d", i, count, i)
		}
	}
}

func TestDeleteWorkspace_GongfengHookCleanup(t *testing.T) {
	withVCSBox(t)
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name": "Gongfeng hook deletion", "slug": "gongfeng-hook-deletion-test",
	})
	dbfx.Exec(t, "INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')", wsID, testUserID)
	var connID string
	deleted := make(chan bool, 1)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/projects/55/hooks/9" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 9, "url": testHandler.vcsWebhookURL(connID)})
			return
		}
		if r.Method == http.MethodDelete {
			var exists bool
			err := testPool.QueryRow(context.Background(), "SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)", wsID).Scan(&exists)
			deleted <- err == nil && !exists
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer provider.Close()
	token, err := testHandler.sealVCSSecret("test-token")
	if err != nil {
		t.Fatal(err)
	}
	connID = dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id": wsID, "provider": "gongfeng", "instance_url": provider.URL, "account_login": "alice",
		"access_token_encrypted": token, "webhook_secret_encrypted": token,
	})
	_, err = testHandler.Queries.AddGongfengRepository(context.Background(), db.AddGongfengRepositoryParams{
		ConnectionID: parseUUID(connID), ProjectID: 55, Path: "acme/widget", WebUrl: provider.URL + "/acme/widget",
	})
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, "DELETE FROM gongfeng_repository WHERE connection_id = $1", connID)
	dbfx.Exec(t, "UPDATE gongfeng_repository SET hook_id = 9 WHERE connection_id = $1", connID)
	req := withURLParam(newRequest("DELETE", "/api/workspaces/"+wsID, nil), "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	select {
	case afterCommit := <-deleted:
		if !afterCommit {
			t.Fatal("remote hook was removed before workspace deletion committed")
		}
	default:
		t.Fatal("workspace deletion did not remove its Gongfeng webhook")
	}
}
