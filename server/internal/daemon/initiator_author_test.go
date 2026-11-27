package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/daemon/repocache"
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
	payload, err := json.Marshal(map[string]string{
		"url":             repoURL,
		"workspace_id":    workspaceID,
		"workdir":         workDir,
		"task_id":         "task-1",
		"initiator_name":  "Untrusted",
		"initiator_email": "untrusted@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(payload)
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

type refreshingRepoCache struct {
	recordingRepoCache
	workDir string
	params  repocache.WorktreeParams
	calls   int
}

func (c *refreshingRepoCache) RefreshInitiatorAuthor(_ context.Context, workDir string, params repocache.WorktreeParams) error {
	c.calls++
	c.workDir, c.params = workDir, params
	return nil
}

func TestRunStartRefreshesInitiatorAuthor(t *testing.T) {
	cache := &refreshingRepoCache{}
	d := &Daemon{repoCache: cache, workspaces: map[string]*workspaceState{"ws": newWorkspaceState("ws", nil, "", nil, json.RawMessage(`{"commit_author_is_initiator_enabled":true}`))}}
	task := Task{WorkspaceID: "ws", InitiatorType: "member", InitiatorName: "Bob", InitiatorEmail: "bob@example.com"}
	d.refreshInitiatorAuthor(context.Background(), task, &execenv.Environment{WorkDir: "/work/task"}, nil)
	if cache.calls != 1 || cache.workDir != "/work/task" || !cache.params.CommitAuthorIsInitiatorEnabled || cache.params.InitiatorName != "Bob" || cache.params.InitiatorEmail != "bob@example.com" {
		t.Fatalf("refresh = %d %q %+v", cache.calls, cache.workDir, cache.params)
	}
	d.refreshInitiatorAuthor(context.Background(), task, &execenv.Environment{WorkDir: "/user/dir", LocalDirectory: true}, nil)
	if cache.calls != 1 {
		t.Fatal("refreshed a user-owned local directory")
	}
}
