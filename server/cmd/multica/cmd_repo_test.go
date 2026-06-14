package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newRepoRegistryTestCmd(serverURL string) *cobra.Command {
	cmd := &cobra.Command{Use: "repo-test"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().StringArray("url", nil, "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("output", "json", "")
	_ = cmd.Flags().Set("server-url", serverURL)
	_ = cmd.Flags().Set("workspace-id", "ws-1")
	return cmd
}

func TestRunRepoAddAppendsAndDedupes(t *testing.T) {
	initialRepos := []workspaceRepo{{URL: "https://git.example.com/web.git"}}
	var patched []workspaceRepo
	patchCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("url", "https://git.example.com/web.git"); err != nil {
		t.Fatal(err)
	}
	err := runRepoAdd(cmd, []string{
		"https://git.example.com/api.git",
		"https://git.example.com/api.git",
	})
	if err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if patchCount != 1 {
		t.Fatalf("patchCount = %d, want 1", patchCount)
	}
	if len(patched) != 2 {
		t.Fatalf("patched repos = %+v, want 2 entries", patched)
	}
	if patched[0].URL != "https://git.example.com/web.git" || patched[1].URL != "https://git.example.com/api.git" {
		t.Fatalf("unexpected patched repos: %+v", patched)
	}
}

func TestRunRepoAddUpdatesDescriptionForExistingRepo(t *testing.T) {
	initialRepos := []workspaceRepo{{URL: "https://git.example.com/web.git", Description: "old"}}
	var patched []workspaceRepo

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("description", "new"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoAdd(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoAdd: %v", err)
	}
	if len(patched) != 1 || patched[0].Description != "new" {
		t.Fatalf("patched repos = %+v, want updated description", patched)
	}
}

func TestRunRepoAddRejectsDescriptionForMultipleRepos(t *testing.T) {
	cmd := newRepoRegistryTestCmd("http://127.0.0.1:0")
	if err := cmd.Flags().Set("description", "shared"); err != nil {
		t.Fatal(err)
	}
	err := runRepoAdd(cmd, []string{"https://git.example.com/a.git", "https://git.example.com/b.git"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--description") {
		t.Fatalf("error = %q, want description guidance", err)
	}
}

func TestRunRepoRemoveDeletesExistingRepos(t *testing.T) {
	initialRepos := []workspaceRepo{
		{URL: "https://git.example.com/web.git"},
		{URL: "https://git.example.com/api.git"},
		{URL: "https://git.example.com/mobile.git"},
	}
	var patched []workspaceRepo

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: initialRepos})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			var body struct {
				Repos []workspaceRepo `json:"repos"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch body: %v", err)
			}
			patched = body.Repos
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1", Repos: body.Repos})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	if err := cmd.Flags().Set("url", "https://git.example.com/mobile.git"); err != nil {
		t.Fatal(err)
	}
	if err := runRepoRemove(cmd, []string{"https://git.example.com/web.git"}); err != nil {
		t.Fatalf("runRepoRemove: %v", err)
	}
	if len(patched) != 1 || patched[0].URL != "https://git.example.com/api.git" {
		t.Fatalf("patched repos = %+v, want only api repo", patched)
	}
}

func TestRunRepoRemoveRejectsMissingRepoWithoutPatch(t *testing.T) {
	patchCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces/ws-1":
			json.NewEncoder(w).Encode(repoWorkspaceResponse{
				ID:    "ws-1",
				Repos: []workspaceRepo{{URL: "https://git.example.com/web.git"}},
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/workspaces/ws-1":
			patchCount++
			json.NewEncoder(w).Encode(repoWorkspaceResponse{ID: "ws-1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := newRepoRegistryTestCmd(srv.URL)
	err := runRepoRemove(cmd, []string{"https://git.example.com/missing.git"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want not found", err)
	}
	if patchCount != 0 {
		t.Fatalf("patchCount = %d, want 0", patchCount)
	}
}
