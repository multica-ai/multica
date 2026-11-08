package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspaceLinkHome(t *testing.T) {
	ws := dbfx.Workspace(t, "Home links", "home-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	project := dbfx.Project(t, "Project home", testutil.Cols{"workspace_id": ws})
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM workspace_link WHERE workspace_id=$1", ws) })
	call := func(method, id string, body any, want int) map[string]any {
		t.Helper()
		req := withURLParam(newRequest(method, "/api/links", body), "id", id)
		req.Header.Set("X-Workspace-ID", ws)
		var out map[string]any
		if method == http.MethodPost {
			testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(want).JSON(&out)
		} else {
			testutil.Call(t, testHandler.UpdateWorkspaceLink, req).Want(want).JSON(&out)
		}
		return out
	}
	create := func(project string, home bool) string {
		t.Helper()
		out := call(http.MethodPost, "", map[string]any{
			"name": "Home", "url_template": "https://example.com", "open_mode": "embed",
			"project_id": project, "is_home": home,
		}, http.StatusCreated)
		if out["is_home"] != home {
			t.Fatalf("create is_home=%v, want %v", out["is_home"], home)
		}
		return out["id"].(string)
	}
	first := create("", true)
	second := create("", false)
	projectHome := create(project, true)
	assertHomes := func(ids ...string) {
		t.Helper()
		rows, err := testPool.Query(context.Background(), "SELECT id::text FROM workspace_link WHERE workspace_id=$1 AND is_home", ws)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]bool{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got[id] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(got) != len(ids) {
			t.Fatalf("homes=%v, want %v", got, ids)
		}
		for _, id := range ids {
			if !got[id] {
				t.Fatalf("missing home %s in %v", id, got)
			}
		}
	}
	assertHomes(first, projectHome)
	call(http.MethodPatch, second, map[string]any{"is_home": true}, http.StatusOK)
	assertHomes(second, projectHome)
	out := call(http.MethodPatch, second, map[string]any{"open_mode": "new_tab"}, http.StatusBadRequest)
	if out["error"] != "is_home requires open_mode to be embed; clear is_home before opening in a new tab" {
		t.Fatalf("unexpected validation message: %v", out)
	}
	call(http.MethodPost, "", map[string]any{"name": "Invalid", "url_template": "https://example.com", "open_mode": "new_tab", "is_home": true}, http.StatusBadRequest)
	assertHomes(second, projectHome)

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for _, id := range []string{first, second} {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			<-start
			req := withURLParam(newRequest(http.MethodPatch, "/api/links", map[string]any{"is_home": true}), "id", id)
			req.Header.Set("X-Workspace-ID", ws)
			w := httptest.NewRecorder()
			testHandler.UpdateWorkspaceLink(w, req)
			results <- w
		}(id)
	}
	close(start)
	workers.Wait()
	for range 2 {
		w := <-results
		if w.Code != http.StatusOK {
			t.Fatalf("concurrent set home: %d %s", w.Code, w.Body.String())
		}
	}
	var winner string
	if err := testPool.QueryRow(context.Background(), "SELECT id::text FROM workspace_link WHERE workspace_id=$1 AND project_id IS NULL AND is_home", ws).Scan(&winner); err != nil {
		t.Fatal(err)
	}
	assertHomes(winner, projectHome)
	call(http.MethodPatch, winner, map[string]any{"is_home": false, "open_mode": "new_tab"}, http.StatusOK)
	assertHomes(projectHome)
	third := create("", true)
	fourth := create("", true)
	assertHomes(fourth, projectHome)
	call(http.MethodPatch, third, map[string]any{"is_home": true}, http.StatusOK)
	assertHomes(third, projectHome)

	// Create and PATCH must share the same scope lock and atomic replacement.
	start = make(chan struct{})
	results = make(chan *httptest.ResponseRecorder, 2)
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		workers.Add(1)
		go func(method string) {
			defer workers.Done()
			<-start
			body := map[string]any{"is_home": true, "open_mode": "embed"}
			if method == http.MethodPost {
				body["name"], body["url_template"], body["open_mode"] = "Concurrent", "https://example.com", "embed"
			}
			req := withURLParam(newRequest(method, "/api/links", body), "id", first)
			req.Header.Set("X-Workspace-ID", ws)
			w := httptest.NewRecorder()
			if method == http.MethodPost {
				testHandler.CreateWorkspaceLink(w, req)
			} else {
				testHandler.UpdateWorkspaceLink(w, req)
			}
			results <- w
		}(method)
	}
	close(start)
	workers.Wait()
	for range 2 {
		w := <-results
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("concurrent create/PATCH: %d %s", w.Code, w.Body.String())
		}
	}
	if err := testPool.QueryRow(context.Background(), "SELECT id::text FROM workspace_link WHERE workspace_id=$1 AND project_id IS NULL AND is_home", ws).Scan(&winner); err != nil {
		t.Fatal(err)
	}
	assertHomes(winner, projectHome)

	// Exercise migration 620 directly, bypassing the handler's home clearing.
	_, err := testPool.Exec(context.Background(), `INSERT INTO workspace_link (id, workspace_id, name, url_template, open_mode, is_home)
		VALUES ($1, $2, 'Duplicate', 'https://example.com', 'embed', true)`, uuid.NewString(), ws)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "workspace_link_home_scope_idx" {
		t.Fatalf("duplicate home INSERT: want 620 unique index violation, got %v", err)
	}
	assertHomes(winner, projectHome)

	// Fail after clearing and updating, at commit, to prove both writes roll back.
	h := *testHandler
	h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	req := withURLParam(newRequest(http.MethodPatch, "/api/links", map[string]any{"is_home": true, "open_mode": "embed"}), "id", second)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, h.UpdateWorkspaceLink, req).Want(http.StatusInternalServerError)
	assertHomes(winner, projectHome)
}
