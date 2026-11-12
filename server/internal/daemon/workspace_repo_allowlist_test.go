package daemon

import (
	"testing"
)

// TestWorkspaceRepoAllowedMatchesAcrossURLForms pins the normalization that
// lets a checkout request in one URL form match a repo registered in another
// (http vs ssh vs scp shorthand). Agents routinely paste the HTTP address
// from the Git host's web UI while the workspace registers the SSH form.
func TestWorkspaceRepoAllowedMatchesAcrossURLForms(t *testing.T) {
	d := newGCTestDaemon(t, nil)

	const wsID = "11111111-1111-1111-1111-111111111111"
	d.mu.Lock()
	d.workspaces[wsID] = &workspaceState{
		workspaceID: wsID,
		allowedRepoURLs: map[string]struct{}{
			"git@gitlab.example.com:group/project.git": {},
		},
		taskRepoURLs: map[string]struct{}{
			"ssh://git@gitlab.example.com:2222/group/task-only.git": {},
		},
	}
	d.mu.Unlock()

	for _, tc := range []struct {
		name string
		url  string
		want bool
	}{
		{"exact registered form", "git@gitlab.example.com:group/project.git", true},
		{"https form of registered ssh repo", "https://gitlab.example.com/group/project.git", true},
		{"http form of registered ssh repo", "http://gitlab.example.com/group/project.git", true},
		{"ssh form of registered scp shorthand", "ssh://git@gitlab.example.com/group/project.git", true},
		{"trailing slash and .git differences", "https://gitlab.example.com/group/project/", true},
		{"exact task repo form", "ssh://git@gitlab.example.com:2222/group/task-only.git", true},
		{"https form of task repo keeps non-default port", "https://gitlab.example.com:2222/group/task-only.git", true},
		{"same host path without the task repo's port", "https://gitlab.example.com/group/task-only.git", false},
		{"different repo on the same host", "https://gitlab.example.com/group/other.git", false},
		{"empty URL", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := d.workspaceRepoAllowed(wsID, tc.url)
			if got != tc.want {
				t.Errorf("workspaceRepoAllowed(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}

	if d.workspaceRepoAllowed("22222222-2222-2222-2222-222222222222", "git@gitlab.example.com:group/project.git") {
		t.Error("an unwatched workspace must not allow the repo")
	}
}
