package handler

// CEREBRO-PATCH(agent-runs-folder): FIR-2697 part 3 — integration tests for the
// automatic Agent Runs folder structure. Uses the shared handler test harness
// (testHandler / testPool / testWorkspaceID / testUserID) and skips when the DB
// is unavailable, like its siblings.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// enableAgentRunsFolders flips the workspace flag on for the test workspace
// (all-zero user_id = workspace-level row) and cleans it up after.
func enableAgentRunsFolders(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		 VALUES ($1, '00000000-0000-0000-0000-000000000000', 'cerebro_agent_runs_folders', true)
		 ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE SET enabled = true`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("enable agent-runs-folders flag: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = 'cerebro_agent_runs_folders'`,
			testWorkspaceID,
		)
	})
}

// cleanupAgentRunsFolders removes every "Agent Runs" root (and, by cascade, its
// descendants) plus the grants stamped on the removed folders, so a test leaves
// no folders behind. The grant table has no FK to artifact_folder, so grants are
// deleted explicitly before the cascade drops the rows.
func cleanupAgentRunsFolders(t *testing.T) {
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `
			WITH RECURSIVE tree AS (
				SELECT id FROM artifact_folder
				WHERE workspace_id = $1 AND parent_id IS NULL AND name = 'Agent Runs'
				UNION ALL
				SELECT f.id FROM artifact_folder f JOIN tree t ON f.parent_id = t.id
			)
			DELETE FROM cerebro_folder_grant
			WHERE surface = 'artifact' AND folder_id IN (SELECT id FROM tree)`,
			testWorkspaceID,
		)
		testPool.Exec(ctx,
			`DELETE FROM artifact_folder WHERE workspace_id = $1 AND parent_id IS NULL AND name = 'Agent Runs'`,
			testWorkspaceID,
		)
	})
}

// seedAgentRunsIssue inserts an issue in the test workspace and returns (id, title).
func seedAgentRunsIssue(t *testing.T, title string) (string, string) {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, number)
		VALUES ($1, $2, 'in_progress', 'none', 'member', $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id`,
		testWorkspaceID, title, testUserID,
	).Scan(&issueID); err != nil {
		t.Fatalf("seed issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID, title
}

// agentSave posts an artifact as an agent-in-a-run (X-Agent-ID + X-Task-ID) and
// returns the created artifact. folderID may be "" for an unfiled save.
func agentSave(t *testing.T, agentID, taskID, kind, title, folderID string) ArtifactResponse {
	t.Helper()
	body := map[string]any{"kind": kind, "title": title, "body": "b"}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	req := newRequest("POST", "/api/artifacts", body)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent CreateArtifact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifacts/"+a.ID, nil), "id", a.ID)
		testHandler.DeleteArtifact(httptest.NewRecorder(), req)
	})
	return a
}

// folderRow reads name, kind, parent_id (as string, "" when NULL) for a folder.
func folderRow(t *testing.T, id string) (name, kind, parent string) {
	t.Helper()
	var p *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT name, kind, parent_id::text FROM artifact_folder WHERE id = $1`, id,
	).Scan(&name, &kind, &p); err != nil {
		t.Fatalf("read folder %s: %v", id, err)
	}
	if p != nil {
		parent = *p
	}
	return
}

// workspaceViewerGrantCount returns how many workspace viewer grants exist on a folder.
func workspaceViewerGrantCount(t *testing.T, folderID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM cerebro_folder_grant
		 WHERE surface = 'artifact' AND folder_id = $1
		   AND grantee_type = 'workspace' AND role = 'viewer'`, folderID,
	).Scan(&n); err != nil {
		t.Fatalf("count grants: %v", err)
	}
	return n
}

func TestAgentRunsFolder_AutoFilesIntoTree(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	issueID, issueTitle := seedAgentRunsIssue(t, "Ship the launch plan")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	art := agentSave(t, agentID, taskID, "report", "Launch notes", "")
	if art.FolderID == nil {
		t.Fatalf("expected the agent save to be auto-filed, folder_id is nil")
	}

	// Walk the four levels leaf → root, asserting name, surface, and that every
	// folder carries its Collections workspace viewer grant from creation.
	runID := *art.FolderID
	runName, runKind, agentID2 := folderRow(t, runID)
	if runName != issueTitle || runKind != "document" {
		t.Fatalf("run folder = (%q,%q), want (%q,document)", runName, runKind, issueTitle)
	}

	agentName, _, memberID := folderRow(t, agentID2)
	if agentName != "Sara" {
		t.Fatalf("agent folder name = %q, want Sara", agentName)
	}

	memberName, _, rootID := folderRow(t, memberID)
	if memberName != handlerTestName {
		t.Fatalf("member folder name = %q, want %q", memberName, handlerTestName)
	}

	rootName, _, rootParent := folderRow(t, rootID)
	if rootName != "Agent Runs" {
		t.Fatalf("root folder name = %q, want Agent Runs", rootName)
	}
	if rootParent != "" {
		t.Fatalf("Agent Runs root should have no parent, got %q", rootParent)
	}

	for _, id := range []string{runID, agentID2, memberID, rootID} {
		if workspaceViewerGrantCount(t, id) != 1 {
			t.Fatalf("folder %s missing its Collections workspace viewer grant", id)
		}
	}
}

func TestAgentRunsFolder_DisabledByDefault(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// No enableAgentRunsFolders call — flag is off.
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "No structure please")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	art := agentSave(t, agentID, taskID, "report", "Doc", "")
	if art.FolderID != nil {
		t.Fatalf("flag off: artifact must not be auto-filed, got folder_id=%v", *art.FolderID)
	}
	var n int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM artifact_folder WHERE workspace_id = $1 AND name = 'Agent Runs'`,
		testWorkspaceID).Scan(&n)
	if n != 0 {
		t.Fatalf("flag off: no Agent Runs folder should exist, found %d", n)
	}
}

func TestAgentRunsFolder_RespectsExplicitFolder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "Explicit wins")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	folder := createSuggestionFolder(t, "Chosen folder", "document")
	art := agentSave(t, agentID, taskID, "report", "Doc", folder.ID)
	if art.FolderID == nil || *art.FolderID != folder.ID {
		t.Fatalf("agent-chosen folder must be respected, got %v want %s", art.FolderID, folder.ID)
	}
}

func TestAgentRunsFolder_MemberSaveUnaffected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	// Plain member save (no agent headers) — must never be auto-filed.
	w := httptest.NewRecorder()
	testHandler.CreateArtifact(w, newRequest("POST", "/api/artifacts", map[string]any{
		"kind": "report", "title": "Member doc", "body": "b",
	}))
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifacts/"+a.ID, nil), "id", a.ID)
		testHandler.DeleteArtifact(httptest.NewRecorder(), req)
	})
	if a.FolderID != nil {
		t.Fatalf("member save must not be auto-filed, got folder_id=%v", *a.FolderID)
	}
}

func TestAgentRunsFolder_IdempotentAcrossSaves(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "Same run twice")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	first := agentSave(t, agentID, taskID, "report", "Doc A", "")
	second := agentSave(t, agentID, taskID, "plan", "Doc B", "")
	if first.FolderID == nil || second.FolderID == nil {
		t.Fatalf("both saves should be filed")
	}
	if *first.FolderID != *second.FolderID {
		t.Fatalf("two saves in the same run must share one run folder: %s vs %s", *first.FolderID, *second.FolderID)
	}
	// Exactly one Agent Runs root for the workspace (no duplicate roots).
	var roots int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM artifact_folder
		 WHERE workspace_id = $1 AND parent_id IS NULL AND name = 'Agent Runs' AND kind = 'document'`,
		testWorkspaceID).Scan(&roots)
	if roots != 1 {
		t.Fatalf("expected exactly one Agent Runs document root, got %d", roots)
	}
}

// TestAgentRunsFolder_ConcurrentFirstSavesShareOneRoot fires several saves for
// the same brand-new run in parallel goroutines (each its own request and
// transaction), reproducing the "two agents save in the same second on an
// agent's very first run" case. Without the per-(workspace,surface) advisory
// lock two of these would both miss the root lookup and create duplicate
// "Agent Runs" roots (the unique key does not dedupe NULL-parent rows). With
// the lock every save must land in one shared run folder under one root.
func TestAgentRunsFolder_ConcurrentFirstSavesShareOneRoot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "Concurrent first run")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	const n = 8
	var wg sync.WaitGroup
	folderIDs := make([]string, n)
	codes := make([]int, n)
	artIDs := make([]string, n)
	// Release all goroutines at once so the requests genuinely overlap.
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			req := newRequest("POST", "/api/artifacts", map[string]any{
				"kind": "report", "title": fmt.Sprintf("Doc %d", i), "body": "b",
			})
			req.Header.Set("X-Agent-ID", agentID)
			req.Header.Set("X-Task-ID", taskID)
			w := httptest.NewRecorder()
			testHandler.CreateArtifact(w, req)
			codes[i] = w.Code
			var a ArtifactResponse
			json.NewDecoder(w.Body).Decode(&a)
			artIDs[i] = a.ID
			if a.FolderID != nil {
				folderIDs[i] = *a.FolderID
			}
		}(i)
	}
	close(start)
	wg.Wait()

	t.Cleanup(func() {
		for _, id := range artIDs {
			if id == "" {
				continue
			}
			req := withURLParam(newRequest("DELETE", "/api/artifacts/"+id, nil), "id", id)
			testHandler.DeleteArtifact(httptest.NewRecorder(), req)
		}
	})

	for i := 0; i < n; i++ {
		if codes[i] != http.StatusCreated {
			t.Fatalf("concurrent save %d: expected 201, got %d", i, codes[i])
		}
		if folderIDs[i] == "" {
			t.Fatalf("concurrent save %d: not auto-filed", i)
		}
		if folderIDs[i] != folderIDs[0] {
			t.Fatalf("concurrent save %d filed into %s, want shared run folder %s", i, folderIDs[i], folderIDs[0])
		}
	}

	var roots int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM artifact_folder
		 WHERE workspace_id = $1 AND parent_id IS NULL AND name = 'Agent Runs' AND kind = 'document'`,
		testWorkspaceID).Scan(&roots)
	if roots != 1 {
		t.Fatalf("expected exactly one Agent Runs document root under concurrency, got %d", roots)
	}
}

func TestAgentRunsFolder_NoteSurfaceSeparateTree(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAgentRunsFolders(t)
	cleanupAgentRunsFolders(t)

	issueID, issueTitle := seedAgentRunsIssue(t, "Note run")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	art := agentSave(t, agentID, taskID, "note", "A note", "")
	if art.FolderID == nil {
		t.Fatalf("note save should be auto-filed")
	}
	name, kind, _ := folderRow(t, *art.FolderID)
	if kind != "note" {
		t.Fatalf("note run folder kind = %q, want note", kind)
	}
	if name != issueTitle {
		t.Fatalf("note run folder name = %q, want %q", name, issueTitle)
	}
}
