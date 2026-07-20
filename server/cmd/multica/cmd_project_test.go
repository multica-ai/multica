package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// validateProjectStatus must accept the five DB-backed statuses and reject
// anything else with a message that lists the valid values. `project create`,
// `project update`, and `project status` all share it (#3925: `--status active`
// used to reach the server and 500 on the CHECK constraint).
func TestValidateProjectStatus(t *testing.T) {
	for _, s := range validProjectStatuses {
		if err := validateProjectStatus(s); err != nil {
			t.Errorf("status %q should be valid, got: %v", s, err)
		}
	}
	err := validateProjectStatus("active")
	if err == nil {
		t.Fatal("status \"active\" should be rejected")
	}
	if !strings.Contains(err.Error(), "planned") {
		t.Errorf("error should list valid statuses, got: %v", err)
	}
}

// newProjectResourceUpdateTestCmd mirrors the flag surface of
// projectResourceUpdateCmd so unit tests can exercise the shortcut-flag plumbing
// without spinning up a server.
func newProjectResourceUpdateTestCmd() *cobra.Command {
	c := &cobra.Command{Use: "update"}
	c.Flags().String("url", "", "")
	c.Flags().String("default-branch-hint", "", "")
	c.Flags().String("local-path", "", "")
	c.Flags().String("daemon-id", "", "")
	c.Flags().String("ref-label", "", "")
	c.Flags().String("ref", "", "")
	c.Flags().String("label", "", "")
	c.Flags().Bool("clear-label", false, "")
	c.Flags().Int32("position", 0, "")
	c.Flags().String("output", "json", "")
	return c
}

// TestBuildResourceRefFromFlagsGithubMergesHint pins the nit fix from MUL-2662
// review round 2: `multica project resource update <p> <r> --default-branch-hint x`
// must rebuild the full github_repo payload by merging the existing `url` —
// otherwise the server sees `{default_branch_hint: "x"}` and 400s.
func TestBuildResourceRefFromFlagsGithubMergesHint(t *testing.T) {
	t.Run("hint-only edit preserves existing url", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("default-branch-hint", "main")
		existing := map[string]any{"url": "https://github.com/multica-ai/multica"}

		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true when default-branch-hint is set")
		}
		if ref["url"] != "https://github.com/multica-ai/multica" {
			t.Errorf("expected merged url, got %v", ref["url"])
		}
		if ref["default_branch_hint"] != "main" {
			t.Errorf("expected merged hint=main, got %v", ref["default_branch_hint"])
		}
	})

	t.Run("hint=empty clears the hint but keeps url", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("default-branch-hint", "")
		existing := map[string]any{
			"url":                 "https://github.com/multica-ai/multica",
			"default_branch_hint": "stale",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if ref["url"] != "https://github.com/multica-ai/multica" {
			t.Errorf("expected url to survive empty-hint clear, got %v", ref["url"])
		}
		if _, ok := ref["default_branch_hint"]; ok {
			t.Errorf("expected default_branch_hint to be cleared, got %v", ref["default_branch_hint"])
		}
	})

	t.Run("url override survives merge", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("url", "https://github.com/multica-ai/new-repo")
		existing := map[string]any{
			"url":                 "https://github.com/multica-ai/multica",
			"default_branch_hint": "main",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if ref["url"] != "https://github.com/multica-ai/new-repo" {
			t.Errorf("expected overridden url, got %v", ref["url"])
		}
		if ref["default_branch_hint"] != "main" {
			t.Errorf("expected merged hint to persist, got %v", ref["default_branch_hint"])
		}
	})

	t.Run("checkout ref edit preserves existing url and hint", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("ref", "release/v2")
		existing := map[string]any{
			"url":                 "https://github.com/multica-ai/multica",
			"default_branch_hint": "main",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if ref["url"] != "https://github.com/multica-ai/multica" {
			t.Errorf("expected merged url, got %v", ref["url"])
		}
		if ref["default_branch_hint"] != "main" {
			t.Errorf("expected merged hint to persist, got %v", ref["default_branch_hint"])
		}
		if ref["ref"] != "release/v2" {
			t.Errorf("expected checkout ref release/v2, got %v", ref["ref"])
		}
	})

	t.Run("empty checkout ref clears existing ref", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("ref", "")
		existing := map[string]any{
			"url": "https://github.com/multica-ai/multica",
			"ref": "stale",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if _, ok := ref["ref"]; ok {
			t.Errorf("expected checkout ref to be cleared, got %v", ref["ref"])
		}
		if ref["url"] != "https://github.com/multica-ai/multica" {
			t.Errorf("expected merged url, got %v", ref["url"])
		}
	})

	t.Run("hint-only with no existing url fails fast", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("default-branch-hint", "main")
		_, _, err := buildResourceRefFromFlags(cmd, "github_repo", nil)
		if err == nil {
			t.Fatalf("expected error when no existing url is available to merge")
		}
	})

	t.Run("no flags set returns has=false", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		ref, has, err := buildResourceRefFromFlags(cmd, "github_repo", map[string]any{"url": "https://x"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if has {
			t.Errorf("expected has=false when no shortcut flag is set, got ref=%v", ref)
		}
	})
}

func TestBuildResourceRefFromRefFlagKeepsJSONEscapeHatch(t *testing.T) {
	t.Run("json payload wins over github shortcuts", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("url", "https://github.com/multica-ai/ignored")
		_ = cmd.Flags().Set("ref", `{"url":"https://github.com/multica-ai/multica","ref":"release/v2"}`)

		ref, has, err := buildResourceRefFromRefFlag(cmd, "github_repo", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		payload, ok := ref.(map[string]any)
		if !ok {
			t.Fatalf("expected JSON object payload, got %T", ref)
		}
		if payload["url"] != "https://github.com/multica-ai/multica" {
			t.Errorf("json payload url = %v", payload["url"])
		}
		if payload["ref"] != "release/v2" {
			t.Errorf("json payload ref = %v", payload["ref"])
		}
	})

	t.Run("broken json is still rejected", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("url", "https://github.com/multica-ai/multica")
		_ = cmd.Flags().Set("ref", `{"url":`)

		_, _, err := buildResourceRefFromRefFlag(cmd, "github_repo", nil)
		if err == nil {
			t.Fatalf("expected invalid JSON error")
		}
		if !strings.Contains(err.Error(), "not valid JSON") {
			t.Fatalf("error = %q, want JSON guidance", err)
		}
	})

	// A github_repo checkout ref that happens to be valid JSON as a bare scalar
	// (a numeric tag, an all-digit short SHA, true/false/null) must be treated
	// as a checkout ref, not silently parsed into the JSON escape hatch. Only
	// "{...}" / "[...]" shaped values are the escape hatch.
	t.Run("bare scalar github ref is a checkout ref, not JSON", func(t *testing.T) {
		for _, gitRef := range []string{"2024", "1234567", "true", "null"} {
			cmd := newProjectResourceUpdateTestCmd()
			_ = cmd.Flags().Set("url", "https://github.com/multica-ai/multica")
			_ = cmd.Flags().Set("ref", gitRef)

			ref, has, err := buildResourceRefFromRefFlag(cmd, "github_repo", nil)
			if err != nil {
				t.Fatalf("ref %q: unexpected error: %v", gitRef, err)
			}
			if !has {
				t.Fatalf("ref %q: expected has=true", gitRef)
			}
			payload, ok := ref.(map[string]any)
			if !ok {
				t.Fatalf("ref %q: expected github_repo shortcut map, got %T", gitRef, ref)
			}
			if payload["url"] != "https://github.com/multica-ai/multica" {
				t.Errorf("ref %q: url = %v", gitRef, payload["url"])
			}
			if payload["ref"] != gitRef {
				t.Errorf("ref %q: checkout ref = %v, want %q", gitRef, payload["ref"], gitRef)
			}
		}
	})

	// The checkout-ref shortcut only applies to github_repo: every other
	// resource type must still reject a non-JSON --ref so the generic escape
	// hatch stays typed.
	t.Run("non-json ref rejected for non-github type", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("ref", "develop")

		_, _, err := buildResourceRefFromRefFlag(cmd, "local_directory", nil)
		if err == nil {
			t.Fatalf("expected error for non-JSON --ref on local_directory")
		}
		if !strings.Contains(err.Error(), "not valid JSON") {
			t.Fatalf("error = %q, want JSON guidance", err)
		}
	})
}

// TestBuildResourceRefFromFlagsLocalDirectoryMerges covers the same merge
// behavior for local_directory: partial edits keep unmentioned fields from the
// existing ref.
func TestBuildResourceRefFromFlagsLocalDirectoryMerges(t *testing.T) {
	t.Run("ref-label only edit preserves existing path + daemon", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("ref-label", "renamed")
		existing := map[string]any{
			"local_path": "/Users/foo/work/a",
			"daemon_id":  "d1",
			"label":      "old",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "local_directory", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if ref["local_path"] != "/Users/foo/work/a" {
			t.Errorf("local_path missing after merge: %v", ref["local_path"])
		}
		if ref["daemon_id"] != "d1" {
			t.Errorf("daemon_id missing after merge: %v", ref["daemon_id"])
		}
		if ref["label"] != "renamed" {
			t.Errorf("label not overridden: %v", ref["label"])
		}
	})

	t.Run("local-path only without existing daemon fails", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("local-path", "/Users/foo/work/b")
		_, _, err := buildResourceRefFromFlags(cmd, "local_directory", nil)
		if err == nil {
			t.Fatalf("expected error when daemon_id is missing from both flags and existing ref")
		}
	})

	t.Run("ref-label cleared on empty input", func(t *testing.T) {
		cmd := newProjectResourceUpdateTestCmd()
		_ = cmd.Flags().Set("ref-label", "")
		existing := map[string]any{
			"local_path": "/Users/foo/work/a",
			"daemon_id":  "d1",
			"label":      "to-clear",
		}
		ref, has, err := buildResourceRefFromFlags(cmd, "local_directory", existing)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !has {
			t.Fatalf("expected has=true")
		}
		if _, ok := ref["label"]; ok {
			t.Errorf("expected embedded label to be cleared, got %v", ref["label"])
		}
	})
}
