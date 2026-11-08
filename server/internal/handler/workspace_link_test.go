package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	w := call("POST", map[string]any{"name": "  Docs  ", "url_template": "https://example.com/{workspace_slug}"}, "", 201)
	var link struct{ ID, Name string }
	if err := json.Unmarshal(w.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	if link.Name != "Docs" {
		t.Fatal(link.Name)
	}
	call("GET", nil, "", 200)
	call("PATCH", map[string]any{"position": 4, "name": "Dashboard"}, link.ID, 200)
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
