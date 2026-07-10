package handler

// CEREBRO-PATCH(attachment-folder): FIR-2697 part 4 — tests for "an
// agent-authored document attached to a chat/issue always has a folder". Reuses
// the shared handler test harness and the part-3 helpers (enableAgentRunsFolders
// / cleanupAgentRunsFolders / seedAgentRunsIssue / agentSave / folderRow /
// workspaceViewerGrantCount) and skips when the DB is unavailable.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// enableAttachmentFolder flips the part-4 workspace flag on for the test
// workspace (all-zero user_id = workspace-level row) and cleans it up after.
func enableAttachmentFolder(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		 VALUES ($1, '00000000-0000-0000-0000-000000000000', 'cerebro_attachment_folder', true)
		 ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE SET enabled = true`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("enable attachment-folder flag: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = 'cerebro_attachment_folder'`,
			testWorkspaceID,
		)
	})
}

// artifactFolderIDFromDB reads an artifact's folder_id ("" when NULL) straight
// from the row, so a test asserts the persisted truth rather than a stale
// response body.
func artifactFolderIDFromDB(t *testing.T, artifactID string) string {
	t.Helper()
	var folderID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT folder_id::text FROM artifact WHERE id = $1`, artifactID,
	).Scan(&folderID); err != nil {
		t.Fatalf("read artifact folder_id %s: %v", artifactID, err)
	}
	if folderID == nil {
		return ""
	}
	return *folderID
}

// createMemberArtifact posts a plain member artifact (no agent headers), which
// is never auto-filed, and returns it. Used to prove part 4 only touches
// agent-authored documents.
func createMemberArtifact(t *testing.T, title string) ArtifactResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateArtifact(w, newRequest("POST", "/api/artifacts", map[string]any{
		"kind": "report", "title": title, "body": "b",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("member CreateArtifact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifacts/"+a.ID, nil), "id", a.ID)
		testHandler.DeleteArtifact(httptest.NewRecorder(), req)
	})
	return a
}

// postAgentComment drives the full CreateComment handler as an agent-in-a-run
// (X-Agent-ID + X-Task-ID) so the FIR-2697 part-4 hook fires exactly as it does
// in production when an agent attaches a document with `--artifact`.
func postAgentComment(t *testing.T, issueID, agentID, taskID, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": body})
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// artifactToken is the exact body fragment the CLI appends for `--artifact`.
func artifactToken(id string) string {
	return "See [document](mention://artifact/" + id + ")"
}

func TestParseArtifactMentions(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"empty", "", nil},
		{"none", "just some text, no tokens", nil},
		{"one", "x [document](mention://artifact/019f2d4a-6062-7db3-9ef2-e41bb3a9b02f) y",
			[]string{"019f2d4a-6062-7db3-9ef2-e41bb3a9b02f"}},
		{"dedup same id twice",
			"a (mention://artifact/11111111-1111-1111-1111-111111111111) b " +
				"(mention://artifact/11111111-1111-1111-1111-111111111111)",
			[]string{"11111111-1111-1111-1111-111111111111"}},
		{"two distinct, first-seen order",
			"(mention://artifact/22222222-2222-2222-2222-222222222222) then " +
				"(mention://artifact/33333333-3333-3333-3333-333333333333)",
			[]string{"22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"}},
		{"ignores member mention", "[@Sofie](mention://agent/44444444-4444-4444-4444-444444444444)", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseArtifactMentions(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("parseArtifactMentions(%q) = %v, want %v", tc.body, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("parseArtifactMentions(%q)[%d] = %q, want %q", tc.body, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestAttachmentFolder_CommentFilesAgentDoc is the end-to-end proof: an agent
// document created WITHOUT a folder (part-3 auto-file off), once attached to an
// issue comment, is filed into the full Agent Runs > owner > agent > run tree,
// every level carrying its Collections workspace-viewer grant.
func TestAttachmentFolder_CommentFilesAgentDoc(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAttachmentFolder(t)
	cleanupAgentRunsFolders(t)

	issueID, issueTitle := seedAgentRunsIssue(t, "Attach fills the folder")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	// Part-3 auto-file is OFF, so the fresh agent document has no folder.
	art := agentSave(t, agentID, taskID, "report", "Launch notes", "")
	if art.FolderID != nil {
		t.Fatalf("precondition: document should start folder-less, got %v", *art.FolderID)
	}

	postAgentComment(t, issueID, agentID, taskID, artifactToken(art.ID))

	runID := artifactFolderIDFromDB(t, art.ID)
	if runID == "" {
		t.Fatalf("attached agent document must have a folder, got none")
	}

	// Walk leaf → root: run (issue title, document surface) → agent → member → root.
	runName, runKind, agentFolderID := folderRow(t, runID)
	if runName != issueTitle || runKind != "document" {
		t.Fatalf("run folder = (%q,%q), want (%q,document)", runName, runKind, issueTitle)
	}
	agentName, _, memberFolderID := folderRow(t, agentFolderID)
	if agentName != "Sara" {
		t.Fatalf("agent folder name = %q, want Sara", agentName)
	}
	memberName, _, rootID := folderRow(t, memberFolderID)
	if memberName != handlerTestName {
		t.Fatalf("member folder name = %q, want %q", memberName, handlerTestName)
	}
	rootName, _, rootParent := folderRow(t, rootID)
	if rootName != "Agent Runs" || rootParent != "" {
		t.Fatalf("root = (%q, parent %q), want (Agent Runs, no parent)", rootName, rootParent)
	}
	for _, id := range []string{runID, agentFolderID, memberFolderID, rootID} {
		if workspaceViewerGrantCount(t, id) != 1 {
			t.Fatalf("folder %s missing its Collections workspace viewer grant", id)
		}
	}
}

// TestAttachmentFolder_DisabledByDefault — flag off, attaching changes nothing.
func TestAttachmentFolder_DisabledByDefault(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// No enableAttachmentFolder call — flag is off.
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "No filing please")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	art := agentSave(t, agentID, taskID, "report", "Doc", "")
	postAgentComment(t, issueID, agentID, taskID, artifactToken(art.ID))

	if got := artifactFolderIDFromDB(t, art.ID); got != "" {
		t.Fatalf("flag off: document must stay folder-less, got folder_id=%q", got)
	}
	var roots int
	testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM artifact_folder WHERE workspace_id = $1 AND name = 'Agent Runs'`,
		testWorkspaceID).Scan(&roots)
	if roots != 0 {
		t.Fatalf("flag off: no Agent Runs folder should exist, found %d", roots)
	}
}

// TestAttachmentFolder_MemberDocSkipped — a member-authored document attached by
// an agent is NOT given a folder (part 4 only touches agent documents).
func TestAttachmentFolder_MemberDocSkipped(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAttachmentFolder(t)
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "Member doc stays put")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	memberDoc := createMemberArtifact(t, "Member wrote this")
	postAgentComment(t, issueID, agentID, taskID, artifactToken(memberDoc.ID))

	if got := artifactFolderIDFromDB(t, memberDoc.ID); got != "" {
		t.Fatalf("member document must not be filed, got folder_id=%q", got)
	}
}

// TestAttachmentFolder_AlreadyFolderedNotMoved — a document that already has a
// folder (the agent's own choice, or part 3) is never moved on attach.
func TestAttachmentFolder_AlreadyFolderedNotMoved(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAttachmentFolder(t)
	cleanupAgentRunsFolders(t)

	issueID, _ := seedAgentRunsIssue(t, "Keep chosen folder")
	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, issueID)

	chosen := createSuggestionFolder(t, "Chosen folder", "document")
	art := agentSave(t, agentID, taskID, "report", "Doc", chosen.ID)
	postAgentComment(t, issueID, agentID, taskID, artifactToken(art.ID))

	if got := artifactFolderIDFromDB(t, art.ID); got != chosen.ID {
		t.Fatalf("already-foldered document must not move: got %q, want %q", got, chosen.ID)
	}
}

// TestAttachmentFolder_ChatReplyFilesAgentDoc proves the second hook: attaching
// an agent document in an agent chat reply files it the same way the comment
// path does. No issue on the run, so the run leaf uses the "Run <id8>" fallback.
func TestAttachmentFolder_ChatReplyFilesAgentDoc(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableAttachmentFolder(t)
	cleanupAgentRunsFolders(t)

	agentID := createHandlerTestAgent(t, "Sara", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, "")
	sessionID := createHandlerTestChatSession(t, agentID)

	art := agentSave(t, agentID, taskID, "report", "Chat doc", "")
	if art.FolderID != nil {
		t.Fatalf("precondition: chat document should start folder-less")
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/chat/sessions/"+sessionID+"/agent-message",
		map[string]any{"content": artifactToken(art.ID)})
	r = withChatTestWorkspaceCtx(t, r)
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "sessionId", sessionID)
	testHandler.SendAgentChatMessage(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("SendAgentChatMessage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	runID := artifactFolderIDFromDB(t, art.ID)
	if runID == "" {
		t.Fatalf("document attached in a chat reply must have a folder, got none")
	}
	_, runKind, _ := folderRow(t, runID)
	if runKind != "document" {
		t.Fatalf("chat-attached run folder kind = %q, want document", runKind)
	}
}
