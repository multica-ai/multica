package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func newSkillImportTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out), runErr
}

func TestRunSkillImportJsonTreatsDuplicateAsStructuredResult(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/skills/import" {
			t.Fatalf("path = %q, want /api/skills/import", r.URL.Path)
		}
		if r.Header.Get("X-Workspace-ID") != "workspace-123" {
			t.Fatalf("X-Workspace-ID = %q, want workspace-123", r.Header.Get("X-Workspace-ID"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "a skill with this name already exists",
			"existing_skill": map[string]any{
				"id":   "skill-123",
				"name": "review-helper",
			},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newSkillImportTestCmd()
	_ = cmd.Flags().Set("url", "https://skills.sh/acme/review-helper")
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error {
		return runSkillImport(cmd, nil)
	})
	if err != nil {
		t.Fatalf("runSkillImport returned error for duplicate import: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out, err)
	}
	if got["error"] != "a skill with this name already exists" {
		t.Fatalf("error = %v", got["error"])
	}
	existing, ok := got["existing_skill"].(map[string]any)
	if !ok {
		t.Fatalf("existing_skill missing or wrong type: %#v", got["existing_skill"])
	}
	if existing["id"] != "skill-123" || existing["name"] != "review-helper" {
		t.Fatalf("existing_skill = %#v", existing)
	}
}

func TestRunSkillSearchRequestsSearchEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		if r.URL.Path != "/api/skills/search" {
			t.Fatalf("expected /api/skills/search, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "react hooks" {
			t.Fatalf("expected q=react hooks, got %q", r.URL.Query().Get("q"))
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"name":          "React",
				"url":           "https://clawhub.ai/ivangdavila/react",
				"source":        "clawhub.ai",
				"repo":          nil,
				"install_count": 62,
				"github_stars":  nil,
				"description":   "React engineering skill",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := &cobra.Command{Use: "search"}
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	if err := runSkillSearch(cmd, []string{"react hooks"}); err != nil {
		t.Fatalf("runSkillSearch: %v", err)
	}
	if gotPath == "" {
		t.Fatal("expected search endpoint to be requested")
	}
}
