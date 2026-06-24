package handler

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// setupWorkspaceSecretService creates a WorkspaceSecretService and sets it on
// testHandler. Returns a cleanup function that restores the previous state.
func setupWorkspaceSecretService(t *testing.T) func() {
	t.Helper()

	key := make([]byte, secretbox.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("create box: %v", err)
	}

	prev := testHandler.WorkspaceSecretService
	testHandler.WorkspaceSecretService = service.NewWorkspaceSecretService(testHandler.Queries, box)
	return func() {
		testHandler.WorkspaceSecretService = prev
	}
}

// TestListWorkspaceSecrets_DoesNotReturnValues verifies that the secrets API
// no longer returns plaintext values even when include_values=true is requested.
func TestListWorkspaceSecrets_DoesNotReturnValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupWorkspaceSecretService(t)
	defer cleanup()

	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/secrets?include_values=true", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceSecretNames(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the response contains secrets array but NOT values map.
	var resp struct {
		Secrets []struct {
			Name      string `json:"name"`
			CreatedBy string `json:"created_by,omitempty"`
		} `json:"secrets"`
	}
	bodyBytes := rec.Body.Bytes()
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		t.Fatalf("parse response: %v", err)
	}

	// values key must not appear in the response.
	if containsKey(string(bodyBytes), `"values"`) {
		t.Fatal("response must not contain 'values' key")
	}
}

// TestListWorkspaceSecrets_AgentIsRejected verifies agents cannot access secrets.
func TestListWorkspaceSecrets_AgentIsRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("test database not available")
	}
	cleanup := setupWorkspaceSecretService(t)
	defer cleanup()

	req := newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/secrets", nil)
	// Simulate an agent actor by setting the server-trusted header pair.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", "00000000-0000-0000-0000-000000000001")
	req = withURLParam(req, "id", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.ListWorkspaceSecretNames(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func containsKey(body, key string) bool {
	for i := 0; i <= len(body)-len(key); i++ {
		if body[i:i+len(key)] == key {
			return true
		}
	}
	return false
}
