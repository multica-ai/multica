package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newSkillImportTestCmd(output *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{Use: "import"}
	cmd.Flags().String("url", "", "")
	cmd.Flags().String("gitee-token", "", "")
	cmd.Flags().Bool("overwrite", false, "")
	cmd.Flags().String("on-conflict", "fail", "")
	cmd.Flags().String("output", "json", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("config", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.SetOut(output)
	return cmd
}

func TestRunSkillImportSendsOnConflictGiteeTokenAndPrintsStructuredResult(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/import" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "updated",
			"skill": map[string]any{
				"id":   "skill-123",
				"name": "review-helper",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	cmd := newSkillImportTestCmd(&out)
	_ = cmd.Flags().Set("url", "https://gitee.com/acme/review-helper")
	_ = cmd.Flags().Set("gitee-token", "token-123")
	_ = cmd.Flags().Set("on-conflict", "overwrite")
	_ = cmd.Flags().Set("output", "json")

	if err := runSkillImport(cmd, nil); err != nil {
		t.Fatalf("runSkillImport returned error: %v", err)
	}
	if request["url"] != "https://gitee.com/acme/review-helper" {
		t.Fatalf("url = %v", request["url"])
	}
	if request["gitee_token"] != "token-123" {
		t.Fatalf("gitee_token = %v", request["gitee_token"])
	}
	if request["on_conflict"] != "overwrite" {
		t.Fatalf("on_conflict = %v, want overwrite", request["on_conflict"])
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode stdout JSON %q: %v", out.String(), err)
	}
	if got["status"] != "updated" {
		t.Fatalf("status = %v", got["status"])
	}
}

func TestRunSkillImportOverwriteFlagMapsToOnConflictOverwrite(t *testing.T) {
	var request map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "updated",
			"skill":  map[string]any{"id": "skill-1", "name": "Review Helper"},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	cmd := newSkillImportTestCmd(&out)
	_ = cmd.Flags().Set("url", "https://gitee.com/acme/review-helper")
	_ = cmd.Flags().Set("overwrite", "true")

	if err := runSkillImport(cmd, nil); err != nil {
		t.Fatalf("runSkillImport: %v", err)
	}
	if request["on_conflict"] != "overwrite" {
		t.Fatalf("on_conflict = %#v, want overwrite", request["on_conflict"])
	}
}

func TestRunSkillImportJsonTreatsDuplicateAsConflictResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/import" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["on_conflict"] != "fail" {
			t.Fatalf("on_conflict = %v, want fail", body["on_conflict"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "conflict",
			"reason": "a skill with this name already exists; use --on-conflict overwrite to replace it or --on-conflict rename to import a copy",
			"existing_skill": map[string]any{
				"id":   "skill-123",
				"name": "review-helper",
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	cmd := newSkillImportTestCmd(&out)
	_ = cmd.Flags().Set("url", "https://skills.sh/acme/review-helper")
	_ = cmd.Flags().Set("output", "json")

	err := runSkillImport(cmd, nil)
	if err == nil {
		t.Fatal("expected duplicate import to return an error")
	}
	var got map[string]any
	if jsonErr := json.Unmarshal(out.Bytes(), &got); jsonErr != nil {
		t.Fatalf("decode stdout JSON %q: %v", out.String(), jsonErr)
	}
	if got["status"] != "conflict" {
		t.Fatalf("status = %v", got["status"])
	}
	if !strings.Contains(strVal(got, "reason"), "--on-conflict overwrite") {
		t.Fatalf("reason = %v", got["reason"])
	}
}
