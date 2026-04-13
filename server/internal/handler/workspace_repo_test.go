package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestValidateAndNormalizeRepos_HappyPath covers the common cases: a github
// repo with a URL, a local repo with an absolute path. Both should succeed
// and come back with auto-generated ids + derived names.
func TestValidateAndNormalizeRepos_HappyPath(t *testing.T) {
	t.Parallel()
	input := []map[string]any{
		{"type": "github", "url": "https://github.com/org/foo.git"},
		{"type": "local", "local_path": "/Users/me/bar"},
	}
	got, err := validateAndNormalizeRepos(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(got))
	}
	if got[0].Type != "github" || got[0].URL == "" || got[0].LocalPath != "" {
		t.Errorf("github repo shape wrong: %+v", got[0])
	}
	if got[0].Name != "foo" {
		t.Errorf("expected name derived from URL; got %q", got[0].Name)
	}
	if got[1].Type != "local" || got[1].LocalPath != "/Users/me/bar" || got[1].URL != "" {
		t.Errorf("local repo shape wrong: %+v", got[1])
	}
	if got[1].Name != "bar" {
		t.Errorf("expected name derived from path; got %q", got[1].Name)
	}
	// ids must be populated and unique.
	if got[0].ID == "" || got[1].ID == "" {
		t.Errorf("ids should be auto-generated")
	}
	if got[0].ID == got[1].ID {
		t.Errorf("ids should be unique")
	}
}

// TestValidateAndNormalizeRepos_Rejects catches invalid inputs that the
// server should refuse rather than silently store.
func TestValidateAndNormalizeRepos_Rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  []map[string]any
		errSub string
	}{
		{
			name:   "github without url",
			input:  []map[string]any{{"type": "github"}},
			errSub: "url is required",
		},
		{
			name:   "local without path",
			input:  []map[string]any{{"type": "local"}},
			errSub: "local_path is required",
		},
		{
			name:   "local path must be absolute",
			input:  []map[string]any{{"type": "local", "local_path": "relative/path"}},
			errSub: "absolute path",
		},
		{
			name:   "unknown type",
			input:  []map[string]any{{"type": "gitlab", "url": "x"}},
			errSub: "invalid type",
		},
		{
			name: "duplicate ids",
			input: []map[string]any{
				{"id": "dup", "type": "github", "url": "a", "name": "a"},
				{"id": "dup", "type": "github", "url": "b", "name": "b"},
			},
			errSub: "duplicate id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateAndNormalizeRepos(tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.errSub)
			}
			if !strings.Contains(err.Error(), tt.errSub) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSub)
			}
		})
	}
}

// TestValidateAndNormalizeRepos_PreservesExplicitIDsAndNames ensures the
// server is a good citizen: when a client already has an id and a name
// (from a previous fetch), round-tripping them must not rewrite them.
func TestValidateAndNormalizeRepos_PreservesExplicitIDsAndNames(t *testing.T) {
	t.Parallel()
	input := []map[string]any{{
		"id":          "11111111-2222-3333-4444-555555555555",
		"name":        "my-custom-name",
		"type":        "github",
		"url":         "https://github.com/org/something-else.git",
		"description": "hello",
	}}
	got, err := validateAndNormalizeRepos(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got[0].ID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("id was rewritten: %q", got[0].ID)
	}
	if got[0].Name != "my-custom-name" {
		t.Errorf("name was rewritten: %q", got[0].Name)
	}
	if got[0].Description != "hello" {
		t.Errorf("description lost: %q", got[0].Description)
	}
}

// TestValidateAndNormalizeRepos_ClearsConflictingFields: switching a repo's
// type server-side must blank the now-irrelevant field so the JSONB row is
// never ambiguous (no github entry with a local_path, no local entry with a
// url).
func TestValidateAndNormalizeRepos_ClearsConflictingFields(t *testing.T) {
	t.Parallel()
	input := []map[string]any{{
		"type":       "local",
		"local_path": "/Users/me/foo",
		"url":        "https://github.com/org/foo",
	}}
	got, _ := validateAndNormalizeRepos(input)
	if got[0].URL != "" {
		t.Errorf("local entry should have empty URL, got %q", got[0].URL)
	}
}

// TestParseWorkspaceRepos_V1Compatibility makes sure pre-migration-040 rows
// (just {url, description}) still deserialize as github repos with a
// derived id and name, so read-path callers never see a malformed repo.
func TestParseWorkspaceRepos_V1Compatibility(t *testing.T) {
	t.Parallel()
	raw, _ := json.Marshal([]map[string]any{
		{"url": "https://github.com/org/legacy.git", "description": "old row"},
	})
	got, err := parseWorkspaceRepos(raw)
	if err != nil {
		t.Fatalf("parseWorkspaceRepos: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(got))
	}
	if got[0].Type != "github" {
		t.Errorf("expected type=github, got %q", got[0].Type)
	}
	if got[0].Name != "legacy" {
		t.Errorf("expected derived name, got %q", got[0].Name)
	}
	if got[0].ID == "" {
		t.Errorf("id should be auto-generated for v1 rows")
	}
}
