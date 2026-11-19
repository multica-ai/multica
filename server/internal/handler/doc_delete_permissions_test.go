package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDocDeletionPermissions(t *testing.T) {
	for _, kind := range []string{"doc", "folder"} {
		for _, tc := range []struct {
			name, role, actor, creator string
			allowed                    bool
		}{
			{"member deletes own", "member", "member", "self", true},
			{"member denied others", "member", "member", "other", false},
			{"member deletes own agent's", "member", "member", "owned-agent", true},
			{"member denied other agent's", "member", "member", "other-agent", false},
			{"member denied former agent's", "member", "member", "former-agent", false},
			{"member denied missing agent's", "member", "member", "missing-agent", false},
			{"owner deletes others", "owner", "member", "other", true},
			{"admin deletes others", "admin", "member", "other", true},
			{"admin deletes missing agent's", "admin", "member", "missing-agent", true},
			{"agent denied even its own", "owner", "agent", "owned-agent", false},
			{"cloud PAT denied admin rights", "admin", "cloud_pat", "other", false},
			{"cloud PAT denied creator rights", "member", "cloud_pat", "self", false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				ws := dbfx.Workspace(t, "doc deletion", "doc-delete-"+uuid.NewString())
				dbfx.Member(t, ws, testUserID, tc.role)
				agent := dbfx.Agent(t, "own agent", "", testutil.Cols{"workspace_id": ws})
				other := dbfx.User(t, "Other", "doc-delete-"+uuid.NewString()+"@example.test")
				dbfx.Member(t, ws, other, "member")
				otherAgent := dbfx.Agent(t, "other agent", "", testutil.Cols{"workspace_id": ws, "owner_id": other})

				creatorUser, creatorAgent := testUserID, ""
				switch tc.creator {
				case "other":
					creatorUser = other
				case "owned-agent", "missing-agent", "former-agent":
					creatorAgent = agent
				case "other-agent":
					creatorUser, creatorAgent = other, otherAgent
				}
				path, create, del, table := "/api/docs", testHandler.CreateDoc, testHandler.DeleteDoc, "doc"
				if kind == "folder" {
					path, create, del, table = "/api/doc-folders", testHandler.CreateDocFolder, testHandler.DeleteDocFolder, "doc_folder"
				}
				req := newRequestAs(creatorUser, http.MethodPost, path, map[string]any{"title": "delete target"})
				req.Header.Set("X-Workspace-ID", ws)
				if creatorAgent != "" {
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", creatorAgent)
				}
				var created struct {
					ID            string `json:"id"`
					CreatedByType string `json:"created_by_type"`
				}
				testutil.Call(t, create, req).Want(http.StatusCreated).JSON(&created)
				id := created.ID
				t.Cleanup(func() {
					dbfx.Exec(t, "DELETE FROM doc_version WHERE doc_id=$1", id)
					dbfx.Exec(t, "DELETE FROM "+table+" WHERE id=$1", id)
				})
				if wantType := map[bool]string{true: "agent", false: "member"}[creatorAgent != ""]; created.CreatedByType != wantType {
					t.Fatalf("created_by_type = %q, want %q", created.CreatedByType, wantType)
				}
				switch tc.creator {
				case "missing-agent":
					dbfx.Exec(t, "DELETE FROM agent WHERE id=$1", agent)
				case "former-agent":
					dbfx.Exec(t, "UPDATE agent SET owner_id=$1 WHERE id=$2", other, agent)
				}

				req = withURLParam(newRequest(http.MethodDelete, path+"/"+id, nil), "id", id)
				req.Header.Set("X-Workspace-ID", ws)
				switch tc.actor {
				case "agent":
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", agent)
				case "cloud_pat":
					req.Header.Set("X-Actor-Source", tc.actor)
				}
				want := http.StatusForbidden
				if tc.allowed {
					want = http.StatusNoContent
				}
				testutil.Call(t, del, req).Want(want)
				if count := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE id=$1", id); (count == 0) != tc.allowed {
					t.Fatalf("remaining %s rows = %d, allowed = %v", table, count, tc.allowed)
				}
			})
		}
	}
}

// A mul_ personal access token authenticates as its human, but Auth sets no
// X-Actor-Source for it, so the handler-level cases above cannot tell it from
// a login session. Go through the real Auth middleware: a PAT must not delete
// even what its owner may delete in the app, while a login session still can.
func TestDocDeletionRefusesPersonalAccessTokens(t *testing.T) {
	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": testUserID, "name": "Doc delete test", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12]})
	session, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": testUserID, "exp": time.Now().Add(time.Hour).Unix()}).SignedString(auth.JWTSecret())
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"doc", "folder"} {
		for _, tc := range []struct {
			name, role, creator, credential string
			allowed                         bool
		}{
			{"member PAT denied own", "member", "self", "pat", false},
			{"admin PAT denied others", "admin", "other", "pat", false},
			{"member session deletes own", "member", "self", "session", true},
			{"admin session deletes others", "admin", "other", "session", true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				ws := dbfx.Workspace(t, "doc deletion pat", "doc-delete-pat-"+uuid.NewString())
				dbfx.Member(t, ws, testUserID, tc.role)
				creator := testUserID
				if tc.creator == "other" {
					creator = dbfx.User(t, "Other", "doc-delete-pat-"+uuid.NewString()+"@example.test")
					dbfx.Member(t, ws, creator, "member")
				}
				path, create, del, table := "/api/docs", testHandler.CreateDoc, testHandler.DeleteDoc, "doc"
				if kind == "folder" {
					path, create, del, table = "/api/doc-folders", testHandler.CreateDocFolder, testHandler.DeleteDocFolder, "doc_folder"
				}
				req := newRequestAs(creator, http.MethodPost, path, map[string]any{"title": "delete target"})
				req.Header.Set("X-Workspace-ID", ws)
				var created struct {
					ID string `json:"id"`
				}
				testutil.Call(t, create, req).Want(http.StatusCreated).JSON(&created)
				id := created.ID
				t.Cleanup(func() {
					dbfx.Exec(t, "DELETE FROM doc_version WHERE doc_id=$1", id)
					dbfx.Exec(t, "DELETE FROM "+table+" WHERE id=$1", id)
				})

				req = withURLParam(testutil.WithHeaders(testutil.JSONRequest(http.MethodDelete, path+"/"+id, nil), "X-Workspace-ID", ws), "id", id)
				token := session
				if tc.credential == "pat" {
					token = pat
				}
				req.Header.Set("Authorization", "Bearer "+token)
				serve := middleware.Auth(testHandler.Queries, nil, nil, nil)(middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(del)))
				want := http.StatusForbidden
				if tc.allowed {
					want = http.StatusNoContent
				}
				testutil.Call(t, serve.ServeHTTP, req).Want(want)
				if count := dbfx.Count(t, "SELECT count(*) FROM "+table+" WHERE id=$1", id); (count == 0) != tc.allowed {
					t.Fatalf("remaining %s rows = %d, allowed = %v", table, count, tc.allowed)
				}
			})
		}
	}
}
