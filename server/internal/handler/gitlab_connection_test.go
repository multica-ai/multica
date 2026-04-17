package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
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
		case "/api/v4/projects/42/labels":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/42/members/all":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/issues":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/hooks":
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"id":11,"url":"x"}`))
			}
		default:
			// Unexpected paths from the sync goroutine should not fail the test,
			// but log them for debugging.
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
	// Immediate response status is 'connecting'; sync goroutine flips to 'connected'.
	if got["connection_status"] != "connecting" {
		t.Errorf("connection_status = %v, want 'connecting'", got["connection_status"])
	}
	if _, hasTok := got["service_token_encrypted"]; hasTok {
		t.Errorf("response leaks service_token_encrypted field: %+v", got)
	}
	if _, hasTok := got["pat_encrypted"]; hasTok {
		t.Errorf("response leaks pat_encrypted field: %+v", got)
	}

	// Wait for sync goroutine to settle before cleanup.
	time.Sleep(100 * time.Millisecond)

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
		case "/api/v4/projects/42/labels":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/42/members/all":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/issues":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/hooks":
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"id":11,"url":"x"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
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

	// Wait for sync goroutine to settle before cleanup.
	time.Sleep(100 * time.Millisecond)

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
		case "/api/v4/projects/1/labels":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/1/members/all":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/1/issues":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/1/hooks":
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"id":11,"url":"x"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
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

	// Wait for any sync goroutine to settle before the next test can run.
	time.Sleep(100 * time.Millisecond)
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
		case "/api/v4/projects/42/labels":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/42/members/all":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/issues":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/42/hooks":
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"id":11,"url":"x"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
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

	// Wait for sync goroutine from the first connect to settle.
	time.Sleep(100 * time.Millisecond)
}

func TestDisconnectGitlabWorkspace_TruncatesCache(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id":1,"username":"svc"}`))
		case "/api/v4/projects/1":
			w.Write([]byte(`{"id":1,"path_with_namespace":"g/a"}`))
		case "/api/v4/projects/1/labels":
			if r.Method == http.MethodGet {
				w.Write([]byte(`[]`))
			} else {
				w.Write([]byte(`{"id":1,"name":"x","color":"#000"}`))
			}
		case "/api/v4/projects/1/members/all":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/1/issues":
			w.Write([]byte(`[]`))
		case "/api/v4/projects/1/hooks":
			if r.Method == http.MethodPost {
				w.Write([]byte(`{"id":11,"url":"x"}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Connect, then wait for sync to finish so we have a baseline state.
	body, _ := json.Marshal(map[string]string{"project": "1", "token": "glpat-x"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	req = withURLParam(req, "id", testWorkspaceID)
	h.ConnectGitlabWorkspace(httptest.NewRecorder(), req)
	time.Sleep(150 * time.Millisecond)

	// Insert a synthetic cached row so we have something to delete.
	h.Queries.UpsertGitlabLabel(context.Background(), db.UpsertGitlabLabelParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		GitlabLabelID: 9999,
		Name:          "synthetic-test-label",
		Color:         "#000",
	})

	// Disconnect.
	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq = withURLParam(delReq, "id", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.DisconnectGitlabWorkspace(rr, delReq)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	// Cache should be empty.
	labels, _ := h.Queries.ListGitlabLabels(context.Background(), parseUUID(testWorkspaceID))
	if len(labels) != 0 {
		t.Errorf("expected cache truncated, found %d labels", len(labels))
	}
}

func TestGitlabConnectedWorkspace_WriteReturns501(t *testing.T) {
	h := buildHandlerWithGitlab(t, "http://unused")
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	h.Queries.CreateWorkspaceGitlabConnection(context.Background(), db.CreateWorkspaceGitlabConnectionParams{
		WorkspaceID:           parseUUID(testWorkspaceID),
		GitlabProjectID:       42,
		GitlabProjectPath:     "team/app",
		ServiceTokenEncrypted: []byte("x"),
		ServiceTokenUserID:    1,
		ConnectionStatus:      "connected",
	})
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Build a tiny router that mounts CreateIssue under the middleware.
	r := chi.NewRouter()
	r.Route("/api/workspaces/{id}/issues", func(r chi.Router) {
		r.Use(middleware.GitlabWritesBlocked(h.Queries))
		r.Post("/", h.CreateIssue)
	})

	body, _ := json.Marshal(map[string]any{"title": "Test", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/workspaces/%s/issues/", testWorkspaceID), bytes.NewReader(body))
	req.Header.Set("X-User-ID", testUserID)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501; body = %s", rr.Code, rr.Body.String())
	}
}

// I-1 fix: comment write routes also need the 501 stopgap.
// The /api/comments/{commentId} subrouter doesn't carry a workspace ID in
// its URL — the middleware falls back to the X-Workspace-ID header (set by
// the workspace-scoped middleware groups in the real router).
func TestGitlabConnectedWorkspace_CommentWriteReturns501(t *testing.T) {
	h := buildHandlerWithGitlab(t, "http://unused")
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	h.Queries.CreateWorkspaceGitlabConnection(context.Background(), db.CreateWorkspaceGitlabConnectionParams{
		WorkspaceID:           parseUUID(testWorkspaceID),
		GitlabProjectID:       42,
		GitlabProjectPath:     "team/app",
		ServiceTokenEncrypted: []byte("x"),
		ServiceTokenUserID:    1,
		ConnectionStatus:      "connected",
	})
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	r := chi.NewRouter()
	r.Route("/api/comments/{commentId}", func(r chi.Router) {
		r.Use(middleware.GitlabWritesBlocked(h.Queries))
		r.Put("/", h.UpdateComment)
		r.Delete("/", h.DeleteComment)
		r.Post("/reactions", h.AddReaction)
		r.Delete("/reactions", h.RemoveReaction)
	})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"update", http.MethodPut, "/api/comments/00000000-0000-0000-0000-000000000000/", `{"content":"x"}`},
		{"delete", http.MethodDelete, "/api/comments/00000000-0000-0000-0000-000000000000/", ``},
		{"add reaction", http.MethodPost, "/api/comments/00000000-0000-0000-0000-000000000000/reactions", `{"emoji":"thumbsup"}`},
		{"remove reaction", http.MethodDelete, "/api/comments/00000000-0000-0000-0000-000000000000/reactions", `{"emoji":"thumbsup"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
			req.Header.Set("X-User-ID", testUserID)
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotImplemented {
				t.Fatalf("%s status = %d, want 501; body = %s", tc.name, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestDisconnectGitlabWorkspace_DeletesGitlabHook(t *testing.T) {
	var hookDeleteHit bool
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v4/projects/7/hooks/11" {
			hookDeleteHit = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id":1,"username":"svc"}`))
		case "/api/v4/projects/7":
			w.Write([]byte(`{"id":7,"path_with_namespace":"g/a"}`))
		default:
			w.Write([]byte(`[]`))
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	// Seed a row WITH webhook_gitlab_id = 11 + an encrypted token.
	encrypted, _ := h.Secrets.Encrypt([]byte("glpat-x"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id,
			webhook_secret, webhook_gitlab_id, connection_status
		) VALUES ($1, 7, 'g/a', $2, 1, 'wh-secret', 11, 'connected')
	`, testWorkspaceID, encrypted)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	delReq := httptest.NewRequest(http.MethodDelete, "/", nil)
	delReq.Header.Set("X-User-ID", testUserID)
	delReq = withURLParam(delReq, "id", testWorkspaceID)
	rr := httptest.NewRecorder()
	h.DisconnectGitlabWorkspace(rr, delReq)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rr.Code)
	}
	if !hookDeleteHit {
		t.Errorf("DeleteProjectHook was not called")
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
