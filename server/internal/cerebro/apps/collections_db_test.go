package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type appAccessFixture struct {
	pool              *pgxpool.Pool
	handler           *Handler
	workspaceID       uuid.UUID
	otherWorkspaceID  uuid.UUID
	appOwnerID        uuid.UUID
	adminID           uuid.UUID
	directMemberID    uuid.UUID
	groupMemberID     uuid.UUID
	workspaceMemberID uuid.UUID
	inheritedMemberID uuid.UUID
	noGrantMemberID   uuid.UUID
	appIDs            map[string]uuid.UUID
}

func newAppAccessFixture(t *testing.T) *appAccessFixture {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not configured")
	}

	ctx := context.Background()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database not reachable: %v", err)
	}
	t.Cleanup(pool.Close)

	suffix := uuid.NewString()
	fixture := &appAccessFixture{pool: pool, handler: NewHandler(pool), appIDs: make(map[string]uuid.UUID)}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM cerebro_folder_grant WHERE surface='app' AND folder_id IN (SELECT id FROM cerebro_app_folder WHERE workspace_id = ANY($1::uuid[]))`, []uuid.UUID{fixture.workspaceID, fixture.otherWorkspaceID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = ANY($1::uuid[])`, []uuid.UUID{fixture.workspaceID, fixture.otherWorkspaceID})
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = ANY($1::uuid[])`, []uuid.UUID{fixture.appOwnerID, fixture.adminID, fixture.directMemberID, fixture.groupMemberID, fixture.workspaceMemberID, fixture.inheritedMemberID, fixture.noGrantMemberID})
	})
	fixture.workspaceID = fixture.insertWorkspace(t, "app-access-"+suffix)
	fixture.otherWorkspaceID = fixture.insertWorkspace(t, "app-access-other-"+suffix)

	fixture.appOwnerID = fixture.insertUser(t, "owner-"+suffix+"@test.local")
	fixture.adminID = fixture.insertUser(t, "admin-"+suffix+"@test.local")
	fixture.directMemberID = fixture.insertUser(t, "direct-"+suffix+"@test.local")
	fixture.groupMemberID = fixture.insertUser(t, "group-"+suffix+"@test.local")
	fixture.workspaceMemberID = fixture.insertUser(t, "workspace-"+suffix+"@test.local")
	fixture.inheritedMemberID = fixture.insertUser(t, "inherited-"+suffix+"@test.local")
	fixture.noGrantMemberID = fixture.insertUser(t, "none-"+suffix+"@test.local")

	for userID, role := range map[uuid.UUID]string{
		fixture.appOwnerID:        "member",
		fixture.adminID:           "admin",
		fixture.directMemberID:    "member",
		fixture.groupMemberID:     "member",
		fixture.workspaceMemberID: "member",
		fixture.inheritedMemberID: "member",
		fixture.noGrantMemberID:   "member",
	} {
		fixture.insertMember(t, fixture.workspaceID, userID, role)
	}

	ownerFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "Owner only")
	adminFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "Admin only")
	directFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "Direct member")
	groupFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "Group")
	workspaceFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "Workspace")
	inheritedParent := fixture.insertFolder(t, fixture.workspaceID, nil, "Inherited parent")
	inheritedChild := fixture.insertFolder(t, fixture.workspaceID, &inheritedParent, "Inherited child")
	noGrantFolder := fixture.insertFolder(t, fixture.workspaceID, nil, "No grant")
	otherFolder := fixture.insertFolder(t, fixture.otherWorkspaceID, nil, "Other workspace")

	fixture.appIDs["owner"] = fixture.insertApp(t, fixture.workspaceID, ownerFolder, fixture.appOwnerID, "owner-app")
	fixture.appIDs["admin"] = fixture.insertApp(t, fixture.workspaceID, adminFolder, fixture.appOwnerID, "admin-app")
	fixture.appIDs["direct member grant"] = fixture.insertApp(t, fixture.workspaceID, directFolder, fixture.appOwnerID, "direct-app")
	fixture.appIDs["group grant"] = fixture.insertApp(t, fixture.workspaceID, groupFolder, fixture.appOwnerID, "group-app")
	fixture.appIDs["workspace grant"] = fixture.insertApp(t, fixture.workspaceID, workspaceFolder, fixture.appOwnerID, "workspace-app")
	fixture.appIDs["inherited ancestor grant"] = fixture.insertApp(t, fixture.workspaceID, inheritedChild, fixture.appOwnerID, "inherited-app")
	fixture.appIDs["no grant"] = fixture.insertApp(t, fixture.workspaceID, noGrantFolder, fixture.appOwnerID, "private-app")
	fixture.appIDs["other workspace"] = fixture.insertApp(t, fixture.otherWorkspaceID, otherFolder, fixture.appOwnerID, "other-workspace-app")

	fixture.insertGrant(t, directFolder, "member", &fixture.directMemberID)
	fixture.insertGrant(t, workspaceFolder, "workspace", nil)
	fixture.insertGrant(t, inheritedParent, "member", &fixture.inheritedMemberID)

	groupID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO cerebro_group (id,workspace_id,name,created_by) VALUES ($1,$2,$3,$4)`, groupID, fixture.workspaceID, "App access group", fixture.appOwnerID); err != nil {
		t.Fatalf("insert group: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cerebro_group_member (group_id,user_id,added_by) VALUES ($1,$2,$3)`, groupID, fixture.groupMemberID, fixture.appOwnerID); err != nil {
		t.Fatalf("insert group member: %v", err)
	}
	fixture.insertGrant(t, groupFolder, "group", &groupID)

	return fixture
}

func (f *appAccessFixture) insertWorkspace(t *testing.T, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO workspace (name,slug,description) VALUES ($1,$2,'') RETURNING id`, slug, slug).Scan(&id); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	return id
}

func (f *appAccessFixture) insertUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO "user" (name,email) VALUES ($1,$2) RETURNING id`, email, email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func (f *appAccessFixture) insertMember(t *testing.T, workspaceID, userID uuid.UUID, role string) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,$3)`, workspaceID, userID, role); err != nil {
		t.Fatalf("insert %s member: %v", role, err)
	}
}

func (f *appAccessFixture) insertFolder(t *testing.T, workspaceID uuid.UUID, parentID *uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO cerebro_app_folder (workspace_id,parent_id,name) VALUES ($1,$2,$3) RETURNING id`, workspaceID, parentID, name).Scan(&id); err != nil {
		t.Fatalf("insert folder %q: %v", name, err)
	}
	return id
}

func (f *appAccessFixture) insertApp(t *testing.T, workspaceID, folderID, ownerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO cerebro_app (workspace_id,slug,name,folder_id,owner_id) VALUES ($1,$2,$3,$4,$5) RETURNING id`, workspaceID, slug+"-"+uuid.NewString(), slug, folderID, ownerID).Scan(&id); err != nil {
		t.Fatalf("insert app %q: %v", slug, err)
	}
	return id
}

func (f *appAccessFixture) insertGrant(t *testing.T, folderID uuid.UUID, granteeType string, granteeID *uuid.UUID) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `INSERT INTO cerebro_folder_grant (surface,folder_id,grantee_type,grantee_id,role,created_by) VALUES ('app',$1,$2,$3,'viewer',$4)`, folderID, granteeType, granteeID, f.appOwnerID); err != nil {
		t.Fatalf("insert %s grant: %v", granteeType, err)
	}
}

func (f *appAccessFixture) request(t *testing.T, method string, appID *uuid.UUID, userID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/cerebro/apps", nil)
	request.Header.Set("X-User-ID", userID.String())
	ctx := middleware.SetMemberContext(request.Context(), f.workspaceID.String(), db.Member{})
	if appID != nil {
		route := chi.NewRouteContext()
		route.URLParams.Add("id", appID.String())
		ctx = context.WithValue(ctx, chi.RouteCtxKey, route)
	}
	recorder := httptest.NewRecorder()
	if appID == nil {
		f.handler.List(recorder, request.WithContext(ctx))
	} else {
		f.handler.Get(recorder, request.WithContext(ctx))
	}
	return recorder
}

func TestAppCollectionAccessMatrixDB(t *testing.T) {
	fixture := newAppAccessFixture(t)
	cases := []struct {
		name    string
		userID  uuid.UUID
		allowed bool
	}{
		{name: "owner", userID: fixture.appOwnerID, allowed: true},
		{name: "admin", userID: fixture.adminID, allowed: true},
		{name: "direct member grant", userID: fixture.directMemberID, allowed: true},
		{name: "group grant", userID: fixture.groupMemberID, allowed: true},
		{name: "workspace grant", userID: fixture.workspaceMemberID, allowed: true},
		{name: "inherited ancestor grant", userID: fixture.inheritedMemberID, allowed: true},
		{name: "no grant", userID: fixture.noGrantMemberID, allowed: false},
		{name: "other workspace", userID: fixture.noGrantMemberID, allowed: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			appID := fixture.appIDs[testCase.name]
			listResponse := fixture.request(t, http.MethodGet, nil, testCase.userID)
			if listResponse.Code != http.StatusOK {
				t.Fatalf("List returned %d: %s", listResponse.Code, listResponse.Body.String())
			}
			var listBody struct {
				Apps []appResponse `json:"apps"`
			}
			if err := json.Unmarshal(listResponse.Body.Bytes(), &listBody); err != nil {
				t.Fatalf("decode List response: %v", err)
			}
			listed := false
			for _, app := range listBody.Apps {
				if app.ID == appID.String() {
					listed = true
					break
				}
			}
			if listed != testCase.allowed {
				t.Fatalf("List visibility for %s = %t, want %t", appID, listed, testCase.allowed)
			}

			getResponse := fixture.request(t, http.MethodGet, &appID, testCase.userID)
			wantStatus := http.StatusNotFound
			if testCase.allowed {
				wantStatus = http.StatusOK
			}
			if getResponse.Code != wantStatus {
				t.Fatalf("Get returned %d, want %d: %s", getResponse.Code, wantStatus, getResponse.Body.String())
			}
			if testCase.allowed {
				var detail appDetailResponse
				if err := json.Unmarshal(getResponse.Body.Bytes(), &detail); err != nil {
					t.Fatalf("decode Get response: %v", err)
				}
				if detail.ID != appID.String() {
					t.Fatalf("Get returned app %s, want %s", detail.ID, appID)
				}
			}
		})
	}
}
