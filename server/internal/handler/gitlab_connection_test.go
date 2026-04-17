package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/gitlab"
	"github.com/multica-ai/multica/server/pkg/secrets"
)

// buildHandlerWithGitlab returns a Handler whose GitLab client points at a fake server URL.
// Reuses the existing testPool so DB queries work against the same fixtures.
func buildHandlerWithGitlab(t *testing.T, fakeGitlabURL string) *Handler {
	t.Helper()
	key := make([]byte, 32)
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	client := gitlab.NewClient(fakeGitlabURL, http.DefaultClient)
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	return New(
		db.New(testPool), testPool, hub, bus, emailSvc, nil, nil,
		cipher, client, true,
	)
}

// buildHandlerGitlabDisabled returns a Handler with GitlabEnabled=false and
// nil cipher/client — the production-equivalent of "feature flag is off".
// Used to verify the early-return guard in every gitlab handler.
func buildHandlerGitlabDisabled(t *testing.T) *Handler {
	t.Helper()
	hub := realtime.NewHub()
	go hub.Run()
	bus := events.New()
	emailSvc := service.NewEmailService()
	return New(
		db.New(testPool), testPool, hub, bus, emailSvc, nil, nil,
		nil, nil, false,
	)
}

func TestConnectGitlabWorkspace_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 555, "username": "svc-bot", "name": "Service Bot"}`))
		case "/api/v4/projects/42":
			w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	// Reset connection state.
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{
		"project": "42",
		"token":   "glpat-abc",
	})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/gitlab/connect", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectGitlabWorkspace(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got map[string]any
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got["gitlab_project_path"] != "team/app" {
		t.Errorf("gitlab_project_path = %v", got["gitlab_project_path"])
	}
	if _, hasTok := got["service_token_encrypted"]; hasTok {
		t.Errorf("response leaks service_token_encrypted field: %+v", got)
	}
	if _, hasTok := got["pat_encrypted"]; hasTok {
		t.Errorf("response leaks pat_encrypted field: %+v", got)
	}

	// Clean up.
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
}

func TestGetGitlabWorkspaceConnection_Connected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 555, "username": "svc-bot"}`))
		case "/api/v4/projects/42":
			w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Set up one connection via the POST handler.
	body, _ := json.Marshal(map[string]string{"project": "42", "token": "glpat-abc"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	h.ConnectGitlabWorkspace(httptest.NewRecorder(), req)

	// Now GET.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("X-User-ID", testUserID)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr, req2)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}
	var got gitlabConnectionResponse
	json.Unmarshal(rr.Body.Bytes(), &got)
	if got.GitlabProjectPath != "team/app" {
		t.Errorf("got %+v", got)
	}

	// Clean up.
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
}

func TestGetGitlabWorkspaceConnection_NotConnected(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer fake.Close()
	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestDisconnectGitlabWorkspace_Success(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 1, "username": "svc"}`))
		case "/api/v4/projects/1":
			w.Write([]byte(`{"id": 1, "path_with_namespace": "g/a"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Create one to delete.
	body, _ := json.Marshal(map[string]string{"project": "1", "token": "glpat-x"})
	postReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	postReq.Header.Set("X-User-ID", testUserID)
	postReq = withURLParam(postReq, "id", testWorkspaceID)
	h.ConnectGitlabWorkspace(httptest.NewRecorder(), postReq)

	// DELETE.
	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq = withURLParam(delReq, "id", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.DisconnectGitlabWorkspace(rr, delReq)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d body = %s", rr.Code, rr.Body.String())
	}

	// GET should now 404.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq.Header.Set("X-User-ID", testUserID)
	getReq = withURLParam(getReq, "id", testWorkspaceID)
	rr2 := httptest.NewRecorder()
	h.GetGitlabWorkspaceConnection(rr2, getReq)
	if rr2.Code != http.StatusNotFound {
		t.Fatalf("after delete, GET should 404, got %d", rr2.Code)
	}
}

func TestConnectGitlabWorkspace_BadToken(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "401 Unauthorized"}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{"project": "42", "token": "bad"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/gitlab/connect", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectGitlabWorkspace(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rr.Code, rr.Body.String())
	}
}

// M3: 403 on the project-permission branch.
func TestConnectGitlabWorkspace_ProjectForbidden(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			// Token authenticates as a real user…
			w.Write([]byte(`{"id": 555, "username": "svc-bot"}`))
		case "/api/v4/projects/42":
			// …but lacks api scope on the project.
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message": "403 Forbidden"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{"project": "42", "token": "glpat-readonly"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/gitlab/connect", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.ConnectGitlabWorkspace(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", rr.Code, rr.Body.String())
	}
}

// M2: second connect for an already-connected workspace returns 409.
func TestConnectGitlabWorkspace_AlreadyConnectedReturns409(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id": 555, "username": "svc-bot"}`))
		case "/api/v4/projects/42":
			w.Write([]byte(`{"id": 42, "path_with_namespace": "team/app"}`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]string{"project": "42", "token": "glpat-abc"})

	// First connect succeeds.
	req1 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req1.Header.Set("X-User-ID", testUserID)
	req1 = withURLParam(req1, "id", testWorkspaceID)
	rr1 := httptest.NewRecorder()
	h.ConnectGitlabWorkspace(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first connect status = %d, want 200; body = %s", rr1.Code, rr1.Body.String())
	}

	// Second connect on the same workspace must be rejected as a conflict.
	req2 := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req2.Header.Set("X-User-ID", testUserID)
	req2 = withURLParam(req2, "id", testWorkspaceID)
	rr2 := httptest.NewRecorder()
	h.ConnectGitlabWorkspace(rr2, req2)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("second connect status = %d, want 409; body = %s", rr2.Code, rr2.Body.String())
	}
}

// M1: feature-flag gate returns 404 from each of the three handlers.
func TestGitlabHandlers_DisabledReturns404(t *testing.T) {
	h := buildHandlerGitlabDisabled(t)

	tests := []struct {
		name   string
		method string
		fn     func(http.ResponseWriter, *http.Request)
	}{
		{"connect", http.MethodPost, h.ConnectGitlabWorkspace},
		{"get", http.MethodGet, h.GetGitlabWorkspaceConnection},
		{"disconnect", http.MethodDelete, h.DisconnectGitlabWorkspace},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", bytes.NewReader([]byte(`{}`)))
			req.Header.Set("X-User-ID", testUserID)
			req = withURLParam(req, "id", testWorkspaceID)
			rr := httptest.NewRecorder()
			tc.fn(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", rr.Code, rr.Body.String())
			}
		})
	}
}
