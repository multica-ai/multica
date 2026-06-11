package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func newSkillCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("content", "", "")
	cmd.Flags().Bool("content-stdin", false, "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newSkillUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("content", "", "")
	cmd.Flags().Bool("content-stdin", false, "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newSkillFilesUpsertTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "upsert"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("path", "", "")
	cmd.Flags().String("content", "", "")
	cmd.Flags().Bool("content-stdin", false, "")
	cmd.Flags().String("content-file", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newSkillBodyCaptureServer(t *testing.T, wantMethod, wantPath string, body *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != wantMethod {
			t.Fatalf("method = %s, want %s", r.Method, wantMethod)
		}
		if r.URL.Path != wantPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "skill-123",
			"name":        "skill-name",
			"path":        "docs/SKILL.md",
			"description": "desc",
			"content":     (*body)["content"],
		})
	}))
}

func setSkillServerEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunSkillCreateReadsContentFileVerbatim(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPost, "/api/skills", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	content := "标题 / Заголовок\n\nBody with `code`, \"quotes\", and a literal \\n.\n"
	path := t.TempDir() + string(os.PathSeparator) + "SKILL.md"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	cmd := newSkillCreateTestCmd()
	_ = cmd.Flags().Set("name", "skill-name")
	_ = cmd.Flags().Set("content-file", path)
	if _, err := captureStdout(t, func() error { return runSkillCreate(cmd, nil) }); err != nil {
		t.Fatalf("runSkillCreate: %v", err)
	}
	if body["content"] != content {
		t.Fatalf("content = %q, want verbatim %q", body["content"], content)
	}
}

func TestRunSkillCreateKeepsInlineContentLiteral(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPost, "/api/skills", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	content := `regex \d and path C:\\new and literal \n done`
	cmd := newSkillCreateTestCmd()
	_ = cmd.Flags().Set("name", "skill-name")
	_ = cmd.Flags().Set("content", content)
	if _, err := captureStdout(t, func() error { return runSkillCreate(cmd, nil) }); err != nil {
		t.Fatalf("runSkillCreate: %v", err)
	}
	if body["content"] != content {
		t.Fatalf("content = %q, want literal inline %q", body["content"], content)
	}
}

func TestRunSkillUpdateReadsContentStdinVerbatim(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPut, "/api/skills/skill-123", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	content := "first line\nsecond line with literal \\n\n"
	cmd := newSkillUpdateTestCmd()
	_ = cmd.Flags().Set("content-stdin", "true")
	pipeStdin(t, content, func() {
		if _, err := captureStdout(t, func() error { return runSkillUpdate(cmd, []string{"skill-123"}) }); err != nil {
			t.Fatalf("runSkillUpdate: %v", err)
		}
	})
	if body["content"] != content {
		t.Fatalf("content = %q, want verbatim %q", body["content"], content)
	}
}

func TestRunSkillFilesUpsertReadsContentFileVerbatim(t *testing.T) {
	var body map[string]any
	srv := newSkillBodyCaptureServer(t, http.MethodPut, "/api/skills/skill-123/files", &body)
	defer srv.Close()
	setSkillServerEnv(t, srv.URL)

	content := "file asset body\n\n"
	path := t.TempDir() + string(os.PathSeparator) + "asset.txt"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write content file: %v", err)
	}

	cmd := newSkillFilesUpsertTestCmd()
	_ = cmd.Flags().Set("path", "docs/SKILL.md")
	_ = cmd.Flags().Set("content-file", path)
	if _, err := captureStdout(t, func() error { return runSkillFilesUpsert(cmd, []string{"skill-123"}) }); err != nil {
		t.Fatalf("runSkillFilesUpsert: %v", err)
	}
	if body["path"] != "docs/SKILL.md" {
		t.Fatalf("path = %v", body["path"])
	}
	if body["content"] != content {
		t.Fatalf("content = %q, want verbatim %q", body["content"], content)
	}
}

func TestRunSkillContentInputsAreMutuallyExclusive(t *testing.T) {
	setSkillServerEnv(t, "http://127.0.0.1:1")

	path := t.TempDir() + string(os.PathSeparator) + "SKILL.md"
	if err := os.WriteFile(path, []byte("body"), 0o644); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}

	cases := []struct {
		name string
		set  func(*cobra.Command)
	}{
		{name: "inline + stdin", set: func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("content", "inline")
			_ = cmd.Flags().Set("content-stdin", "true")
		}},
		{name: "inline + file", set: func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("content", "inline")
			_ = cmd.Flags().Set("content-file", path)
		}},
		{name: "stdin + file", set: func(cmd *cobra.Command) {
			_ = cmd.Flags().Set("content-stdin", "true")
			_ = cmd.Flags().Set("content-file", path)
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newSkillCreateTestCmd()
			_ = cmd.Flags().Set("name", "skill-name")
			tt.set(cmd)
			err := runSkillCreate(cmd, nil)
			if err == nil {
				t.Fatalf("expected mutually-exclusive error")
			}
			if !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("error = %v, want mutually exclusive", err)
import "testing"

func TestIsSkillsShBatchImportURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"canonical two segment", "https://skills.sh/everyinc/compound-engineering-plugin", true},
		{"no scheme two segment", "skills.sh/everyinc/compound-engineering-plugin", true},
		{"http two segment", "http://skills.sh/owner/repo", true},
		{"trailing slash two segment", "https://skills.sh/owner/repo/", true},
		{"single skill three segment", "https://skills.sh/owner/repo/skill", false},
		{"no scheme single skill three segment", "skills.sh/owner/repo/skill", false},
		{"four segment path", "https://skills.sh/owner/repo/skill/extra", false},
		{"github two segment", "https://github.com/owner/repo", false},
		{"bare owner repo", "owner/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSkillsShBatchImportURL(tt.raw); got != tt.want {
				t.Fatalf("isSkillsShBatchImportURL(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestRunSkillContentFileAndStdinRejectEmptyInput(t *testing.T) {
	setSkillServerEnv(t, "http://127.0.0.1:1")

	emptyPath := t.TempDir() + string(os.PathSeparator) + "empty.md"
	if err := os.WriteFile(emptyPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}

	cmd := newSkillCreateTestCmd()
	_ = cmd.Flags().Set("name", "skill-name")
	_ = cmd.Flags().Set("content-file", emptyPath)
	if err := runSkillCreate(cmd, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty content-file error = %v", err)
	}

	cmd = newSkillCreateTestCmd()
	_ = cmd.Flags().Set("name", "skill-name")
	_ = cmd.Flags().Set("content-stdin", "true")
	pipeStdin(t, "", func() {
		if err := runSkillCreate(cmd, nil); err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("empty content-stdin error = %v", err)
		}
	})
}

func TestRunSkillInlineEmptyContentKeepsExistingBehavior(t *testing.T) {
	var createBody map[string]any
	createSrv := newSkillBodyCaptureServer(t, http.MethodPost, "/api/skills", &createBody)
	defer createSrv.Close()
	setSkillServerEnv(t, createSrv.URL)

	createCmd := newSkillCreateTestCmd()
	_ = createCmd.Flags().Set("name", "skill-name")
	_ = createCmd.Flags().Set("content", "")
	if _, err := captureStdout(t, func() error { return runSkillCreate(createCmd, nil) }); err != nil {
		t.Fatalf("runSkillCreate: %v", err)
	}
	if _, ok := createBody["content"]; ok {
		t.Fatalf("create body unexpectedly included empty content: %#v", createBody)
	}

	var updateBody map[string]any
	updateSrv := newSkillBodyCaptureServer(t, http.MethodPut, "/api/skills/skill-123", &updateBody)
	defer updateSrv.Close()
	setSkillServerEnv(t, updateSrv.URL)

	updateCmd := newSkillUpdateTestCmd()
	_ = updateCmd.Flags().Set("content", "")
	if _, err := captureStdout(t, func() error { return runSkillUpdate(updateCmd, []string{"skill-123"}) }); err != nil {
		t.Fatalf("runSkillUpdate: %v", err)
	}
	if updateBody["content"] != "" {
		t.Fatalf("update content = %q, want empty string", updateBody["content"])
	}

	upsertCmd := newSkillFilesUpsertTestCmd()
	_ = upsertCmd.Flags().Set("path", "docs/SKILL.md")
	_ = upsertCmd.Flags().Set("content", "")
	if err := runSkillFilesUpsert(upsertCmd, []string{"skill-123"}); err == nil || !strings.Contains(err.Error(), "--content is required") {
		t.Fatalf("upsert inline empty error = %v", err)
func TestIntValDefaultsMissingOrWrongShape(t *testing.T) {
	if got := intVal(map[string]any{"imported": float64(3)}, "imported"); got != 3 {
		t.Fatalf("intVal float64 = %d, want 3", got)
	}
	if got := intVal(map[string]any{}, "imported"); got != 0 {
		t.Fatalf("intVal missing = %d, want 0", got)
	}
	if got := intVal(map[string]any{"imported": "3"}, "imported"); got != 0 {
		t.Fatalf("intVal wrong type = %d, want 0", got)
	}
}
