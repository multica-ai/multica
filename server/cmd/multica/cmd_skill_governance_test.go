// CEREBRO-PATCH(skill-cli-governance): FIR-2628 — tests for the skill
// ownership / change-request / version / fork CLI surface added in
// cmd_skill_governance.go.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --- Cobra test-command factories. These mirror the real subcommand flag
// registration in cmd_skill_governance.go without going through cobra's
// root command, so unit tests can invoke runSkillXxx directly. ---

func newSkillProposeTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "propose"}
	c.Flags().String("title", "", "")
	c.Flags().String("description", "", "")
	c.Flags().String("version", "", "")
	c.Flags().String("content", "", "")
	c.Flags().String("content-file", "", "")
	c.Flags().StringArray("file", nil, "")
	c.Flags().String("output", "json", "")
	return c
}

func newSkillChangeRequestsListTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "change-requests"}
	c.Flags().String("skill", "", "")
	c.Flags().Bool("pending", false, "")
	c.Flags().String("output", "json", "")
	return c
}

func newSkillReviewTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "review"}
	c.Flags().Bool("approve", false, "")
	c.Flags().Bool("reject", false, "")
	c.Flags().String("comment", "", "")
	c.Flags().String("output", "json", "")
	return c
}

func newSkillVersionsTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "versions"}
	c.Flags().String("output", "json", "")
	return c
}

func newSkillDiffTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "diff"}
	c.Flags().String("from", "", "")
	c.Flags().String("to", "", "")
	c.Flags().String("output", "json", "")
	return c
}

func newSkillOwnershipTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "ownership"}
	c.Flags().String("owner", "", "")
	c.Flags().String("owner-id", "", "")
	c.Flags().StringArray("approver", nil, "")
	c.Flags().StringArray("approver-id", nil, "")
	c.Flags().Bool("clear-approvers", false, "")
	c.Flags().String("output", "json", "")
	return c
}

func newSkillForkTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "fork"}
	c.Flags().String("name", "", "")
	c.Flags().String("description", "", "")
	c.Flags().String("owner", "", "")
	c.Flags().String("owner-id", "", "")
	c.Flags().String("output", "json", "")
	return c
}

// captureStdout swaps os.Stdout with a pipe for the duration of fn so tests
// can assert on the table / json output emitted by cli.PrintJSON / PrintTable.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	_ = w.Close()
	return <-done
}

// ---------------------------------------------------------------------------
// propose
// ---------------------------------------------------------------------------

func TestRunSkillProposeSendsSessionRefAndPayload(t *testing.T) {
	const skillID = "11111111-1111-1111-1111-111111111111"

	tmp := t.TempDir()
	helperPath := filepath.Join(tmp, "helper.sh")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/"+skillID+"/change-requests" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               "cr-1",
			"skill_id":         skillID,
			"title":            got["title"],
			"base_version":     "1.0.0",
			"proposed_version": got["proposed_version"],
			"status":           "pending",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")
	t.Setenv("MULTICA_TASK_ID", "task-abc-123")

	cmd := newSkillProposeTestCmd()
	_ = cmd.Flags().Set("title", "Better wording")
	_ = cmd.Flags().Set("description", "Cleaner phrasing for first paragraph")
	_ = cmd.Flags().Set("version", "1.1.0")
	_ = cmd.Flags().Set("content", "# New body")
	_ = cmd.Flags().Set("file", "scripts/helper.sh="+helperPath)
	_ = cmd.Flags().Set("file", helperPath) // basename fallback

	output := captureStdout(t, func() {
		if err := runSkillPropose(cmd, []string{skillID}); err != nil {
			t.Fatalf("runSkillPropose: %v", err)
		}
	})

	if got["title"] != "Better wording" {
		t.Errorf("title = %v, want %q", got["title"], "Better wording")
	}
	if got["description"] != "Cleaner phrasing for first paragraph" {
		t.Errorf("description = %v", got["description"])
	}
	if got["proposed_version"] != "1.1.0" {
		t.Errorf("proposed_version = %v", got["proposed_version"])
	}
	if got["proposed_content"] != "# New body" {
		t.Errorf("proposed_content = %v", got["proposed_content"])
	}
	if got["work_session_id"] != "task-abc-123" {
		t.Errorf("work_session_id = %v, want %q", got["work_session_id"], "task-abc-123")
	}
	files, _ := got["proposed_files"].([]any)
	if len(files) != 2 {
		t.Fatalf("proposed_files len = %d, want 2", len(files))
	}
	first := files[0].(map[string]any)
	if first["path"] != "scripts/helper.sh" {
		t.Errorf("file[0].path = %v", first["path"])
	}
	second := files[1].(map[string]any)
	if second["path"] != "helper.sh" {
		t.Errorf("file[1].path = %v, want basename fallback", second["path"])
	}
	if !strings.Contains(output, "cr-1") {
		t.Errorf("output missing CR id: %q", output)
	}
}

func TestRunSkillProposeRequiresTitleAndVersion(t *testing.T) {
	cmd := newSkillProposeTestCmd()
	if err := runSkillPropose(cmd, []string{"any"}); err == nil || !strings.Contains(err.Error(), "--title") {
		t.Fatalf("expected --title error, got %v", err)
	}
	cmd = newSkillProposeTestCmd()
	_ = cmd.Flags().Set("title", "x")
	if err := runSkillPropose(cmd, []string{"any"}); err == nil || !strings.Contains(err.Error(), "--version") {
		t.Fatalf("expected --version error, got %v", err)
	}
}

func TestRunSkillProposeContentAndContentFileMutuallyExclusive(t *testing.T) {
	cmd := newSkillProposeTestCmd()
	_ = cmd.Flags().Set("title", "x")
	_ = cmd.Flags().Set("version", "1.1.0")
	_ = cmd.Flags().Set("content", "inline")
	_ = cmd.Flags().Set("content-file", "/tmp/whatever")
	if err := runSkillPropose(cmd, []string{"any"}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually-exclusive error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// change-requests list
// ---------------------------------------------------------------------------

func TestRunSkillChangeRequestsList(t *testing.T) {
	const skillID = "22222222-2222-2222-2222-222222222222"

	t.Run("per-skill", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/skills/"+skillID+"/change-requests" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "cr-1", "skill_id": skillID, "status": "pending", "base_version": "1.0.0", "proposed_version": "1.1.0", "title": "Tweak"},
			})
		}))
		defer srv.Close()

		t.Setenv("MULTICA_SERVER_URL", srv.URL)
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "tok")

		cmd := newSkillChangeRequestsListTestCmd()
		_ = cmd.Flags().Set("skill", skillID)
		out := captureStdout(t, func() {
			if err := runSkillChangeRequestsList(cmd, nil); err != nil {
				t.Fatalf("list: %v", err)
			}
		})
		if !strings.Contains(out, "cr-1") {
			t.Errorf("missing cr-1 in output: %q", out)
		}
	})

	t.Run("workspace pending", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/skills/change-requests" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "cr-9", "status": "pending", "base_version": "1.0.0", "proposed_version": "2.0.0", "title": "Big change"},
			})
		}))
		defer srv.Close()

		t.Setenv("MULTICA_SERVER_URL", srv.URL)
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "tok")

		cmd := newSkillChangeRequestsListTestCmd()
		out := captureStdout(t, func() {
			if err := runSkillChangeRequestsList(cmd, nil); err != nil {
				t.Fatalf("list: %v", err)
			}
		})
		if !strings.Contains(out, "cr-9") {
			t.Errorf("missing cr-9 in output: %q", out)
		}
	})

	t.Run("skill and pending are mutually exclusive", func(t *testing.T) {
		t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
		t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
		t.Setenv("MULTICA_TOKEN", "tok")

		cmd := newSkillChangeRequestsListTestCmd()
		_ = cmd.Flags().Set("skill", skillID)
		_ = cmd.Flags().Set("pending", "true")
		if err := runSkillChangeRequestsList(cmd, nil); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
			t.Fatalf("expected mutually-exclusive error, got %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// review
// ---------------------------------------------------------------------------

func TestRunSkillReview(t *testing.T) {
	const crID = "33333333-3333-3333-3333-333333333333"

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/change-requests/"+crID+"/review" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               crID,
			"status":           "merged",
			"base_version":     "1.0.0",
			"proposed_version": "1.1.0",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	t.Run("approve with comment", func(t *testing.T) {
		body = nil
		cmd := newSkillReviewTestCmd()
		_ = cmd.Flags().Set("approve", "true")
		_ = cmd.Flags().Set("comment", "LGTM")
		if err := runSkillReview(cmd, []string{crID}); err != nil {
			t.Fatalf("review: %v", err)
		}
		if body["action"] != "approve" {
			t.Errorf("action = %v", body["action"])
		}
		if body["comment"] != "LGTM" {
			t.Errorf("comment = %v", body["comment"])
		}
	})

	t.Run("approve and reject both fails", func(t *testing.T) {
		cmd := newSkillReviewTestCmd()
		_ = cmd.Flags().Set("approve", "true")
		_ = cmd.Flags().Set("reject", "true")
		if err := runSkillReview(cmd, []string{crID}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("neither fails", func(t *testing.T) {
		cmd := newSkillReviewTestCmd()
		if err := runSkillReview(cmd, []string{crID}); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// versions + diff
// ---------------------------------------------------------------------------

func TestRunSkillVersions(t *testing.T) {
	const skillID = "44444444-4444-4444-4444-444444444444"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/"+skillID+"/versions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"version": "1.0.0", "description": "init", "created_at": "2026-05-31T20:00:00Z"},
			{"version": "1.1.0", "description": "tweak", "created_at": "2026-05-31T21:00:00Z"},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	cmd := newSkillVersionsTestCmd()
	out := captureStdout(t, func() {
		if err := runSkillVersions(cmd, []string{skillID}); err != nil {
			t.Fatalf("versions: %v", err)
		}
	})
	if !strings.Contains(out, "1.0.0") || !strings.Contains(out, "1.1.0") {
		t.Errorf("missing versions in output: %q", out)
	}
}

func TestDiffSkillVersions(t *testing.T) {
	from := map[string]any{
		"version": "1.0.0",
		"content": "line a\nline b\nline c\n",
		"files":   []any{map[string]any{"path": "a.md", "content": "alpha\nbeta\n"}},
	}
	to := map[string]any{
		"version": "1.1.0",
		"content": "line a\nline B\nline c\n",
		"files":   []any{map[string]any{"path": "a.md", "content": "alpha\nGAMMA\nbeta\n"}, map[string]any{"path": "b.md", "content": "new\n"}},
	}
	got := diffSkillVersions(from, to)
	if len(got) != 3 {
		t.Fatalf("expected 3 file diffs (SKILL.md, a.md, b.md), got %d", len(got))
	}
	if got[0].Path != "SKILL.md" {
		t.Errorf("first diff path = %q, want SKILL.md", got[0].Path)
	}
	// SKILL.md should mark "line b" → "line B"
	want := []string{"- line b", "+ line B"}
	if !containsAll(got[0].Lines, want) {
		t.Errorf("SKILL.md diff missing %v: %v", want, got[0].Lines)
	}
	// a.md should add GAMMA
	if !containsAny(got[1].Lines, "+ GAMMA") {
		t.Errorf("a.md diff missing GAMMA add: %v", got[1].Lines)
	}
	// b.md is a new file — every line is +
	for _, ln := range got[2].Lines {
		if ln != "" && !strings.HasPrefix(ln, "+ ") && !strings.HasPrefix(ln, "- ") {
			t.Errorf("b.md unexpected line: %q", ln)
		}
	}
}

func containsAll(haystack, needles []string) bool {
	for _, n := range needles {
		if !containsAny(haystack, n) {
			return false
		}
	}
	return true
}
func containsAny(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestRunSkillDiffMissingVersion(t *testing.T) {
	const skillID = "55555555-5555-5555-5555-555555555555"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"version": "1.0.0", "content": "x"},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	cmd := newSkillDiffTestCmd()
	_ = cmd.Flags().Set("from", "1.0.0")
	_ = cmd.Flags().Set("to", "9.9.9")
	if err := runSkillDiff(cmd, []string{skillID}); err == nil || !strings.Contains(err.Error(), "9.9.9") {
		t.Fatalf("expected missing-version error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ownership
// ---------------------------------------------------------------------------

func TestRunSkillOwnership(t *testing.T) {
	const (
		skillID  = "66666666-6666-6666-6666-666666666666"
		ownerID  = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		apprvr1  = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
		apprvr2  = "cccccccc-cccc-cccc-cccc-cccccccccccc"
		memberA  = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	)

	var body map[string]any
	members := []map[string]any{
		{"user_id": memberA, "name": "Bob Builder"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/ws-1/members":
			_ = json.NewEncoder(w).Encode(members)
		case "/api/agents":
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		case "/api/skills/" + skillID + "/ownership":
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":           skillID,
				"owner_id":     body["owner_id"],
				"approver_ids": body["approver_ids"],
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	t.Run("ids passthrough", func(t *testing.T) {
		body = nil
		cmd := newSkillOwnershipTestCmd()
		_ = cmd.Flags().Set("owner-id", ownerID)
		_ = cmd.Flags().Set("approver-id", apprvr1)
		_ = cmd.Flags().Set("approver-id", apprvr2)
		if err := runSkillOwnership(cmd, []string{skillID}); err != nil {
			t.Fatalf("ownership: %v", err)
		}
		if body["owner_id"] != ownerID {
			t.Errorf("owner_id = %v", body["owner_id"])
		}
		approvers, _ := body["approver_ids"].([]any)
		if len(approvers) != 2 {
			t.Fatalf("approvers len = %d", len(approvers))
		}
	})

	t.Run("name resolution", func(t *testing.T) {
		body = nil
		cmd := newSkillOwnershipTestCmd()
		_ = cmd.Flags().Set("owner", "Bob Builder")
		if err := runSkillOwnership(cmd, []string{skillID}); err != nil {
			t.Fatalf("ownership: %v", err)
		}
		if body["owner_id"] != memberA {
			t.Errorf("owner_id = %v, want %q", body["owner_id"], memberA)
		}
	})

	t.Run("clear approvers sends empty list", func(t *testing.T) {
		body = nil
		cmd := newSkillOwnershipTestCmd()
		_ = cmd.Flags().Set("clear-approvers", "true")
		if err := runSkillOwnership(cmd, []string{skillID}); err != nil {
			t.Fatalf("ownership: %v", err)
		}
		approvers, ok := body["approver_ids"].([]any)
		if !ok {
			t.Fatalf("approver_ids missing: %#v", body)
		}
		if len(approvers) != 0 {
			t.Errorf("expected empty approvers, got %d", len(approvers))
		}
	})

	t.Run("no flags fails", func(t *testing.T) {
		cmd := newSkillOwnershipTestCmd()
		if err := runSkillOwnership(cmd, []string{skillID}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("owner and owner-id mutually exclusive", func(t *testing.T) {
		cmd := newSkillOwnershipTestCmd()
		_ = cmd.Flags().Set("owner", "Bob")
		_ = cmd.Flags().Set("owner-id", ownerID)
		if err := runSkillOwnership(cmd, []string{skillID}); err == nil {
			t.Fatal("expected error")
		}
	})
}

// ---------------------------------------------------------------------------
// fork
// ---------------------------------------------------------------------------

func TestRunSkillFork(t *testing.T) {
	const (
		parentID = "77777777-7777-7777-7777-777777777777"
		newID    = "88888888-8888-8888-8888-888888888888"
	)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/skills/"+parentID+"/forks" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":              newID,
			"name":            body["name"],
			"owner_id":        body["owner_id"],
			"current_version": "1.0.0",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	cmd := newSkillForkTestCmd()
	_ = cmd.Flags().Set("name", "my-fork")
	_ = cmd.Flags().Set("description", "for me")
	if err := runSkillFork(cmd, []string{parentID}); err != nil {
		t.Fatalf("fork: %v", err)
	}
	if body["name"] != "my-fork" {
		t.Errorf("name = %v", body["name"])
	}
	if body["description"] != "for me" {
		t.Errorf("description = %v", body["description"])
	}
}

func TestRunSkillForkRequiresName(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "tok")

	cmd := newSkillForkTestCmd()
	if err := runSkillFork(cmd, []string{"any"}); err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("expected --name error, got %v", err)
	}
}
