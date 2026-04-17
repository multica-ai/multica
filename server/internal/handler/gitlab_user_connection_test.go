package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestConnectUserGitlab_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Errorf("path = %s, want /api/v4/user", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice", "name": "Alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	body, _ := json.Marshal(map[string]string{"token": "glpat-user-abc"})
	req := httptest.NewRequest(http.MethodPost, "/api/me/gitlab/connect", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectUserGitlab(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["gitlab_username"] != "alice" {
		t.Errorf("gitlab_username = %v", got["gitlab_username"])
	}
	if _, hasTok := got["pat_encrypted"]; hasTok {
		t.Errorf("response leaks pat_encrypted: %+v", got)
	}
}

func TestConnectUserGitlab_BadToken(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"401 Unauthorized"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)

	body, _ := json.Marshal(map[string]string{"token": "bad"})
	req := httptest.NewRequest(http.MethodPost, "/api/me/gitlab/connect", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectUserGitlab(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestGetUserGitlabConnection_Connected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	// Seed via the connect handler.
	body, _ := json.Marshal(map[string]string{"token": "glpat-x"})
	connReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	connReq.Header.Set("X-User-ID", testUserID)
	connReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	h.ConnectUserGitlab(httptest.NewRecorder(), connReq)

	req := httptest.NewRequest(http.MethodGet, "/api/me/gitlab/connect", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetUserGitlabConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["connected"] != true || got["gitlab_username"] != "alice" {
		t.Errorf("got %+v", got)
	}
}

func TestGetUserGitlabConnection_NotConnected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/me/gitlab/connect", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetUserGitlabConnection(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["connected"] != false {
		t.Errorf("connected = %v, want false", got["connected"])
	}
}

func TestDisconnectUserGitlab_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 999, "username": "alice"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	body, _ := json.Marshal(map[string]string{"token": "glpat-x"})
	connReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	connReq.Header.Set("X-User-ID", testUserID)
	connReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	h.ConnectUserGitlab(httptest.NewRecorder(), connReq)

	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.DisconnectUserGitlab(rr, delReq)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}

	// GET should now show disconnected.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq.Header.Set("X-User-ID", testUserID)
	getReq.Header.Set("X-Workspace-ID", testWorkspaceID)
	getRr := httptest.NewRecorder()
	h.GetUserGitlabConnection(getRr, getReq)
	var got map[string]any
	json.Unmarshal(getRr.Body.Bytes(), &got)
	if got["connected"] != false {
		t.Errorf("connected = %v after delete, want false", got["connected"])
	}
}
