package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkspaceLinkValidation(t *testing.T) {
	for _, tc := range []struct {
		name, url      string
		project, valid bool
	}{
		{"https", "https://example.com/{workspace_slug}?id={workspace_id}", false, true},
		{"project", "https://example.com/{project_id}", true, true},
		{"http", "http://example.com", false, false},
		{"javascript", "javascript:alert(1)", false, false},
		{"data", "data:text/html,test", false, false},
		{"relative", "/docs", false, false},
		{"protocol relative", "//host/x", false, false},
		{"backslash", `https:\host`, false, false},
		{"double backslash", `https:\\host`, false, false},
		{"tab", "https://example.com/	x", false, false},
		{"newline", "https://example.com/\nx", false, false},
		{"encoded braces", "https://example.com/%7Bworkspace_slug%7D", false, true},
		{"empty userinfo", "https://@host/", false, false},
		{"malformed authority", "https://:443/", false, false},
		{"unknown", "https://example.com/{unknown}", false, false},
		{"project in workspace", "https://example.com/{project_id}", false, false},
		{"host", "https://{workspace_slug}.example.com", false, false},
		{"userinfo", "https://user:pass@example.com", false, false},
		{"long", "https://example.com/" + strings.Repeat("a", 2048), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLinkURL(tc.url, tc.project)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
			if tc.name == "unknown" && !strings.Contains(err.Error(), "unknown") {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkspaceLinkLifecycle(t *testing.T) {
	ws := dbfx.Workspace(t, "Links", "links-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM workspace_link WHERE workspace_id=$1", ws) })
	call := func(method string, body any, id string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := newRequest(method, "/api/links?workspace_id="+ws, body)
		req.Header.Set("X-Workspace-ID", ws)
		req = withURLParam(req, "id", id)
		w := httptest.NewRecorder()
		switch method {
		case "POST":
			testHandler.CreateWorkspaceLink(w, req)
		case "GET":
			testHandler.ListWorkspaceLinks(w, req)
		case "PATCH":
			testHandler.UpdateWorkspaceLink(w, req)
		case "DELETE":
			testHandler.DeleteWorkspaceLink(w, req)
		}
		if w.Code != want {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		return w
	}
	w := call("POST", map[string]any{
		"name":              "  Docs  ",
		"icon":              "book-open",
		"url_template":      "https://example.com/{workspace_slug}",
		"open_mode":         "new_tab",
		"allow_same_origin": true,
		"position":          7,
	}, "", http.StatusCreated)
	var link WorkspaceLinkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if link.Name != "Docs" {
		t.Fatal(link.Name)
	}
	call("GET", nil, "", 200)
	w = call("PATCH", map[string]any{"position": 4, "name": "Dashboard"}, link.ID, http.StatusOK)
	var patched WorkspaceLinkResponse
	if err := json.Unmarshal(w.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Name != "Dashboard" || patched.Position != 4 || patched.Icon != link.Icon ||
		patched.URLTemplate != link.URLTemplate || patched.OpenMode != link.OpenMode ||
		patched.AllowSameOrigin != link.AllowSameOrigin {
		t.Fatalf("partial PATCH did not preserve omitted fields: %+v", patched)
	}
	w = call("PATCH", map[string]any{"position": 0, "allow_same_origin": false}, link.ID, http.StatusOK)
	if err := json.Unmarshal(w.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Position != 0 || patched.AllowSameOrigin || patched.Name != "Dashboard" ||
		patched.Icon != link.Icon || patched.URLTemplate != link.URLTemplate || patched.OpenMode != link.OpenMode {
		t.Fatalf("explicit zero/false PATCH was not applied correctly: %+v", patched)
	}
	call("DELETE", nil, link.ID, 204)
	call("PATCH", map[string]any{"name": "Gone"}, link.ID, 404)
	for i := 0; i < maxLinksPerScope; i++ {
		call("POST", map[string]any{"name": "Docs", "url_template": "https://example.com"}, "", 201)
	}
	call("POST", map[string]any{"name": "Too many", "url_template": "https://example.com"}, "", 400)
	if _, err := testPool.Exec(context.Background(), `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, ws, testUserID); err != nil {
		t.Fatal(err)
	}
	call("GET", nil, "", 200)
	call("POST", map[string]any{"name": "Denied", "url_template": "https://example.com"}, "", 403)
	call("PATCH", map[string]any{"name": "Denied"}, uuid.NewString(), 403)
	call("DELETE", nil, uuid.NewString(), 403)
}

func TestWorkspaceLinkConcurrentDisjointPatches(t *testing.T) {
	ws := dbfx.Workspace(t, "Concurrent links", "link-concurrent-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	id := dbfx.Insert(t, "workspace_link", testutil.Cols{
		"workspace_id": ws,
		"name":         "Docs",
		"url_template": "https://example.com/old",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var blockerPID int
	if err := blocker.QueryRow(ctx, `
		SELECT pg_backend_pid() FROM workspace_link WHERE id=$1 FOR UPDATE
	`, id).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	for _, body := range []map[string]any{
		{"position": 12},
		{"url_template": "https://example.com/new"},
	} {
		req := newRequest(http.MethodPatch, "/api/links/"+id, body).WithContext(ctx)
		req.Header.Set("X-Workspace-ID", ws)
		req = withURLParam(req, "id", id)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			w := httptest.NewRecorder()
			testHandler.UpdateWorkspaceLink(w, req)
			results <- w
		}()
	}
	close(start)

	// Keep both requests behind the same row lock. Before the fix they can
	// both read stale values and then block in UPDATE; with FOR UPDATE they
	// block before reading. Release only once both are in that lock queue.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := testPool.QueryRow(ctx, `
			WITH RECURSIVE waiting AS (
				SELECT pid FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))
				UNION
				SELECT a.pid FROM pg_stat_activity a
				JOIN waiting w ON w.pid = ANY(pg_blocking_pids(a.pid))
			)
			SELECT count(*) FROM waiting
		`, blockerPID).Scan(&waiting); err != nil {
			t.Fatalf("wait for concurrent PATCH lock queue: %v", err)
		}
		if waiting >= 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("concurrent PATCH requests did not reach the row lock")
		}
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case w := <-results:
			if w.Code != http.StatusOK {
				t.Fatalf("concurrent PATCH: %d %s", w.Code, w.Body.String())
			}
		case <-ctx.Done():
			t.Fatal("concurrent PATCH did not complete")
		}
	}
	stored, err := testHandler.Queries.GetWorkspaceLink(ctx, db.GetWorkspaceLinkParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(ws),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Position != 12 || stored.UrlTemplate != "https://example.com/new" {
		t.Fatalf("disjoint PATCH changes were lost: position=%d url=%q", stored.Position, stored.UrlTemplate)
	}
}

func TestWorkspaceLinkCrossWorkspaceMutations(t *testing.T) {
	ws := dbfx.Workspace(t, "Other link scope", "link-foreign-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	id := dbfx.Insert(t, "workspace_link", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "Private workspace docs",
		"url_template": "https://example.com/original",
	})
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		req := withURLParam(newRequest(method, "/api/links/"+id, map[string]any{"name": "Denied"}), "id", id)
		req.Header.Set("X-Workspace-ID", ws)
		if method == http.MethodPatch {
			testutil.Call(t, testHandler.UpdateWorkspaceLink, req).Want(http.StatusNotFound)
		} else {
			testutil.Call(t, testHandler.DeleteWorkspaceLink, req).Want(http.StatusNotFound)
		}
	}
	stored, err := testHandler.Queries.GetWorkspaceLink(context.Background(), db.GetWorkspaceLinkParams{
		ID:          parseUUID(id),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Private workspace docs" || stored.UrlTemplate != "https://example.com/original" {
		t.Fatalf("foreign workspace mutations changed the link: %+v", stored)
	}
}

func TestWorkspaceLinkCrossWorkspaceProject(t *testing.T) {
	project := createTestProjectForResources(t, "Link foreign project")
	ws := dbfx.Workspace(t, "Other links", "links-other-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	req := newRequest("POST", "/api/links?workspace_id="+ws, map[string]any{"name": "Foreign", "url_template": "https://example.com", "project_id": project.ID})
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(http.StatusNotFound)
	req = newRequest("GET", "/api/links?workspace_id="+ws+"&project_id="+project.ID, nil)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.ListWorkspaceLinks, req).Want(http.StatusNotFound)
}

func TestWorkspaceLinkProjectDeletion(t *testing.T) {
	project := createTestProjectForResources(t, "Link cleanup")
	req := newRequest("POST", "/api/links?workspace_id="+testWorkspaceID, map[string]any{"name": "Project docs", "url_template": "https://example.com/{project_id}", "project_id": project.ID})
	testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(http.StatusCreated)
	req = withURLParam(newRequest("DELETE", "/api/projects/"+project.ID, nil), "id", project.ID)
	testutil.Call(t, testHandler.DeleteProject, req).Want(http.StatusNoContent)
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_link WHERE project_id=$1`, project.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan links: %d", count)
	}
}

func TestWorkspaceLinkValidationHTTP(t *testing.T) {
	for _, tc := range []struct{ name, url, icon, mode string }{
		{"http", "http://example.com", "", "embed"},
		{"javascript", "javascript:alert(1)", "", "embed"},
		{"unknown variable", "https://example.com/{unknown}", "", "embed"},
		{"project variable", "https://example.com/{project_id}", "", "embed"},
		{"host variable", "https://{workspace_id}.example.com", "", "embed"},
		{"userinfo", "https://user:pass@example.com", "", "embed"},
		{"bad icon", "https://example.com", "random text", "embed"},
		{"bad mode", "https://example.com", "", "popup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest("POST", "/api/links", map[string]any{"name": "Docs", "url_template": tc.url, "icon": tc.icon, "open_mode": tc.mode})
			testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(http.StatusBadRequest)
		})
	}
}

func TestWorkspaceLinkDBErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{{pgx.ErrNoRows, 404}, {errors.New("connection failed"), 500}} {
		w := httptest.NewRecorder()
		writeLinkDBError(w, tc.err)
		if w.Code != tc.want {
			t.Fatalf("%v: got %d want %d", tc.err, w.Code, tc.want)
		}
	}
}

func TestWorkspaceLinkScopes(t *testing.T) {
	ws := dbfx.Workspace(t, "Link scopes", "link-scopes-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "admin")
	project := dbfx.Project(t, "Scoped links", testutil.Cols{"workspace_id": ws})
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM workspace_link WHERE workspace_id=$1", ws) })
	create := func(projectID string, want int) {
		t.Helper()
		req := newRequest("POST", "/api/links", map[string]any{"name": "Docs", "url_template": "https://example.com", "project_id": projectID})
		req.Header.Set("X-Workspace-ID", ws)
		testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(want)
	}
	create("", http.StatusCreated)
	for i := 0; i < maxLinksPerScope; i++ {
		create(project, http.StatusCreated)
	}
	create(project, http.StatusBadRequest)
	for _, scope := range []struct {
		project string
		want    int
	}{{"", 1}, {project, maxLinksPerScope}} {
		req := newRequest("GET", "/api/links?project_id="+scope.project, nil)
		req.Header.Set("X-Workspace-ID", ws)
		var out struct {
			Links []WorkspaceLinkResponse `json:"links"`
		}
		testutil.Call(t, testHandler.ListWorkspaceLinks, req).Want(http.StatusOK).JSON(&out)
		if len(out.Links) != scope.want {
			t.Fatalf("scope %q: %d links, want %d", scope.project, len(out.Links), scope.want)
		}
	}
}

func TestWorkspaceLinkFields(t *testing.T) {
	for _, icon := range []string{"", "globe", "book-open", "🚀", "❤️", "🇫🇷", "👨‍👩‍👧", "👍🏽", "1️⃣", "©️"} {
		link := db.WorkspaceLink{Name: "  " + strings.Repeat("名", 40) + "  ", Icon: icon, UrlTemplate: "https://example.com", OpenMode: "embed"}
		if err := validateWorkspaceLink(&link); err != nil {
			t.Fatalf("icon %q: %v", icon, err)
		}
		if link.Name != strings.Repeat("名", 40) {
			t.Fatal("name was not trimmed")
		}
	}
	for _, icon := range []string{"abc", "🚀🚀", "a", "https://example.com/icon.png"} {
		link := db.WorkspaceLink{Name: "Docs", Icon: icon, UrlTemplate: "https://example.com", OpenMode: "embed"}
		if err := validateWorkspaceLink(&link); err == nil {
			t.Fatalf("invalid icon accepted: %q", icon)
		}
	}
	for _, name := range []string{"  ", strings.Repeat("名", 41)} {
		link := db.WorkspaceLink{Name: name, UrlTemplate: "https://example.com", OpenMode: "embed"}
		if err := validateWorkspaceLink(&link); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
}

func TestWorkspaceLinkIconByteLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		icon  string
		valid bool
	}{
		{"family emoji fits", "👨‍👩‍👧‍👦", true},
		{"32 bytes", strings.Repeat("🚀\u200d", 4) + "🚀", true},
		{"over 32 bytes", strings.Repeat("🚀\u200d", 5) + "🚀", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			link := db.WorkspaceLink{
				Name:        "Docs",
				Icon:        tc.icon,
				UrlTemplate: "https://example.com",
				OpenMode:    "embed",
			}
			err := validateWorkspaceLink(&link)
			if (err == nil) != tc.valid {
				t.Fatalf("%d-byte icon valid=%v, error=%v", len(tc.icon), tc.valid, err)
			}
			if !tc.valid && !strings.Contains(err.Error(), "32 bytes") {
				t.Fatalf("expected icon byte-limit error, got %v", err)
			}
		})
	}
}

func TestWorkspaceLinkWorkspaceDeletion(t *testing.T) {
	ws := dbfx.Workspace(t, "Links teardown", "link-teardown-"+uuid.NewString())
	dbfx.Member(t, ws, testUserID, "owner")
	req := newRequest("POST", "/api/links", map[string]any{"name": "Docs", "url_template": "https://example.com"})
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.CreateWorkspaceLink, req).Want(http.StatusCreated)
	req = withURLParam(newRequest("DELETE", "/api/workspaces/"+ws, nil), "id", ws)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_link WHERE workspace_id=$1`, ws).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan workspace links: %d", count)
	}
}
