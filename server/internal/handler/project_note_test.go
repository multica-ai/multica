package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createNoteTestProject spins up a project and returns it plus a cleanup that
// deletes it. Every note test needs one and the boilerplate is identical.
func createNoteTestProject(t *testing.T, title string) (ProjectResponse, func()) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	return project, func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	}
}

func createNote(t *testing.T, projectID string, body map[string]any) ProjectNoteResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/notes", body)
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectNote(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectNote: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var note ProjectNoteResponse
	if err := json.NewDecoder(w.Body).Decode(&note); err != nil {
		t.Fatalf("decode CreateProjectNote: %v", err)
	}
	return note
}

func TestProjectNoteLifecycle(t *testing.T) {
	project, cleanup := createNoteTestProject(t, "Note lifecycle project")
	defer cleanup()

	// A note created without a body starts empty — the "blank notepad" case.
	created := createNote(t, project.ID, map[string]any{"title": "Release journal"})
	if created.Title != "Release journal" {
		t.Errorf("created.Title = %q, want Release journal", created.Title)
	}
	if created.BodyMd != "" {
		t.Errorf("created.BodyMd = %q, want empty", created.BodyMd)
	}
	if created.Position != 0 {
		t.Errorf("created.Position = %d, want 0 for the first note", created.Position)
	}

	// The list view carries titles and sizes but never bodies.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+project.ID+"/notes", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectNotes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectNotes: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "body_md") {
		t.Errorf("list response leaked body_md: %s", w.Body.String())
	}

	// Detail returns the body.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/notes/"+created.ID, nil)
	req = withURLParams(req, "id", project.ID, "noteId", created.ID)
	testHandler.GetProjectNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProjectNote: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+project.ID+"/notes/"+created.ID, nil)
	req = withURLParams(req, "id", project.ID, "noteId", created.ID)
	testHandler.DeleteProjectNote(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProjectNote: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestProjectNoteAppendPreservesBody is the core guarantee behind `note append`:
// appending must never lose what was already stored.
func TestProjectNoteAppendPreservesBody(t *testing.T) {
	project, cleanup := createNoteTestProject(t, "Note append project")
	defer cleanup()

	note := createNote(t, project.ID, map[string]any{
		"title":   "Journal",
		"body_md": "## Day 1",
	})

	appendTo := func(chunk string) ProjectNoteResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+project.ID+"/notes/"+note.ID+"/append", map[string]any{
			"body_md": chunk,
		})
		req = withURLParams(req, "id", project.ID, "noteId", note.ID)
		testHandler.AppendProjectNote(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("AppendProjectNote: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var out ProjectNoteResponse
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode AppendProjectNote: %v", err)
		}
		return out
	}

	after := appendTo("## Day 2")
	if !strings.Contains(after.BodyMd, "## Day 1") {
		t.Errorf("append dropped the original body: %q", after.BodyMd)
	}
	if !strings.Contains(after.BodyMd, "## Day 2") {
		t.Errorf("append did not add the new chunk: %q", after.BodyMd)
	}
	if after.BodyMd != "## Day 1\n\n## Day 2" {
		t.Errorf("append separator wrong: %q", after.BodyMd)
	}

	// A second append keeps stacking rather than replacing.
	after = appendTo("## Day 3")
	if after.BodyMd != "## Day 1\n\n## Day 2\n\n## Day 3" {
		t.Errorf("second append body = %q", after.BodyMd)
	}

	// Whitespace-only appends are rejected so a pad cannot fill with blank lines.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/notes/"+note.ID+"/append", map[string]any{
		"body_md": "   \n  ",
	})
	req = withURLParams(req, "id", project.ID, "noteId", note.ID)
	testHandler.AppendProjectNote(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("whitespace-only append: expected 400, got %d", w.Code)
	}
}

// TestProjectNoteAppendToEmptyBody guards the CASE branch in the SQL: appending
// to a fresh note must not leave a leading blank line.
func TestProjectNoteAppendToEmptyBody(t *testing.T) {
	project, cleanup := createNoteTestProject(t, "Note append-empty project")
	defer cleanup()

	note := createNote(t, project.ID, map[string]any{"title": "Fresh"})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+project.ID+"/notes/"+note.ID+"/append", map[string]any{
		"body_md": "first line",
	})
	req = withURLParams(req, "id", project.ID, "noteId", note.ID)
	testHandler.AppendProjectNote(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AppendProjectNote: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out ProjectNoteResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.BodyMd != "first line" {
		t.Errorf("append to empty note = %q, want %q", out.BodyMd, "first line")
	}
}

func TestProjectNoteTitleValidation(t *testing.T) {
	project, cleanup := createNoteTestProject(t, "Note title validation project")
	defer cleanup()

	cases := []struct {
		name  string
		title string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"over 200 chars", strings.Repeat("x", 201)},
		// 201 CJK runes is 603 bytes. A byte-based check would also reject this,
		// so this case only proves the limit is not *looser* than documented.
		{"over 200 CJK runes", strings.Repeat("笔", 201)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/projects/"+project.ID+"/notes", map[string]any{
				"title": tc.title,
			})
			req = withURLParam(req, "id", project.ID)
			testHandler.CreateProjectNote(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestProjectNoteTitleAcceptsCJK is the regression test for the rune-vs-byte
// bug: with a byte-based limit, a 100-character Chinese title is 300 bytes and
// was rejected even though the error text promises 200 characters. Anything at
// or under 200 runes must be accepted regardless of script.
func TestProjectNoteTitleAcceptsCJK(t *testing.T) {
	project, cleanup := createNoteTestProject(t, "Note CJK title project")
	defer cleanup()

	for _, tc := range []struct {
		name  string
		title string
	}{
		{"100 CJK runes", strings.Repeat("笔", 100)},
		{"exactly 200 CJK runes", strings.Repeat("记", 200)},
		{"emoji beyond the BMP", strings.Repeat("🎉", 100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			note := createNote(t, project.ID, map[string]any{"title": tc.title})
			if note.Title != tc.title {
				t.Errorf("title round-trip changed the value")
			}
		})
	}
}

// TestProjectNoteCrossProjectAccessDenied covers the second ownership check in
// loadNoteForProject: a note id that is real, and in the same workspace, must
// still 404 when requested under a different project.
func TestProjectNoteCrossProjectAccessDenied(t *testing.T) {
	projectA, cleanupA := createNoteTestProject(t, "Note owner project")
	defer cleanupA()
	projectB, cleanupB := createNoteTestProject(t, "Note intruder project")
	defer cleanupB()

	note := createNote(t, projectA.ID, map[string]any{
		"title":   "Private",
		"body_md": "secret",
	})

	// Read it via project B.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+projectB.ID+"/notes/"+note.ID, nil)
	req = withURLParams(req, "id", projectB.ID, "noteId", note.ID)
	testHandler.GetProjectNote(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-project GET: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Writing through the wrong project must fail the same way.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+projectB.ID+"/notes/"+note.ID+"/append", map[string]any{
		"body_md": "injected",
	})
	req = withURLParams(req, "id", projectB.ID, "noteId", note.ID)
	testHandler.AppendProjectNote(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("cross-project append: expected 404, got %d", w.Code)
	}
}

// TestDeleteProjectRemovesNotes verifies the application-level cascade. There is
// no FK on project_note.project_id, so nothing but the explicit sweep in
// DeleteProject keeps notes from outliving their project.
func TestDeleteProjectRemovesNotes(t *testing.T) {
	project, _ := createNoteTestProject(t, "Note cascade project")

	note := createNote(t, project.ID, map[string]any{
		"title":   "Doomed",
		"body_md": "goes away with the project",
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProject: unexpected status %d: %s", w.Code, w.Body.String())
	}

	// The row must be gone, not merely unreachable through the project route.
	noteUUID, err := parseUUIDLoose(note.ID)
	if err != nil {
		t.Fatalf("parse note id: %v", err)
	}
	if _, err := testHandler.Queries.GetProjectNote(req.Context(), noteUUID); err == nil {
		t.Error("note survived project deletion; the cascade in DeleteProject is not running")
	}
}

// TestDeleteWorkspaceRemovesNotes is the workspace-level counterpart to
// TestDeleteProjectRemovesNotes. project_note has no FK to workspace, so
// nothing prunes it implicitly — the sweep has to live inside DeleteWorkspace's
// multi-CTE statement. Without it, user-authored markdown would be stranded in
// the database permanently, with no reaper and no way to reach it.
func TestDeleteWorkspaceRemovesNotes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	const slug = "handler-tests-delete-project-notes"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, '')
		RETURNING id
	`, "Handler Test Note Workspace Cascade", slug).Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title, status)
		VALUES ($1, 'Note Workspace Cascade Project', 'planned')
		RETURNING id
	`, wsID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}

	var noteID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project_note (project_id, workspace_id, title, body_md)
		VALUES ($1, $2, 'Stranded journal', 'text that must not outlive the workspace')
		RETURNING id
	`, projectID, wsID).Scan(&noteID); err != nil {
		t.Fatalf("create note: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/workspaces/"+wsID, nil), "id", wsID)
	testHandler.DeleteWorkspace(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var remaining int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM project_note WHERE workspace_id = $1`, wsID,
	).Scan(&remaining); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if remaining != 0 {
		t.Errorf("notes survived workspace deletion (count = %d); the sweep in DeleteWorkspace is not running", remaining)
	}
}
