package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestValidateLocalDirectoryRef_Mode exercises the mode normalization directly
// so it runs without a database (the HTTP-layer test below covers the full
// handler path under CI with postgres).
func TestValidateLocalDirectoryRef_Mode(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		want    string
		wantErr bool
	}{
		{"empty normalizes to in_place", "", localDirectoryModeInPlace, false},
		{"in_place accepted", "in_place", localDirectoryModeInPlace, false},
		{"worktree accepted", "worktree", localDirectoryModeWorktree, false},
		{"whitespace trimmed", "  worktree  ", localDirectoryModeWorktree, false},
		{"unknown rejected", "turbo", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref, _ := json.Marshal(map[string]any{
				"local_path": "/Users/foo/work",
				"daemon_id":  "d1",
				"mode":       tc.mode,
			})
			out, err := validateLocalDirectoryRef(ref)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var parsed localDirectoryRef
			if err := json.Unmarshal(out, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if parsed.Mode != tc.want {
				t.Errorf("Mode=%q, want %q", parsed.Mode, tc.want)
			}
		})
	}
}

// createLocalDirResource creates a local_directory resource on project and
// returns the decoded response, failing the test on any non-201.
func createLocalDirResource(t *testing.T, projectID string, ref map[string]any) ProjectResourceResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref":  ref,
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestProjectResourceLocalDirectoryMode(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": "mode test"})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	defer func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()

	t.Run("empty mode normalizes to in_place and round-trips", func(t *testing.T) {
		resp := createLocalDirResource(t, project.ID, map[string]any{
			"local_path": t.TempDir(), "daemon_id": "d1",
		})
		var ref localDirectoryRef
		if err := json.Unmarshal(resp.ResourceRef, &ref); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ref.Mode != localDirectoryModeInPlace {
			t.Errorf("Mode=%q, want %q", ref.Mode, localDirectoryModeInPlace)
		}
	})

	t.Run("in_place and worktree are accepted", func(t *testing.T) {
		for _, mode := range []string{localDirectoryModeInPlace, localDirectoryModeWorktree} {
			resp := createLocalDirResource(t, project.ID, map[string]any{
				"local_path": t.TempDir(), "daemon_id": "d-" + mode, "mode": mode,
			})
			var ref localDirectoryRef
			if err := json.Unmarshal(resp.ResourceRef, &ref); err != nil {
				t.Fatalf("unmarshal %s: %v", mode, err)
			}
			if ref.Mode != mode {
				t.Errorf("mode=%s: stored Mode=%q", mode, ref.Mode)
			}
		}
	})

	t.Run("unknown mode is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
			"resource_type": "local_directory",
			"resource_ref":  map[string]any{"local_path": t.TempDir(), "daemon_id": "d-bad", "mode": "turbo"},
		})
		req = withURLParam(req, "id", project.ID)
		testHandler.CreateProjectResource(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for bogus mode, got %d: %s", w.Code, w.Body.String())
		}
	})
}
