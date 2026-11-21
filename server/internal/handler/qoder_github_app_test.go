package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/qoderruntime"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// withQoderGitHubApp wires the App identity and a stubbed GitHub API for
// repository-token issuance tests. It returns the installation ID bound to
// the test workspace.
func withQoderGitHubApp(t *testing.T, handler http.HandlerFunc) int64 {
	t.Helper()
	pemBytes, _ := generateTestRSAKeyPEM(t)
	t.Setenv("GITHUB_APP_ID", "11111")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", string(pemBytes))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	oldBase := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = oldBase })

	const installationID int64 = 909090
	_, err := testHandler.Queries.CreateGitHubInstallation(context.Background(), db.CreateGitHubInstallationParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		InstallationID: installationID,
		AccountLogin:   "acme",
		AccountType:    "Organization",
	})
	if err != nil {
		t.Fatalf("CreateGitHubInstallation: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_installation WHERE installation_id = $1`, installationID)
	})
	return installationID
}

// The issued installation token request must name exactly one repository and
// the minimal permission set, and the response must confirm that repository;
// the returned token never reaches anything wider.
func TestIssueQoderRepositoryTokenSingleRepositoryScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	var requestBodies []map[string]any
	installationID := withQoderGitHubApp(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/installations/909090/access_tokens" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requestBodies = append(requestBodies, body)
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing App JWT: %q", auth)
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":        "ghs_one_repo",
			"expires_at":   "2026-11-09T12:00:00Z",
			"repositories": []map[string]any{{"name": "app", "owner": map[string]any{"login": "acme"}}},
		})
	})

	token, gotInstallation, err := testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "acme", "app")
	if err != nil {
		t.Fatalf("issueQoderRepositoryToken: %v", err)
	}
	if token != "ghs_one_repo" || gotInstallation != installationID {
		t.Fatalf("token=%q installation=%d", token, gotInstallation)
	}
	if len(requestBodies) != 1 {
		t.Fatalf("expected one token request, got %d", len(requestBodies))
	}
	want := map[string]any{"repositories": []any{"app"}, "permissions": map[string]any{"contents": "write", "pull_requests": "write", "metadata": "read"}}
	raw, _ := json.Marshal(requestBodies[0])
	rawWant, _ := json.Marshal(want)
	if string(raw) != string(rawWant) {
		t.Fatalf("token request scope: got %s want %s", raw, rawWant)
	}
}

// A workspace installation that does not cover the repository — wrong owner,
// or GitHub refusing the single-repository grant — must surface
// ErrGitHubAppNotCovered so the bridge falls back to a clear rejection.
func TestIssueQoderRepositoryTokenNotCovered(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withQoderGitHubApp(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	if _, _, err := testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "someone-else", "app"); !errors.Is(err, qoderruntime.ErrGitHubAppNotCovered) {
		t.Fatalf("wrong owner: err=%v", err)
	}
	if _, _, err := testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "acme", "not-granted"); !errors.Is(err, qoderruntime.ErrGitHubAppNotCovered) {
		t.Fatalf("revoked repository: err=%v", err)
	}
}

// GitHub's two 422 refusals mean different things: ungranted permissions are
// an operator-actionable configuration gap, while a repository the
// installation cannot see is plain "not covered". Conflating them sends the
// admin hunting for a missing repository when the App needs permissions.
func TestIssueQoderRepositoryTokenPermissionRefusal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	message := "The repository acme/app was not found in the installation."
	withQoderGitHubApp(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": message})
	})
	_, _, err := testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "acme", "app")
	if !errors.Is(err, qoderruntime.ErrGitHubAppNotCovered) {
		t.Fatalf("repository refusal: err=%v", err)
	}
	message = "The permissions requested are not granted to this installation."
	_, _, err = testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "acme", "app")
	if !errors.Is(err, qoderruntime.ErrGitHubAppPermissions) {
		t.Fatalf("permission refusal: err=%v", err)
	}
	if !strings.Contains(err.Error(), "approve Contents and Pull requests write access") {
		t.Fatalf("permission refusal is not actionable: %v", err)
	}
}

// A GitHub response whose repositories do not confirm the requested single
// repository is a boundary violation; no token is handed out.
func TestIssueQoderRepositoryTokenRejectsWiderScope(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	withQoderGitHubApp(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":        "ghs_wide",
			"repositories": []map[string]any{},
		})
	})
	if token, _, err := testHandler.issueQoderRepositoryToken(context.Background(), testWorkspaceID, "acme", "app"); err == nil || token != "" {
		t.Fatalf("wide-scope token accepted: token=%q err=%v", token, err)
	}
}
