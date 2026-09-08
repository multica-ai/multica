package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWorkspaceCheckoutCannotUseWorkspaceOrSiblingRepo(t *testing.T) {
	const workspace = "ws-test"
	const repo = "https://github.com/example/workspace-repo"
	dir := t.TempDir()
	d := newRepoCheckoutTestDaemon(t, workspace, repo, dir, &recordingRepoCache{})
	for _, policy := range []map[string]string{{}, {"https://github.com/example/approved": strings.Repeat("a", 40)}} {
		d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{WorkspaceID: workspace, TaskID: "task-1", WorkDir: dir, RepositoryPolicy: policy})
		payload, _ := json.Marshal(map[string]string{"workspace_id": workspace, "task_id": "task-1", "workdir": dir, "url": repo})
		w := httptest.NewRecorder()
		d.repoCheckoutHandler()(w, authorizedRepoCheckoutRequest(bytes.NewReader(payload)))
		if w.Code != http.StatusForbidden {
			t.Fatalf("workspace repo bypassed run policy: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestRunWorkspaceCheckoutRejectsBaseOverride(t *testing.T) {
	const repo = "https://github.com/example/approved"
	dir := t.TempDir()
	d := newRepoCheckoutTestDaemon(t, "ws-test", repo, dir, &recordingRepoCache{})
	d.registerActiveRepoCheckoutTask("mat_repo_checkout_test", activeRepoCheckoutTask{WorkspaceID: "ws-test", TaskID: "task-1", WorkDir: dir, RepositoryPolicy: map[string]string{repo: strings.Repeat("a", 40)}})
	payload, _ := json.Marshal(map[string]string{"workspace_id": "ws-test", "task_id": "task-1", "workdir": dir, "url": repo, "ref": "main"})
	w := httptest.NewRecorder()
	d.repoCheckoutHandler()(w, authorizedRepoCheckoutRequest(bytes.NewReader(payload)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("mutable base override accepted: %d", w.Code)
	}
}
