package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gitlabsync "github.com/multica-ai/multica/server/internal/gitlab"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateIssue_WriteThroughHumanWithoutPATUsesServicePAT(t *testing.T) {
	var capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				w.Write([]byte(`{"id":9901,"iid":99,"title":"From Multica","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
		w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 99`, testWorkspaceID)

	// Seed a workspace_gitlab_connection so the handler takes write-through.
	encrypted, _ := h.Secrets.Encrypt([]byte("svc-token-xyz"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			gitlab_project_id = EXCLUDED.gitlab_project_id,
			service_token_encrypted = EXCLUDED.service_token_encrypted,
			service_token_user_id = EXCLUDED.service_token_user_id
	`, testWorkspaceID, encrypted)

	// Wire a real resolver on the handler so the write-through branch works.
	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{
		"title":    "From Multica",
		"status":   "todo",
		"priority": "medium",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if capturedToken != "svc-token-xyz" {
		t.Errorf("PRIVATE-TOKEN sent to gitlab = %q, want svc-token-xyz (service PAT)", capturedToken)
	}

	// Verify the cache row exists with the GitLab IID.
	var iid int
	testPool.QueryRow(context.Background(),
		`SELECT gitlab_iid FROM issue WHERE workspace_id = $1::uuid AND title = 'From Multica'`,
		testWorkspaceID).Scan(&iid)
	if iid != 99 {
		t.Errorf("cached gitlab_iid = %d, want 99", iid)
	}
}

func TestCreateIssue_WriteThroughHumanWithPATUsesUserPAT(t *testing.T) {
	var capturedToken string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/user":
			w.Write([]byte(`{"id":555,"username":"alice"}`))
		case "/api/v4/projects/42/issues":
			if r.Method == http.MethodPost {
				capturedToken = r.Header.Get("PRIVATE-TOKEN")
				w.Write([]byte(`{"id":9902,"iid":100,"title":"From Alice","state":"opened",
					"labels":["status::todo","priority::medium"],"updated_at":"2026-04-17T15:00:00Z"}`))
				return
			}
		}
	}))
	defer fake.Close()

	h := buildHandlerWithGitlab(t, fake.URL)
	defer h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1::uuid AND gitlab_iid = 100`, testWorkspaceID)
	defer h.Queries.DeleteUserGitlabConnection(context.Background(), db.DeleteUserGitlabConnectionParams{
		UserID:      parseUUID(testUserID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})

	svcEnc, _ := h.Secrets.Encrypt([]byte("svc-token"))
	usrEnc, _ := h.Secrets.Encrypt([]byte("user-token-alice"))
	testPool.Exec(context.Background(), `
		INSERT INTO workspace_gitlab_connection (
			workspace_id, gitlab_project_id, gitlab_project_path,
			service_token_encrypted, service_token_user_id, connection_status
		) VALUES ($1, 42, 'g/a', $2, 1, 'connected')
		ON CONFLICT (workspace_id) DO UPDATE SET
			service_token_encrypted = EXCLUDED.service_token_encrypted
	`, testWorkspaceID, svcEnc)
	h.Queries.UpsertUserGitlabConnection(context.Background(), db.UpsertUserGitlabConnectionParams{
		UserID:         parseUUID(testUserID),
		WorkspaceID:    parseUUID(testWorkspaceID),
		GitlabUserID:   555,
		GitlabUsername: "alice",
		PatEncrypted:   usrEnc,
	})

	h.SetGitlabResolver(gitlabsync.NewResolver(h.Queries, func(_ context.Context, b []byte) (string, error) {
		plain, err := h.Secrets.Decrypt(b)
		if err != nil {
			return "", err
		}
		return string(plain), nil
	}))

	body, _ := json.Marshal(map[string]any{"title": "From Alice", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d", rr.Code)
	}
	if capturedToken != "user-token-alice" {
		t.Errorf("PRIVATE-TOKEN = %q, want user-token-alice", capturedToken)
	}
}

func TestCreateIssue_LegacyPathWhenNoGitlabConnection(t *testing.T) {
	// No workspace_gitlab_connection row → handler takes the legacy direct-DB
	// path. (Same behaviour as pre-Phase-3a.)
	h := buildHandlerWithGitlab(t, "http://unused")
	h.Queries.DeleteWorkspaceGitlabConnection(context.Background(), parseUUID(testWorkspaceID))

	body, _ := json.Marshal(map[string]any{"title": "Legacy", "status": "todo", "priority": "medium"})
	req := httptest.NewRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rr := httptest.NewRecorder()

	h.CreateIssue(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
}
