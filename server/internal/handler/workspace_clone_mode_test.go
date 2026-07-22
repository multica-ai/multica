package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateAndNormalizeWorkspaceRepos_CloneMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "omitted clone_mode stays omitted",
			input: []map[string]any{{"url": "https://example.com/org/repo.git"}},
			want:  `[{"url":"https://example.com/org/repo.git"}]`,
		},
		{
			name:  "full is preserved",
			input: []map[string]any{{"url": "https://example.com/org/repo.git", "clone_mode": "full"}},
			want:  `[{"url":"https://example.com/org/repo.git","clone_mode":"full"}]`,
		},
		{
			name:  "on-demand is preserved",
			input: []map[string]any{{"url": "https://example.com/org/repo.git", "clone_mode": "on-demand"}},
			want:  `[{"url":"https://example.com/org/repo.git","clone_mode":"on-demand"}]`,
		},
		{
			name:  "case and surrounding space are normalized",
			input: []map[string]any{{"url": "https://example.com/org/repo.git", "clone_mode": "  On-Demand "}},
			want:  `[{"url":"https://example.com/org/repo.git","clone_mode":"on-demand"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := validateAndNormalizeWorkspaceRepos(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

// `--depth` was considered and rejected in favour of a blobless partial clone,
// so a caller asking for depth-style shallow behaviour must get a clear error
// rather than a silently ignored field.
func TestValidateAndNormalizeWorkspaceRepos_RejectsUnknownCloneMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"shallow", "depth", "blob:none", "1", "partial"} {
		_, err := validateAndNormalizeWorkspaceRepos([]map[string]any{
			{"url": "https://example.com/org/repo.git", "clone_mode": mode},
		})
		if err == nil {
			t.Fatalf("clone_mode %q should have been rejected", mode)
		}
		if !strings.Contains(err.Error(), "clone_mode") {
			t.Fatalf("error for %q should name the field, got %v", mode, err)
		}
	}
}

func TestNormalizeWorkspaceRepos_PreservesCloneMode(t *testing.T) {
	t.Parallel()

	got := normalizeWorkspaceRepos([]RepoData{
		{URL: " https://example.com/a.git ", CloneMode: "on-demand"},
		{URL: "https://example.com/b.git"},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(got))
	}
	if got[0].URL != "https://example.com/a.git" {
		t.Fatalf("URL was not trimmed: %q", got[0].URL)
	}
	if got[0].CloneMode != "on-demand" {
		t.Fatalf("clone mode dropped on the way to the daemon: %q", got[0].CloneMode)
	}
	if got[1].CloneMode != "" {
		t.Fatalf("expected empty clone mode, got %q", got[1].CloneMode)
	}
}

// The daemon uses repos_version to notice that its view of the registry is
// stale. Clone mode changes what a cold clone does, so it has to participate.
func TestWorkspaceReposVersion_TracksCloneMode(t *testing.T) {
	t.Parallel()

	full := workspaceReposVersion([]RepoData{{URL: "https://example.com/a.git"}})
	onDemand := workspaceReposVersion([]RepoData{{URL: "https://example.com/a.git", CloneMode: "on-demand"}})
	if full == onDemand {
		t.Fatal("repos_version must change when a repo switches clone mode")
	}

	// Ordering and description still must not affect the version.
	a := workspaceReposVersion([]RepoData{
		{URL: "https://example.com/a.git", CloneMode: "on-demand"},
		{URL: "https://example.com/b.git", Description: "one"},
	})
	b := workspaceReposVersion([]RepoData{
		{URL: "https://example.com/b.git", Description: "two"},
		{URL: "https://example.com/a.git", CloneMode: "on-demand"},
	})
	if a != b {
		t.Fatalf("repos_version must ignore order and description, got %s vs %s", a, b)
	}
}

// Older daemons parse the claim/repos payload with their own struct. A repo
// left in full mode must serialize exactly as it did before this field
// existed, so upgrading the server cannot perturb them.
func TestRepoDataOmitsEmptyCloneMode(t *testing.T) {
	t.Parallel()

	out, err := json.Marshal(RepoData{URL: "https://example.com/a.git"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := string(out), `{"url":"https://example.com/a.git"}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
