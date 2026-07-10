package handler

// CEREBRO-PATCH(folder-suggestion): FIR-2697 part 2 — integration tests for the
// folder-suggestion lifecycle (propose -> accept moves / reject leaves in
// place). Uses the shared handler test harness (testHandler / testPool /
// testWorkspaceID / testUserID). Skips when the DB is unavailable, like its
// siblings.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// enableFolderSuggestions flips the workspace feature flag on for the test
// workspace (all-zero user_id = workspace-level row) and cleans it up after.
func enableFolderSuggestions(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO cerebro_feature_flags (workspace_id, user_id, flag_key, enabled)
		 VALUES ($1, '00000000-0000-0000-0000-000000000000', 'cerebro_folder_suggestions', true)
		 ON CONFLICT (workspace_id, user_id, flag_key) DO UPDATE SET enabled = true`,
		testWorkspaceID,
	); err != nil {
		t.Fatalf("enable folder suggestions flag: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM cerebro_feature_flags WHERE workspace_id = $1 AND flag_key = 'cerebro_folder_suggestions'`,
			testWorkspaceID,
		)
	})
}

func createSuggestionArtifact(t *testing.T, kind, title string) ArtifactResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateArtifact(w, newRequest("POST", "/api/artifacts", map[string]any{
		"kind":  kind,
		"title": title,
		"body":  "body",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateArtifact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifacts/"+a.ID, nil), "id", a.ID)
		testHandler.DeleteArtifact(httptest.NewRecorder(), req)
	})
	return a
}

func createSuggestionFolder(t *testing.T, name, kind string) ArtifactFolderResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateArtifactFolder(w, newRequest("POST", "/api/artifact-folders", map[string]any{
		"name": name,
		"kind": kind,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateArtifactFolder: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var f ArtifactFolderResponse
	json.NewDecoder(w.Body).Decode(&f)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifact-folders/"+f.ID, nil), "id", f.ID)
		testHandler.DeleteArtifactFolder(httptest.NewRecorder(), req)
	})
	return f
}

// propose posts a folder suggestion for an artifact and returns the recorder.
func propose(t *testing.T, artifactID, folderID, reason string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/artifacts/"+artifactID+"/folder-suggestion", map[string]any{
		"folder_id": folderID,
		"reason":    reason,
	}), "id", artifactID)
	testHandler.CreateArtifactFolderSuggestion(w, req)
	return w
}

func getArtifact(t *testing.T, id string) ArtifactResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.GetArtifact(w, withURLParam(newRequest("GET", "/api/artifacts/"+id, nil), "id", id))
	if w.Code != http.StatusOK {
		t.Fatalf("GetArtifact: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	return a
}

func TestFolderSuggestion_ProposeAcceptMovesArtifact(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	art := createSuggestionArtifact(t, "report", "Doc to file")
	folder := createSuggestionFolder(t, "Target folder", "document")

	// Propose — must not move the artifact yet.
	w := propose(t, art.ID, folder.ID, "belongs here")
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sug ArtifactFolderSuggestionResponse
	json.NewDecoder(w.Body).Decode(&sug)
	if sug.Status != "pending" {
		t.Fatalf("expected pending, got %q", sug.Status)
	}
	if got := getArtifact(t, art.ID); got.FolderID != nil {
		t.Fatalf("artifact must NOT be moved on propose; folder_id=%v", *got.FolderID)
	}

	// The pending proposal is readable via the artifact endpoint.
	gw := httptest.NewRecorder()
	testHandler.GetArtifactFolderSuggestion(gw, withURLParam(newRequest("GET", "/api/artifacts/"+art.ID+"/folder-suggestion", nil), "id", art.ID))
	if gw.Code != http.StatusOK {
		t.Fatalf("get suggestion: expected 200, got %d", gw.Code)
	}
	var got struct {
		Suggestion *ArtifactFolderSuggestionResponse `json:"suggestion"`
	}
	json.NewDecoder(gw.Body).Decode(&got)
	if got.Suggestion == nil || got.Suggestion.FolderName != "Target folder" {
		t.Fatalf("expected pending suggestion with folder name, got %+v", got.Suggestion)
	}

	// Accept — now the artifact moves.
	aw := httptest.NewRecorder()
	testHandler.AcceptArtifactFolderSuggestion(aw, withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/accept", nil), "id", sug.ID))
	if aw.Code != http.StatusOK {
		t.Fatalf("accept: expected 200, got %d: %s", aw.Code, aw.Body.String())
	}
	moved := getArtifact(t, art.ID)
	if moved.FolderID == nil || *moved.FolderID != folder.ID {
		t.Fatalf("artifact should be in folder %s after accept, got %v", folder.ID, moved.FolderID)
	}

	// A second accept is a conflict — the proposal is already resolved.
	aw2 := httptest.NewRecorder()
	testHandler.AcceptArtifactFolderSuggestion(aw2, withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/accept", nil), "id", sug.ID))
	if aw2.Code != http.StatusConflict {
		t.Fatalf("double accept: expected 409, got %d: %s", aw2.Code, aw2.Body.String())
	}
}

func TestFolderSuggestion_RejectLeavesArtifactInPlace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	art := createSuggestionArtifact(t, "report", "Doc to keep")
	folder := createSuggestionFolder(t, "Rejected target", "document")

	w := propose(t, art.ID, folder.ID, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sug ArtifactFolderSuggestionResponse
	json.NewDecoder(w.Body).Decode(&sug)

	rw := httptest.NewRecorder()
	testHandler.RejectArtifactFolderSuggestion(rw, withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/reject", nil), "id", sug.ID))
	if rw.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if got := getArtifact(t, art.ID); got.FolderID != nil {
		t.Fatalf("artifact must stay at root after reject, got folder_id=%v", *got.FolderID)
	}
}

// A resolve that lost the race (here: the proposal was rejected first) must not
// move the document — the accept sees a non-pending row, returns 409, and the
// artifact stays at root. This guards the atomic claim-then-move fix (FIR-2697
// part 2 review): move + status flip commit together or not at all, so a losing
// accept can never leave a moved document logged as "rejected".
func TestFolderSuggestion_LosingResolveDoesNotMove(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	art := createSuggestionArtifact(t, "report", "Doc")
	folder := createSuggestionFolder(t, "Contested folder", "document")

	w := propose(t, art.ID, folder.ID, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sug ArtifactFolderSuggestionResponse
	json.NewDecoder(w.Body).Decode(&sug)

	// Reject first — this terminal-states the row.
	rw := httptest.NewRecorder()
	testHandler.RejectArtifactFolderSuggestion(rw, withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/reject", nil), "id", sug.ID))
	if rw.Code != http.StatusOK {
		t.Fatalf("reject: expected 200, got %d: %s", rw.Code, rw.Body.String())
	}

	// A now-late accept on the same proposal must 409 and must NOT move the doc.
	aw := httptest.NewRecorder()
	testHandler.AcceptArtifactFolderSuggestion(aw, withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/accept", nil), "id", sug.ID))
	if aw.Code != http.StatusConflict {
		t.Fatalf("late accept: expected 409, got %d: %s", aw.Code, aw.Body.String())
	}
	if got := getArtifact(t, art.ID); got.FolderID != nil {
		t.Fatalf("a losing accept must not move the artifact, got folder_id=%v", *got.FolderID)
	}
}

func TestFolderSuggestion_CrossSurfaceRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	// A document proposed into a NOTE folder must be rejected — separate trees.
	art := createSuggestionArtifact(t, "report", "Doc")
	noteFolder := createSuggestionFolder(t, "A note folder", "note")

	w := propose(t, art.ID, noteFolder.ID, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-surface propose: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFolderSuggestion_SupersedesPrevious(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	art := createSuggestionArtifact(t, "report", "Doc")
	folderA := createSuggestionFolder(t, "Folder A", "document")
	folderB := createSuggestionFolder(t, "Folder B", "document")

	if w := propose(t, art.ID, folderA.ID, ""); w.Code != http.StatusCreated {
		t.Fatalf("first propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := propose(t, art.ID, folderB.ID, ""); w.Code != http.StatusCreated {
		t.Fatalf("second propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Only one pending proposal survives, and it points at folder B.
	var pending int
	var pendingFolder string
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*), coalesce(max(folder_id::text), '') FROM cerebro_artifact_folder_suggestion
		 WHERE artifact_id = $1 AND status = 'pending'`,
		art.ID,
	).Scan(&pending, &pendingFolder); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 1 {
		t.Fatalf("expected exactly 1 pending proposal, got %d", pending)
	}
	if pendingFolder != folderB.ID {
		t.Fatalf("pending proposal should point at folder B (%s), got %s", folderB.ID, pendingFolder)
	}
}

func TestFolderSuggestion_AgentCannotAccept(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableFolderSuggestions(t)

	art := createSuggestionArtifact(t, "report", "Doc")
	folder := createSuggestionFolder(t, "Human-only folder", "document")

	w := propose(t, art.ID, folder.ID, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sug ArtifactFolderSuggestionResponse
	json.NewDecoder(w.Body).Decode(&sug)

	// An agent actor (valid X-Agent-ID + X-Task-ID pair) must be refused.
	agentID := createHandlerTestAgent(t, "suggestion-bot", []byte("[]"))
	taskID := createHandlerTestTaskForAgentOnIssue(t, agentID, "")
	aw := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/artifact-folder-suggestions/"+sug.ID+"/accept", nil), "id", sug.ID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.AcceptArtifactFolderSuggestion(aw, req)
	if aw.Code != http.StatusForbidden {
		t.Fatalf("agent accept: expected 403, got %d: %s", aw.Code, aw.Body.String())
	}
	// And the artifact stayed put.
	if got := getArtifact(t, art.ID); got.FolderID != nil {
		t.Fatalf("artifact must not move on a refused accept, got folder_id=%v", *got.FolderID)
	}
}

func TestFolderSuggestion_DisabledFlagHidesFeature(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	// Flag intentionally NOT enabled.
	art := createSuggestionArtifact(t, "report", "Doc")
	folder := createSuggestionFolder(t, "Folder", "document")

	w := propose(t, art.ID, folder.ID, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("propose with flag off: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}
