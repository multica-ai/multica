package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testFolderID  = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testFolderID2 = "aaaa1111-bbbb-cccc-dddd-eeeeeeeeeeee"
	testProjectID = "11111111-2222-3333-4444-555555555555"
)

type projectFolderRequest struct {
	Method, Path string
	Body         map[string]any
}

// projectFolderServer serves the folder list and records every other request.
func projectFolderServer(t *testing.T, requests *[]projectFolderRequest) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/api/project-folders" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"folders": []any{
					map[string]any{"id": testFolderID, "name": "Launch"},
					map[string]any{"id": testFolderID2, "name": "Ops"},
				},
				"filed": []any{map[string]any{"project_id": testProjectID, "folder_id": testFolderID}},
			})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		*requests = append(*requests, projectFolderRequest{r.Method, r.URL.Path, body})
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/project-folders/file":
			_ = json.NewEncoder(w).Encode(map[string]any{"project_id": body["project_id"], "folder_id": body["folder_id"]})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": testFolderID, "name": body["name"]})
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
}

func TestProjectFolderCommandsFileUnderProject(t *testing.T) {
	names := map[string]bool{}
	for _, c := range projectFolderCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"list", "create", "rename", "delete", "file", "unfile"} {
		if !names[want] {
			t.Fatalf("missing project folder command %q", want)
		}
	}
}

func TestRunProjectFolderListCountsProjects(t *testing.T) {
	var requests []projectFolderRequest
	projectFolderServer(t, &requests)
	out, err := captureStdout(t, func() error { return runProjectFolderList(projectFolderListCmd, nil) })
	if err != nil {
		t.Fatalf("runProjectFolderList: %v", err)
	}
	if !strings.Contains(out, "Launch") || !strings.Contains(out, "Ops") {
		t.Fatalf("list output missing folders: %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Launch") && !strings.HasSuffix(strings.TrimSpace(line), "1") {
			t.Fatalf("Launch should hold 1 project: %q", line)
		}
	}
}

func TestRunProjectFolderRenameAcceptsNameOrPrefix(t *testing.T) {
	for _, ref := range []string{"launch", "aaaaaaaa", testFolderID} {
		var requests []projectFolderRequest
		projectFolderServer(t, &requests)
		if _, err := captureStdout(t, func() error { return runProjectFolderRename(projectFolderRenameCmd, []string{ref, "Launch 2"}) }); err != nil {
			t.Fatalf("rename %q: %v", ref, err)
		}
		if len(requests) != 1 || requests[0].Method != http.MethodPatch ||
			requests[0].Path != "/api/project-folders/"+testFolderID || requests[0].Body["name"] != "Launch 2" {
			t.Fatalf("rename %q sent %+v", ref, requests)
		}
	}
}

func TestResolveProjectFolderRefRejectsAmbiguousPrefix(t *testing.T) {
	var requests []projectFolderRequest
	projectFolderServer(t, &requests)
	client, err := newAPIClient(projectFolderListCmd)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProjectFolderRef(t.Context(), client, "aaaa"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous error, got %v", err)
	}
	if _, err := resolveProjectFolderRef(t.Context(), client, "Missing"); err == nil {
		t.Fatal("want error for an unknown folder")
	}
}

func TestRunProjectFolderFileAndUnfile(t *testing.T) {
	var requests []projectFolderRequest
	projectFolderServer(t, &requests)
	_ = projectFolderFileCmd.Flags().Set("folder", "Ops")
	t.Cleanup(func() { _ = projectFolderFileCmd.Flags().Set("folder", "") })
	if _, err := captureStdout(t, func() error { return runProjectFolderFile(projectFolderFileCmd, []string{testProjectID}) }); err != nil {
		t.Fatalf("file: %v", err)
	}
	if _, err := captureStdout(t, func() error { return runProjectFolderUnfile(projectFolderUnfileCmd, []string{testProjectID}) }); err != nil {
		t.Fatalf("unfile: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %+v", requests)
	}
	if requests[0].Path != "/api/project-folders/file" || requests[0].Body["project_id"] != testProjectID || requests[0].Body["folder_id"] != testFolderID2 {
		t.Fatalf("file sent %+v", requests[0])
	}
	if v, ok := requests[1].Body["folder_id"]; !ok || v != nil {
		t.Fatalf("unfile must send folder_id null, sent %+v", requests[1].Body)
	}
}

func TestRunProjectFolderFileRequiresFolder(t *testing.T) {
	_ = projectFolderFileCmd.Flags().Set("folder", "")
	if err := runProjectFolderFile(projectFolderFileCmd, []string{testProjectID}); err == nil || !strings.Contains(err.Error(), "--folder is required") {
		t.Fatalf("got %v", err)
	}
}

func TestRunProjectFolderDelete(t *testing.T) {
	var requests []projectFolderRequest
	projectFolderServer(t, &requests)
	if err := runProjectFolderDelete(projectFolderDeleteCmd, []string{"Ops"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(requests) != 1 || requests[0].Method != http.MethodDelete || requests[0].Path != "/api/project-folders/"+testFolderID2 {
		t.Fatalf("delete sent %+v", requests)
	}
}
