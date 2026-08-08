package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// FIR-4624: a folder's contents must be reachable regardless of how many newer
// documents the workspace has. Before the `folder` query param existed the
// Documents list had to fetch the newest N workspace-wide and narrow the result
// client-side, so every folder whose documents fell outside that window rendered
// as "This folder is empty".

func createScopeFolder(t *testing.T, name string) ArtifactFolderResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateArtifactFolder(w, newRequest("POST", "/api/artifact-folders", map[string]any{
		"name": name,
		"kind": "document",
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

func createScopeArtifact(t *testing.T, title, folderID string) ArtifactResponse {
	t.Helper()
	body := map[string]any{"kind": "note", "title": title, "body": "x"}
	if folderID != "" {
		body["folder_id"] = folderID
	}
	w := httptest.NewRecorder()
	testHandler.CreateArtifact(w, newRequest("POST", "/api/artifacts", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateArtifact %s: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)
	t.Cleanup(func() {
		req := withURLParam(newRequest("DELETE", "/api/artifacts/"+a.ID, nil), "id", a.ID)
		testHandler.DeleteArtifact(httptest.NewRecorder(), req)
	})
	return a
}

func searchArtifacts(t *testing.T, query string) []ArtifactResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.SearchArtifacts(w, newRequest("GET", "/api/artifacts"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("SearchArtifacts%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
	}
	var out []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&out)
	return out
}

// The regression test. The filed document is created FIRST, then buried under
// newer unfiled documents, so it sits outside a small newest-first window —
// exactly the shape that made real folders look empty. Asking for the folder
// must return it anyway.
func TestSearchArtifacts_FolderScope_SurvivesNewerDocuments(t *testing.T) {
	folder := createScopeFolder(t, "FIR-4624 scope folder")
	filed := createScopeArtifact(t, "FIR-4624 filed document", folder.ID)

	for _, title := range []string{
		"FIR-4624 newer a", "FIR-4624 newer b", "FIR-4624 newer c",
	} {
		createScopeArtifact(t, title, "")
	}

	// A window smaller than the number of newer documents cannot contain the
	// filed one — the folder scope has to be applied by the server, not after.
	got := searchArtifacts(t, "?folder="+folder.ID+"&limit=2")
	if len(got) != 1 {
		t.Fatalf("expected exactly the 1 document in the folder, got %d: %+v", len(got), got)
	}
	if got[0].ID != filed.ID {
		t.Fatalf("expected the filed document %s, got %s", filed.ID, got[0].ID)
	}
}

// "root" is the All documents view: unfiled documents only.
func TestSearchArtifacts_FolderScope_Root(t *testing.T) {
	folder := createScopeFolder(t, "FIR-4624 root-check folder")
	filed := createScopeArtifact(t, "FIR-4624 root-check filed", folder.ID)
	unfiled := createScopeArtifact(t, "FIR-4624 root-check unfiled", "")

	got := searchArtifacts(t, "?folder=root")

	var sawUnfiled bool
	for _, a := range got {
		if a.ID == filed.ID {
			t.Fatalf("folder=root must not return the filed document %s", filed.ID)
		}
		if a.ID == unfiled.ID {
			sawUnfiled = true
		}
		if a.FolderID != nil && *a.FolderID != "" {
			t.Fatalf("folder=root returned a filed document: %+v", a)
		}
	}
	if !sawUnfiled {
		t.Fatalf("folder=root did not return the unfiled document %s", unfiled.ID)
	}
}

// Omitting the param keeps the previous workspace-wide behaviour.
func TestSearchArtifacts_FolderScope_OmittedReturnsBoth(t *testing.T) {
	folder := createScopeFolder(t, "FIR-4624 unscoped folder")
	filed := createScopeArtifact(t, "FIR-4624 unscoped filed", folder.ID)
	unfiled := createScopeArtifact(t, "FIR-4624 unscoped unfiled", "")

	got := searchArtifacts(t, "?q="+url.QueryEscape("FIR-4624 unscoped"))

	var sawFiled, sawUnfiled bool
	for _, a := range got {
		if a.ID == filed.ID {
			sawFiled = true
		}
		if a.ID == unfiled.ID {
			sawUnfiled = true
		}
	}
	if !sawFiled || !sawUnfiled {
		t.Fatalf("unscoped search must span every folder; filed=%v unfiled=%v", sawFiled, sawUnfiled)
	}
}

func TestSearchArtifacts_FolderScope_RejectsGarbage(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.SearchArtifacts(w, newRequest("GET", "/api/artifacts?folder=not-a-uuid", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a non-uuid folder, got %d: %s", w.Code, w.Body.String())
	}
}
