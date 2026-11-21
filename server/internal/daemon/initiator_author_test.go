package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepoCheckoutUsesTrustedInitiatorAuthor(t *testing.T) {
	const workspaceID = "ws-checkout"
	const repoURL = "https://github.com/org/repo.git"
	workDir := t.TempDir()
	cache := &recordingRepoCache{lookupPath: "/cache/org/repo.git"}
	d := newRepoCheckoutTestDaemon(t, workspaceID, repoURL, workDir, cache)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(WorkspaceReposResponse{
			WorkspaceID: workspaceID,
			Repos:       []RepoData{{URL: repoURL}},
			Settings:    json.RawMessage(`{"commit_author_is_initiator_enabled":true}`),
		})
	}))
	t.Cleanup(srv.Close)
	d.client = NewClient(srv.URL)
	d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{
		WorkspaceID:    workspaceID,
		TaskID:         "task-1",
		WorkDir:        workDir,
		InitiatorType:  "member",
		InitiatorName:  "Human",
		InitiatorEmail: "human@example.com",
	})
	body := `{"url":"` + repoURL + `","workspace_id":"` + workspaceID + `","workdir":"` + workDir + `","task_id":"task-1","initiator_name":"Untrusted","initiator_email":"untrusted@example.com"}`
	rec := httptest.NewRecorder()
	d.repoCheckoutHandler().ServeHTTP(rec, authorizedRepoCheckoutRequest(strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("checkout status = %d: %s", rec.Code, rec.Body.String())
	}
	params := cache.lastCreateParams()
	if !params.CommitAuthorIsInitiatorEnabled || params.InitiatorType != "member" || params.InitiatorName != "Human" || params.InitiatorEmail != "human@example.com" {
		t.Fatalf("checkout did not use trusted initiator: %+v", params)
	}
}

func TestWorkspaceCommitAuthorIsInitiatorEnabled(t *testing.T) {
	for _, tc := range []struct {
		name, settings string
		want           bool
	}{
		{"missing", "", false},
		{"empty", `{}`, false},
		{"enabled", `{"commit_author_is_initiator_enabled":true}`, true},
		{"disabled", `{"commit_author_is_initiator_enabled":false}`, false},
		{"paused", `{"github_enabled":false,"commit_author_is_initiator_enabled":true}`, false},
		{"independent trailer", `{"co_authored_by_enabled":false,"commit_author_is_initiator_enabled":true}`, true},
		{"malformed", `invalid`, false},
		{"wrong type", `{"commit_author_is_initiator_enabled":"true"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Daemon{workspaces: map[string]*workspaceState{"ws": newWorkspaceState("ws", nil, "", nil, json.RawMessage(tc.settings))}}
			if got := d.workspaceCommitAuthorIsInitiatorEnabled("ws"); got != tc.want {
				t.Fatalf("enabled = %v, want %v", got, tc.want)
			}
			if d.workspaceCommitAuthorIsInitiatorEnabled("unknown") {
				t.Fatal("unknown workspace enabled")
			}
		})
	}
}
