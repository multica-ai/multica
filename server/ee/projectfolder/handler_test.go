package projectfolder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/extension"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var (
	testPool    *pgxpool.Pool
	testHandler *handler.Handler
	testService *Service
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL := os.Getenv("DATABASE_URL")
	dbConfigured := dbURL != ""
	if !dbConfigured {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	pool, err := pgxpool.New(ctx, dbURL)
	if err == nil {
		err = pool.Ping(ctx)
	}
	if err != nil {
		if dbConfigured {
			fmt.Fprintln(os.Stderr, "project folder tests: database from DATABASE_URL not reachable:", err)
			os.Exit(1)
		}
		fmt.Printf("Skipping tests: database not reachable: %v\n", err)
		os.Exit(0)
	}
	hub := realtime.NewHub()
	go hub.Run()
	testPool = pool
	testHandler = handler.New(db.New(pool), pool, hub, events.New(), service.NewEmailService(), nil, nil, analytics.NoopClient{}, handler.Config{})
	testService = New(testHandler)
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// env is a workspace of its own with an owner and a plain member.
type env struct {
	fx          *testutil.Fixture
	workspaceID string
	ownerID     string
	memberID    string
}

func newEnv(t *testing.T) env {
	t.Helper()
	fx := testutil.New(testPool, "", "")
	suffix := uuid.NewString()[:8]
	ownerID := fx.User(t, "Folder Owner", "folder-owner-"+suffix+"@multica.ai")
	memberID := fx.User(t, "Folder Member", "folder-member-"+suffix+"@multica.ai")
	workspaceID := fx.Workspace(t, "Folder Tests", "folder-tests-"+suffix)
	fx.Member(t, workspaceID, ownerID, "owner")
	fx.Member(t, workspaceID, memberID, "member")
	fx = testutil.New(testPool, workspaceID, ownerID)
	fx.Cleanup(t, `DELETE FROM project_folder WHERE workspace_id = $1`, workspaceID)
	return env{fx: fx, workspaceID: workspaceID, ownerID: ownerID, memberID: memberID}
}

func (e env) request(userID, method, path string, body any, params ...string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Workspace-ID", e.workspaceID)
	if len(params) > 0 {
		rctx := chi.NewRouteContext()
		for i := 0; i+1 < len(params); i += 2 {
			rctx.URLParams.Add(params[i], params[i+1])
		}
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}
	return req
}

func (e env) create(t *testing.T, name string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testService.Create, e.request(e.ownerID, http.MethodPost, "/api/project-folders", map[string]any{"name": name}))
}

func (e env) createFolder(t *testing.T, name string) FolderResponse {
	t.Helper()
	var folder FolderResponse
	e.create(t, name).Want(http.StatusCreated).JSON(&folder)
	return folder
}

func (e env) file(t *testing.T, userID, projectID string, folderID *string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testService.File, e.request(userID, http.MethodPost, "/api/project-folders/file", map[string]any{
		"project_id": projectID, "folder_id": folderID,
	}))
}

func (e env) list(t *testing.T, userID string) ListResponse {
	t.Helper()
	var list ListResponse
	testutil.Call(t, testService.List, e.request(userID, http.MethodGet, "/api/project-folders", nil)).Want(http.StatusOK).JSON(&list)
	return list
}

func (e env) rename(t *testing.T, folderID, name string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testService.Rename, e.request(e.ownerID, http.MethodPatch, "/api/project-folders/"+folderID, map[string]any{"name": name}, "id", folderID))
}

func (e env) delete(t *testing.T, folderID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testService.Delete, e.request(e.ownerID, http.MethodDelete, "/api/project-folders/"+folderID, nil, "id", folderID))
}

func (e env) folderOf(t *testing.T, projectID string) *string {
	t.Helper()
	var folderID *string
	e.fx.QueryRow(t, `SELECT folder_id::text FROM project WHERE id = $1`, projectID).Scan(&folderID)
	return folderID
}

func ptr(s string) *string { return &s }

func TestFolderNames(t *testing.T) {
	e := newEnv(t)
	folder := e.createFolder(t, "  Revenue  ")
	if folder.Name != "Revenue" {
		t.Fatalf("name = %q, want trimmed %q", folder.Name, "Revenue")
	}
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"", http.StatusBadRequest},
		{"line\nbreak", http.StatusBadRequest},
		{strings.Repeat("a", 65), http.StatusBadRequest},
		{"REVENUE", http.StatusConflict},
	} {
		e.create(t, tc.name).Want(tc.status)
	}

	var renamed FolderResponse
	e.rename(t, folder.ID, "Growth").Want(http.StatusOK).JSON(&renamed)
	if renamed.Name != "Growth" {
		t.Fatalf("renamed to %q, want Growth", renamed.Name)
	}
	list := e.list(t, e.memberID)
	if len(list.Folders) != 1 || list.Folders[0].Name != "Growth" {
		t.Fatalf("list = %+v, want the renamed folder", list.Folders)
	}
	e.rename(t, uuid.NewString(), "x").Want(http.StatusNotFound)
}

func TestFileProject(t *testing.T) {
	e := newEnv(t)
	folder := e.createFolder(t, "Commerce")
	projectID := e.fx.Project(t, "Billing")

	e.file(t, e.memberID, projectID, ptr(folder.ID)).Want(http.StatusOK)
	if got := e.folderOf(t, projectID); got == nil || *got != folder.ID {
		t.Fatalf("folder_id = %v, want %s", got, folder.ID)
	}
	list := e.list(t, e.memberID)
	if len(list.Filed) != 1 || list.Filed[0] != (FiledProject{ProjectID: projectID, FolderID: folder.ID}) {
		t.Fatalf("filed = %+v, want the project in %s", list.Filed, folder.ID)
	}

	e.file(t, e.ownerID, projectID, ptr(uuid.NewString())).Want(http.StatusBadRequest)
	e.file(t, e.ownerID, uuid.NewString(), ptr(folder.ID)).Want(http.StatusNotFound)

	e.file(t, e.ownerID, projectID, nil).Want(http.StatusOK)
	if got := e.folderOf(t, projectID); got != nil {
		t.Fatalf("moved out, folder_id = %s", *got)
	}
}

func TestListLeavesOutProjectsTheCallerCannotSee(t *testing.T) {
	e := newEnv(t)
	folder := e.createFolder(t, "Secret")
	private := e.fx.Project(t, "Private", testutil.Cols{"visibility": "private", "folder_id": folder.ID})
	e.fx.InsertNoID(t, "project_member", testutil.Cols{"workspace_id": e.workspaceID, "project_id": private, "user_id": e.ownerID},
		`project_id = $1`, private)

	if filed := e.list(t, e.ownerID).Filed; len(filed) != 1 {
		t.Fatalf("project member sees %+v, want the private project", filed)
	}
	if filed := e.list(t, e.memberID).Filed; len(filed) != 0 {
		t.Fatalf("non-member sees %+v, want nothing", filed)
	}
	e.file(t, e.memberID, private, nil).Want(http.StatusNotFound)
}

func TestDeleteMovesProjectsOut(t *testing.T) {
	e := newEnv(t)
	folder := e.createFolder(t, "Infra")
	projectID := e.fx.Project(t, "Observability", testutil.Cols{"folder_id": folder.ID})
	var before string
	e.fx.QueryRow(t, `SELECT updated_at::text FROM project WHERE id = $1`, projectID).Scan(&before)

	e.delete(t, folder.ID).Want(http.StatusNoContent)

	if got := e.folderOf(t, projectID); got != nil {
		t.Fatalf("project still in deleted folder %s", *got)
	}
	var after string
	e.fx.QueryRow(t, `SELECT updated_at::text FROM project WHERE id = $1`, projectID).Scan(&after)
	if after != before {
		t.Fatalf("deleting the folder edited the project: updated_at %s -> %s", before, after)
	}
	e.delete(t, folder.ID).Want(http.StatusNotFound)
}

func TestFiledEventFollowsPrivateProjectAudience(t *testing.T) {
	if !extension.IsProjectEvent(EventFiled) {
		t.Fatalf("%s is not registered as a project event; filing a private project would reach the whole workspace", EventFiled)
	}
}
