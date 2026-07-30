package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectspace"
)

func TestProjectSpaceFolderImportLifecycle(t *testing.T) {
	svc, err := projectspace.NewForTest(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := testHandler.ProjectSpace
	testHandler.ProjectSpace = svc
	t.Cleanup(func() { testHandler.ProjectSpace = previous })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Project space import",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/projects/"+project.ID, nil), "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	content := []byte("project-space-evidence")
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/space/imports", map[string]any{
		"batch_name": "research",
		"files": []map[string]any{{
			"relative_path": "nested/evidence.txt",
			"content_type":  "text/plain",
			"size_bytes":    len(content),
		}},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectSpaceImport(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectSpaceImport: %d %s", w.Code, w.Body.String())
	}
	var created projectSpaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if len(created.Files) != 1 {
		t.Fatalf("created import files = %d, want 1", len(created.Files))
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/space/imports/"+created.ID+"/files/"+created.Files[0].ID, nil)
	req.Body = io.NopCloser(bytes.NewReader(content))
	req.ContentLength = int64(len(content))
	req = withURLParams(req, "id", project.ID, "importId", created.ID, "fileId", created.Files[0].ID)
	testHandler.UploadProjectSpaceImportFile(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UploadProjectSpaceImportFile: %d %s", w.Code, w.Body.String())
	}
	var uploaded projectSpaceImportFileResponse
	if err := json.NewDecoder(w.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("detected content type = %q", uploaded.ContentType)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/space/imports/"+created.ID+"/complete", nil)
	req = withURLParams(req, "id", project.ID, "importId", created.ID)
	testHandler.CompleteProjectSpaceImport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteProjectSpaceImport: %d %s", w.Code, w.Body.String())
	}
	var completed projectSpaceImportResponse
	if err := json.NewDecoder(w.Body).Decode(&completed); err != nil {
		t.Fatal(err)
	}
	if completed.Status != "completed" || completed.CompletedFiles != 1 {
		t.Fatalf("completed import = %+v", completed)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/space/files?path=inbox/uploads", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectSpaceFiles(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectSpaceFiles: %d %s", w.Code, w.Body.String())
	}
}

func TestProjectSpaceImportRejectsUnsafePaths(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Unsafe project space import",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/projects/"+project.ID, nil), "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/space/imports", map[string]any{
		"batch_name": "unsafe",
		"files": []map[string]any{{
			"relative_path": "../secret.txt",
			"content_type":  "text/plain",
			"size_bytes":    1,
		}},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectSpaceImport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsafe path: got %d %s", w.Code, w.Body.String())
	}
}
