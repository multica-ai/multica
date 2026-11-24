package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskWorkspaceGrantRequiresLiveAuthority(t *testing.T) {
	suffix := uuid.NewString()[:8]
	owner := dbfx.User(t, "grant owner", "grant-owner-"+suffix+"@example.test")
	originator := dbfx.User(t, "grant originator", "grant-origin-"+suffix+"@example.test")
	target := dbfx.Workspace(t, "grant target", "grant-target-"+suffix)
	ownerMember := dbfx.Member(t, target, owner, "member")
	originMember := dbfx.Member(t, target, originator, "member")
	runtime := dbfx.Runtime(t, "grant runtime")
	agent := dbfx.Agent(t, "grant agent", runtime, testutil.Cols{"owner_id": owner})
	task := dbfx.Task(t, agent, testutil.Cols{
		"status": "running", "runtime_id": runtime, "originator_user_id": originator, "accountable_user_id": originator, "originator_source": "direct_human",
	})
	dbfx.InsertNoID(t, "agent_workspace_grant", testutil.Cols{"agent_id": agent, "workspace_id": target, "created_by": owner}, "agent_id=$1 AND workspace_id=$2", agent, target)
	params := db.AuthorizeTaskWorkspaceGrantParams{
		TaskID: util.MustParseUUID(task), AgentID: util.MustParseUUID(agent),
		HomeWorkspaceID: util.MustParseUUID(testWorkspaceID), TargetWorkspaceID: util.MustParseUUID(target),
	}
	check := func(want bool) {
		t.Helper()
		row, err := testHandler.Queries.AuthorizeTaskWorkspaceGrant(context.Background(), params)
		if want && (err != nil || row.OwnerID != util.MustParseUUID(owner) || row.OriginatorUserID != util.MustParseUUID(originator)) {
			t.Fatalf("grant rejected: row=%+v err=%v", row, err)
		}
		if !want && err != pgx.ErrNoRows {
			t.Fatalf("grant unexpectedly accepted: row=%+v err=%v", row, err)
		}
	}
	check(true)
	privateProject := dbfx.Project(t, "grant private project", testutil.Cols{"workspace_id": target, "visibility": "private"})
	privateUUID := util.MustParseUUID(privateProject)
	visible := func() bool {
		t.Helper()
		hidden, err := testHandler.hiddenProjectsForAgentRun(context.Background(), util.MustParseUUID(target), agent, task)
		if err != nil {
			t.Fatalf("private project access: %v", err)
		}
		return hidden.CanSeeProject(privateUUID)
	}
	if visible() {
		t.Fatal("run saw a private target project without either principal's project access")
	}
	dbfx.InsertNoID(t, "project_member", testutil.Cols{"workspace_id": target, "project_id": privateProject, "user_id": owner}, "project_id=$1 AND user_id=$2", privateProject, owner)
	if visible() {
		t.Fatal("owner's private project access bypassed the originator")
	}
	dbfx.InsertNoID(t, "project_member", testutil.Cols{"workspace_id": target, "project_id": privateProject, "user_id": originator}, "project_id=$1 AND user_id=$2", privateProject, originator)
	if !visible() {
		t.Fatal("project stayed hidden after both principals joined")
	}
	dbfx.Exec(t, "UPDATE member SET role='guest' WHERE id=$1", originMember)
	check(false)
	dbfx.Exec(t, "UPDATE member SET role='member' WHERE id=$1", originMember)
	dbfx.Exec(t, "UPDATE member SET role='guest' WHERE id=$1", ownerMember)
	check(false)
	dbfx.Exec(t, "UPDATE member SET role='member' WHERE id=$1", ownerMember)
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_source='delegation' WHERE id=$1", task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET originator_source='direct_human', status='completed' WHERE id=$1", task)
	check(false)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='running' WHERE id=$1", task)
	dbfx.Exec(t, "DELETE FROM agent_workspace_grant WHERE agent_id=$1 AND workspace_id=$2", agent, target)
	check(false)
}

func TestGrantedTaskTokenCanCreateAndReadTargetIssue(t *testing.T) {
	// The runtime owner is a home-only member. Auth must use the agent owner
	// for target membership while preserving the source agent and task actor.
	home := dbfx.Workspace(t, "grant source", "grant-source-"+uuid.NewString()[:8])
	runtimeOwner := dbfx.User(t, "runtime owner", "runtime-owner-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, home, runtimeOwner, "member")
	dbfx.Member(t, home, testUserID, "member")
	runtime := dbfx.Runtime(t, "grant source runtime", testutil.Cols{"workspace_id": home, "owner_id": runtimeOwner})
	agent := dbfx.Agent(t, "grant source agent", runtime, testutil.Cols{"workspace_id": home})
	task := dbfx.Task(t, agent, testutil.Cols{
		"status": "running", "runtime_id": runtime, "originator_user_id": testUserID,
		"accountable_user_id": testUserID, "originator_source": "direct_human",
	})
	dbfx.InsertNoID(t, "agent_workspace_grant", testutil.Cols{"agent_id": agent, "workspace_id": testWorkspaceID, "created_by": testUserID}, "agent_id=$1 AND workspace_id=$2", agent, testWorkspaceID)
	token := "mat_cross_workspace_" + uuid.NewString()
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": auth.HashToken(token), "task_id": task, "agent_id": agent,
		"workspace_id": home, "user_id": runtimeOwner, "expires_at": testutil.Raw("now() + interval '1 hour'"),
	})
	router := chi.NewRouter()
	router.Use(middleware.Auth(testHandler.Queries, nil, nil, nil))
	router.Use(middleware.RequireWorkspaceMember(testHandler.Queries))
	router.Route("/api/issues", func(r chi.Router) {
		r.Get("/", testHandler.ListIssues)
		r.Post("/", testHandler.CreateIssue)
		r.Get("/{id}", testHandler.GetIssue)
	})
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		req.Header.Set("X-User-ID", runtimeOwner)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	created := call(http.MethodPost, "/api/issues/", []byte(`{"title":"Granted target issue"}`))
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var issue map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &issue); err != nil {
		t.Fatal(err)
	}
	id, _ := issue["id"].(string)
	if id == "" || issue["workspace_id"] != testWorkspaceID || issue["creator_type"] != "agent" || issue["creator_id"] != agent {
		t.Fatalf("target issue lost source attribution: %+v", issue)
	}
	read := call(http.MethodGet, "/api/issues/"+id, nil)
	if read.Code != http.StatusOK {
		t.Fatalf("get: %d %s", read.Code, read.Body.String())
	}
	listed := call(http.MethodGet, "/api/issues/?limit=100", nil)
	if listed.Code != http.StatusOK || !bytes.Contains(listed.Body.Bytes(), []byte(id)) {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
}
