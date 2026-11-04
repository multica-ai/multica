package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// stubRepoDocs is the git client the handler tests run against: it returns the
// docs it is given, so no repository is cloned.
type stubRepoDocs struct{ docs []service.RepoDoc }

func (s stubRepoDocs) Snapshot(context.Context, service.RepoSyncSettings) ([]service.RepoDoc, error) {
	return s.docs, nil
}

// The repo-docs setting: defaults when none is saved, validation on save, a run
// now that syncs and records the outcome, and a disabled setting that does nothing.
func TestKnowledgeRepoSyncSettingIsValidatedAndRunOnDemand(t *testing.T) {
	newKnowledgeFixture(t)
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM knowledge_repo_sync WHERE workspace_id = $1`, testWorkspaceID); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	testHandler.Knowledge.RepoDocs = stubRepoDocs{docs: []service.RepoDoc{{
		Path: "docs/adr/0001-storage.md", Content: "# Storage\n\n## Summary\n\nStorage is a table.\n",
	}}}
	t.Cleanup(func() { testHandler.Knowledge.RepoDocs = nil })

	var initial service.RepoSyncStatus
	testutil.Call(t, testHandler.GetKnowledgeRepoSync, newRequest("GET", "/api/knowledge/repo-sync", nil)).
		Want(http.StatusOK).JSON(&initial)
	if initial.Enabled || initial.Branch != "main" || initial.DocsPath != "docs" || initial.KBKey != "engineering" {
		t.Fatalf("default setting = %+v, want off with main, docs and engineering", initial.RepoSyncSettings)
	}

	// Only https addresses are accepted, and a body that does not decode is refused.
	insecure := newRequest("PUT", "/api/knowledge/repo-sync", map[string]any{
		"enabled": true, "repo_url": "http://example.test/acme/app.git",
	})
	testutil.Call(t, testHandler.UpdateKnowledgeRepoSync, insecure).Want(http.StatusBadRequest)
	badBody := newRequest("PUT", "/api/knowledge/repo-sync", nil)
	badBody.Body = http.NoBody
	testutil.Call(t, testHandler.UpdateKnowledgeRepoSync, badBody).Want(http.StatusBadRequest)

	// A disabled setting saves and a run does nothing.
	off := newRequest("PUT", "/api/knowledge/repo-sync", map[string]any{
		"enabled": false, "repo_url": "https://example.test/acme/app.git",
	})
	var saved service.RepoSyncStatus
	testutil.Call(t, testHandler.UpdateKnowledgeRepoSync, off).Want(http.StatusOK).JSON(&saved)
	if saved.Enabled || saved.RepoURL != "https://example.test/acme/app.git" {
		t.Fatalf("saved disabled setting = %+v", saved.RepoSyncSettings)
	}
	var idle service.RepoSyncStatus
	testutil.Call(t, testHandler.RunKnowledgeRepoSync, newRequest("POST", "/api/knowledge/repo-sync/run", nil)).
		Want(http.StatusOK).JSON(&idle)
	if idle.LastSyncedAt != nil {
		t.Fatalf("a disabled setting synced: %+v", idle)
	}

	// Enabled, the run creates the document and stamps the last sync.
	on := newRequest("PUT", "/api/knowledge/repo-sync", map[string]any{
		"enabled": true, "repo_url": "https://example.test/acme/app.git", "branch": "main",
		"docs_path": "docs", "knowledge_base_key": "engineering",
	})
	testutil.Call(t, testHandler.UpdateKnowledgeRepoSync, on).Want(http.StatusOK).JSON(&saved)
	if !saved.Enabled {
		t.Fatalf("enabled setting = %+v", saved.RepoSyncSettings)
	}
	var ran service.RepoSyncStatus
	testutil.Call(t, testHandler.RunKnowledgeRepoSync, newRequest("POST", "/api/knowledge/repo-sync/run", nil)).
		Want(http.StatusOK).JSON(&ran)
	if ran.LastSyncedAt == nil || ran.LastError != "" {
		t.Fatalf("run did not record a clean sync: %+v", ran)
	}

	// A base the workspace does not have is refused.
	missing := newRequest("PUT", "/api/knowledge/repo-sync", map[string]any{
		"enabled": true, "repo_url": "https://example.test/acme/app.git", "knowledge_base_key": "nowhere",
	})
	testutil.Call(t, testHandler.UpdateKnowledgeRepoSync, missing).Want(http.StatusBadRequest)
}
